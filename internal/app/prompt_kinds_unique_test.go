package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPromptAndPickerKindsAreUnique: like confirm kinds, a prompt or
// picker kind shared by two flows answers one flow with the other's
// action (the agent packs source prompt and the kube save screen prompt
// both once held 3790). The numbers are hand-picked in many files, so
// this walks every const of type promptKindEnum and pickerKindEnum.
func TestPromptAndPickerKindsAreUnique(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, typeName := range []string{"promptKindEnum", "pickerKindEnum"} {
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
					if !ok || id.Name != typeName || len(vals) == 0 {
						continue
					}
					v, ok := constValue(vals[0], int64(i))
					if !ok {
						t.Fatalf("%s: %s: cannot read the value", f, vs.Names[0].Name)
					}
					name := vs.Names[0].Name
					if other, dup := seen[v]; dup {
						t.Errorf("%s: %s and %s are both %d", typeName, other, name, v)
					}
					seen[v] = name
				}
			}
		}
		if len(seen) < 20 {
			t.Fatalf("%s: found only %d kinds; the walk is broken", typeName, len(seen))
		}
	}
}
