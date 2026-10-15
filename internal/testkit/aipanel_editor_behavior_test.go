package testkit

import (
	"github.com/amin-jalali/termocode/internal/theme"
	"strings"
	"testing"
	"time"
)

// Clickable things in the editor area must stay clickable with the AI
// chat panel open: their hit areas come from the same (narrower) pane
// width the view draws them at. See welcome_behavior_test.go for the
// Welcome cards.

// The terminal panel's right-hand controls (↑ maximize, − minimize,
// × close) sit at the right end of its tab bar.
func TestTerminalBarControlsWithAIPanel(t *testing.T) {
	d := editorDriver(t, nil)
	openAIPanel(d)
	clickCard(d, "Open shell", aiPanelEdge(d))
	if !d.WaitForText("Terminal ›", 5*time.Second) {
		t.Fatalf("terminal did not open:\n%s", screen(d))
	}
	x, y, ok := findIn(d, "↑−"+theme.GlyphClose.String(), 0, aiPanelEdge(d))
	if !ok {
		t.Fatalf("no terminal controls:\n%s", screen(d))
	}
	d.Click(x+2, y) // ×: close the panel
	if !d.WaitFor(func() bool { return !strings.Contains(screen(d), "↑−"+theme.GlyphClose.String()) }, 3*time.Second) {
		t.Fatalf("× on the terminal bar did not close the panel:\n%s", screen(d))
	}
}
