package assets

import (
	"strings"
	"testing"
)

// budgetPTerm is the phone Term page's AI parts (pterm.js), gzipped: the
// chips, the Ask AI field and the line menu.
const budgetPTerm = 3 << 10

// The phone Term page's AI parts are in pterm.js alone: it imports
// nothing (the page gives it Preact, the link and the menus), the page
// loads it by its fixed path, and the first files do not carry it.
func TestPTermChunkHoldsTheAIParts(t *testing.T) {
	js, page := readBuilt(t, "pterm.js"), readPage(t)
	if !strings.Contains(js, "Ask AI to write a command") || !strings.Contains(js, `kind:"term"`) {
		t.Error("pterm.js does not hold the Ask AI field and the term references")
	}
	if strings.Contains(page, "Ask AI to write a command") {
		t.Error("the page's first files carry the Term page's AI parts")
	}
	for _, bad := range []string{"import ", "import(", "app-chunk"} {
		if strings.Contains(js, bad) {
			t.Errorf("pterm.js imports something (%q): the page gives it what it needs", bad)
		}
	}
	if !strings.Contains(readBuilt(t, "home.js"), `import("/assets/pterm.js")`) {
		t.Error("home.js does not load /assets/pterm.js")
	}
	if n := gzSize(t, "pterm.js"); n > budgetPTerm {
		t.Errorf("pterm.js is %d B gzipped, budget %d B", n, budgetPTerm)
	} else {
		t.Logf("gzipped: pterm.js %d B (with the phone's terminal)", n)
	}
}
