package assets

import (
	"strings"
	"testing"
)

// budgetPages is the phone's tool pages (pages.js, UI pages: Settings,
// Run and Debug), gzipped; budgetPagesCSS is their stylesheet. Only a
// phone or tablet loads them, when one of the pages opens.
const (
	budgetPages    = 14 << 10
	budgetPagesCSS = 3 << 10
)

// Spec (UI pages): the tool pages are pages.js and pages.css alone; the
// page loads them (lib/pages.ts), pages.js imports nothing (the page
// gives it Preact and the session), and neither the page's first files
// nor the workbench nor home.js carry the pages' code.
func TestPerfGuardPagesChunk(t *testing.T) {
	js, page := readBuilt(t, "pages.js"), readPage(t)
	for _, bad := range []string{"import ", "import{", "from\""} {
		if strings.Contains(js, bad) {
			t.Errorf("pages.js imports something (%q): the page gives it what it needs", bad)
		}
	}
	if !strings.Contains(page, `import("/assets/pages.js")`) || !strings.Contains(page, "/assets/pages.css") {
		t.Error("the page does not load /assets/pages.js and /assets/pages.css")
	}
	for _, f := range []string{"index.html", "app.js", "workbench.js", "home.js"} {
		if strings.Contains(readBuilt(t, f), "pg-picker") {
			t.Errorf("%s carries the tool pages' code", f)
		}
	}
	if !strings.Contains(js, "pg-picker") || !strings.Contains(readBuilt(t, "pages.css"), ".pg-row") {
		t.Error("pages.js or pages.css is not the tool pages")
	}
	if n := gzSize(t, "pages.js"); n > budgetPages {
		t.Errorf("pages.js is %d B gzipped, budget %d B", n, budgetPages)
	}
	if n := gzSize(t, "pages.css"); n > budgetPagesCSS {
		t.Errorf("pages.css is %d B gzipped, budget %d B", n, budgetPagesCSS)
	}
}
