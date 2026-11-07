package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/keymap"
)

// Spec (Group D): F9 toggles a breakpoint, F10 steps over, F11 steps into
// and Shift+F11 (F23) steps out.
func TestDebugStepKeysAreBound(t *testing.T) {
	km := keymap.Default()
	for key, want := range map[tea.KeyType]keymap.Action{
		tea.KeyF9:  keymap.ActionDebugToggleBreakpoint,
		tea.KeyF10: keymap.ActionDebugStepOver,
		tea.KeyF11: keymap.ActionDebugStepInto,
	} {
		if got := km.Match(tea.KeyMsg{Type: key}); got != want {
			t.Errorf("%v: %v, want %v", key, keymap.ActionName(got), keymap.ActionName(want))
		}
	}
	if keys := km.KeysFor(keymap.ActionDebugStepOut); len(keys) == 0 || keys[0] != "f23" { // xterm's Shift+F11
		t.Errorf("step out keys %v", keys)
	}
}

// Spec (merge5): every palette entry these features added answers in the
// UI when picked with nothing set up: a card, a picker, a prompt, a page
// or a toast that says what is missing. None of them does nothing, and
// none panics.
func TestMerge5PaletteEntriesAnswer(t *testing.T) {
	for _, id := range []string{
		"ai-chat-doc-revisions", "ai-chat-docs", "ai-dial", "ai-run-task-list",
		"containers-check-updates", "containers-registry-catalog",
		"db-chart", "db-copy-as", "db-copy-table-data", "db-export-tables", "db-heatmap", "db-import-new-table",
		"db-refresh-object", "db-result-create-table", "db-run-dump", "db-schema-cache-info", "db-search-data",
		"http-clear-script-vars", "http-export-har", "http-import-collection", "http-ws-close", "http-ws-send",
		"services-deploy-git", "workspace-open-container", "workspace-reopen-devcontainer",
		"worktree-feedback", "worktree-follow", "worktree-new-multi", "worktree-run", "worktree-run-open",
	} {
		t.Run(id, func(t *testing.T) {
			m := aiTestModel(t)
			m.ai.provider = fakeAIProvider{reply: "ok"}
			m.ws.Root = t.TempDir()
			m.services, m.kube, m.wt = newServicesState(), newKubeState(), newWTState() // as New makes them
			t.Cleanup(m.kube.shutdown)
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked: %v", id, r)
				}
			}()
			cmd := m.dispatchPaletteAction(id)
			if cmd == nil && m.toast.Empty() && !m.confirmOpen && !m.pickerOpen && !m.promptOpen {
				t.Errorf("%s did nothing the user can see", id)
			}
		})
	}
}

// Spec (DBX-26, DBX-41): the result grid toggles flip their setting for
// the next page; Clear Inline Results drops every inline result; the
// entries that read the editor (cell menu, run smallest, run to file) do
// nothing without one and never panic.
func TestMerge5DatabaseResultEntries(t *testing.T) {
	withConfigDir(t)
	m := &Model{w: 120, h: 40}
	s := m.dbConsole()
	rows, types := s.showRowNums(), s.showColTypes()
	m.dispatchPaletteAction("db-result-row-numbers")
	m.dispatchPaletteAction("db-result-column-types")
	if s.showRowNums() == rows || s.showColTypes() == types {
		t.Fatalf("toggles: row numbers %v→%v, types %v→%v", rows, s.showRowNums(), types, s.showColTypes())
	}
	s.inline = map[string][]string{"/w/a.sql\x00SELECT 1": {"1"}}
	m.dispatchPaletteAction("db-inline-clear")
	if len(s.inline) != 0 {
		t.Fatalf("inline results kept: %v", s.inline)
	}
	for _, id := range []string{"db-result-cell-menu", "db-run-smallest", "db-run-to-file"} {
		if cmd := m.dispatchPaletteAction(id); cmd != nil || m.confirmOpen || m.pickerOpen {
			t.Errorf("%s acted without an editor", id)
		}
	}
}
