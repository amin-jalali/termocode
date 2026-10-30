package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (WEB-21, never-do 17): the web code sends nvim only constant Lua
// chunks (paths and text go as RPC arguments), and never a command line
// or keys.
func TestNoDynamicLuaInWeb(t *testing.T) {
	files, _ := filepath.Glob("web_*.go")
	fset := token.NewFileSet()
	consts := map[string]bool{}
	var parsed []*ast.File
	for _, f := range append(files, "ai_terminal_tools.go") {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.CONST {
				for _, s := range g.Specs {
					for _, n := range s.(*ast.ValueSpec).Names {
						consts[n.Name] = true
					}
				}
			}
		}
		if strings.HasPrefix(f, "web_") {
			parsed = append(parsed, af)
		}
	}
	lua := map[string]bool{"ExecLua": true, "ExecLuaResult": true, "ExecLuaArgs": true, "EvalLuaArgs": true,
		"EvalLuaString": true, "EvalLuaStringArgs": true}
	never := map[string]bool{"Command": true, "Input": true, "OpenFile": true}
	n := 0
	for _, af := range parsed {
		ast.Inspect(af, func(x ast.Node) bool {
			call, ok := x.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pos := fset.Position(call.Pos())
			if never[sel.Sel.Name] {
				t.Errorf("%s: web code calls %s", pos, sel.Sel.Name)
			}
			if !lua[sel.Sel.Name] {
				return true
			}
			n++
			id, ok := call.Args[0].(*ast.Ident)
			if !ok || !consts[id.Name] {
				t.Errorf("%s: %s with a chunk that is not a named constant", pos, sel.Sel.Name)
			}
			return true
		})
	}
	if n < 6 {
		t.Fatalf("only %d Lua calls found: the check looks at the wrong files", n)
	}
}
