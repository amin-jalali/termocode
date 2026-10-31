package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestConfirmKindsAreUnique: two card kinds with one number share one
// answer path, so an answer to one card runs the other card's action
// (the TOTP code card once answered as a web ask). Kinds live in many
// files with hand-picked numbers; this walks every const of type
// confirmKindEnum (N, N + iota and the implicit repeats of a group).
func TestConfirmKindsAreUnique(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			var typ ast.Expr
			var vals []ast.Expr
			for i, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				if vs.Type != nil || len(vs.Values) > 0 {
					typ, vals = vs.Type, vs.Values
				}
				id, ok := typ.(*ast.Ident)
				if !ok || id.Name != "confirmKindEnum" || len(vals) == 0 {
					continue
				}
				v, ok := constValue(vals[0], int64(i))
				if !ok {
					t.Fatalf("%s: %s: cannot read the value", f, vs.Names[0].Name)
				}
				name := vs.Names[0].Name
				if other, dup := seen[v]; dup {
					t.Errorf("%s and %s are both %d", other, name, v)
				}
				seen[v] = name
			}
		}
	}
	if len(seen) < 50 {
		t.Fatalf("found only %d kinds; the walk is broken", len(seen))
	}
}

func constValue(e ast.Expr, iota int64) (int64, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		n, err := strconv.ParseInt(x.Value, 0, 64)
		return n, err == nil
	case *ast.Ident:
		if x.Name == "iota" {
			return iota, true
		}
	case *ast.ParenExpr:
		return constValue(x.X, iota)
	case *ast.BinaryExpr:
		a, ok1 := constValue(x.X, iota)
		b, ok2 := constValue(x.Y, iota)
		if ok1 && ok2 && x.Op == token.ADD {
			return a + b, true
		}
	}
	return 0, false
}
