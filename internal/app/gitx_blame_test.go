package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
)

// gitxBlameRepo is covGitRepo with a.txt changed in two more commits:
// "second" changed line 2, "third" changed line 1. The editor shows
// a.txt with the cursor on line 2.
func gitxBlameRepo(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "one\nTWO\nthree\n")
	covCommit(t, dir, "second")
	covWrite(t, dir, "a.txt", "ONE\nTWO\nthree\n")
	covCommit(t, dir, "third")
	covRefresh(m)
	m.editor.SetMeta(filepath.Join(dir, "a.txt"), false, "text")
	m.cursorLine = 2
	return m, dir
}

func gitxExtHas(m *Model, text string) bool {
	for _, e := range m.statusState().Ext {
		if strings.Contains(e, text) {
			return true
		}
	}
	return false
}

// GITX-3: the status bar says who and when; the inline text adds the
// subject; an uncommitted line says so.
func TestGitxBlameTexts(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	defer clock.Set(func() time.Time { return now })()
	l := git.BlameRangeLine{Hash: "abc1234", Author: "Tess", When: now.Add(-72 * time.Hour).Unix(), Subject: "fix the parser"}
	if got := blameStatusText(l); got != "Tess, 3 days ago" {
		t.Errorf("status %q", got)
	}
	if got := blameInlineText(l); got != "Tess, 3 days ago · fix the parser" {
		t.Errorf("inline %q", got)
	}
	u := git.BlameRangeLine{Hash: "uncommitted"}
	if got := blameStatusText(u); got != "Not committed yet" {
		t.Errorf("uncommitted status %q", got)
	}
	if got := blameInlineText(u); got != "You · Not committed yet" {
		t.Errorf("uncommitted inline %q", got)
	}
}

// GITX-4 (D3, D4): annotations name a run of lines from one commit once,
// pad the rest to the same width, and colour by age: 0 newest, 4 oldest.
func TestGitxBlameAnnotateRows(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	defer clock.Set(func() time.Time { return now })()
	old := git.BlameRangeLine{Hash: "1111111", Full: "1111111aaaa", Author: "Ann Old", When: now.AddDate(-2, 0, 0).Unix()}
	mid := git.BlameRangeLine{Hash: "2222222", Full: "2222222bbbb", Author: "Bo", When: now.AddDate(0, -6, 0).Unix()}
	nw := git.BlameRangeLine{Hash: "3333333", Full: "3333333cccc", Author: "Cy", When: now.Add(-time.Hour).Unix()}
	rows := blameAnnotateRows([]git.BlameRangeLine{old, old, mid, nw, {Hash: "uncommitted"}})
	if len(rows) != 5 {
		t.Fatalf("rows %d", len(rows))
	}
	w := len([]rune(rows[0].Text))
	for i, r := range rows {
		if len([]rune(r.Text)) != w {
			t.Errorf("row %d width %d want %d: %q", i, len([]rune(r.Text)), w, r.Text)
		}
	}
	if !strings.HasPrefix(rows[0].Text, "1111111 Ann Old") || strings.TrimSpace(rows[1].Text) != "" {
		t.Errorf("run not named once: %q / %q", rows[0].Text, rows[1].Text)
	}
	if !strings.Contains(rows[4].Text, "not committed") {
		t.Errorf("uncommitted %q", rows[4].Text)
	}
	if rows[0].Age != 4 || rows[3].Age != 0 || rows[2].Age <= rows[3].Age || rows[2].Age >= rows[0].Age {
		t.Errorf("ages %d %d %d", rows[0].Age, rows[2].Age, rows[3].Age)
	}
	if rows[4].Age != 0 {
		t.Errorf("uncommitted age %d", rows[4].Age)
	}
}

