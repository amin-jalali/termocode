package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/worktreeform"
)

func wtIssueAgents(t *testing.T, n int) {
	t.Helper()
	old := wtNewAgents
	wtNewAgents = func() []agents.Agent { return agents.Registry[:n] }
	t.Cleanup(func() { wtNewAgents = old })
}

// CMP-32: a picked issue opens Worktree: New… filled in: the issue is
// linked, the branch comes from its title, the first agent is picked
// and the task names the issue. Cancel stays the first button; nothing
// is made until Create.
func TestWTFromIssuePrefill(t *testing.T) {
	withConfigDir(t)
	wtIssueAgents(t, 1)
	title := "Login crashes\x1b[31m on empty password!"
	f := wtDemoForm()
	wtPrefillIssue(f, gitxIssue{Number: 12, Title: title})
	m := &Model{w: 150, h: 42, wt: newWTState()}
	m.onWTNewPrep(wtNewPrepMsg{f: f})
	if !m.wtFormOpen() {
		t.Fatal("the card did not open")
	}
	if got := m.connect.InputValue("issue"); got != "#12" {
		t.Errorf("issue %q", got)
	}
	task := m.connect.InputValue("task")
	if !strings.Contains(task, "#12") || !strings.Contains(task, "Login crashes on empty password!") || strings.ContainsRune(task, '\x1b') {
		t.Errorf("task %q", task)
	}
	if m.connect.Choice("agent") != 1 {
		t.Errorf("agent %d", m.connect.Choice("agent"))
	}
	if b := m.connect.Card().Buttons; len(b) == 0 || b[0].ID != "cancel" {
		t.Errorf("buttons %v", b)
	}
	want := worktreeform.BranchFromIssue(12, "Login crashes on empty password!")
	if br, _ := f.draft(m.wtFormValues()); br != want {
		t.Errorf("branch %q, want %q", br, want)
	}
	if !strings.Contains(ansi.Strip(m.connect.Box()), want) {
		t.Errorf("the card does not show the branch:\n%s", ansi.Strip(m.connect.Box()))
	}
	if m.wt.form.vals != nil {
		t.Error("the prefill stays after the card opened")
	}
}

// No installed agent: the Agent row stays on none.
func TestWTFromIssueNoAgent(t *testing.T) {
	withConfigDir(t)
	wtIssueAgents(t, 0)
	f := wtDemoForm()
	wtPrefillIssue(f, gitxIssue{Number: 3, Title: "Dark mode"})
	m := &Model{w: 150, h: 42, wt: newWTState()}
	m.onWTNewPrep(wtNewPrepMsg{f: f})
	if !m.wtFormOpen() || m.connect.Choice("agent") != 0 {
		t.Fatalf("open %v agent %d", m.wtFormOpen(), m.connect.Choice("agent"))
	}
}

// The task is one line, without control bytes, and short.
func TestWTIssueTask(t *testing.T) {
	task := wtIssueTask(gitxIssue{Number: 7, Title: "a\nb\tc\x07" + strings.Repeat("x", 500)})
	if strings.ContainsAny(task, "\n\t\x07") || len(task) > 400 {
		t.Errorf("task %q", task)
	}
	if !strings.HasPrefix(task, "Work on issue #7: a b c") || !strings.Contains(task, "gh issue view 7") {
		t.Errorf("task %q", task)
	}
}

// Worktree: Start from Issue… lists the open issues; a pick reads the
// repository and fills the New worktree form. Git: Issues… has the same
// row. The AI never runs it.
func TestWTFromIssuePalette(t *testing.T) {
	m, dir := covGitRepo(t)
	m.wt = newWTState()
	f := &prFakeGH{answers: map[string]string{"issue list": gitxIssuesJSON}}
	m.ciW().run = f.run
	covMsg[gitxDoneMsg](t, m.dispatchPaletteAction(wtFromIssuePaletteID)).apply(m)
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "issue-12,issue-9" {
		t.Fatalf("issues %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	cmd := m.handlePickerSelect(picker.SelectMsg{ID: "issue-12"})
	if cmd == nil {
		t.Fatal("no form for the issue")
	}
	prep, ok := cmd().(wtNewPrepMsg)
	if !ok || prep.err != nil || prep.f.issueNum != 12 || prep.f.root != dir && !strings.HasSuffix(prep.f.root, dir) {
		t.Fatalf("prep %+v", prep)
	}
	if prep.f.vals["issue"] != "#12" {
		t.Errorf("vals %v", prep.f.vals)
	}
	// From Git: Issues…, the action row opens the same form.
	covMsg[gitxDoneMsg](t, m.dispatchPaletteAction("git-issues")).apply(m)
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "issue-9"})
	m.pickerOpen = false
	cmd = m.handlePickerSelect(picker.SelectMsg{ID: "worktree"})
	if cmd == nil {
		t.Fatal("no form from Git: Issues…")
	}
	if prep, ok := cmd().(wtNewPrepMsg); !ok || prep.f == nil || prep.f.issueNum != 9 {
		t.Fatalf("prep %+v", prep)
	}
	if p := aiActionPolicies[wtFromIssuePaletteID]; p.Gate != aiGateNever {
		t.Errorf("policy %+v", p)
	}
}

// I in the Worktrees view starts from an issue.
func TestWTFromIssueKey(t *testing.T) {
	m, _ := covGitRepo(t)
	m.wt = newWTState()
	f := &prFakeGH{answers: map[string]string{"issue list": gitxIssuesJSON}}
	m.ciW().run = f.run
	_, cmd := m.handleWTSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	covMsg[gitxDoneMsg](t, cmd).apply(m)
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "issue-12,issue-9" {
		t.Fatalf("issues %v", gitxPickerIDs(m))
	}
	found := false
	for _, r := range worktreeKeyRows() {
		found = found || r.Key == "I" && strings.Contains(r.What, "start from an issue")
	}
	if !found {
		t.Error("I is not on the key card")
	}
}
