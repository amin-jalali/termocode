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

const (
	confirmKindAIShipPush confirmKindEnum = 2904
	confirmKindAIShipPR   confirmKindEnum = 2905
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
}

// aiShipOpsFor builds the real ops (git and gh in the workspace).
var aiShipOpsFor = func(m *Model) aiShipOps {
	dir, _ := m.rootDir()
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

func (o aiShipReal) FailedLog(ctx context.Context, r ghci.Run) (ghci.FailedLog, error) {
	return ghci.FetchFailedLog(ctx, o.run, o.dir, r.ID)
}

// aiShip is a running ship loop.
type aiShip struct {
	gen    int // a newer ship (or Stop) ends older messages
	branch string
	ops    aiShipOps
	fixes  int
	fixing bool        // a fix turn runs in the chat
	pr     *ghci.NewPR // the open PR card's request
	last   aiShipProbe
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
		s.ship = nil
		return m.aiInfo("Ship stopped")
	}
	m.gitEnsureLoaded()
	if !m.gitIsRepo || m.gitBranch.Name == "" || strings.HasPrefix(m.gitBranch.Name, "(") {
		return m.aiInfo("Ship needs a git branch: open a folder with a git repository")
	}
	s.shipGen++
	s.ship = &aiShip{gen: s.shipGen, branch: m.gitBranch.Name, ops: aiShipOpsFor(m)}
	return tea.Batch(m.aiInfo("Shipping "+s.ship.branch+": push, pull request, CI, fixes · run the command again to stop"), m.aiShipProbeCmd())
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
	return m.aiInfo(text)
}

// handleAIShipMsg routes the ship messages. ok=false: not ours.
func (m *Model) handleAIShipMsg(msg tea.Msg) (tea.Cmd, bool) {
	sh := m.aiS().ship
	cur := func(gen int) bool { return sh != nil && sh.gen == gen }
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
	}
	// CI of the pushed head: runs for an older commit mean it did not start yet.
	state := p.snap.State()
	if p.head != "" && p.snap.HeadSHA != "" && p.snap.HeadSHA != p.head {
		state = ghci.StatePending
	}
	switch state {
	case ghci.StatePass:
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
	}
	return m.aiShipStop("Ship stopped")
}
