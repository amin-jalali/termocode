package assets

import (
	"os"
	"regexp"
	"testing"
)

// Every icon class of styles/codicons.css names the vendored font's own
// code point for that icon (its codicon.css), so an icon never shows the
// wrong picture.
func TestCodiconClassesMatchTheFont(t *testing.T) {
	own, err := os.ReadFile("src/styles/codicons.css")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := os.ReadFile("vendor/vscode-codicons@0.0.45/dist/codicon.css")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, m := range regexp.MustCompile(`\.codicon-([a-z0-9-]+):before \{ content: "(\\[0-9a-f]+)"`).FindAllStringSubmatch(string(ref), -1) {
		want[m[1]] = m[2]
	}
	n := 0
	for _, m := range regexp.MustCompile(`\.cod-([a-z0-9-]+)::before \{ content: "(\\[0-9a-f]+)"; \}`).FindAllStringSubmatch(string(own), -1) {
		n++
		if want[m[1]] != m[2] {
			t.Errorf("cod-%s is %s, the font has %q", m[1], m[2], want[m[1]])
		}
	}
	if n < 20 {
		t.Fatalf("only %d icon classes read", n)
	}
}
