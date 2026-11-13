package assets

import (
	"strings"
	"testing"
)

// A link's sheet and the port preview (WEB-54) are in link.js alone: the
// phone's page loads it on the first tap and gives it Preact, the sheet,
// the link, the strings and the toasts, so link.js imports nothing and
// the page's first files do not carry it.
func TestLinkChunkHoldsTheSheet(t *testing.T) {
	ln, page, wb := readBuilt(t, "link.js"), readPage(t), readBuilt(t, "workbench.js")
	for _, want := range []string{`"web.forward"`, `"fwd.ready"`, `link-ask`} {
		if !strings.Contains(ln, want) {
			t.Errorf("link.js has no %s", want)
		}
		if strings.Contains(page, want) || strings.Contains(wb, want) {
			t.Errorf("the page's files or workbench.js hold %s; it belongs in link.js", want)
		}
	}
	for _, bad := range []string{"import ", "import(", "app-chunk"} {
		if strings.Contains(ln, bad) {
			t.Errorf("link.js imports something (%q): the page gives it what it needs", bad)
		}
	}
	if !strings.Contains(page, `import("/assets/link.js")`) {
		t.Error("the page does not load /assets/link.js")
	}
	if strings.Contains(readBuilt(t, "index.html"), "link.js") {
		t.Error("index.html names link.js: the page loads it on the first tap")
	}
}
