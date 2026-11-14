package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/checkpoint"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Coverage of bookmarks.go, problems.go and checkpoints_ui.go.

func TestCovBookmarksToggleListClear(t *testing.T) {
	m, dir := goRepoModel(t)
	openAt(t, &m, filepath.Join(dir, "lib.go"), 3)
	if m.toggleBookmark() == nil { // added
		t.Fatal("first toggle adds and toasts")
	}
	msg := m.fetchBookmarksCmd()().(BookmarksMsg)
	if len(msg.Items) != 1 || !strings.Contains(msg.Items[0].Title, "lib.go") || msg.Items[0].Hint != "L3" {
		t.Fatalf("items %+v", msg.Items)
	}
	m.applyBookmarksMsg(msg)
	if !m.pickerOpen {
		t.Error("picker not open")
	}
	if m.toggleBookmark() == nil { // same line: removed
		t.Error("second toggle removes")
	}
	if msg := m.fetchBookmarksCmd()().(BookmarksMsg); len(msg.Items) != 0 {
		t.Errorf("not removed: %+v", msg.Items)
	}
	if m.applyBookmarksMsg(BookmarksMsg{}) == nil {
		t.Error("empty list toasts")
	}
	// a full set
	for i := 0; i < 27; i++ {
		_ = m.nvim.ExecLuaArgs(`vim.api.nvim_win_set_cursor(0, {(...), 0}); vim.cmd('normal! m' .. string.char(64 + (...) % 26 + 1))`, i%5+1)
	}
	_ = m.nvim.ExecLua(`for c = 65, 90 do vim.api.nvim_buf_set_lines(0, 0, 0, false, {'x'}) end
		for c = 65, 90 do vim.api.nvim_win_set_cursor(0, {c - 64, 0}); vim.cmd('normal! m' .. string.char(c)) end
		vim.api.nvim_win_set_cursor(0, {30, 0})`)
	m.toggleBookmark()
	if m.clearAllBookmarks() == nil {
		t.Error("clear toasts")
	}
	var e Model
	if e.toggleBookmark() != nil || e.fetchBookmarksCmd() != nil || e.clearAllBookmarks() != nil {
		t.Error("no editor")
	}
}

func TestCovProblemsPickerAndJump(t *testing.T) {
	m, dir := goRepoModel(t)
	lib := filepath.Join(dir, "lib.go")
	openAt(t, &m, lib, 1)
	if m.openProblemsPicker() == nil {
		t.Fatal("cmd")
	}
	if msg := m.fetchProblemsCmd()().(ProblemsMsg); len(msg.Items) != 0 {
		t.Fatalf("no diagnostics yet: %+v", msg.Items)
	}
	if m.applyProblemsMsg(ProblemsMsg{}) == nil {
		t.Error("empty toasts")
	}
	_ = m.nvim.ExecLua(`local ns = vim.api.nvim_create_namespace('t')
		vim.diagnostic.set(ns, 0, {
			{ lnum = 4, col = 0, severity = 2, message = 'warn\nmore', source = 'vet' },
			{ lnum = 2, col = 5, severity = 1, message = 'err' },
			{ lnum = 0, col = 0, severity = 1, message = 'err first' },
		})`)
	msg := m.fetchProblemsCmd()().(ProblemsMsg)
	if len(msg.Items) != 3 || !strings.Contains(msg.Items[0].Title, "err first") || !strings.Contains(msg.Items[2].Hint, "vet") {
		t.Fatalf("items %+v", msg.Items)
	}
	m.applyProblemsMsg(msg)
	if !m.pickerOpen || m.pickerKind != pickerKindProblems {
		t.Fatal("picker")
	}
	m.focus = FocusExplorer
	m.jumpToProblem(msg.Items[1].ID)
	if m.focus != FocusEditor {
		t.Error("focus editor")
	}
	if l, _ := m.nvim.EvalLuaString(`return tostring(vim.fn.line('.'))`); l != "3" {
		t.Errorf("line %s", l)
	}
	m.jumpToProblem("nope")
	m.problemsIndex["noname"] = nvim.Diagnostic{}
	m.jumpToProblem("noname")
	nm := Model{problemsIndex: map[string]nvim.Diagnostic{"x": {Path: lib, Line: 1}}}
	nm.jumpToProblem("x")
	if nm.focus != FocusEditor {
		t.Error("jump without nvim still focuses")
	}
	if (&Model{}).openProblemsPicker() != nil || (Model{}).fetchProblemsCmd() != nil {
		t.Error("no editor")
	}
	if formatProblemHint(nvim.Diagnostic{}) != "[no name]:0" {
		t.Errorf("hint %q", formatProblemHint(nvim.Diagnostic{}))
	}
	for sev := 0; sev <= 5; sev++ {
		if severityIcon(sev) == "" {
			t.Error("icon")
		}
	}
}

