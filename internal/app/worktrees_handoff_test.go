package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtHandoffSeams swaps the git read, the note write and the agent start
// of a handoff; it returns what was written and started.
type wtHandoffRec struct {
	base, text, file string
	started          []string // "agent dir task"
}

func wtHandoffSeams(t *testing.T) *wtHandoffRec {
	t.Helper()
	rec := &wtHandoffRec{file: "/src/app/.git/termocode/handoff/toast-1.md"}
	oldG, oldW, oldS, oldA := wtHandoffGather, wtHandoffWrite, wtHandoffStart, wtNewAgents
	wtHandoffGather = func(e worktrees.Entry, base string) (worktrees.Handoff, error) {
		rec.base = base
		return worktrees.Handoff{Title: e.Title(), Branch: e.Branch, Base: base, Path: e.Path,
			Commits: []string{"fix the race"}, Changed: []string{"toast.go"}}, nil
	}
	wtHandoffWrite = func(dir, text string) (string, error) {
		rec.text = text
		return rec.file, nil
	}
	wtHandoffStart = func(m *Model, a agents.Agent, dir, task string) tea.Cmd {
		rec.started = append(rec.started, a.ID+" "+dir+" "+task)
		return nil
	}
	wtNewAgents = func() []agents.Agent {
		return []agents.Agent{{ID: "claude", Title: "Claude Code", Bin: "claude"}, {ID: "codex", Title: "Codex", Bin: "codex"}}
	}
	t.Cleanup(func() { wtHandoffGather, wtHandoffWrite, wtHandoffStart, wtNewAgents = oldG, oldW, oldS, oldA })
	return rec
}

// CMP-11: H opens the handoff card (Cancel first) with a preview of the
// note; Hand off writes the note, starts the picked agent in the same
// worktree with a one-line task that names the note, and leaves a note
// on the worktree.
func TestWorktreeHandoffSameWorktree(t *testing.T) {
	rec := wtHandoffSeams(t)
	m := wtLinkModel(t)
	wtLinkDrive(t, m, wtKey(t, m, "H"))
	if !m.connectOpen || m.connect.Card().Title != wtHandoffTitle {
		t.Fatalf("no handoff card: open %v", m.connectOpen)
	}
	if b := m.connect.Card().Buttons; len(b) < 2 || b[0].ID != "cancel" {
		t.Fatalf("Cancel is not first: %+v", b)
	}
	box := ansi.Strip(m.connect.Box())
	for _, want := range []string{"fix/toast-race", "fix the race", "Claude Code"} {
		if !strings.Contains(box, want) {
			t.Errorf("card misses %q:\n%s", want, box)
		}
	}
	if rec.base != "main" {
		t.Errorf("base %q, want the default branch main", rec.base)
	}
	m.connect.SetInputValue("extra", "keep the old API")
	wtLinkDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "go"} })
	if m.connectOpen || m.wt.handoff != nil {
		t.Fatalf("card still open:\n%s", ansi.Strip(m.connect.Box()))
	}
	if !strings.Contains(rec.text, "keep the old API") || !strings.Contains(rec.text, "fix the race") {
		t.Errorf("note:\n%s", rec.text)
	}
	if len(rec.started) != 1 || !strings.HasPrefix(rec.started[0], "claude /src/app.worktrees/toast ") || !strings.Contains(rec.started[0], rec.file) {
		t.Fatalf("started %v", rec.started)
	}
	md, _ := worktrees.Load(m.wt.common)
	got := md["/src/app.worktrees/toast"]
	if last, ok := got.LastNote(); !ok || !strings.Contains(last.Text, "Handed off to Claude Code") || got.Agent != "claude" {
		t.Errorf("meta %+v", got)
	}
}

// Cancel writes and starts nothing; an agent of the same kind already in
// the worktree stays on the card as an error.
func TestWorktreeHandoffCancelAndBusyAgent(t *testing.T) {
	rec := wtHandoffSeams(t)
	m := wtLinkModel(t)
	wtLinkDrive(t, m, wtKey(t, m, "H"))
	wtLinkDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "cancel"} })
	if m.connectOpen || rec.text != "" || len(rec.started) != 0 {
		t.Fatalf("cancel did something: open %v text %q started %v", m.connectOpen, rec.text, rec.started)
	}
	m.terminalTabs = []terminalTab{{Agent: "claude", Worktree: "/src/app.worktrees/toast"}}
	wtLinkDrive(t, m, wtKey(t, m, "H"))
	wtLinkDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "go"} })
	if !m.connectOpen || !strings.Contains(ansi.Strip(m.connect.Box()), "already runs in this worktree") {
		t.Fatalf("no busy error:\n%s", ansi.Strip(m.connect.Box()))
	}
	if rec.text != "" || len(rec.started) != 0 {
		t.Errorf("handed off anyway: %q %v", rec.text, rec.started)
	}
}

// New child worktree: the note is written, the card closes, no agent
// starts yet; the New worktree form opens later with the agent picked
// and the task typed in.
func TestWorktreeHandoffToChild(t *testing.T) {
	rec := wtHandoffSeams(t)
	m := wtLinkModel(t)
	wtLinkDrive(t, m, wtKey(t, m, "H"))
	m.connect = connectcard.New(wtHandoffCardFor(m.wt.handoff, 1, 1, ""))
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	cmd, _ := m.handleWTMsg(connectcard.ButtonMsg{ID: "go"})
	if cmd == nil {
		t.Fatal("no write")
	}
	msg, ok := cmd().(wtHandoffWroteMsg)
	if !ok {
		t.Fatal("not a write result")
	}
	if next, _ := m.handleWTMsg(msg); next == nil {
		t.Error("the child form is not asked for")
	}
	if m.connectOpen || len(rec.started) != 0 || rec.text == "" {
		t.Fatalf("open %v started %v text %q", m.connectOpen, rec.started, rec.text)
	}
	// The form the prep opens: the agent and the task are filled in.
	f := &wtForm{root: "/src/app", from: "fix/toast-race", parent: "/src/app.worktrees/toast", parentTitle: "toast",
		preTask: worktrees.HandoffPrompt(rec.file, "toast"), preAgent: "codex"}
	m.onWTNewPrep(wtNewPrepMsg{f: f})
	if !m.wtFormOpen() {
		t.Fatal("no form")
	}
	if v := m.wtFormValues(); v.agent != 2 || !strings.Contains(v.task, rec.file) || v.from != "fix/toast-race" {
		t.Errorf("form values %+v", v)
	}
}

// The command is in the palette and the guide's keys; the AI never
// starts a handoff.
func TestWorktreeHandoffPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range worktreePaletteItems() {
		found = found || it.ID == wtHandoffPaletteID
	}
	if !found {
		t.Error("not in the palette")
	}
	if p, ok := aiActionPolicyFor(wtHandoffPaletteID); !ok || p.Gate == aiGateAuto {
		t.Errorf("the AI may hand off: %+v", p)
	}
	rows := ""
	for _, r := range worktreeKeyRows() {
		rows += r.Key + " " + r.What + "\n"
	}
	if !strings.Contains(rows, "hand off") {
		t.Errorf("no H row:\n%s", rows)
	}
}
