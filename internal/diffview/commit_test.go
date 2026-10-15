package diffview

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// commitHeader is a sample commit for the commit-mode tests.
func commitHeader() Header {
	return Header{
		Hash: "a1b2c3d", FullHash: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678",
		Subject: "Fix flaky explorer test on slow CI",
		Body:    "The test waited 100 ms for the tree to load.\nOn a loaded CI box this is too short.",
		Author:  "Amin Jalali", DateRel: "2 hours ago", DateAbs: "2026-10-15 14:32",
		Parents: []string{"f31bb77"},
		Refs:    []Ref{{Name: "main", Kind: RefLocal}, {Name: "origin/main", Kind: RefRemote}, {Name: "v1.2", Kind: RefTag}},
		Files:   3, Add: 15, Del: 4,
		OldLabel: "− f31bb77 (parent)", NewLabel: "+ a1b2c3d (this commit)",
	}
}

func commitFiles() []File {
	return []File{
		{Name: "explorer_test.go", Status: 'M', Add: 2, Del: 2, Old: oldText, New: newText},
		{Name: "load_wait.go", Status: 'A', Add: 13, Pending: true},
		{Name: "logo.png", Status: 'M', Binary: true},
	}
}

func commitButtons() []Button {
	return []Button{
		{ID: "copy-sha", Label: "Copy SHA"},
		{ID: "open-file", Label: "Open file"},
		{ID: "open-at-commit", Label: "File @ commit"},
		{ID: "revert", Label: "Revert…"},
		{ID: "cherry-pick", Label: "Cherry-pick…"},
		{ID: "hide", Label: "Close"},
	}
}

func commitCard(w, h int) Model {
	m := NewCommit(commitHeader(), commitFiles(), commitButtons())
	m.SetSize(w, h)
	return m
}

// Spec (commit view §3.2): the header shows the short SHA, the first
// branch, the subject, the author (never an e-mail), both dates, the
// parent, every ref and the commit's totals; the side labels name the
// parent and this commit.
func TestCommitHeaderContent(t *testing.T) {
	s := plain(commitCard(150, 42))
	for _, want := range []string{"Commit a1b2c3d", "main", "Fix flaky explorer test on slow CI",
		"The test waited 100 ms", "Amin Jalali", "2 hours ago", "2026-10-15 14:32", "parent f31bb77",
		"origin/main", "v1.2", "3 files", "+15", "−4", "− f31bb77 (parent)", "+ a1b2c3d (this commit)", " Raw "} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	for i, l := range strings.Split(s, "\n") {
		if w := ansi.StringWidth(l); w != 146 {
			t.Errorf("row %d is %d cells wide, want 146: %q", i, w, l)
		}
	}
}

// headerRows counts the rows between the title and the side labels.
func headerRows(t *testing.T, m Model) int {
	t.Helper()
	rows := strings.Split(plain(m), "\n")
	title, labels := -1, -1
	for i, l := range rows {
		if title < 0 && strings.Contains(l, "Commit a1b2c3d") {
			title = i
		}
		if labels < 0 && strings.Contains(l, "(this commit)") {
			labels = i
		}
	}
	if title < 0 || labels < 0 {
		t.Fatalf("no title or labels row:\n%s", plain(m))
	}
	return labels - title - 1
}

// Spec (§4.3): the header takes subject + body (up to 3 lines) + meta on
// tall screens, subject + meta below 30 rows; b folds and opens the body.
func TestCommitHeaderRows(t *testing.T) {
	m := commitCard(150, 42)
	if n := headerRows(t, m); n != 4 { // subject, 2 body lines, meta
		t.Errorf("150x42: %d header rows, want 4:\n%s", n, plain(m))
	}
	m, _ = key(m, "b")
	if n := headerRows(t, m); n != 2 {
		t.Errorf("body folded: %d header rows, want 2:\n%s", n, plain(m))
	}
	if !strings.Contains(plain(m), "2 lines") {
		t.Errorf("folded body gives no hint:\n%s", plain(m))
	}
	if n := headerRows(t, commitCard(100, 26)); n != 2 {
		t.Errorf("100x26: %d header rows, want 2", n)
	}
}