// GITX-3 (D8): Toggle Blame in Status Bar loads the file's blame once and
// shows the cursor line's author; it is saved, and a second toggle hides
// it.
func TestGitxStatusBarBlame(t *testing.T) {
	m, _ := gitxBlameRepo(t)
	cmd := m.dispatchPaletteAction("git-status-blame")
	if cmd == nil {
		t.Fatal("no load")
	}
	m.onGitBlameLoaded(covMsg[gitBlameLoadedMsg](t, cmd))
	if !gitxExtHas(m, "Tess, ") {
		t.Fatalf("no blame in the status bar: %q", m.statusState().Ext)
	}
	if v := loadSettings().GitStatusBlame; v == nil || !*v {
		t.Error("not saved")
	}
	found := false
	for _, s := range m.statusSpans() {
		if s.id == "status:blame" {
			found = true
		}
	}
	if !found {
		t.Error("no clickable blame span")
	}
	m.dispatchPaletteAction("git-status-blame")
	if gitxExtHas(m, "Tess, ") {
		t.Error("still shown after the second toggle")
	}
}

// GITX-3 (D2): Toggle Inline Blame shows author, age and subject of the
// cursor line and follows the cursor.
func TestGitxInlineBlame(t *testing.T) {
	m, _ := gitxBlameRepo(t)
	m.onGitBlameLoaded(covMsg[gitBlameLoadedMsg](t, m.dispatchPaletteAction("git-inline-blame")))
	if !strings.Contains(m.gitBlame.inlineText, "· second") {
		t.Fatalf("inline %q", m.gitBlame.inlineText)
	}
	m.cursorLine = 1
	m.paintInlineBlame()
	if !strings.Contains(m.gitBlame.inlineText, "· third") {
		t.Errorf("inline did not follow the cursor: %q", m.gitBlame.inlineText)
	}
	m.dispatchPaletteAction("git-inline-blame")
	if m.gitBlame.inlineText != "" {
		t.Errorf("still painted: %q", m.gitBlame.inlineText)
	}
}

// GITX-3 (D11) / GITX-4 (D5): Open Commit of Line opens the line's
// commit; an uncommitted line says so.
func TestGitxOpenLineCommit(t *testing.T) {
	m, dir := gitxBlameRepo(t)
	want := covGit(t, dir, "rev-parse", "--short", "HEAD~1")
	msg := covMsg[gitCommitDiffReadyMsg](t, m.dispatchPaletteAction("git-blame-commit"))
	if !strings.HasPrefix(msg.name, want) {
		t.Errorf("opened %q want %s", msg.name, want)
	}
	covWrite(t, dir, "a.txt", "ONE\nnew\nthree\n")
	m.gitBlame.key = "" // the file changed on disk
	if cmd := m.dispatchPaletteAction("git-blame-commit"); cmd == nil || !toastHas(m, "Not committed yet") {
		t.Error("no note for an uncommitted line")
	}
}

// GITX-4 (D3, D7): annotations load for the file; Blame Options… turns
// whitespace and move detection on and saves them.
func TestGitxBlameAnnotateAndOptions(t *testing.T) {
	m, _ := gitxBlameRepo(t)
	m.onGitBlameLoaded(covMsg[gitBlameLoadedMsg](t, m.dispatchPaletteAction("git-blame-annotate")))
	if !m.gitBlame.annotate || len(m.gitBlame.rows) != 3 {
		t.Fatalf("annotate %v rows %d", m.gitBlame.annotate, len(m.gitBlame.rows))
	}
	m.dispatchPaletteAction("git-blame-options")
	if !m.pickerOpen || m.pickerKind != pickerKindBlameOptions {
		t.Fatal("no options picker")
	}
	m.pickerOpen = false
	cmd := m.handlePickerSelect(picker.SelectMsg{ID: "blame-opt-ws"})
	if !m.gitBlame.ignoreWS {
		t.Fatal("whitespace option not on")
	}
	if v := loadSettings().GitBlameIgnoreWS; v == nil || !*v {
		t.Error("whitespace option not saved")
	}
	if cmd == nil {
		t.Error("no reload after the option changed")
	}
	m.handlePickerSelect(picker.SelectMsg{ID: "blame-opt-moves"})
	if !m.gitBlame.moves {
		t.Error("move detection not on")
	}
	m.dispatchPaletteAction("git-blame-annotate")
	if m.gitBlame.annotate || m.gitBlame.rows != nil {
		t.Error("annotations still on")
	}
}

