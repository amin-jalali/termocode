package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Hand off a worktree to a new agent (CMP-11) ──────────────────────────
//
// H in WORKTREES (Worktree: Hand Off Selected…) passes the selected
// worktree's work to a new agent. git is read off the UI thread for the
// note (worktrees.Handoff: the branch and its base, the commits since
// the base, the changed and uncommitted files, the agents' notes); no
// paid request runs. The card (Cancel first) shows where (this worktree
// or a new child worktree), which agent, one optional line for it and a
// preview of the note. Hand off writes the note to
// <git-common-dir>/termocode/handoff/ (never into the project, secrets
// redacted), then either starts the agent in this worktree with a
// one-line task that names the note, or opens the New worktree form for
// a child with the agent picked and the task typed in. The old agent
// keeps running (s puts it to sleep). The AI never starts a handoff.

const (
	wtHandoffTitle        = "Hand off"
	wtHandoffPaletteID    = "worktree-handoff"
	wtHandoffPreviewLines = 9
)

func init() {
	aiActionPolicies[wtHandoffPaletteID] = aiNever("a handoff starts a new agent; the user picks it and checks the note")
}

// wtHandoffForm is the open handoff card.
type wtHandoffForm struct {
	path, title, from string // from: the child's start point
	h                 worktrees.Handoff
	agents            []agents.Agent
	busy              bool
	fail              string
	gen               int
}

// wtHandoffPrepMsg is the git read for a new card.
type wtHandoffPrepMsg struct {
	path, title, from string
	h                 worktrees.Handoff
	err               error
}

// wtHandoffWroteMsg is the end of the note write.
type wtHandoffWroteMsg struct {
	gen   int
	file  string
	child bool
	agent agents.Agent
	err   error
}

// wtHandoffRedact hides secrets and keys in the note.
func wtHandoffRedact(s string) string { return ai.Redact(ai.RedactSecrets(s)) }

// wtHandoffGather reads the facts of worktree e against base ("" = the
// repository's default branch). A var: tests.
var wtHandoffGather = func(e worktrees.Entry, base string) (worktrees.Handoff, error) {
	h := worktrees.Handoff{Title: e.Title(), Branch: e.Branch, Path: e.Path,
		Notes: e.Meta.Notes, Checkpoint: e.Meta.Checkpoint, Link: wtHandoffLink(e.Meta)}
	dir := e.Path
	if base == "" {
		base = git.DefaultBranch(dir)
	}
	if base != "" && base != e.Branch {
		if mb, err := git.MergeBase(dir, base, "HEAD"); err == nil && mb != "" {
			h.Base = base
			h.Commits, _ = git.SubjectsSince(dir, mb, worktrees.HandoffMaxCommits+1)
			h.Changed, _ = git.WorkTreeDiffFiles(dir, mb)
		}
	}
	st, err := git.Status(dir)
	if err != nil {
		return h, err
	}
	for _, f := range st {
		h.Dirty = append(h.Dirty, f.Code+" "+f.Path)
	}
	return h, nil
}

// wtHandoffLink is the worktree's linked issue or PR in one line.
func wtHandoffLink(md worktrees.Meta) string {
	s := ""
	switch {
	case md.PR > 0:
		s = "PR #" + strconv.Itoa(md.PR)
	case md.Issue > 0:
		s = "#" + strconv.Itoa(md.Issue)
	default:
		return ""
	}
	if md.Link.Title != "" {
		s += " " + md.Link.Title
	}
	return s
}

// wtHandoffWrite writes the note of the worktree at dir and returns its
// file. A var: tests.
var wtHandoffWrite = func(dir, text string) (string, error) {
	state, err := git.AgentStateDir(dir)
	if err != nil {
		return "", err
	}
	file := worktrees.HandoffFile(state, dir, clock.Now())
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return "", err
	}
	return file, os.WriteFile(file, []byte(text), 0o600)
}

// wtHandoffStart starts the agent with its task. A var: tests.
var wtHandoffStart = func(m *Model, a agents.Agent, dir, task string) tea.Cmd {
	return m.startAgentWithTask(a, dir, task)
}