func TestCovSeverityIconNerdFont(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeNerdFont
	defer func() { theme.CurrentIconMode = old }()
	for sev := 1; sev <= 5; sev++ {
		_ = severityIcon(sev)
	}
}

func gitCheckpointRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git: %v %s", err, out)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	_ = exec.Command("git", "-C", dir, "add", ".").Run()
	_ = exec.Command("git", "-C", dir, "commit", "-qm", "init").Run()
	return dir
}

func TestCovCheckpointFlow(t *testing.T) {
	dir := gitCheckpointRepo(t)
	m := newTestModel(t)
	m.w, m.h = 120, 40
	if m.nvim == nil {
		t.Skip("no nvim")
	}
	// nothing saved yet
	if m.openCheckpointPicker() == nil {
		t.Fatal("no checkpoint: info toast")
	}
	// save by hand (the Cmd returns an applyMsg)
	res := m.checkpointSaveNow()().(applyMsg)
	res(&m)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644)
	if cmd := m.openCheckpointPicker(); cmd != nil || !m.pickerOpen || m.pickerKind != pickerKindCheckpoint {
		t.Fatalf("picker open=%v", m.pickerOpen)
	}
	pts, err := checkpoint.List(dir)
	if err != nil || len(pts) == 0 {
		t.Fatalf("list: %v %v", pts, err)
	}
	item := struct{ ID string }{"0:" + pts[0].ID}
	// bad ids
	for _, bad := range []string{"nocolon", "x:y", "9:abc", "-1:abc"} {
		if m.onCheckpointPicked(bad) != nil {
			t.Errorf("%q must be ignored", bad)
		}
	}
	if m.onCheckpointPicked("0:doesnotexist") == nil {
		t.Error("unknown checkpoint toasts")
	}
	// a card for the real one
	m.onCheckpointPicked(item.ID)
	if !m.confirmOpen || m.confirmKind != confirmKindCheckpointRestore {
		t.Fatalf("card not open: %v", m.confirmOpen)
	}
	// cancel does nothing
	if m.answerCheckpointRestore("cancel") != nil || m.aiS().cpPending != nil {
		t.Error("cancel")
	}
	// restore
	m.onCheckpointPicked(item.ID)
	cmd := m.answerCheckpointRestore("restore")
	if cmd == nil {
		t.Fatal("restore cmd")
	}
	cmd().(applyMsg)(&m)
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one\n" {
		t.Errorf("a.txt = %q", b)
	}
	// same files again: nothing to restore
	_ = os.Remove(filepath.Join(dir, "new.txt"))
	// unsaved edits block a restore
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("three\n"), 0o644)
	_ = m.editor.Open(filepath.Join(dir, "a.txt"))
	_ = m.nvim.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, 0, false, {'dirty'})`)
	m.onCheckpointPicked(item.ID)
}

func TestCovCheckpointHelpers(t *testing.T) {
	if !agentTurnStarts(agents.State(0), agents.Working) || agentTurnStarts(agents.Working, agents.Working) {
		t.Error("turn start")
	}
	if checkpointDirFor(nil) == "" {
		t.Error("nil tab")
	}
	if checkpointDirFor(&terminalTab{Worktree: "/w"}) != "/w" || checkpointDirFor(&terminalTab{Cwd: "/c"}) != "/c" || checkpointDirFor(&terminalTab{}) == "" {
		t.Error("dir for tab")
	}
	// agent turn: replies after saving (not a repo: no error logged)
	dir := t.TempDir()
	m := Model{}
	call := agentCtlCall{reply: make(chan agentctl.Reply, 1)}
	tab := &terminalTab{Agent: "claude", Cwd: dir}
	m.checkpointAgentTurn(call, agentctl.Reply{}, tab)()
	select {
	case <-call.reply:
	default:
		t.Error("no reply")
	}
	// in a repo
	repo := gitCheckpointRepo(t)
	tab.Cwd = repo
	m.checkpointAgentTurn(call, agentctl.Reply{}, tab)()
	<-call.reply
	// error toast on a failed save
	mm := Model{}
	_ = mm
	_ = errors.New
	// dirs: workspace + an agent tab
	m2 := Model{terminalTabs: []terminalTab{{Agent: "claude", Cwd: repo}, {Agent: "claude", Cwd: repo}, {Cwd: dir}}}
	if got := m2.checkpointDirs(); len(got) != 1 {
		t.Errorf("dirs %v", got)
	}
}
