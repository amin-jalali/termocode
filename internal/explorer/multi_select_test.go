package explorer

import (
	"path/filepath"
	"sort"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// buildModelWithFiles makes an explorer Model rooted in t.TempDir() with
// every name in `names` created at the top level (files only). Returns the
// model and the absolute paths in the visible-row order they appear.
func buildModelWithFiles(t *testing.T, names []string) (Model, []string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		mustWrite(t, filepath.Join(dir, n), "")
	}
	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := Model{root: root}
	m.SetSize(60, 20)
	vis := m.visible()
	paths := make([]string, len(vis))
	for i, e := range vis {
		paths[i] = e.Node.Path
	}
	return m, paths
}

func TestToggleSelectionAddsAndRemoves(t *testing.T) {
	m, paths := buildModelWithFiles(t, []string{"a.txt", "b.txt", "c.txt"})
	if len(paths) < 4 { // root + 3 files
		t.Fatalf("expected at least 4 visible rows, got %d", len(paths))
	}
	// Pick the file rows (skip row 0 which is the root entry).
	a, b := paths[1], paths[2]

	m.ToggleSelection(a)
	if !m.IsSelected(a) {
		t.Fatalf("after toggle, %s should be selected", a)
	}
	if got := m.Selection(); len(got) != 1 || got[0] != a {
		t.Fatalf("Selection() = %v, want [%s]", got, a)
	}

	m.ToggleSelection(b)
	got := m.Selection()
	sort.Strings(got)
	want := []string{a, b}
	sort.Strings(want)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Selection() = %v, want %v", got, want)
	}

	// Re-toggling `a` removes it.
	m.ToggleSelection(a)
	if m.IsSelected(a) {
		t.Fatalf("after second toggle, %s should NOT be selected", a)
	}
	if got := m.Selection(); len(got) != 1 || got[0] != b {
		t.Fatalf("Selection() = %v, want [%s]", got, b)
	}
}

func TestSelectRangeInclusiveBothDirections(t *testing.T) {
	m, paths := buildModelWithFiles(t, []string{"a.txt", "b.txt", "c.txt", "d.txt"})
	// paths: [root, a.txt, b.txt, c.txt, d.txt] — exact ordering depends
	// on sort, but root first then alpha. Walk the visible list.
	if len(paths) < 5 {
		t.Fatalf("expected at least 5 visible rows, got %d", len(paths))
	}
	a := paths[1]
	d := paths[4]

	// Forward range a..d should include a, b, c, d.
	m.SelectRange(a, d)
	got := m.Selection()
	sort.Strings(got)
	want := append([]string(nil), paths[1:5]...)
	sort.Strings(want)
	if len(got) != 4 {
		t.Fatalf("forward range len = %d, want 4 (got=%v)", len(got), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("forward range mismatch [%d]: got %s want %s", i, got[i], want[i])
		}
	}

	// Reverse range d..a should produce the same set (inclusive endpoints,
	// both directions).
	m.ClearSelection()
	m.SelectRange(d, a)
	got = m.Selection()
	sort.Strings(got)
	if len(got) != 4 {
		t.Fatalf("reverse range len = %d, want 4 (got=%v)", len(got), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("reverse range mismatch [%d]: got %s want %s", i, got[i], want[i])
		}
	}
}

func TestClearSelection(t *testing.T) {
	m, paths := buildModelWithFiles(t, []string{"a.txt", "b.txt"})
	m.ToggleSelection(paths[1])
	m.ToggleSelection(paths[2])
	if len(m.Selection()) != 2 {
		t.Fatalf("setup: expected 2 selected")
	}
	m.ClearSelection()
	if got := m.Selection(); len(got) != 0 {
		t.Fatalf("after ClearSelection, len = %d (%v)", len(got), got)
	}
	if m.IsSelected(paths[1]) {
		t.Fatalf("after ClearSelection, IsSelected should be false")
	}
}

