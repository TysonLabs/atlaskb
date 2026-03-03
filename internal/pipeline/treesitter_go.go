package pipeline

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// ExtractedCall represents a resolved function call extracted from source code.
type ExtractedCall struct {
	CallerQN   string // Qualified name of the calling function
	CalleeQN   string // Qualified name of the called function
	Line       int    // Source line of the call
	Confidence string // "high", "moderate", "low"
}

// ExtractedInheritance represents a struct embedding (Go's composition mechanism).
type ExtractedInheritance struct {
	ChildQN  string // Qualified name of the embedding struct
	ParentQN string // Qualified name of the embedded type
	Kind     string // "embeds"
	Line     int    // Source line of the embedding
}

// goImportMap maps import alias/package name to the full import path.
// e.g., "fmt" -> "fmt", "models" -> "github.com/foo/bar/internal/models"
type goImportMap map[string]string

// buildGoImportMap parses Go imports from a file and builds an alias -> path map.
// Uses go/parser in ImportsOnly mode (very fast, no full parse).
func buildGoImportMap(repoPath, relPath string) goImportMap {
	fset := token.NewFileSet()
	absPath := filepath.Join(repoPath, relPath)
	f, err := parser.ParseFile(fset, absPath, nil, parser.ImportsOnly)
	if err != nil {
		return nil
	}

	m := make(goImportMap)
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		if imp.Name != nil {
			// Explicit alias
			m[imp.Name.Name] = importPath
		} else {
			// Default: last segment of import path
			parts := strings.Split(importPath, "/")
			m[parts[len(parts)-1]] = importPath
		}
	}
	return m
}

// ExtractGoCalls walks the Tree-sitter AST to find all function call expressions,
// resolves caller and callee using the SuffixIndex, and returns extracted calls.
func ExtractGoCalls(root *sitter.Node, source []byte, relPath, repoPath string, idx *SuffixIndex) []ExtractedCall {
	if root == nil {
		return nil
	}

	importMap := buildGoImportMap(repoPath, relPath)
	callerPkg := deriveModuleName(relPath)

	var calls []ExtractedCall
	walkTree(root, func(node *sitter.Node) {
		if node.Type() != "call_expression" {
			return
		}

		// Extract the callee from the call expression
		calleeName, calleeConfidence := extractCallee(node, source, importMap, idx, callerPkg)
		if calleeName == "" {
			return
		}

		// Find the enclosing function
		callerName := findEnclosingFunction(node, source, callerPkg, idx)
		if callerName == "" {
			return
		}

		// Don't create self-calls
		if callerName == calleeName {
			return
		}

		calls = append(calls, ExtractedCall{
			CallerQN:   callerName,
			CalleeQN:   calleeName,
			Line:       int(node.StartPoint().Row) + 1, // Tree-sitter uses 0-based lines
			Confidence: calleeConfidence,
		})
	})

	return calls
}

