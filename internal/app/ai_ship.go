package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
)

// Ship a branch to merge-ready (OMX-15): AI: Ship Branch to Merge-Ready…
// joins the existing pieces into one loop for the current branch (an
// agent's work after a plan run, or your own): push → open a pull
// request → watch CI → when CI is red, send the failing log to the AI
// chat to fix and commit → push again … and stop when CI is green on the
// pull request. It never merges. Every outward step (each push, the pull
// request) waits on a card whose safe choice is first; a fix turn is a
// normal chat turn (its edits get the usual review). At most
// aiShipMaxFixes fix turns; Stop (the palette again) ends the loop.
//
// CI babysitter (CMP-34): AI: Ship and Merge When Green… runs the same
// loop and adds two steps. A pull request with merge conflicts gets an
// AI turn that merges the base in and commits (it counts toward the fix
// cap). When CI is green on a mergeable, non-draft pull request without
// requested changes, a merge card asks (Cancel first); the merge is
// pinned to the green commit, so a later push makes GitHub refuse it.

const (
	confirmKindAIShipPush  confirmKindEnum = 2904
	confirmKindAIShipPR    confirmKindEnum = 2905
	confirmKindAIShipMerge confirmKindEnum = 2909
)

// aiShipMaxFixes caps the AI fix turns of one ship.
const aiShipMaxFixes = 3

// aiShipPoll is how often CI is asked while it runs (a var for tests).
var aiShipPoll = 20 * time.Second

// aiShipFixNote ends the fix message: the loop needs a commit.
const aiShipFixNote = "\n\nFix the cause, run the failing check locally if you can, then commit the fix with git_commit (one commit). Do not push: termocode asks the user before each push."

// aiShipProbe is what the branch looks like now.
type aiShipProbe struct {
	branch, base string
	subjects     []string // commits base does not have
	unpushed     int
	pushed       bool
	head         string // local HEAD sha
	snap         ghci.Snapshot
}

// aiShipOps are the outside calls of the loop (fakes in tests).
type aiShipOps interface {
	Probe(ctx context.Context, branch string) (aiShipProbe, error)
	Push(ctx context.Context, branch string) error
	CreatePR(ctx context.Context, p ghci.NewPR) (string, error)
	FailedLog(ctx context.Context, run ghci.Run) (ghci.FailedLog, error)
	Merge(ctx context.Context, number int, method ghci.MergeMethod, sha string) error
}

// aiShipOpsFor builds the real ops (git and gh in dir; "": the
// workspace).
var aiShipOpsFor = func(m *Model, dir string) aiShipOps {
	if dir == "" {
		dir, _ = m.rootDir()
	}
	return aiShipReal{dir: dir, run: m.ciW().runner()}
}

type aiShipReal struct {
	dir string
	run ghci.Runner
}

func (o aiShipReal) Probe(ctx context.Context, branch string) (aiShipProbe, error) {
	p := aiShipProbe{branch: branch}
	base, err := ghci.DefaultBranch(ctx, o.run, o.dir)
	if err != nil {
		return p, err
	}
	p.base = base
	p.unpushed, p.pushed = git.Unpushed(o.dir)
	if p.subjects, err = git.BranchSubjects(o.dir, base); err != nil {
		return p, err
	}
	p.head, _ = git.HeadSHA(o.dir)
	p.snap, err = ghci.Fetch(ctx, o.run, o.dir, branch)
	return p, err
}

func (o aiShipReal) Push(_ context.Context, branch string) error { return pushCurrent(o.dir, branch) }

func (o aiShipReal) CreatePR(ctx context.Context, p ghci.NewPR) (string, error) {
	return ghci.Create(ctx, o.run, o.dir, p)
}

func (o aiShipReal) Merge(ctx context.Context, n int, method ghci.MergeMethod, sha string) error {
	return ghci.MergeHead(ctx, o.run, o.dir, n, method, sha)
}

func (o aiShipReal) FailedLog(ctx context.Context, r ghci.Run) (ghci.FailedLog, error) {
	return ghci.FetchFailedLog(ctx, o.run, o.dir, r.ID)
}

// aiShip is a running ship loop.
type aiShip struct {
	gen      int // a newer ship (or Stop) ends older messages
	branch   string
	dir      string // a plan run's worktree ("": the workspace)
	ops      aiShipOps
	fixes    int
	fixing   bool        // a fix turn runs in the chat
	merge    bool        // CMP-34: offer the merge card when green
	mergeN   int         // the merge card's pull request
	mergeSHA string      // the green commit the merge card is for
	pr       *ghci.NewPR // the open PR card's request
	last     aiShipProbe
}

