package pipeline

import (
	"context"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// TreeSitterEngine wraps Tree-sitter parsers for supported languages.
// Currently supports Go only; additional languages can be added by
// registering new parsers.
type TreeSitterEngine struct {
	goParser *sitter.Parser
}

// NewTreeSitterEngine creates a TreeSitterEngine with a Go parser.
func NewTreeSitterEngine() (*TreeSitterEngine, error) {
	e := &TreeSitterEngine{}
	e.goParser = sitter.NewParser()
	e.goParser.SetLanguage(golang.GetLanguage())
	return e, nil
}

// ParseGo parses Go source code and returns the root AST node.
func (e *TreeSitterEngine) ParseGo(ctx context.Context, content []byte) (*sitter.Node, error) {
	tree, err := e.goParser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, err
	}
	return tree.RootNode(), nil
}

// Close releases all parser resources.
func (e *TreeSitterEngine) Close() {
	if e.goParser != nil {
		e.goParser.Close()
	}
}
