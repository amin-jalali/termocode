package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// Every overlay follows a resize at many sizes, takes the mouse, and
// never draws past the screen.
func TestCovCoreOverlaysResize(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	f := filepath.Join(t.TempDir(), "recent.go")
	_ = os.WriteFile(f, []byte("package x\n"), 0o644)
	pushRecent(f)

	open := map[string]func(m *Model){
		"palette": func(m *Model) { _ = m.openKeybindingPicker() },
		"recents": func(m *Model) { _ = m.openRecentFilePicker() },
		"search":  func(m *Model) { _ = m.dispatchPaletteAction("find-files-overlay") },
		"confirm": func(m *Model) { m.openDeleteConfirm("/tmp/x.go") },
		"prompt":  func(m *Model) { m.openGotoLinePrompt() },
		"menu":    func(m *Model) { m.openTabMenu(1, 10, 1) },
		"settings": func(m *Model) {
			_ = m.openSettingsPicker()
		},
	}
	sizes := [][2]int{{120, 40}, {80, 24}, {50, 16}, {200, 60}, {36, 12}}
	for name, o := range open {
		mm := m
		o(&mm)
		if mm.overlayKind() == "" && !mm.searchOpen && !mm.confirmOpen {
			t.Errorf("%s did not open", name)
			continue
		}
		for _, sz := range sizes {
			mm, _ = mm.update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			if mm.w != sz[0] || mm.h != sz[1] {
				t.Errorf("%s: size %dx%d after %v", name, mm.w, mm.h, sz)
			}
			covFitsScreen(t, mm, name)
			mm, _ = mm.update(tea.MouseMsg{X: mm.w / 2, Y: mm.h / 2, Type: tea.MouseMotion, Action: tea.MouseActionMotion})
			mm, _ = mm.update(tea.MouseMsg{X: mm.w / 2, Y: mm.h / 2, Type: tea.MouseWheelDown, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
		}
	}
}

// Overlays stack: the guide opens over Settings and Esc goes back to it.
func TestCovCoreOverlaysStack(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	_ = m.openSettingsPicker()
	m, cmd := m.update(tea.KeyMsg{Type: tea.KeyF13}) // Shift+F1: explain
	m = helpFeed(t, m, cmd)
	if !m.previewOpen || !m.settingsModalOpen {
		t.Fatalf("guide over settings: preview %v settings %v", m.previewOpen, m.settingsModalOpen)
	}
	covFitsScreen(t, m, "guide over settings")
	m, cmd = m.update(tea.KeyMsg{Type: tea.KeyEsc})
	m = helpFeed(t, m, cmd)
	if m.previewOpen || !m.settingsModalOpen {
		t.Errorf("esc: preview %v settings %v", m.previewOpen, m.settingsModalOpen)
	}
}

// nvim state: a reload names the file once, a cursor move drops the
// code action hint, errors show, fetch timers wait for nvim.
func TestCovCoreNvimStateMessages(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	m.preferredCodeAction = &nvim.CodeAction{Title: "fix"}
	m.preferredCodeActionLine = 3
	m, _ = m.update(StateMsg{Path: "/w/a.go", CursorLine: 7, LastReloadSeq: 1, LastReloadPath: "/w/a.go"})
	if m.preferredCodeAction != nil {
		t.Error("the hint stayed after the cursor moved")
	}
	if !strings.Contains(toastText(m), "Reloaded") {
		t.Errorf("reload toast:\n%s", toastText(m))
	}
	m, _ = m.update(StateMsg{Path: "/w/a.go", LastReloadSeq: 2})
	if m.lastReloadSeq != 2 {
		t.Error("an empty reload path did not advance the counter")
	}
	m, _ = m.update(nvim.ErrMsg{Err: errors.New("E492: Not an editor command")})
	if !strings.Contains(m.err, "E492") {
		t.Errorf("err = %q", m.err)
	}
	var none Model
	none.w, none.h = 80, 24
	if got, _ := none.update(stateFetchBusyMsg{}); got.stateFetchPending {
		t.Error("a fetch timer without nvim")
	}
	none.stateFetchPending = true
	if got, _ := none.update(stateFetchDueMsg{}); got.stateFetchPending || !got.stateLastFetch.IsZero() {
		t.Error("a fetch without nvim")
	}
}
