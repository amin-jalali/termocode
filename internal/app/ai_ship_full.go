package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// Ship a worktree in one flow (CMP-33): S on a WORKTREES card (Worktree:
// Ship Selected…) runs every step from work to done:
//
//	commit (AI message) → push → pull request (AI title and text) →
//	CI (AI fixes when red, as in AI: Ship Branch to Merge-Ready) →
//	merge → archive the worktree
//
// Each step waits on a card whose safe choice is first: the commit card
// (Cancel · Commit and ship · Edit message…), the push and PR cards of
// the ship loop (ai_ship.go), the merge card (Cancel · Merge · Squash
// and merge · Rebase and merge) and the archive card of the view
// (worktrees_archive.go). Cancel or Esc on any card stops there; nothing
// went out that the user did not say yes to. The AI never starts it.

const (
	confirmKindWTShipCommit confirmKindEnum = 4731
	confirmKindAIShipMerge  confirmKindEnum = 4732
	promptKindWTShipMessage promptKindEnum  = 4733
	wtShipPaletteID                         = "worktree-ship"

	aiShipPRMaxTokens = 800
)

// Tests swap these: the git calls of the commit step.
var (
	wtShipDiffRun   = git.ChangesDiff
	wtShipCommitRun = git.CommitAll
)

// wtShipPending is the worktree the commit card (or its prompt) is about.
type wtShipPending struct {
	path, branch, title string
	dirty               int
	message             string
	note                string // why the message is not the AI's ("" = it is)
}

type (
	wtShipPrepMsg struct { // the dirty count and the message are ready
		p   wtShipPending
		err error
	}
	wtShipCommittedMsg struct {
		p       wtShipPending
		message string
		err     error
	}
	aiShipPRTextMsg struct { // the AI's pull request text
		gen  int
		p    aiShipProbe
		text string
		err  error
	}
	aiShipMergedMsg struct {
		gen    int
		number int
		err    error
	}
)

func init() {
	aiActionPolicies[wtShipPaletteID] = aiNever("it commits, pushes, opens and merges a pull request: the user starts it")
}

// worktreeShip is S: ship the cursor's worktree.
func (m *Model) worktreeShip() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	return m.wtShipItem(it)
}

// wtShipItem starts the ship of it: the commit card when it has changes,
// else the ship loop at once.
func (m *Model) wtShipItem(it wtItem) tea.Cmd {
	e := it.e
	switch {
	case e.Meta.State == worktrees.StateArchived:
		return m.pushToastDetail(toast.Info, "This worktree is archived", "x restores its folder; then S ships it.")
	case e.Gone:
		return m.pushToastDetail(toast.Info, "Its folder is gone", "F forgets it.")
	case e.Kind == worktrees.KindAgent:
		return m.pushToastDetail(toast.Info, "This is an agent lane", "Its AI run keeps or discards it (Worktree changes in the chat).")
	case e.Branch == "" || e.Detached:
		return m.pushToastDetail(toast.Info, "Ship needs a branch", "This worktree is on a bare commit: check out a branch in it first.")
	case m.ai != nil && m.ai.ship != nil:
		return m.pushToastDetail(toast.Info, "A ship is already running", "Run AI: Ship Branch to Merge-Ready to stop it first.")
	}
	p := wtShipPending{path: e.Path, branch: e.Branch, title: wtTitle(it)}
	var (
		prov  ai.Provider
		model string
		useAI = m.aiConfigured()
	)
	if useAI {
		s := m.aiS()
		prov, model = s.provider, s.cfg.InlineModel
	}
	return func() tea.Msg {
		n, err := wtDirtyRun(p.path)
		if err != nil || n <= 0 {
			return wtShipPrepMsg{p: p, err: err}
		}
		p.dirty = n
		p.message, p.note = wtShipMessage(p, prov, model, useAI)
		return wtShipPrepMsg{p: p}
	}
}

// wtShipMessage asks the AI for the commit message of p's changes; the
// fallback names the branch and says why.
func wtShipMessage(p wtShipPending, prov ai.Provider, model string, useAI bool) (msg, note string) {
	fallback := "Work on " + p.branch
	if !useAI || prov == nil {
		return fallback, "No AI is set up: Edit message… to write your own."
	}
	diff, cut, err := wtShipDiffRun(p.path, false, gitAIDiffMax)
	if err != nil || strings.TrimSpace(diff) == "" {
		return fallback, "The changes could not be read for the AI: Edit message… to write your own."
	}
	recent, _ := git.RecentMessages(p.path, 10)
	ctx, cancel := context.WithTimeout(context.Background(), gitAITimeout)
	defer cancel()
	text, last := ai.Collect(prov.Stream(ctx, ai.Request{Messages: gitAIMessagePrompt("commit", diff, cut, recent), Model: model, MaxTokens: gitAIMaxTokens}))
	if text = gitAICleanReply(text); last.Err != nil || text == "" {
		return fallback, "The AI did not answer: Edit message… to write your own."
	}
	return text, ""
}