// Spec (§3.1): SetBounds docks the card at a rectangle (the editor pane);
// zero bounds centre it again.
func TestBoundsDock(t *testing.T) {
	m := commitCard(150, 42)
	m.SetBounds(34, 1, 116, 39)
	if x, y := m.Origin(); x != 34 || y != 1 {
		t.Errorf("origin %d,%d want 34,1", x, y)
	}
	rows := strings.Split(plain(m), "\n")
	if len(rows) != 39 || ansi.StringWidth(rows[0]) != 116 {
		t.Errorf("box %dx%d, want 116x39", ansi.StringWidth(rows[0]), len(rows))
	}
	if x, y, w, h := m.Rect(); x != 34 || y != 1 || w != 116 || h != 39 {
		t.Errorf("Rect %d,%d %dx%d", x, y, w, h)
	}
	if _, _, in := m.HandleMouse(10, 10, tea.MouseActionPress, tea.MouseButtonLeft); in {
		t.Error("a click left of a docked card landed on it")
	}
	m.SetBounds(0, 0, 0, 0)
	if x, _ := m.Origin(); x != 2 {
		t.Errorf("undocked origin x %d, want 2", x)
	}
}

// Spec (§3.3): selecting a file whose text is not loaded shows Loading…
// and asks the app for it; SetFileText then shows its diff.
func TestPendingFileEmitsLoad(t *testing.T) {
	m := commitCard(150, 42)
	m, msg := key(m, "tab")
	if lm, ok := msg.(LoadFileMsg); !ok || lm.File != 1 {
		t.Fatalf("tab to a pending file: %#v", msg)
	}
	if !strings.Contains(plain(m), "Loading…") {
		t.Errorf("pending file does not say Loading…:\n%s", plain(m))
	}
	m.SetFileText(1, "", "package x\n\nfunc wait() {}\n")
	s := plain(m)
	if strings.Contains(s, "Loading…") || !strings.Contains(s, "func wait() {}") {
		t.Errorf("after SetFileText:\n%s", s)
	}
	// A loaded file does not ask again.
	m, _ = key(m, "tab")
	if _, msg := key(m, "tab"); msg != nil {
		if _, ok := msg.(LoadFileMsg); ok {
			t.Errorf("loaded file asked again: %#v", msg)
		}
	}
}

// Spec (§3.3): the file list shows each file's status letter and its
// numstat counts; a binary file says bin and its body says so.
func TestCommitFileListAndBinary(t *testing.T) {
	m := commitCard(150, 42)
	s := plain(m)
	for _, want := range []string{"M ", "explorer_test.go", "+2 −2", "A ", "load_wait.go", "+13 −0", "logo.png", "bin"} {
		if !strings.Contains(s, want) {
			t.Errorf("file list lacks %q:\n%s", want, s)
		}
	}
	m.Select(2)
	if !strings.Contains(plain(m), "Binary file") {
		t.Errorf("binary body:\n%s", plain(m))
	}
	m.SetFileNote(0, "Too big to diff here · r raw")
	m.Select(0)
	if !strings.Contains(plain(m), "Too big to diff here") {
		t.Errorf("note not shown:\n%s", plain(m))
	}
}