type (
	aiShipProbeMsg struct {
		gen int
		p   aiShipProbe
		err error
	}
	aiShipStepMsg struct { // a push or a PR finished
		gen    int
		what   string
		detail string
		err    error
	}
	aiShipTickMsg struct{ gen int }
	aiShipLogMsg  struct {
		gen int
		run ghci.Run
		log ghci.FailedLog
		err error
	}
)

// toggleAIShip starts the loop on the current branch, or stops it.
func (m *Model) toggleAIShip() tea.Cmd {
	s := m.aiS()
	if s.ship != nil {
		return m.aiShipStop("Ship stopped")
	}
	m.gitEnsureLoaded()
	if !m.gitIsRepo || m.gitBranch.Name == "" || strings.HasPrefix(m.gitBranch.Name, "(") {
		return m.aiInfo("Ship needs a git branch: open a folder with a git repository")
	}
	return m.startAIShip(m.gitBranch.Name, "")
}

// toggleAIShipMerge starts the babysitter (ship, fix conflicts, merge
// card when green) on the current branch, or stops a running ship.
func (m *Model) toggleAIShipMerge() tea.Cmd {
	cmd := m.toggleAIShip()
	if sh := m.aiS().ship; sh != nil {
		sh.merge = true
		return tea.Batch(m.aiInfo("Babysitting "+sh.branch+": CI fixes, conflicts, then a merge card when green · run the command again to stop"), cmd)
	}
	return cmd
}

// startAIShip starts the loop on branch, checked out in dir ("": the
// workspace; else a plan run's worktree, where its fix turns work too).
func (m *Model) startAIShip(branch, dir string) tea.Cmd {
	s := m.aiS()
	s.shipGen++
	s.ship = &aiShip{gen: s.shipGen, branch: branch, dir: dir, ops: aiShipOpsFor(m, dir)}
	return tea.Batch(m.aiInfo("Shipping "+branch+": push, pull request, CI, fixes · run AI: Ship Branch to Merge-Ready again to stop"), m.aiShipProbeCmd())
}

func (m *Model) aiShipProbeCmd() tea.Cmd {
	sh := m.aiS().ship
	if sh == nil {
		return nil
	}
	gen, ops, branch := sh.gen, sh.ops, sh.branch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		p, err := ops.Probe(ctx, branch)
		return aiShipProbeMsg{gen: gen, p: p, err: err}
	}
}

// aiShipStop ends the loop with a message.
func (m *Model) aiShipStop(text string) tea.Cmd {
	m.aiS().ship = nil
	m.aiS().turnRoot = ""
	return m.aiInfo(text)
}

// handleAIShipMsg routes the ship messages. ok=false: not ours.
func (m *Model) handleAIShipMsg(msg tea.Msg) (tea.Cmd, bool) {
	// Not m.aiS(): every message passes here, and building the AI state
	// opens the secret store (it writes secrets.json) for nothing.
	cur := func(gen int) bool { return m.ai != nil && m.ai.ship != nil && m.ai.ship.gen == gen }
	switch msg := msg.(type) {
	case aiShipProbeMsg:
		if !cur(msg.gen) {
			return nil, true
		}
		return m.onAIShipProbe(msg), true
	case aiShipStepMsg:
		if !cur(msg.gen) {
			return nil, true
		}
		if msg.err != nil {
			return m.aiShipStop(msg.what + " failed: " + ai.Redact(msg.err.Error()) + " · ship stopped"), true
		}
		if msg.what == "Merge" {
			return m.aiShipStop("Merged " + msg.detail), true
		}
		return tea.Batch(m.aiInfo(msg.what+" done "+msg.detail), m.aiShipProbeCmd()), true
	case aiShipTickMsg:
		if !cur(msg.gen) {
			return nil, true
		}
		return m.aiShipProbeCmd(), true
	case aiShipLogMsg:
		if !cur(msg.gen) {
			return nil, true
		}
		return m.onAIShipLog(msg), true
	case aiPlanShipMsg: // ai_plan_worktree.go
		return m.onAIPlanShip(msg), true
	}
	return nil, false
}

