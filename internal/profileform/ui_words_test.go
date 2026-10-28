package profileform

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// slotWord finds the word "slot" in a UI string.
var slotWord = regexp.MustCompile(`(?i)\bslots?\b`)

// Spec (AIP-7): the profile form and the Add AI card never say "slot" to
// the user; a step picks a model from one list. Only the expert form
// (AI: Add Model Slot…, in internal/app) still names slots. A string that
// is exactly a profiles.json field name ("slot") is a key, not UI text.
func TestUIStringsNeverSaySlot(t *testing.T) {
	for _, dir := range []string{".", filepath.Join("..", "addai")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("no Go files in %s", dir)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			fs := token.NewFileSet()
			file, err := parser.ParseFile(fs, f, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil || s == "slot" {
					return true
				}
				if slotWord.MatchString(s) {
					t.Errorf("%s: UI string says slot: %q", fs.Position(lit.Pos()), s)
				}
				return true
			})
		}
	}
}