// Spec (§3.5): every footer button and the title controls are click
// targets; ids the card does not own come back as ActionMsg with the
// current file.
func TestFooterButtonsAndHits(t *testing.T) {
	m := commitCard(150, 42)
	for label, id := range map[string]string{
		" Copy SHA ": "copy-sha", " Open file ": "open-file", " File @ commit ": "open-at-commit",
		" Revert… ": "revert", " Cherry-pick… ": "cherry-pick", " Raw ": "raw", "a1b2c3d": "copy-sha",
		"parent f31bb77": "parent:f31bb77",
	} {
		x, y := findText(t, m, label)
		if got := m.ButtonAt(x, y); got != id {
			t.Errorf("%q: button %q, want %q", label, got, id)
			continue
		}
		if id == "copy-sha" && label == "a1b2c3d" {
			continue
		}
		_, msg := click(m, x, y)
		if am, ok := msg.(ActionMsg); !ok || am.ID != id || am.File != 0 {
			t.Errorf("%q: click gave %#v", label, msg)
		}
	}
	x, y := findText(t, m, " Close ")
	if _, msg := click(m, x, y); msg != (HideMsg{}) {
		t.Errorf("Close: %#v", msg)
	}
	// Narrow: the middle buttons fold into ⋯ (ActionMsg "more"), Close stays.
	n := commitCard(100, 30)
	s := plain(n)
	if !strings.Contains(s, " Close ") || !strings.Contains(s, " Copy SHA ") || strings.Contains(s, "Cherry-pick…") {
		t.Errorf("100 wide footer:\n%s", s)
	}
	x, y = findLast(t, n, " "+icons().fold+" ")
	if _, msg := click(n, x, y); msg == nil || msg.(ActionMsg).ID != "more" {
		t.Errorf("⋯: %#v", msg)
	}
	if got := n.FoldedButtons(); len(got) == 0 || got[len(got)-1].ID != "cherry-pick" {
		t.Errorf("folded buttons %v", got)
	}
}

// findLast is findText from the bottom row up (the footer).
func findLast(t *testing.T, m Model, s string) (int, int) {
	t.Helper()
	x0, y0 := m.Origin()
	rows := strings.Split(plain(m), "\n")
	for y := len(rows) - 1; y >= 0; y-- {
		if i := strings.Index(rows[y], s); i >= 0 {
			return x0 + ansi.StringWidth(rows[y][:i]) + 1, y0 + y
		}
	}
	t.Fatalf("%q not on the card:\n%s", s, plain(m))
	return 0, 0
}

// Spec (§3.5): in commit mode r is Raw (not reject), y copies, o opens,
// O opens at the commit, v reverts, c cherry-picks; t still toggles split.
func TestCommitKeys(t *testing.T) {
	for k, id := range map[string]string{"r": "raw", "y": "copy-sha", "o": "open-file", "O": "open-at-commit", "v": "revert", "c": "cherry-pick"} {
		_, msg := key(commitCard(150, 42), k)
		if am, ok := msg.(ActionMsg); !ok || am.ID != id {
			t.Errorf("%s: %#v, want ActionMsg %q", k, msg, id)
		}
	}
	m := commitCard(150, 42)
	split := m.splitNow()
	m, _ = key(m, "t")
	if m.splitNow() == split {
		t.Error("t did not toggle the layout")
	}
	if _, msg := key(commitCard(150, 42), "esc"); msg != (HideMsg{}) {
		t.Errorf("esc: %#v", msg)
	}
}

// Spec (§3.7): at 80x24 the card has no file list; the footer says which
// file of how many, and Tab still steps.
func TestNoFileListAt80x24(t *testing.T) {
	m := commitCard(80, 24)
	s := plain(m)
	if strings.Contains(s, "FILES") {
		t.Errorf("file list at 80x24:\n%s", s)
	}
	if !strings.Contains(s, "file 1 of 3") {
		t.Errorf("no file position:\n%s", s)
	}
	for i, l := range strings.Split(s, "\n") {
		if w := ansi.StringWidth(l); w != 76 {
			t.Errorf("row %d is %d wide", i, w)
		}
	}
}

// Spec: a commit that changes no file still shows its header.
func TestCommitWithoutFiles(t *testing.T) {
	m := NewCommit(commitHeader(), nil, commitButtons())
	m.SetSize(120, 30)
	if s := plain(m); !strings.Contains(s, "Fix flaky explorer test") || !strings.Contains(s, "No file changes") {
		t.Errorf("empty commit:\n%s", s)
	}
}

// Spec: review mode keeps r as reject (commit mode must not leak).
func TestReviewKeepsReject(t *testing.T) {
	m := demo(true)
	m, _ = key(m, "r")
	if d := m.Decisions(0); d[0] != Rejected {
		t.Errorf("r in review: %v", d)
	}
}