// onAIShipProbe decides the next step.
func (m *Model) onAIShipProbe(msg aiShipProbeMsg) tea.Cmd {
	sh := m.aiS().ship
	p := msg.p
	sh.last = p
	switch {
	case msg.err != nil:
		return m.aiShipStop("Ship stopped: " + ai.Redact(msg.err.Error()))
	case p.branch == p.base:
		return m.aiShipStop("You are on " + p.base + ", the default branch: ship a feature branch")
	case len(p.subjects) == 0:
		return m.aiShipStop("No commits on " + p.branch + " that " + p.base + " does not have: nothing to ship")
	case !p.pushed || p.unpushed > 0:
		return m.askAIShipPush(p)
	case p.snap.PR == nil || p.snap.PR.State != "OPEN":
		return m.askAIShipPR(p)
	case p.snap.PR.Conflicting():
		return m.aiShipConflictTurn(p)
	}
	// CI of the pushed head: runs for an older commit mean it did not start yet.
	state := p.snap.State()
	if p.head != "" && p.snap.HeadSHA != "" && p.snap.HeadSHA != p.head {
		state = ghci.StatePending
	}
	switch state {
	case ghci.StatePass:
		if sh.merge {
			return m.aiShipMergeStep(p)
		}
		return m.aiShipStop("Merge-ready: CI is green on " + aiShipPRName(p.snap.PR) + ". Merge it yourself when you want.")
	case ghci.StateFail:
		if sh.fixes >= aiShipMaxFixes {
			return m.aiShipStop(fmt.Sprintf("CI is still red after %d AI fixes: ship stopped", aiShipMaxFixes))
		}
		runs := p.snap.FailedRuns()
		gen, ops, run := sh.gen, sh.ops, runs[0]
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			fl, err := ops.FailedLog(ctx, run)
			return aiShipLogMsg{gen: gen, run: run, log: fl, err: err}
		}
	}
	gen := sh.gen // pending or no runs yet: ask again later
	return tea.Tick(aiShipPoll, func(time.Time) tea.Msg { return aiShipTickMsg{gen: gen} })
}

func aiShipPRName(pr *ghci.PR) string {
	if pr == nil {
		return "the pull request"
	}
	return "pull request #" + strconv.Itoa(pr.Number)
}

// onAIShipLog sends the failing log to the AI chat as a fix turn.
func (m *Model) onAIShipLog(msg aiShipLogMsg) tea.Cmd {
	s := m.aiS()
	sh := s.ship
	if msg.err != nil {
		return m.aiShipStop("Could not read the CI log: " + ai.Redact(msg.err.Error()) + " · ship stopped")
	}
	if s.streaming || s.chatCancel != nil {
		gen := sh.gen // the chat is busy: try again soon
		return tea.Tick(aiShipPoll, func(time.Time) tea.Msg { return aiShipTickMsg{gen: gen} })
	}
	sh.fixes++
	sh.fixing = true
	text := ghci.FixPrompt(sh.branch, msg.run, msg.log) + aiShipFixNote
	s.turnRoot = sh.dir // a plan run's worktree: the fix is made there
	return tea.Batch(m.aiInfo(fmt.Sprintf("CI is red: AI fix %d of %d", sh.fixes, aiShipMaxFixes)), m.openAIPanel(true), m.sendAIChat(text))
}

// aiShipOnChatDone goes on after a fix turn (called when a chat turn ends).
func (m *Model) aiShipOnChatDone() tea.Cmd {
	sh := m.aiS().ship
	if sh == nil || !sh.fixing {
		return nil
	}
	sh.fixing = false
	return m.aiShipProbeCmd()
}

// aiShipConflictTurn sends the merge-conflict fix to the AI chat (one
// fix turn under the cap); the chat busy: ask again soon.
func (m *Model) aiShipConflictTurn(p aiShipProbe) tea.Cmd {
	s := m.aiS()
	sh := s.ship
	if sh.fixes >= aiShipMaxFixes {
		return m.aiShipStop(fmt.Sprintf("%s still has merge conflicts after %d AI fixes: ship stopped", aiShipPRName(p.snap.PR), aiShipMaxFixes))
	}
	if s.streaming || s.chatCancel != nil {
		gen := sh.gen
		return tea.Tick(aiShipPoll, func(time.Time) tea.Msg { return aiShipTickMsg{gen: gen} })
	}
	sh.fixes++
	sh.fixing = true
	text := ghci.ConflictPrompt(p.branch, p.base, p.snap.PR.Number) + aiShipFixNote
	s.turnRoot = sh.dir
	return tea.Batch(m.aiInfo(fmt.Sprintf("Merge conflicts: AI fix %d of %d", sh.fixes, aiShipMaxFixes)), m.openAIPanel(true), m.sendAIChat(text))
}