// worktreeHandoff is H: read git for the selected worktree, then open
// the card.
func (m *Model) worktreeHandoff() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	if it.e.Gone || it.e.Meta.State == worktrees.StateArchived {
		return m.pushToastDetail(toast.Info, "Nothing to hand off here", "Its folder is gone or archived.")
	}
	if m.connectOpen {
		return nil
	}
	if len(wtNewAgents()) == 0 {
		return m.pushToastDetail(toast.Info, "No agent to hand off to", "Install an agent CLI first. Guide: guides/agents.md")
	}
	e := it.e
	// The base: the parent's branch for a child, else the main
	// checkout's branch (read from the list: no git on the UI thread).
	base, mainBranch := "", ""
	for _, p := range m.wt.entries {
		if e.Meta.Parent != "" && p.Path == e.Meta.Parent && p.Branch != "" {
			base = p.Branch
		}
		if p.Main && p.Branch != "" {
			mainBranch = p.Branch
		}
	}
	if base == "" {
		base = mainBranch
	}
	from := e.Branch
	if from == "" {
		from = e.Head
	}
	title := wtTitle(it)
	return func() tea.Msg {
		h, err := wtHandoffGather(e, base)
		h.Title = title
		return wtHandoffPrepMsg{path: e.Path, title: title, from: from, h: h, err: err}
	}
}

// onWTHandoffPrep opens the card.
func (m *Model) onWTHandoffPrep(msg wtHandoffPrepMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Warn, "The worktree could not be read", msg.err.Error())
	}
	if m.connectOpen { // another card came first
		return nil
	}
	ags := wtNewAgents()
	if len(ags) > wtFormAgentsMax {
		ags = ags[:wtFormAgentsMax]
	}
	f := &wtHandoffForm{path: msg.path, title: msg.title, from: msg.from, h: msg.h, agents: ags}
	m.wt.handoff = f
	m.connect = connectcard.New(wtHandoffCardFor(f, 0, 0, ""))
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	return nil
}

// wtHandoffCardFor builds the card (pure: tests and goldens).
func wtHandoffCardFor(f *wtHandoffForm, where, agent int, extra string) connectcard.Card {
	opts := make([]string, 0, len(f.agents))
	for _, a := range f.agents {
		opts = append(opts, a.Title)
	}
	branch := f.h.Branch
	if branch == "" {
		branch = "detached"
	}
	rows := []connectcard.Row{
		{Kind: connectcard.Text, Text: "Worktree  " + f.title + theme.ASCIIText(" · ") + branch},
		{Kind: connectcard.Dim, Text: "A new agent gets a note of this work. The old agent keeps running."},
		{Kind: connectcard.Blank},
		{Kind: connectcard.Choice, ID: "where", Text: "Where", Options: []string{"this worktree", "new child worktree"}, Selected: where},
		{Kind: connectcard.Choice, ID: "agent", Text: "Agent", Options: opts, Selected: agent},
		{Kind: connectcard.Input, ID: "extra", Text: "Add", Inline: true, Placeholder: "optional: one line for the new agent", Value: extra},
		{Kind: connectcard.Blank},
		{Kind: connectcard.Dim, Text: "The note (saved in the git folder, secrets hidden):"},
	}
	n := 0
	for _, l := range strings.Split(f.h.Text(wtHandoffRedact), "\n") {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "# ") {
			continue
		}
		if n == wtHandoffPreviewLines {
			rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "  …"})
			break
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "  " + l})
		n++
	}
	switch {
	case f.busy:
		rows = append(rows, connectcard.Row{Kind: connectcard.Status, Text: theme.GlyphWait.String() + " Writing the note…"})
	case f.fail != "":
		rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: f.fail})
	}
	return connectcard.Card{Title: wtHandoffTitle, Rows: rows, Form: true,
		Buttons: []connectcard.Button{{ID: "cancel", Title: "Cancel"}, {ID: "go", Title: "Hand off"}}}
}

func (m Model) wtHandoffOpen() bool {
	return m.wt != nil && m.wt.handoff != nil && m.connectOpen && m.connect.Card().Title == wtHandoffTitle
}

// refreshWTHandoff redraws the open card with its typed values.
func (m *Model) refreshWTHandoff() {
	if !m.wtHandoffOpen() {
		return
	}
	m.connect.SetCard(wtHandoffCardFor(m.wt.handoff, m.connect.Choice("where"), m.connect.Choice("agent"), m.connect.InputValue("extra")))
}

