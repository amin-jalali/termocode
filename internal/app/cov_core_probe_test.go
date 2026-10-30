package app

import (
	"strings"
	"testing"
)

// covProbeAll checks that a click on every hit target of m resolves to
// that target, and returns the ids seen.
func covProbeAll(t *testing.T, m Model, what string) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	for _, h := range m.hitTargets() {
		x, y := h.X+h.W/2, h.Y+h.H/2
		got := m.probeClick(x, y)
		seen[got] = true
		if got != h.ID && !strings.HasPrefix(got, strings.SplitN(h.ID, ":", 2)[0]) {
			t.Errorf("%s: click on %s at (%d,%d) went to %q", what, h.ID, x, y, got)
		}
	}
	return seen
}

// Every click target of the main screens takes its own clicks: the
// launcher chip, the ⋮ menu rows, the panel content, the editor.
func TestCovCoreProbeTargets(t *testing.T) {
	m := covNvimModel(t, 140, 40)
	covEditorFile(t, &m, "p.go", "package p\n")

	m.actionsOpen, m.actionsPinned = true, false
	m.applyLayout()
	if seen := covProbeAll(t, m, "launcher"); !seen["actions:launcher"] {
		t.Errorf("launcher not probed: %v", seen)
	}
	m.actionsOpen = false

	m.openOverflowMenu()
	covProbeAll(t, m, "overflow menu")
	if x, y, ok := covHitCell(m, func(x, y int) bool { in, i := m.overflowMenuHitAt(x, y); return in && i >= 0 }); ok {
		if got := m.probeClick(x, y); !strings.HasPrefix(got, "overflow:item:") {
			t.Errorf("overflow row probe: %q", got)
		}
	}
	if x, y, ok := covHitCell(m, func(x, y int) bool { in, i := m.overflowMenuHitAt(x, y); return in && i < 0 }); ok {
		if got := m.probeClick(x, y); got != "overflow:menu" {
			t.Errorf("overflow border probe: %q", got)
		}
	}
	m.overflowMenuOpen = false

	m.showPanelTab(panelKindProblems)
	x, y, w, h, ok := m.panelContentRect()
	if ok {
		if got := m.probeClick(x+w/2, y+h/2); got != "panel:content" {
			t.Errorf("panel content probe: %q", got)
		}
	}
	covProbeAll(t, m, "panel")
	pane := m.editorPaneLayout()
	if got := m.probeClick(pane.X+3, pane.Top+2); got != "editor" {
		t.Errorf("editor probe: %q", got)
	}
	// The activity bar below its icons.
	if got := m.probeClick(1, m.h-3); !strings.HasPrefix(got, "activity") {
		t.Errorf("activity probe: %q", got)
	}
}

// Overlays take every click on the probe copy.
func TestCovCoreProbeOverlays(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	for name, open := range map[string]func(*Model){
		"settings": func(m *Model) { _ = m.openSettingsPicker() },
		"menu":     func(m *Model) { m.openTabMenu(1, 10, 1) },
		"prompt":   func(m *Model) { m.openGotoLinePrompt() },
		"search":   func(m *Model) { _ = m.dispatchPaletteAction("find-files-overlay") },
		"picker":   func(m *Model) { _ = m.openKeybindingPicker() },
		"confirm":  func(m *Model) { m.openDeleteConfirm("/w/x.go") },
	} {
		mm := m
		open(&mm)
		got := mm.probeClick(2, 2)
		if got == "" {
			t.Errorf("%s: a click went nowhere", name)
		}
		if mm.pickerOpen {
			if got := mm.probeClick(mm.w/2, mm.h/2); !strings.HasPrefix(got, "picker") {
				t.Errorf("picker row probe: %q", got)
			}
		}
	}
	// The guide: a link or the page.
	m.openCheatSheet()
	if got := m.probeClick(m.w/2, m.h/2); got == "" {
		t.Error("guide probe went nowhere")
	}
}
