package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
)

// Spec: the activity bar has an Itermoc item below Databases; a click on it
// resolves to the item and opens the view.
func TestItermocActivityBarItem(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	m.activity.SetHeight(m.h)
	m.activity.SetActive(activity.ViewFiles)
	var item hitTarget
	for _, h := range m.hitTargets() {
		if h.ID == "activity:Itermoc" {
			item = h
		}
	}
	if item.ID == "" {
		t.Fatal("no Itermoc item in the activity bar")
	}
	if item.Label == "" {
		t.Error("the item draws nothing")
	}
	if got := m.probeClick(item.X+2, item.Y); got != item.ID {
		t.Errorf("probeClick = %q, want %q", got, item.ID)
	}
	if name, _ := m.tooltipLabel(item.ID); name != "Itermoc" {
		t.Errorf("tooltip %q", name)
	}
	if tooltipHelp(item.ID) != "guides/itermoc.md#the-view" {
		t.Errorf("help topic %q", tooltipHelp(item.ID))
	}
}
