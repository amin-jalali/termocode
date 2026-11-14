package assets

import (
	"regexp"
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

// Spec (NEW-7): the port preview's own strings in link.js have Persian
// texts too (cut once for the budget): the same keys and placeholders,
// picked when the page is in Persian (<html lang="fa">).
func TestLinkChunkSpeaksPersian(t *testing.T) {
	src := readSrc(t, "src/link.ts")
	en, fa := linkStrings(t, src, "FS"), linkStrings(t, src, "FA")
	if len(en) == 0 {
		t.Fatal("link.ts has no FS strings")
	}
	for k, v := range en {
		f, ok := fa[k]
		if !ok || f == "" {
			t.Errorf("link.ts FA lacks %s", k)
			continue
		}
		if strings.Contains(v, "{port}") != strings.Contains(f, "{port}") {
			t.Errorf("link.ts FA %s does not keep {port}: %q", k, f)
		}
	}
	if len(fa) != len(en) {
		t.Errorf("FA has %d keys, FS %d", len(fa), len(en))
	}
	if !strings.Contains(src, `documentElement.lang === "fa"`) {
		t.Error("link.ts does not pick Persian by the page's language")
	}
	if !strings.Contains(readBuilt(t, "link.js"), "رساندن درگاه") {
		t.Error("link.js has no Persian Forward port")
	}
}

// linkStrings reads the object `const name = { k: "v", ... };` of src.
func linkStrings(t *testing.T, src, name string) map[string]string {
	t.Helper()
	i := strings.Index(src, "const "+name+" = {")
	if i < 0 {
		return nil
	}
	body := src[i:]
	body = body[:strings.Index(body, "};")]
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s*(\w+): "([^"]*)",`).FindAllStringSubmatch(body, -1) {
		out[m[1]] = m[2]
	}
	return out
}