func TestCtrlClickTogglesViaHandleMouse(t *testing.T) {
	m, paths := buildModelWithFiles(t, []string{"a.txt", "b.txt"})
	// HandleMouse maps panel-local Y back through the chrome budget.
	// Body row 0 == visible[0] (root), body row 1 == visible[1] (a.txt),
	// body row 2 == visible[2] (b.txt). headerChromeRows() for h=20 is 4.
	chrome := m.HeaderChromeRows()
	rowAY := chrome + 1
	rowBY := chrome + 2

	// Ctrl+click row A — adds to selection, doesn't open.
	var cmd tea.Cmd
	m, cmd = m.HandleMouse(2, rowAY, tea.MouseLeft, true /*ctrl*/, false /*shift*/)
	if cmd != nil {
		t.Fatalf("ctrl+click should not return an OpenFileMsg cmd, got %v", cmd)
	}
	if !m.IsSelected(paths[1]) {
		t.Fatalf("ctrl+click on row A: A not selected. selection=%v", m.Selection())
	}

	// Ctrl+click row B — adds B too.
	m, _ = m.HandleMouse(2, rowBY, tea.MouseLeft, true, false)
	got := m.Selection()
	sort.Strings(got)
	want := []string{paths[1], paths[2]}
	sort.Strings(want)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("after two ctrl+clicks: got %v want %v", got, want)
	}

	// Ctrl+click row A again — removes it.
	m, _ = m.HandleMouse(2, rowAY, tea.MouseLeft, true, false)
	if m.IsSelected(paths[1]) {
		t.Fatalf("ctrl+click on A second time should remove it. selection=%v", m.Selection())
	}
	if !m.IsSelected(paths[2]) {
		t.Fatalf("ctrl+click on A should NOT remove B. selection=%v", m.Selection())
	}
}

func TestShiftClickRangeSelectsContiguous(t *testing.T) {
	m, paths := buildModelWithFiles(t, []string{"a.txt", "b.txt", "c.txt", "d.txt"})
	chrome := m.HeaderChromeRows()
	rowAY := chrome + 1 // a.txt
	rowDY := chrome + 4 // d.txt

	// Plain click on A sets cursor + anchor; single-select.
	var openMsg tea.Cmd
	m, openMsg = m.HandleMouse(2, rowAY, tea.MouseLeft, false, false)
	if openMsg == nil {
		t.Fatalf("plain click on a.txt should emit OpenFileMsg")
	}
	if got := m.Selection(); len(got) != 0 {
		t.Fatalf("plain click should clear multi-selection, got %v", got)
	}

	// Shift+click on D extends from anchor (A) through D inclusive.
	m, _ = m.HandleMouse(2, rowDY, tea.MouseLeft, false, true /*shift*/)
	got := m.Selection()
	sort.Strings(got)
	want := append([]string(nil), paths[1:5]...) // a..d
	sort.Strings(want)
	if len(got) != 4 {
		t.Fatalf("shift+click range len = %d, want 4 (got=%v)", len(got), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("shift+click range mismatch [%d]: got %s want %s", i, got[i], want[i])
		}
	}
}

func TestPlainClickClearsMultiSelection(t *testing.T) {
	m, paths := buildModelWithFiles(t, []string{"a.txt", "b.txt", "c.txt"})
	chrome := m.HeaderChromeRows()

	// Build up a 2-item multi-selection via Ctrl+click.
	m, _ = m.HandleMouse(2, chrome+1, tea.MouseLeft, true, false)
	m, _ = m.HandleMouse(2, chrome+2, tea.MouseLeft, true, false)
	if len(m.Selection()) != 2 {
		t.Fatalf("setup: expected 2 selected, got %v", m.Selection())
	}

	// Plain click on row C clears the multi-selection AND opens c.txt.
	var cmd tea.Cmd
	m, cmd = m.HandleMouse(2, chrome+3, tea.MouseLeft, false, false)
	if got := m.Selection(); len(got) != 0 {
		t.Fatalf("plain click did not clear selection: %v", got)
	}
	if cmd == nil {
		t.Fatalf("plain click on a file should emit OpenFileMsg cmd")
	}
	if msg := cmd(); msg == nil {
		t.Fatalf("OpenFileMsg cmd returned nil msg")
	} else if open, ok := msg.(OpenFileMsg); !ok {
		t.Fatalf("expected OpenFileMsg, got %T", msg)
	} else if open.Path != paths[3] {
		t.Fatalf("OpenFileMsg.Path = %s, want %s", open.Path, paths[3])
	}
}
