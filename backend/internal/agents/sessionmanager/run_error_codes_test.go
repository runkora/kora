package sessionmanager

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Every run error code and loop kind the session-manager emits comes from the
// platform/events constants, which are the schema enums (Q29). A string literal
// in their place would reach events as unknown.
func TestRunErrorCodesComeFromTheEventsConstants(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				typ, _ := n.Type.(*ast.Ident)
				if typ == nil || (typ.Name != "RunErrorPayload" && typ.Name != "loopFinding") {
					return true
				}
				for _, el := range n.Elts {
					kv, _ := el.(*ast.KeyValueExpr)
					if key, _ := kv.Key.(*ast.Ident); kv != nil && key != nil && (key.Name == "Code" || key.Name == "kind") {
						if lit, ok := kv.Value.(*ast.BasicLit); ok {
							t.Errorf("%s: %s %s is the literal %s", fset.Position(lit.Pos()), typ.Name, key.Name, lit.Value)
						}
					}
				}
			case *ast.CallExpr:
				sel, _ := n.Fun.(*ast.SelectorExpr)
				// The position of the code argument in each emitting call.
				at, ok := map[string]int{"endTurnWithError": 0, "emitRunError": 2}[selName(sel)]
				if !ok || len(n.Args) <= at {
					return true
				}
				if lit, ok := n.Args[at].(*ast.BasicLit); ok {
					t.Errorf("%s: %s takes the literal code %s", fset.Position(lit.Pos()), sel.Sel.Name, lit.Value)
				}
			}
			return true
		})
	}
}

func selName(sel *ast.SelectorExpr) string {
	if sel == nil {
		return ""
	}
	return sel.Sel.Name
}