// ExtractGoEmbeddings walks the Tree-sitter AST to find struct embeddings
// (anonymous/embedded fields in struct declarations).
func ExtractGoEmbeddings(root *sitter.Node, source []byte, relPath, repoPath string, idx *SuffixIndex) []ExtractedInheritance {
	if root == nil {
		return nil
	}

	importMap := buildGoImportMap(repoPath, relPath)
	callerPkg := deriveModuleName(relPath)

	var embeddings []ExtractedInheritance
	walkTree(root, func(node *sitter.Node) {
		if node.Type() != "type_spec" {
			return
		}

		// Get the type name
		nameNode := nodeChildByFieldName(node, "name")
		if nameNode == nil {
			return
		}
		typeName := nameNode.Content(source)

		// Resolve the struct's qualified name
		structQN, _, structFound := idx.Resolve(typeName, callerPkg)
		if !structFound {
			return
		}

		// Find the struct_type child
		typeNode := nodeChildByFieldName(node, "type")
		if typeNode == nil || typeNode.Type() != "struct_type" {
			return
		}

		// Find field_declaration_list
		fieldList := findChildByType(typeNode, "field_declaration_list")
		if fieldList == nil {
			return
		}

		// Walk field declarations looking for embedded types (no name, just type)
		for i := 0; i < int(fieldList.NamedChildCount()); i++ {
			field := fieldList.NamedChild(i)
			if field.Type() != "field_declaration" {
				continue
			}

			// An embedded field has no explicit name — it only has a type.
			// In Tree-sitter Go grammar, embedded fields have a single "type" child
			// and no "name" children.
			if isEmbeddedField(field, source) {
				embeddedTypeName := extractEmbeddedTypeName(field, source)
				if embeddedTypeName == "" {
					continue
				}

				// Resolve the embedded type
				parentQN := resolveEmbeddedType(embeddedTypeName, importMap, idx, callerPkg)
				if parentQN == "" {
					continue
				}

				embeddings = append(embeddings, ExtractedInheritance{
					ChildQN:  structQN,
					ParentQN: parentQN,
					Kind:     "embeds",
					Line:     int(field.StartPoint().Row) + 1,
				})
			}
		}
	})

	return embeddings
}

// extractCallee extracts the callee's qualified name from a call_expression node.
func extractCallee(callNode *sitter.Node, source []byte, importMap goImportMap, idx *SuffixIndex, callerPkg string) (string, string) {
	// The function being called is the first child of call_expression
	fnNode := callNode.NamedChild(0)
	if fnNode == nil {
		return "", ""
	}

	switch fnNode.Type() {
	case "identifier":
		// Direct call: e.g., Foo()
		name := fnNode.Content(source)
		qn, conf, found := idx.Resolve(name, callerPkg)
		if found {
			return qn, conf
		}

	case "selector_expression":
		// Qualified call: e.g., pkg.Func() or obj.Method()
		operand := nodeChildByFieldName(fnNode, "operand")
		field := nodeChildByFieldName(fnNode, "field")
		if operand == nil || field == nil {
			return "", ""
		}

		operandStr := operand.Content(source)
		fieldStr := field.Content(source)

		// Check if operand is an imported package
		if _, isImport := importMap[operandStr]; isImport {
			// This is a package.Function call — try to resolve via index
			// The target might be in our roster if it's an internal package
			qn, conf, found := idx.Resolve(fieldStr, operandStr)
			if found {
				return qn, conf
			}
			// Try with the import path's last segment as the package
			importPath := importMap[operandStr]
			parts := strings.Split(importPath, "/")
			pkgName := parts[len(parts)-1]
			qn, conf, found = idx.Resolve(fieldStr, pkgName)
			if found {
				return qn, conf
			}
			return "", ""
		}

		// Otherwise try as receiver.method
		combined := operandStr + "." + fieldStr
		qn, conf, found := idx.Resolve(combined, callerPkg)
		if found {
			return qn, conf
		}
	}

	return "", ""
}

// findEnclosingFunction walks up the AST to find the enclosing function/method declaration.
func findEnclosingFunction(node *sitter.Node, source []byte, callerPkg string, idx *SuffixIndex) string {
	current := node.Parent()
	for current != nil {
		switch current.Type() {
		case "function_declaration":
			nameNode := nodeChildByFieldName(current, "name")
			if nameNode != nil {
				name := nameNode.Content(source)
				qn, _, found := idx.Resolve(name, callerPkg)
				if found {
					return qn
				}
			}
			return ""

		case "method_declaration":
			// Method: get receiver type and method name
			nameNode := nodeChildByFieldName(current, "name")
			receiver := nodeChildByFieldName(current, "receiver")
			if nameNode != nil && receiver != nil {
				methodName := nameNode.Content(source)
				receiverType := extractReceiverType(receiver, source)
				if receiverType != "" {
					combined := receiverType + "." + methodName
					qn, _, found := idx.Resolve(combined, callerPkg)
					if found {
						return qn
					}
				}
			}
			return ""
		}
		current = current.Parent()
	}
	return ""
}

