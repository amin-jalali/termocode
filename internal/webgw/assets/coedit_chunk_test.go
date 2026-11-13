package assets

import (
	"io/fs"
	"strings"
	"testing"
)

// budgetCoedit is live editing together (coedit.js, G-COL): the editor's
// sync with the laptop, the merge of both sides' typing and the other
// people's carets, gzipped. The page loads it when an editor opens.
const budgetCoedit = 6 << 10

// Live editing together is in coedit.js alone: the phone's page holds
// none of its code, and coedit.js imports nothing (the page gives it its
// signals and link through init), so it never brings a second copy of a
// module. The page budget stays 120 KiB.
func TestCoeditChunkHoldsLiveEditing(t *testing.T) {
	if _, err := fs.Stat(FS(), "coedit.js"); err != nil {
		t.Fatalf("no coedit.js: %v", err)
	}
	co, page := readBuilt(t, "coedit.js"), readPage(t)
	for _, want := range []string{`"buf.set_text"`, `"buf.cursor"`} {
		if !strings.Contains(co, want) {
			t.Errorf("coedit.js has no %s", want)
		}
	}
	if strings.Contains(page, `"buf.cursor"`) {
		t.Error("the phone's page sends buf.cursor itself; it belongs in coedit.js")
	}
	for _, bad := range []string{"import ", "import(", "app-chunk"} {
		if strings.Contains(co, bad) {
			t.Errorf("coedit.js imports something (%q): the page gives it what it needs", bad)
		}
	}
	if !strings.Contains(page, `import("/assets/coedit.js")`) {
		t.Error("the page does not load /assets/coedit.js")
	}
	if strings.Contains(readBuilt(t, "index.html"), "coedit.js") {
		t.Error("index.html names coedit.js: the page loads it when an editor opens")
	}
	if n := gzSize(t, "coedit.js"); n > budgetCoedit {
		t.Errorf("coedit.js is %d B gzipped, budget %d B", n, budgetCoedit)
	} else {
		t.Logf("gzipped: coedit.js %d B (on first edit)", n)
	}
}
