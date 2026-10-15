package activity

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The View cache must not hide items added (or removed) after the bar
// was drawn: a process extension registers its panel while the app runs.
func TestSetExtraItemsRedraws(t *testing.T) {
	t.Cleanup(func() { SetExtraItems(nil) })
	SetExtraItems(nil)
	m := New()
	m.SetHeight(40)
	before := ansi.Strip(m.View())
	if strings.Contains(before, "Q") {
		t.Fatalf("bar already shows Q:\n%s", before)
	}
	SetExtraItems([]ExtraItem{{Label: "Quux", Glyph: "Q"}})
	if after := ansi.Strip(m.View()); !strings.Contains(after, "Q") {
		t.Errorf("new extension item not drawn:\n%s", after)
	}
	SetExtraItems(nil)
	if again := ansi.Strip(m.View()); strings.Contains(again, "Q") {
		t.Errorf("removed extension item still drawn:\n%s", again)
	}
}