// GITX-4 (D6): Blame Before This Change opens the file as it was before
// the line's commit, annotated, on the line's old number.
func TestGitxBlameBefore(t *testing.T) {
	m, dir := gitxBlameRepo(t)
	first := covGit(t, dir, "rev-parse", "--short", "HEAD~2")
	msg := covMsg[gitRevisionReadyMsg](t, m.dispatchPaletteAction("git-blame-before"))
	if msg.rev != first || msg.rel != "a.txt" || msg.line != 2 || !msg.annotate {
		t.Fatalf("rev %q rel %q line %d annotate %v", msg.rev, msg.rel, msg.line, msg.annotate)
	}
	if msg.text != "one\ntwo\nthree\n" {
		t.Errorf("text %q", msg.text)
	}
	// From the oldest revision there is nothing before.
	m.editor.SetMeta(gitRevBufName(first, "a.txt"), false, "text")
	m.gitBlame.key = ""
	if cmd := m.dispatchPaletteAction("git-blame-before"); cmd == nil || !toastHas(m, "first commit") {
		t.Error("no note at the first commit")
	}
}

// GITX-5 (D12): Show Line History lists the commits that changed the
// cursor line; Enter opens one like the log.
func TestGitxLineHistory(t *testing.T) {
	m, dir := gitxBlameRepo(t)
	m.dispatchPaletteAction("git-line-history")
	if !m.pickerOpen || m.pickerKind != pickerKindGitLog {
		t.Fatal("no picker")
	}
	second := covGit(t, dir, "rev-parse", "--short", "HEAD~1")
	first := covGit(t, dir, "rev-parse", "--short", "HEAD~2")
	v := ansi.Strip(m.picker.View())
	i, j := strings.Index(v, second+"  second"), strings.Index(v, first+"  first")
	if i < 0 || j < i || strings.Contains(v, "third") {
		t.Errorf("rows (want second, then first, no third):\n%s", v)
	}
	if cur, ok := m.picker.Current(); !ok || cur.ID != "commit-"+second {
		t.Errorf("current %+v", cur)
	}
}

// GITX-5 (D14): Previous / Next Revision step the file through its
// commits; past the newest one is the working file again.
func TestGitxRevisionStepping(t *testing.T) {
	m, dir := gitxBlameRepo(t)
	third := covGit(t, dir, "rev-parse", "--short", "HEAD")
	second := covGit(t, dir, "rev-parse", "--short", "HEAD~1")
	// The file is clean: its previous revision is the one before HEAD.
	msg := covMsg[gitRevisionReadyMsg](t, m.dispatchPaletteAction("git-rev-prev"))
	if msg.rev != second || msg.text != "one\nTWO\nthree\n" {
		t.Fatalf("prev %q %q", msg.rev, msg.text)
	}
	m.editor.SetMeta(gitRevBufName(second, "a.txt"), false, "text")
	msg = covMsg[gitRevisionReadyMsg](t, m.dispatchPaletteAction("git-rev-next"))
	if msg.rev != third {
		t.Errorf("next %q want %s", msg.rev, third)
	}
	m.editor.SetMeta(gitRevBufName(third, "a.txt"), false, "text")
	m.dispatchPaletteAction("git-rev-next")
	if !toastHas(m, "working file") {
		t.Error("no way back to the working file")
	}
	// A changed file steps back to HEAD's version first.
	m.editor.SetMeta(filepath.Join(dir, "a.txt"), false, "text")
	covWrite(t, dir, "a.txt", "changed\n")
	msg = covMsg[gitRevisionReadyMsg](t, m.dispatchPaletteAction("git-rev-prev"))
	if msg.rev != third {
		t.Errorf("prev of a changed file %q want %s", msg.rev, third)
	}
}

// GITX-5: revision buffer names round-trip.
func TestGitxRevBufName(t *testing.T) {
	rev, rel, ok := parseGitRevBufName(gitRevBufName("abc1234", "dir/a b.go"))
	if !ok || rev != "abc1234" || rel != "dir/a b.go" {
		t.Errorf("%q %q %v", rev, rel, ok)
	}
	if _, _, ok := parseGitRevBufName("/home/x/a.go"); ok {
		t.Error("a plain path parsed as a revision")
	}
}