// aiShipMergeStep is green CI in babysitter mode: a blocked pull request
// stops the ship with the reason; GitHub still computing waits; else the
// merge card.
func (m *Model) aiShipMergeStep(p aiShipProbe) tea.Cmd {
	sh := m.aiS().ship
	pr := p.snap.PR
	name := aiShipPRName(pr)
	switch {
	case pr.Draft:
		return m.aiShipStop("CI is green, but " + name + " is a draft: mark it ready, then merge it yourself or run the command again")
	case pr.Review == "CHANGES_REQUESTED":
		return m.aiShipStop("CI is green, but " + name + " has changes requested: ship stopped, nothing merged")
	case pr.Mergeable == "UNKNOWN":
		gen := sh.gen // GitHub is still computing: ask again soon
		return tea.Tick(aiShipPoll, func(time.Time) tea.Msg { return aiShipTickMsg{gen: gen} })
	}
	sha := p.snap.HeadSHA
	if sha == "" {
		sha = p.head
	}
	sh.mergeN, sh.mergeSHA = pr.Number, sha
	text := fmt.Sprintf("CI is green on %s", name)
	if pr.Title != "" {
		text += " · " + pr.Title
	}
	text += "\n" + p.branch + " → " + p.base + " · commit " + shortSHA(sha) + "\n"
	if r := ciReviewText(pr.Review); r != "" {
		text += r + "\n"
	}
	text += fmt.Sprintf("\nAI fixes used: %d of %d.\n", sh.fixes, aiShipMaxFixes)
	text += "\nOnly this commit is merged: a newer push makes GitHub refuse. Branch rules are never bypassed and the branch is kept.\nCancel stops the ship."
	m.confirm = confirm.Risky("Ship: merge the pull request?", text,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "squash", Title: "Squash and Merge", Style: confirm.StylePrimary},
		confirm.Button{ID: "merge", Title: "Merge Commit"},
		confirm.Button{ID: "rebase", Title: "Rebase and Merge"}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIShipMerge
	return nil
}

// askAIShipPush shows the push card (Not now first).
func (m *Model) askAIShipPush(p aiShipProbe) tea.Cmd {
	text := "Push " + p.branch + " to origin"
	if p.pushed {
		text += fmt.Sprintf(" (%d new commit%s)", p.unpushed, plural(p.unpushed))
	}
	text += "?\n"
	for i, sub := range p.subjects {
		if i == 8 {
			text += "\n    …"
			break
		}
		text += "\n    " + sub
	}
	text += "\n\nA plain push, never forced. Not now stops the ship."
	m.confirm = confirm.Risky("Ship: push the branch?", text,
		confirm.Button{ID: "cancel", Title: "Not now"},
		confirm.Button{ID: "push", Title: "Push", Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIShipPush
	return nil
}

// askAIShipPR shows the pull request card (Cancel first).
func (m *Model) askAIShipPR(p aiShipProbe) tea.Cmd {
	title, body := ghci.Compose(p.subjects)
	req := ghci.NewPR{Title: title, Body: body, Base: p.base, Branch: p.branch}
	m.aiS().ship.pr = &req
	text := "Title: " + title + "\n" + p.branch + " → " + p.base + "\n"
	if body != "" {
		text += "\nDescription\n" + body + "\n"
	}
	text += "\nCancel stops the ship."
	m.confirm = confirm.Risky("Ship: open a pull request?", text,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "create", Title: "Create", Style: confirm.StylePrimary},
		confirm.Button{ID: "draft", Title: "Create as Draft"}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIShipPR
	return nil
}

// answerAIShipCard answers the push and PR cards; anything but the
// outward choice (Esc too) stops the ship.
func (m *Model) answerAIShipCard(kind confirmKindEnum, id string) tea.Cmd {
	sh := m.aiS().ship
	if sh == nil {
		return nil
	}
	gen, ops, branch := sh.gen, sh.ops, sh.branch
	switch {
	case kind == confirmKindAIShipPush && id == "push":
		return func() tea.Msg {
			err := ops.Push(context.Background(), branch)
			return aiShipStepMsg{gen: gen, what: "Push", err: err}
		}
	case kind == confirmKindAIShipPR && (id == "create" || id == "draft") && sh.pr != nil:
		req := *sh.pr
		req.Draft = id == "draft"
		sh.pr = nil
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			url, err := ops.CreatePR(ctx, req)
			return aiShipStepMsg{gen: gen, what: "Pull request", detail: url, err: err}
		}
	case kind == confirmKindAIShipMerge && (id == "squash" || id == "merge" || id == "rebase") && sh.mergeN > 0:
		n, sha, method := sh.mergeN, sh.mergeSHA, ghci.MergeMethod(id)
		sh.mergeN, sh.mergeSHA = 0, ""
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err := ops.Merge(ctx, n, method, sha)
			return aiShipStepMsg{gen: gen, what: "Merge", detail: "pull request #" + strconv.Itoa(n), err: err}
		}
	}
	return m.aiShipStop("Ship stopped")
}
