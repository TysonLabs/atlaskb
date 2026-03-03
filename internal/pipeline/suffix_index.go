package pipeline

import "strings"

// SuffixIndex provides fast O(1) lookups from raw call expressions to
// canonical EntityEntry qualified names. Built from the ctags EntityRoster.
type SuffixIndex struct {
	byShortName map[string][]EntityEntry // "Bar" -> multiple matches
	byQualName  map[string]EntityEntry   // "pipeline::Bar" -> single match
	byPkgName   map[string][]EntityEntry // "pipeline" -> all entities in pkg
}

// BuildSuffixIndex constructs a SuffixIndex from the entity roster produced
// by ctags (Phase 1.5). The index enables three resolution strategies:
// exact qualified name, package+name, and short name.
func BuildSuffixIndex(roster []EntityEntry) *SuffixIndex {
	idx := &SuffixIndex{
		byShortName: make(map[string][]EntityEntry),
		byQualName:  make(map[string]EntityEntry),
		byPkgName:   make(map[string][]EntityEntry),
	}

	for _, e := range roster {
		// Index by qualified name (unique)
		idx.byQualName[e.QualifiedName] = e

		// Index by short name (may have duplicates across packages)
		idx.byShortName[e.Name] = append(idx.byShortName[e.Name], e)

		// Index by package/module name
		pkg := extractPackage(e.QualifiedName)
		if pkg != "" {
			idx.byPkgName[pkg] = append(idx.byPkgName[pkg], e)
		}
	}

	return idx
}

// Resolve attempts to find the qualified name for a raw call expression.
// Priority: exact qualified name > package+name > short name (with ambiguity detection).
// callerPkg is the module/package of the caller, used to prefer same-package matches.
// Returns the resolved qualified name, a confidence level, and whether a match was found.
func (idx *SuffixIndex) Resolve(raw, callerPkg string) (qualName, confidence string, found bool) {
	// 1. Exact qualified name match
	if e, ok := idx.byQualName[raw]; ok {
		return e.QualifiedName, "high", true
	}

	// 2. Package.Name match (e.g., "pipeline::Foo" from raw "Foo" with callerPkg "pipeline")
	// Also handle "Receiver.Method" patterns like "Store.Create"
	if parts := strings.SplitN(raw, ".", 2); len(parts) == 2 {
		// Try as receiver.method: look up "pkg::Receiver.Method"
		receiverName := parts[0]
		methodName := parts[1]

		// Find receiver entities, prefer same package
		if receivers, ok := idx.byShortName[receiverName]; ok {
			for _, r := range receivers {
				pkg := extractPackage(r.QualifiedName)
				methodQN := pkg + "::" + receiverName + "." + methodName
				if _, ok := idx.byQualName[methodQN]; ok {
					conf := "moderate"
					if pkg == callerPkg {
						conf = "high"
					}
					return methodQN, conf, true
				}
			}
		}
	}

	// 3. Same-package short name match
	if callerPkg != "" {
		qn := callerPkg + "::" + raw
		if e, ok := idx.byQualName[qn]; ok {
			return e.QualifiedName, "high", true
		}
	}

	// 4. Short name match (may be ambiguous)
	if matches, ok := idx.byShortName[raw]; ok {
		if len(matches) == 1 {
			return matches[0].QualifiedName, "moderate", true
		}
		// Ambiguous: prefer same package
		if callerPkg != "" {
			for _, m := range matches {
				if extractPackage(m.QualifiedName) == callerPkg {
					return m.QualifiedName, "moderate", true
				}
			}
		}
		// Still ambiguous, return first match with low confidence
		return matches[0].QualifiedName, "low", true
	}

	return "", "", false
}

// extractPackage extracts the package/module prefix from a qualified name.
// e.g., "pipeline::Foo" -> "pipeline", "pipeline::Foo.Bar" -> "pipeline"
func extractPackage(qualifiedName string) string {
	idx := strings.Index(qualifiedName, "::")
	if idx < 0 {
		return ""
	}
	return qualifiedName[:idx]
}
