package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/git"
)

// SCN-18: the plan of a drag and drop move in the explorer.
func TestPlanExplorerMove(t *testing.T) {
	root := "/w"
	have := map[string]bool{"/w/notes/b.txt": true}
	exists := func(p string) bool { return have[p] }
	cases := []struct {
		name    string
		srcs    []string
		dest    string
		moves   []git.Move
		problem string
	}{
		{"file into folder", []string{"/w/a.txt"}, "/w/notes", []git.Move{{From: "/w/a.txt", To: "/w/notes/a.txt"}}, ""},
		{"already there", []string{"/w/notes/x.txt"}, "/w/notes", nil, ""},
		{"name taken", []string{"/w/b.txt"}, "/w/notes", nil, "b.txt already exists in notes"},
		{"folder onto itself", []string{"/w/notes"}, "/w/notes", nil, "A folder cannot move into itself"},
		{"folder into its child", []string{"/w/notes"}, "/w/notes/deep", nil, "A folder cannot move into itself"},
		{"workspace root", []string{"/w"}, "/w/notes", nil, "w is a workspace root"},
		{"folder and its file", []string{"/w/src", "/w/src/a.go"}, "/w/notes", nil, "Select a folder or its files, not both"},
		{"two of the same name", []string{"/w/x/a.txt", "/w/y/a.txt"}, "/w/notes", nil, "a.txt already exists in notes"},
		{"two files", []string{"/w/a.txt", "/w/c.txt"}, "/w/notes", []git.Move{{From: "/w/a.txt", To: "/w/notes/a.txt"}, {From: "/w/c.txt", To: "/w/notes/c.txt"}}, ""},
	}
	for _, c := range cases {
		moves, problem := planExplorerMove(c.srcs, c.dest, []string{root}, exists)
		if problem != c.problem {
			t.Errorf("%s: problem = %q, want %q", c.name, problem, c.problem)
		}
		if len(moves) != len(c.moves) {
			t.Errorf("%s: moves = %v, want %v", c.name, moves, c.moves)
			continue
		}
		for i := range moves {
			if moves[i] != c.moves[i] {
				t.Errorf("%s: move %d = %v, want %v", c.name, i, moves[i], c.moves[i])
			}
		}
	}
}

