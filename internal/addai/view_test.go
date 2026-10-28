package addai

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

var allSteps = []string{"pick", "method", "key", "key-busy", "url", "local", "local-bad", "signin-bad",
	"signin-ask", "signin-install", "signin-browser", "signin-code", "signin-device", "signin-models", "signin-nonode", "done"}

// Spec: every step fits 80x24 and 200x50 (every line the same width, no
// wider than the screen) and draws every button it has.
func TestEveryStepFits(t *testing.T) {
	for _, step := range allSteps {
		for _, sz := range [][2]int{{80, 24}, {200, 50}} {
			m := at(t, step)
			m.SetSize(sz[0], sz[1])
			lines := strings.Split(m.Box(), "\n")
			w := ansi.StringWidth(lines[0])
			if w > sz[0] || len(lines) > sz[1] {
				t.Errorf("%s at %dx%d: %dx%d", step, sz[0], sz[1], w, len(lines))
			}
			for i, l := range lines {
				if ansi.StringWidth(l) != w {
					t.Errorf("%s at %dx%d line %d: %d cells, want %d", step, sz[0], sz[1], i, ansi.StringWidth(l), w)
				}
			}
			// Every button is a hit target with its title.
			rects := m.ButtonRects()
			if len(rects) != len(m.Card().Buttons) {
				t.Errorf("%s: %d button targets for %d buttons", step, len(rects), len(m.Card().Buttons))
			}
			for _, r := range rects {
				if r.Title == "" || r.W <= 0 {
					t.Errorf("%s: target %+v", step, r)
				}
			}
		}
	}
}

// Spec: the words are plain: no raw error text, no "slot", and the
// errors are one sentence.
func TestPlainWords(t *testing.T) {
	for _, step := range allSteps {
		s := ansi.Strip(at(t, step).Box())
		for _, bad := range []string{"slot", "Slot", "error:", "HTTP", "401"} {
			if strings.Contains(s, bad) {
				t.Errorf("%s says %q:\n%s", step, bad, s)
			}
		}
	}
}
