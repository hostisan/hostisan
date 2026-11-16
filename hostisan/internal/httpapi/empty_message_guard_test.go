package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestNoEmptyBadRequestMessage fails when a handler can answer 400 without a message:
// the UI then has nothing to show but "Something went wrong". Every 400 names what
// was refused (a msgid such as invalidFormatMsg, with its args).
func TestNoEmptyBadRequestMessage(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	isEmpty := func(e ast.Expr) bool {
		lit, ok := e.(*ast.BasicLit)
		return ok && lit.Kind == token.STRING && (lit.Value == `""` || lit.Value == "``")
	}
	is400 := func(e ast.Expr) bool {
		sel, ok := e.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "StatusBadRequest"
	}
	for _, en := range entries {
		name := en.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				id, ok := n.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				a := n.Args
				switch {
				case id.Name == "WriteError" && len(a) == 4 && is400(a[1]) && isEmpty(a[3]),
					id.Name == "sysReject" && len(a) >= 3 && is400(a[0]) && isEmpty(a[2]),
					id.Name == "invalid" && len(a) == 2 && isEmpty(a[1]):
					t.Errorf("%s: 400 with an empty message", fset.Position(n.Pos()))
				}
			case *ast.CompositeLit:
				if id, ok := n.Type.(*ast.Ident); !ok || id.Name != "ErrorBody" {
					return true
				}
				msg := false
				for _, el := range n.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Message" && !isEmpty(kv.Value) {
							msg = true
						}
					}
				}
				if !msg {
					t.Errorf("%s: ErrorBody without a message", fset.Position(n.Pos()))
				}
			}
			return true
		})
	}
}
