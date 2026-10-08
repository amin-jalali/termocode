package app

import (
	"os/exec"
	"strings"
	"testing"

	"termocode/internal/git"
	"termocode/internal/nvim"
	"termocode/internal/toast"
)

func TestBlendHexAndXterm(t *testing.T) {
	if got := blendHex("#ffffff", "#000000", 0.5); got != "#808080" {
		t.Errorf("blend 50%% = %s", got)
	}
	if got := blendHex("#ff0000", "#0000ff", 0); got != "#0000ff" {
		t.Errorf("alpha 0 should return bg, got %s", got)
	}
	if got := blendHex("color12", "#101010", 0.5); got != "#101010" {
		t.Errorf("invalid fg should return bg, got %s", got)
	}
	for n, want := range map[int][3]int{0: {0, 0, 0}, 15: {255, 255, 255}, 16: {0, 0, 0}, 231: {255, 255, 255}, 232: {8, 8, 8}, 196: {255, 0, 0}} {
		r, g, b := xterm256RGB(n)
		if [3]int{r, g, b} != want {
			t.Errorf("xterm256RGB(%d) = %d,%d,%d want %v", n, r, g, b, want)
		}
	}
	if h := hexOfToken(196); !strings.HasPrefix(h, "#") || len(h) != 7 {
		t.Errorf("hexOfToken must always be #rrggbb, got %q", h)
	}
}

func TestConflictHelpers(t *testing.T) {
	b := git.ConflictBlock{OursLabel: "HEAD", TheirsLabel: "feature"}
	o, th, both := conflictActionTitles(b)
	if o != "Accept Current Change (HEAD)" || th != "Accept Incoming Change (feature)" || both != "Accept Both Changes" {
		t.Errorf("titles = %q %q %q", o, th, both)
	}
	if conflictResolutionFor(synthKindConflictTheirs) != git.AcceptTheirs ||
		conflictResolutionFor(synthKindConflictBoth) != git.AcceptBoth ||
		conflictResolutionFor(synthKindConflictOurs) != git.AcceptOurs {
		t.Error("resolution mapping wrong")
	}
	if !strings.Contains(conflictsLeftDetail(0), "No conflicts left") || !strings.HasPrefix(conflictsLeftDetail(3), "3 conflicts") {
		t.Error("conflictsLeftDetail wording")
	}
	if g := codeActionGroup(nvim.CodeAction{Kind: synthKindConflictBoth, Title: "Accept Both Changes"}); g != "Quick Fix" {
		t.Errorf("conflict actions should group under Quick Fix, got %q", g)
	}
}

func TestGitPanelConflictsSection(t *testing.T) {
	m := Model{
		gitCollapsed: map[string]bool{},
		gitFiles: []git.FileStatus{
			{Code: "UU", Path: "both.go"},
			{Code: "M ", Path: "staged.go"},
			{Code: " M", Path: "changed.go"},
		},
	}
	rows := m.gitPanelRows()
	if rows[0].kind != gitRowSection || rows[0].section != gitSecConflicts {
		t.Fatalf("first row should be CONFLICTS header: %+v", rows[0])
	}
	if rows[1].kind != gitRowFile || rows[1].section != gitSecConflicts || m.gitFiles[rows[1].fileIndex].Path != "both.go" {
		t.Fatalf("row1 should be the conflicted file: %+v", rows[1])
	}
	// The UU file must not also appear under STAGED / CHANGES.
	for _, r := range rows[2:] {
		if r.kind == gitRowFile && m.gitFiles[r.fileIndex].Path == "both.go" {
			t.Errorf("conflicted file leaked into section %v", r.section)
		}
	}
	if staged, changed := m.gitFileCounts(); staged != 1 || changed != 1 {
		t.Errorf("counts = %d/%d, want 1/1", staged, changed)
	}
	if m.gitConflictCount() != 1 {
		t.Errorf("conflict count = %d", m.gitConflictCount())
	}
	m.gitToggleSection(gitSecConflicts)
	if rows := m.gitPanelRows(); rows[1].kind == gitRowFile {
		t.Error("collapsed CONFLICTS still shows its files")
	}
}

func TestCloneProgressText(t *testing.T) {
	if got := cloneProgressText(git.CloneProgress{Phase: "Receiving objects", Percent: 42}); got != "Receiving objects 42%" {
		t.Errorf("got %q", got)
	}
	if got := cloneProgressText(git.CloneProgress{Phase: "Cloning", Percent: -1}); got != "Cloning…" {
		t.Errorf("got %q", got)
	}
}

func TestWelcomeHasCloneCard(t *testing.T) {
	withConfigDir(t)
	idx := -1
	for i, a := range welcomeQuickActions() {
		if a.id == welcomeCardClone {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("no Clone repository card")
	}
	found := false
	for _, h := range welcomeQuickActionHits(120, 40) {
		if h.Index == idx {
			found = true
		}
	}
	if !found {
		t.Error("clone card has no click hit zone")
	}
	m := Model{toast: toast.New()}
	nm, _ := m.runWelcomeCard(idx)
	mm := nm.(Model)
	if !mm.promptOpen || mm.promptKind != promptKindCloneURL {
		t.Errorf("clone card should open the URL prompt (open=%v kind=%v)", mm.promptOpen, mm.promptKind)
	}
}

// TestConflictLuaInNvim runs the real Lua against a headless nvim: paint
// places extmarks, ]x-style jumping finds the block, and a resolution is one
// undo step.
func TestConflictLuaInNvim(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.ExecLua(conflictLua()); err != nil {
		t.Fatalf("conflictLua: %v", err)
	}
	text := "top\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feat\nend"
	if err := c.ExecLuaArgs(`vim.api.nvim_buf_set_lines(0, 0, -1, false, ...)`, strings.Split(text, "\n")); err != nil {
		t.Fatal(err)
	}
	n, err := c.EvalLuaString(`
		_G.termocode_conflict_paint()
		local ns = vim.api.nvim_create_namespace('TermocodeConflicts')
		return tostring(#vim.api.nvim_buf_get_extmarks(0, ns, 0, -1, {}))`)
	if err != nil || n != "5" {
		t.Fatalf("extmarks = %q (%v), want 5", n, err)
	}
	if ln, _ := c.EvalLuaString(`vim.api.nvim_win_set_cursor(0, {1, 0}); return _G.termocode_conflict_jump(1)`); ln != "2" {
		t.Errorf("jump = %q, want 2", ln)
	}
	lines, _ := c.CurrentBufLines()
	b, ok := git.ConflictAt(git.ParseConflictLines(lines), 2)
	if !ok {
		t.Fatal("no block at line 3")
	}
	// Close the undo block of the setup edit so undo only reverts the resolve.
	if err := c.ExecLua(`vim.cmd("let &undolevels = &undolevels")`); err != nil {
		t.Fatal(err)
	}
	if err := c.ReplaceCurrentBufLines(b.Start, b.End+1, b.Replacement(git.AcceptBoth)); err != nil {
		t.Fatal(err)
	}
	got, _ := c.CurrentBufLines()
	if strings.Join(got, "|") != "top|ours|theirs|end" {
		t.Fatalf("resolved = %q", got)
	}
	_ = c.Command("undo")
	got, _ = c.CurrentBufLines()
	if strings.Join(got, "\n") != text {
		t.Errorf("one undo should restore the markers, got %q", got)
	}
}
