package lspinstall

import (
	"strings"
	"testing"
)

// Spec (OMX-28): ast-grep installs with npm into tools/ast-grep and is
// never listed with the language servers or debug adapters.
func TestAstGrepTool(t *testing.T) {
	tl, ok := Lookup("ast-grep")
	if !ok || tl.Category != CategoryCodeTool || tl.InstallKind != KindNpm || tl.Spec != "@ast-grep/cli" || tl.Bin != "ast-grep" {
		t.Fatalf("ast-grep = %+v (%v)", tl, ok)
	}
	p, err := BuildPlan(tl, "/tools/ast-grep", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if argv := strings.Join(p.Steps[0].Argv, " "); !strings.HasPrefix(argv, "npm install --prefix /tools/ast-grep ") || strings.Contains(argv, " -g") {
		t.Errorf("argv %q", argv)
	}
	for _, cat := range []Category{CategoryLSP, CategoryDAP, CategoryAgentCLI} {
		for _, x := range ByCategory(cat) {
			if x.Name == "ast-grep" {
				t.Errorf("listed as category %d", cat)
			}
		}
	}
}