// onWTShipPrep opens the commit card, or starts the ship of a clean
// worktree.
func (m *Model) onWTShipPrep(msg wtShipPrepMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Warn, "The worktree could not be read", msg.err.Error())
	}
	if msg.p.dirty == 0 {
		return m.startWTShip(msg.p)
	}
	if m.confirmOpen {
		return nil
	}
	p := msg.p
	var b strings.Builder
	b.WriteString("First its " + wtPlural(p.dirty, "changed file") + " " + map[bool]string{true: "is", false: "are"}[p.dirty == 1] +
		" committed (all of them, one commit) with this message:\n")
	for _, l := range strings.Split(p.message, "\n") {
		b.WriteString("\n    " + l)
	}
	if p.note != "" {
		b.WriteString("\n\n" + p.note)
	}
	b.WriteString("\n\nThen: push → pull request (AI text) → CI (AI fixes when red) → merge → archive. Each step asks first; Cancel on any card stops there.")
	m.aiS().shipCommit = &p
	m.confirm = confirm.Risky("Ship "+p.title+"?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "commit", Title: "Commit and ship", Style: confirm.StylePrimary},
		confirm.Button{ID: "edit", Title: "Edit message…"}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTShipCommit
	return nil
}

// answerWTShipCommit answers the commit card ("" = Esc = Cancel).
func (m *Model) answerWTShipCommit(id string) tea.Cmd {
	s := m.aiS()
	p := s.shipCommit
	s.shipCommit = nil
	if p == nil {
		return nil
	}
	switch id {
	case "commit":
		return m.wtShipCommit(*p, p.message)
	case "edit":
		s.shipCommit = p
		m.prompt = prompt.New("Ship: commit message", "Commit message (Enter commits and ships, Esc stops):", p.message)
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindWTShipMessage
	}
	return nil
}

// handleWTShipPromptSubmit commits with the user's message and ships.
func (m *Model) handleWTShipPromptSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindWTShipMessage {
		return nil, false
	}
	s := m.aiS()
	p := s.shipCommit
	s.shipCommit = nil
	if p == nil {
		return nil, true
	}
	msg := strings.TrimSpace(value)
	if msg == "" {
		return m.pushToastDetail(toast.Info, "Nothing committed", "An empty message commits nothing; S asks again."), true
	}
	return m.wtShipCommit(*p, msg), true
}

func (m *Model) wtShipCommit(p wtShipPending, message string) tea.Cmd {
	return func() tea.Msg {
		return wtShipCommittedMsg{p: p, message: message, err: wtShipCommitRun(p.path, message)}
	}
}

func (m *Model) onWTShipCommitted(msg wtShipCommittedMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Warn, "The commit failed: ship stopped", ai.Redact(msg.err.Error()))
	}
	subject, _, _ := strings.Cut(msg.message, "\n")
	return tea.Batch(m.pushToastDetail(toast.Info, "Committed in "+msg.p.title, subject), m.startWTShip(msg.p))
}

// startWTShip starts the full ship loop on p's branch and folder.
func (m *Model) startWTShip(p wtShipPending) tea.Cmd {
	if m.ai != nil && m.ai.ship != nil {
		return m.pushToastDetail(toast.Info, "A ship is already running", "Run AI: Ship Branch to Merge-Ready to stop it first.")
	}
	cmd := m.startAIShip(p.branch, p.path)
	sh := m.aiS().ship
	sh.full, sh.wtPath = true, p.path
	return cmd
}

// handleWTShipMsg routes the messages of the full ship (cur: the ship
// of gen still runs).
func (m *Model) handleWTShipMsg(msg tea.Msg, cur func(int) bool) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case wtShipPrepMsg:
		return m.onWTShipPrep(msg), true
	case wtShipCommittedMsg:
		return m.onWTShipCommitted(msg), true
	case aiShipPRTextMsg:
		if !cur(msg.gen) {
			return nil, true
		}
		return m.onAIShipPRText(msg), true
	case aiShipMergedMsg:
		if !cur(msg.gen) {
			return nil, true
		}
		return m.onAIShipMerged(msg), true
	}
	return nil, false
}

// aiShipPRText asks the AI for the pull request title and description;
// without an AI the card shows the text made from the commits.
func (m *Model) aiShipPRText(p aiShipProbe) tea.Cmd {
	if !m.aiConfigured() {
		return m.askAIShipPR(p)
	}
	s := m.aiS()
	sh := s.ship
	gen, prov, model, dir := sh.gen, s.provider, s.cfg.InlineModel, sh.dir
	if dir == "" {
		dir, _ = m.rootDir()
	}
	return tea.Batch(m.aiInfo("Asking the AI for the pull request text…"), func() tea.Msg {
		stat, _ := runGitOut(dir, "diff", "--stat", "--no-color", p.base+"...HEAD")
		ctx, cancel := context.WithTimeout(context.Background(), gitAITimeout)
		defer cancel()
		text, last := ai.Collect(prov.Stream(ctx, ai.Request{Messages: aiShipPRPrompt(p, stat), Model: model, MaxTokens: aiShipPRMaxTokens}))
		return aiShipPRTextMsg{gen: gen, p: p, text: text, err: last.Err}
	})
}