// dragTree is a workspace with a.txt and the folder notes, the app on it
// and the Files explorer shown.
func dragTree(t *testing.T) (Model, string) {
	t.Helper()
	withConfigDir(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	m := covNew(t)
	m.w, m.h = 120, 40
	m.showExp = true
	m.applyLayout()
	if r := m.explorer.Roots(); len(r) > 0 {
		dir = r[0] // the path the tree uses for the folder
	}
	return m, dir
}

// rowOf is the panel-local row where the explorer draws path.
func explorerRowOf(t *testing.T, m Model, path string) int {
	t.Helper()
	for y := 0; y < m.h; y++ {
		if n, ok := m.explorer.VisibleAt(y); ok && n.Path == path {
			return y
		}
	}
	t.Fatalf("%s is not in the tree", path)
	return 0
}

// mouseDrag presses on row y0, moves with the left button held (as a
// terminal reports it) to y1 and lets go there.
func mouseDrag(m Model, y0, y1 int) Model {
	x := activity.Width + 4
	m, _ = m.handleMouse(tea.MouseMsg{X: x, Y: y0, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m, _ = m.handleMouse(tea.MouseMsg{X: x, Y: y1, Type: tea.MouseLeft, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m, _ = m.handleMouse(tea.MouseMsg{X: x, Y: y1, Type: tea.MouseRelease, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	return m
}

// SCN-18: dropping a file on a folder asks first. Cancel has the focus
// and Move is not red; nothing moves until Move is picked.
func TestExplorerDragAsksThenMoves(t *testing.T) {
	m, dir := dragTree(t)
	a, notes := filepath.Join(dir, "a.txt"), filepath.Join(dir, "notes")
	m = mouseDrag(m, explorerRowOf(t, m, a), explorerRowOf(t, m, notes))
	if !m.confirmOpen || m.confirmKind != confirmKindExplorerMove {
		t.Fatalf("no move card: open=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	checkCardRule(t, "explorer move", m, false)
	var ids []string
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	if strings.Join(ids, ",") != "cancel,move" {
		t.Errorf("buttons = %v, want cancel,move", ids)
	}
	if m.explorer.DropTarget() != "" {
		t.Errorf("drop target %q still lit after the drop", m.explorer.DropTarget())
	}
	if _, err := os.Stat(a); err != nil {
		t.Fatalf("a.txt moved before the card was answered: %v", err)
	}
	m.confirmOpen = false
	m.handleConfirmSelect("move")
	if b, err := os.ReadFile(filepath.Join(notes, "a.txt")); err != nil || string(b) != "alpha\n" {
		t.Fatalf("notes/a.txt = %q, %v", b, err)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Errorf("a.txt is still at the top: %v", err)
	}
}

// SCN-18: Cancel on the move card leaves the files where they are.
func TestExplorerDragCancelKeepsFiles(t *testing.T) {
	m, dir := dragTree(t)
	a, notes := filepath.Join(dir, "a.txt"), filepath.Join(dir, "notes")
	m = mouseDrag(m, explorerRowOf(t, m, a), explorerRowOf(t, m, notes))
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if _, err := os.Stat(a); err != nil {
		t.Errorf("a.txt is gone after Cancel: %v", err)
	}
	if _, err := os.Stat(filepath.Join(notes, "a.txt")); !os.IsNotExist(err) {
		t.Errorf("notes/a.txt made after Cancel: %v", err)
	}
}

// SCN-18: a move never replaces a file that is there when Move is picked.
func TestExplorerMoveRefusesATakenName(t *testing.T) {
	m, dir := dragTree(t)
	a, notes := filepath.Join(dir, "a.txt"), filepath.Join(dir, "notes")
	m = mouseDrag(m, explorerRowOf(t, m, a), explorerRowOf(t, m, notes))
	if err := os.WriteFile(filepath.Join(notes, "a.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.confirmOpen = false
	m.handleConfirmSelect("move")
	if b, _ := os.ReadFile(filepath.Join(notes, "a.txt")); string(b) != "keep\n" {
		t.Errorf("notes/a.txt = %q, the move replaced it", b)
	}
	if _, err := os.Stat(a); err != nil {
		t.Errorf("a.txt is gone: %v", err)
	}
	if !strings.Contains(m.err, "already exists") {
		t.Errorf("m.err = %q, want it to say the name is taken", m.err)
	}
}

// SCN-18: a drop on the folder the file is already in, or on its own
// row, does nothing and opens no card.
func TestExplorerDropInPlaceDoesNothing(t *testing.T) {
	m, dir := dragTree(t)
	a := filepath.Join(dir, "a.txt")
	m = mouseDrag(m, explorerRowOf(t, m, a), explorerRowOf(t, m, dir))
	if m.confirmOpen {
		t.Errorf("a card opened for a drop into the same folder")
	}
	if _, err := os.Stat(a); err != nil {
		t.Errorf("a.txt moved: %v", err)
	}
}

// SCN-18: a terminal reports a drag as left-button events; they must not
// act like more clicks (each one toggled the folder under the pointer).
func TestExplorerDragMotionIsNotAClick(t *testing.T) {
	m, dir := dragTree(t)
	notes := filepath.Join(dir, "notes")
	y := explorerRowOf(t, m, notes)
	x := activity.Width + 4
	m, _ = m.handleMouse(tea.MouseMsg{X: x, Y: y, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	open := strings.Join(m.explorer.ExpandedPaths(), "\n")
	for i := 0; i < 3; i++ {
		m, _ = m.handleMouse(tea.MouseMsg{X: x + i, Y: y, Type: tea.MouseLeft, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	}
	m, _ = m.handleMouse(tea.MouseMsg{X: x, Y: y, Type: tea.MouseRelease, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if got := strings.Join(m.explorer.ExpandedPaths(), "\n"); got != open {
		t.Errorf("motion toggled folders: before %q, after %q", open, got)
	}
	if m.confirmOpen {
		t.Errorf("a card opened for a press that did not leave its row")
	}
}