// extractReceiverType extracts the type name from a method receiver parameter list.
// Handles both value receivers (t Type) and pointer receivers (t *Type).
func extractReceiverType(receiver *sitter.Node, source []byte) string {
	// receiver is a parameter_list node containing parameter_declaration(s)
	for i := 0; i < int(receiver.NamedChildCount()); i++ {
		param := receiver.NamedChild(i)
		if param.Type() != "parameter_declaration" {
			continue
		}
		// Find the type child
		typeNode := nodeChildByFieldName(param, "type")
		if typeNode == nil {
			continue
		}
		// Handle pointer receiver: *Type
		if typeNode.Type() == "pointer_type" {
			inner := typeNode.NamedChild(0)
			if inner != nil {
				return inner.Content(source)
			}
		}
		// Value receiver or type_identifier
		return typeNode.Content(source)
	}
	return ""
}

// isEmbeddedField checks if a field_declaration represents an embedded type
// (no explicit field name, just a type).
func isEmbeddedField(field *sitter.Node, source []byte) bool {
	// In Tree-sitter Go grammar, an embedded field typically looks like:
	// (field_declaration type: <type_node>)
	// A regular field has: (field_declaration name: <identifier> type: <type_node>)
	//
	// We check by looking at child structure: embedded fields have no "name" field.
	nameNode := nodeChildByFieldName(field, "name")
	return nameNode == nil
}

// extractEmbeddedTypeName extracts the type name from an embedded field.
func extractEmbeddedTypeName(field *sitter.Node, source []byte) string {
	typeNode := nodeChildByFieldName(field, "type")
	if typeNode == nil {
		// For embedded fields, the type might be the first named child
		if field.NamedChildCount() > 0 {
			child := field.NamedChild(0)
			if child.Type() == "type_identifier" || child.Type() == "qualified_type" {
				return child.Content(source)
			}
			// Pointer embedding: *Type
			if child.Type() == "pointer_type" && child.NamedChildCount() > 0 {
				return child.NamedChild(0).Content(source)
			}
		}
		return ""
	}

	switch typeNode.Type() {
	case "type_identifier":
		return typeNode.Content(source)
	case "pointer_type":
		if typeNode.NamedChildCount() > 0 {
			return typeNode.NamedChild(0).Content(source)
		}
	case "qualified_type":
		// e.g., pkg.Type — extract just the type name
		return typeNode.Content(source)
	}
	return ""
}

// resolveEmbeddedType resolves an embedded type name to a qualified name.
func resolveEmbeddedType(typeName string, importMap goImportMap, idx *SuffixIndex, callerPkg string) string {
	// Check for qualified type (pkg.Type)
	if parts := strings.SplitN(typeName, ".", 2); len(parts) == 2 {
		pkgAlias := parts[0]
		name := parts[1]
		if _, ok := importMap[pkgAlias]; ok {
			importPath := importMap[pkgAlias]
			pathParts := strings.Split(importPath, "/")
			pkgName := pathParts[len(pathParts)-1]
			qn, _, found := idx.Resolve(name, pkgName)
			if found {
				return qn
			}
		}
		return ""
	}

	// Simple type name — resolve via index
	qn, _, found := idx.Resolve(typeName, callerPkg)
	if found {
		return qn
	}
	return ""
}

// walkTree performs a depth-first traversal of the AST, calling fn for each node.
func walkTree(node *sitter.Node, fn func(*sitter.Node)) {
	fn(node)
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil {
			walkTree(child, fn)
		}
	}
}

// nodeChildByFieldName returns the child node for a given field name, or nil.
func nodeChildByFieldName(node *sitter.Node, name string) *sitter.Node {
	return node.ChildByFieldName(name)
}

// findChildByType returns the first child node of the given type.
func findChildByType(node *sitter.Node, typeName string) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && child.Type() == typeName {
			return child
		}
	}
	return nil
}