// aiShipPRPrompt asks for a title line, a blank line and the description.
func aiShipPRPrompt(p aiShipProbe, stat string) []ai.Message {
	sys := "You write pull request texts. Answer with the text only: no code fence, no comments. " +
		"First line: the title, at most 72 characters. Then a blank line, then a short description: what changes, why, and how to test it."
	var b strings.Builder
	b.WriteString("Branch " + p.branch + " into " + p.base + ".\n\nCommits:\n")
	for _, s := range p.subjects {
		b.WriteString("- " + s + "\n")
	}
	if strings.TrimSpace(stat) != "" {
		b.WriteString("\nFiles:\n" + gitAIFence(stat, false))
	}
	return []ai.Message{{Role: ai.RoleSystem, Content: sys}, {Role: ai.RoleUser, Content: b.String()}}
}

// aiShipSplitPR is the title and body of the AI's answer ("" title: no
// usable answer).
func aiShipSplitPR(text string) (title, body string) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "```"), "```"))
		if i := strings.IndexByte(text, '\n'); i >= 0 && !strings.Contains(text[:i], " ") {
			text = text[i+1:] // a language tag
		}
	}
	title, body, _ = strings.Cut(strings.TrimSpace(text), "\n")
	title = strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(strings.TrimSpace(title), "Title:"), "# "))
	if r := []rune(title); len(r) > 100 {
		title = string(r[:100])
	}
	return title, strings.TrimSpace(body)
}

func (m *Model) onAIShipPRText(msg aiShipPRTextMsg) tea.Cmd {
	title, body := aiShipSplitPR(msg.text)
	if msg.err != nil || title == "" {
		t, b := ghci.Compose(msg.p.subjects)
		return m.askAIShipPRText(msg.p, t, b, "The AI did not answer: this text is made from the commits.")
	}
	return m.askAIShipPRText(msg.p, title, body, "The AI wrote this text from the commits; edit it on the pull request later if you like.")
}

// askAIShipMerge shows the merge card (Cancel first) on green CI.
func (m *Model) askAIShipMerge(p aiShipProbe) tea.Cmd {
	text := "CI is green on " + aiShipPRName(p.snap.PR) + ": " + p.branch + " → " + p.base + ".\n\n" +
		"Merge it now (gh pr merge, never --admin)? The branch stays; after the merge termocode offers to archive the worktree.\n\n" +
		"Cancel stops here: it is merge-ready, merge it yourself when you want."
	m.confirm = confirm.Risky("Ship: merge the pull request?", text,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "merge", Title: "Merge", Style: confirm.StylePrimary},
		confirm.Button{ID: "squash", Title: "Squash and merge"},
		confirm.Button{ID: "rebase", Title: "Rebase and merge"}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIShipMerge
	return nil
}

// answerAIShipMerge merges on a merge choice; anything else stops at
// merge-ready.
func (m *Model) answerAIShipMerge(id string) tea.Cmd {
	sh := m.aiS().ship
	pr := sh.last.snap.PR
	var method ghci.MergeMethod
	switch id {
	case "merge":
		method = ghci.MergeCommit
	case "squash":
		method = ghci.MergeSquash
	case "rebase":
		method = ghci.MergeRebase
	}
	if method == "" || pr == nil {
		return m.aiShipStop("Merge-ready: " + aiShipPRName(pr) + " is green. Merge it yourself when you want.")
	}
	gen, ops, number := sh.gen, sh.ops, pr.Number
	return tea.Batch(m.aiInfo("Merging "+aiShipPRName(pr)+"…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gitAITimeout)
		defer cancel()
		return aiShipMergedMsg{gen: gen, number: number, err: ops.Merge(ctx, number, method)}
	})
}

// onAIShipMerged ends the ship and offers to archive the worktree.
func (m *Model) onAIShipMerged(msg aiShipMergedMsg) tea.Cmd {
	path := m.aiS().ship.wtPath
	name := aiShipPRName(&ghci.PR{Number: msg.number})
	if msg.err != nil {
		return m.aiShipStop("Merge failed: " + ai.Redact(msg.err.Error()) + " · ship stopped")
	}
	done := m.aiShipStop("Merged " + name)
	if path == "" {
		return done
	}
	it, ok := m.wtItemByPath(path)
	if !ok {
		return done
	}
	if t, d := wtRefuse(it); t != "" {
		return tea.Batch(done, m.pushToastDetail(toast.Info, "Merged "+name+"; the worktree stays", t+". "+d))
	}
	return tea.Batch(done, m.wtArchiveItem(it))
}