func (m *Model) closeWTHandoff() {
	if m.wtHandoffOpen() {
		m.connectOpen = false
	}
	m.wt.handoff = nil
}

// handleWTHandoffMsg runs the card. ok=false: not ours.
func (m *Model) handleWTHandoffMsg(msg tea.Msg) (tea.Cmd, bool) {
	if m.wt == nil {
		return nil, false
	}
	switch msg := msg.(type) {
	case wtHandoffPrepMsg:
		return m.onWTHandoffPrep(msg), true
	case wtHandoffWroteMsg:
		return m.onWTHandoffWrote(msg), true
	}
	if m.wt.handoff == nil {
		return nil, false
	}
	switch msg := msg.(type) {
	case connectcard.ButtonMsg:
		switch msg.ID {
		case "cancel":
			m.closeWTHandoff()
		case "go":
			return m.wtHandoffGo(), true
		}
		return nil, true
	case connectcard.SubmitMsg, connectcard.KeyMsg:
		return nil, true // only the button hands off
	case connectcard.CloseMsg:
		m.closeWTHandoff()
		return nil, true
	}
	return nil, false
}

// wtHandoffGo checks the card and writes the note in the background.
func (m *Model) wtHandoffGo() tea.Cmd {
	f := m.wt.handoff
	if f.busy {
		return nil
	}
	child := m.connect.Choice("where") == 1
	pick := m.connect.Choice("agent")
	f.fail = ""
	if pick < 0 || pick >= len(f.agents) {
		f.fail = "Pick an agent."
		m.refreshWTHandoff()
		return nil
	}
	ag := f.agents[pick]
	if !child {
		for _, t := range m.terminalTabs {
			if agentStartedIn(t, ag.ID, f.path) {
				f.fail = ag.Title + " already runs in this worktree: pick another agent or a new child worktree."
				m.refreshWTHandoff()
				return nil
			}
		}
	}
	h := f.h
	h.Extra = strings.TrimSpace(m.connect.InputValue("extra"))
	text := h.Text(wtHandoffRedact)
	f.busy = true
	f.gen++
	gen, dir := f.gen, f.path
	m.refreshWTHandoff()
	return func() tea.Msg {
		file, err := wtHandoffWrite(dir, text)
		return wtHandoffWroteMsg{gen: gen, file: file, child: child, agent: ag, err: err}
	}
}

// onWTHandoffWrote starts the agent here, or opens the child form.
func (m *Model) onWTHandoffWrote(msg wtHandoffWroteMsg) tea.Cmd {
	f := m.wt.handoff
	if f == nil || msg.gen != f.gen {
		return nil
	}
	f.busy = false
	if msg.err != nil {
		f.fail = "The note could not be written: " + msg.err.Error()
		m.refreshWTHandoff()
		return nil
	}
	m.closeWTHandoff()
	task := worktrees.HandoffPrompt(msg.file, f.title)
	if msg.child {
		return tea.Batch(
			m.pushToastDetail(toast.Info, "Handoff note written", "Name the child worktree, then Create + Start. "+wtShortPath(msg.file)),
			m.openWorktreeNewPrefilled(f.path, f.title, f.from, task, msg.agent.ID))
	}
	if err := m.wtTouchMeta(f.path, func(md *worktrees.Meta) {
		md.Agent = msg.agent.ID
		md.AddNote("Handed off to "+msg.agent.Title, clock.Now())
	}); err != nil {
		recordError("[worktrees] handoff note: " + err.Error())
	}
	return tea.Batch(
		m.pushToastDetail(toast.Info, "Handed off to "+msg.agent.Title, "It reads the note: "+wtShortPath(msg.file)),
		wtHandoffStart(m, msg.agent, f.path, task))
}

// openWorktreeNewPrefilled opens the New worktree form for a child of
// parent with the agent and the task filled in.
func (m *Model) openWorktreeNewPrefilled(parent, parentTitle, from, task, agentID string) tea.Cmd {
	c := m.openWorktreeNewFrom(parent, parentTitle, from)
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		msg := c()
		if p, ok := msg.(wtNewPrepMsg); ok && p.f != nil {
			p.f.preTask, p.f.preAgent = task, agentID
			return p
		}
		return msg
	}
}
