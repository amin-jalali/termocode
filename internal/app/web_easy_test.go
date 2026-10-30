//go:build unix

package app

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
)

// webOpenedLinks swaps webOpenPath: each opened redirect page's text
// goes to the channel.
func webOpenedLinks(t *testing.T, fail error) chan string {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", webTestDir(t))
	t.Chdir(t.TempDir())
	opened := make(chan string, 4)
	old := webOpenPath
	t.Cleanup(func() { webOpenPath = old })
	webOpenPath = func(path string) error {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		opened <- string(b)
		return fail
	}
	return opened
}

// Spec (web-easy): the user's own palette command opens the browser in
// one step, with no setting and no card; again on a running panel it
// opens a new one-time link. "Web: Start Panel" is the same command.
func TestWebPaletteOpensBrowserAtOnce(t *testing.T) {
	for _, id := range []string{"web-start", "web-start-panel"} {
		t.Run(id, func(t *testing.T) {
			r := newWebPanelRig(t)
			writeUserConfig(t, `{}`)
			opened := webOpenedLinks(t, nil)
			if _, ok := r.m.dispatchWebPalette(id); !ok {
				t.Fatalf("%s is not a web palette id", id)
			}
			if r.m.confirmOpen {
				t.Fatal("a card came up")
			}
			if r.m.webPanel == nil {
				t.Fatal("the panel did not start")
			}
			r.pumpUntil("the panel up", func() bool { return r.m.webPanel != nil && r.m.webPanel.up })
			first := <-opened
			if !strings.Contains(first, "#s=") {
				t.Fatalf("redirect page %q", first)
			}
			r.m.dispatchWebPalette(id)
			if r.m.confirmOpen {
				t.Fatal("a card came up on a running panel")
			}
			select {
			case again := <-opened:
				if again == first || !strings.Contains(again, "#s=") {
					t.Fatal("no new one-time link")
				}
			default:
				t.Fatal("nothing opened on a running panel")
			}
		})
	}
}

// Spec (web-easy): the palette entry is "Web: Open in Browser" ("Web:
// Start Panel" stays as another name), and typing "web" puts it first.
func TestWebPaletteOpenInBrowserFirst(t *testing.T) {
	titles := map[string]string{}
	for _, it := range webPaletteItems() {
		titles[it.ID] = it.Title
	}
	if titles["web-start"] != "Web: Open in Browser" || titles["web-start-panel"] != "Web: Start Panel" {
		t.Fatalf("titles %v", titles)
	}
	p := picker.NewItems(" Command Palette ", paletteItems())
	for _, r := range "web" {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if it, ok := p.Current(); !ok || it.ID != "web-start" {
		t.Fatalf(`"web" selects %q (%q)`, it.ID, it.Title)
	}
}
