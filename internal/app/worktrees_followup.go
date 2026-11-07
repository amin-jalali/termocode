package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── PR feedback to the lane's agent (CMP-31) ─────────────────────────────
//
// W in WORKTREES (Worktree: Send PR Feedback to Agent…) reads the
// worktree's pull request with gh: open review threads, conversation
// comments that call the agent (@termocode or @agent) and a failed CI
// run with its failing log. A Cancel-first card shows a short preview;
// Send writes the whole feedback (secrets redacted) to a file in the git
// folder and types one line into the agent tab of that worktree, which
// tells the agent to read it, fix it and commit (not push). Without an
// agent tab the feedback goes into the AI chat input instead, for the
// user to send.
//
// w (Worktree: Follow PR Feedback) turns the loop on for a worktree with
// a linked PR: while the view shows, termocode reads the PR again every
// wtFollowEvery and asks with the same card when something is new.
// Nothing is typed without a click; Skip (and Esc) marks what the card
// showed as seen so it does not ask again; W sends everything open at
// any time. The AI can never start either one.

const (
	confirmKindWTFeedback confirmKindEnum = 4734
	wtFeedbackPaletteID                   = "worktree-feedback"
	wtFollowPaletteID                     = "worktree-follow"
	wtFollowEvery                         = 2 * time.Minute
	wtFollowMaxReads                      = 2 // follow reads started per load
	wtFeedbackPreview                     = 6 // comment lines shown on the card
)

func init() {
	aiActionPolicies[wtFeedbackPaletteID] = aiNever("types into the user's agent tab; the user sends PR feedback with W in WORKTREES")
	aiActionPolicies[wtFollowPaletteID] = aiNever("the follow loop is the user's choice per worktree")
}

// errWTNoPR: the worktree's branch has no pull request.
var errWTNoPR = errors.New("no pull request")

// wtFeedbackType types line into the agent tab buf (a var: tests).
var wtFeedbackType = func(m *Model, buf int, line string) { m.agentType(buf, line) }

// wtFeedbackPending is the open feedback card.
type wtFeedbackPending struct {
	path, title string
	auto        bool
	fb          ghci.Feedback
	buf         int    // the agent tab's BufID; -1: none (the AI chat gets it)
	agent       string // its title
}

// wtFeedbackMsg is the end of a gh read of a worktree's PR feedback.
type wtFeedbackMsg struct {
	path, title string
	auto        bool
	fb          ghci.Feedback
	err         error
}

// worktreeFeedback is W: read the cursor's PR feedback, then ask.
func (m *Model) worktreeFeedback() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	switch {
	case it.e.Gone:
		return m.pushToastDetail(toast.Info, "Its folder is gone", "F forgets it.")
	case m.wt.off:
		return m.pushToastDetail(toast.Info, "A demo has no pull request", "Open a real repository to send PR feedback.")
	case it.e.Branch == "" && it.e.Meta.PR <= 0:
		return m.pushToastDetail(toast.Info, "No branch, so no pull request", "Check out a branch or link its PR with i.")
	}
	if c := m.wtFeedbackRead(it, false); c != nil {
		return tea.Batch(c, m.pushToastDetail(toast.Info, "Reading the PR feedback with gh…", wtTitle(it)))
	}
	return nil
}

// wtFeedbackRead reads the feedback in the background. auto: the
// follow loop (only what is new, quiet errors).
func (m *Model) wtFeedbackRead(it wtItem, auto bool) tea.Cmd {
	st := m.wt
	path := it.e.Path
	if st.feedbackReading == nil {
		st.feedbackReading = map[string]bool{}
	}
	if st.feedbackReading[path] {
		return nil
	}
	st.feedbackReading[path] = true
	pr, branch, title := it.e.Meta.PR, it.e.Branch, wtTitle(it)
	var seen map[int64]bool
	var sent int64
	if auto {
		seen, sent = it.e.Meta.FollowSeenSet(), it.e.Meta.FollowRun
	}
	run := wtGHRun()
	return func() tea.Msg {
		ctx := context.Background()
		msg := wtFeedbackMsg{path: path, title: title, auto: auto}
		if pr <= 0 {
			snap, err := ghci.Fetch(ctx, run, path, branch)
			switch {
			case err != nil:
				msg.err = err
				return msg
			case snap.PR == nil:
				msg.err = errWTNoPR
				return msg
			}
			pr = snap.PR.Number
		}
		msg.fb, msg.err = ghci.FetchFeedback(ctx, run, path, branch, pr, seen, sent)
		return msg
	}
}

// onWTFeedback shows the card for a read (or says why not).
func (m *Model) onWTFeedback(msg wtFeedbackMsg) tea.Cmd {
	st := m.wt
	delete(st.feedbackReading, msg.path)
	if msg.auto {
		if st.followAt == nil {
			st.followAt = map[string]time.Time{}
		}
		st.followAt[msg.path] = clock.Now()
	}
	switch {
	case msg.err != nil && msg.auto:
		recordError("[worktrees] follow " + msg.path + ": " + msg.err.Error())
		return nil
	case errors.Is(msg.err, errWTNoPR):
		return m.pushToastDetail(toast.Info, "No pull request for "+msg.title, "Open one first; then W sends its review comments and failing CI to the agent.")
	case msg.err != nil && ghci.Quiet(msg.err):
		return m.pushToastDetail(toast.Info, "Could not read the PR feedback", wtGHWhy(msg.err)+".")
	case msg.err != nil:
		return m.pushToastDetail(toast.Warn, "Could not read the PR feedback", msg.err.Error())
	case msg.fb.Empty() && msg.auto:
		return nil
	case msg.fb.Empty():
		return m.pushToastDetail(toast.Info, "Nothing to send for PR #"+strconv.Itoa(msg.fb.PR), "No open review comments, no @termocode mention and no failing CI.")
	case m.confirmOpen || m.connectOpen:
		if msg.auto {
			delete(st.followAt, msg.path) // ask on the next load
			return nil
		}
		return m.pushToastDetail(toast.Info, "Another card is open", "Close it, then press W again.")
	}
	return m.askWTFeedback(msg)
}

// wtFeedbackAgent is the first agent tab of this window in path.
func (m *Model) wtFeedbackAgent(path string) (int, string) {
	bufs, names := m.wtAgentTabsIn([]string{path})
	for i, b := range bufs {
		if b >= 0 {
			return b, names[i]
		}
	}
	return -1, ""
}

// wtFeedbackFile is where the feedback of a worktree's PR is written:
// the git folder (never committed, never in the worktree's files).
func (st *wtState) wtFeedbackFile(path string, pr int) string {
	base := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, filepath.Base(path))
	dir := st.common
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "termocode")
	}
	return filepath.Join(dir, "termocode", "feedback", base+"-pr-"+strconv.Itoa(pr)+".md")
}

// askWTFeedback shows the card (Cancel or Skip first).
func (m *Model) askWTFeedback(msg wtFeedbackMsg) tea.Cmd {
	buf, agent := m.wtFeedbackAgent(msg.path)
	p := &wtFeedbackPending{path: msg.path, title: msg.title, auto: msg.auto, fb: msg.fb, buf: buf, agent: agent}
	m.wt.feedbackAsk = p

	fb := msg.fb
	var b strings.Builder
	b.WriteString("PR #" + strconv.Itoa(fb.PR) + " of " + msg.title + ": " + fb.Summary() + ".\n")
	shown := 0
	for _, t := range fb.Threads {
		for _, c := range t.Comments {
			if fb.Seen[c.ID] || shown == wtFeedbackPreview {
				continue
			}
			place := t.Path
			if t.Line > 0 {
				place += ":" + strconv.Itoa(t.Line)
			}
			b.WriteString("\n  " + place + "  " + c.Author + ": " + wtFeedbackOneLine(c.Body))
			shown++
		}
	}
	for _, c := range fb.Mentions {
		if shown == wtFeedbackPreview {
			break
		}
		b.WriteString("\n  conversation  " + c.Author + ": " + wtFeedbackOneLine(c.Body))
		shown++
	}
	if fb.Run != nil {
		line := "\n  CI  " + fb.Run.Workflow
		if fb.Log.Job != "" {
			line += " · job " + fb.Log.Job
		}
		if fb.Log.Step != "" {
			line += " · step " + fb.Log.Step
		}
		b.WriteString(line)
	}
	file := m.wt.wtFeedbackFile(msg.path, fb.PR)
	b.WriteString("\n\n")
	if buf >= 0 {
		b.WriteString("Send writes all of it (secrets hidden) to " + wtShortPath(file) + " and types one line into " + agent + ": read the file, fix it, commit, do not push.")
	} else {
		b.WriteString("No agent works in this worktree: the feedback goes into the AI chat input, and you press Enter to send it.")
	}
	first := confirm.Button{ID: "cancel", Title: "Cancel"}
	if msg.auto {
		first = confirm.Button{ID: "skip", Title: "Skip"}
		b.WriteString("\n\nSkip marks these as seen; new feedback asks again. W sends everything open at any time.")
	}
	send := confirm.Button{ID: "send", Title: "Send to " + agent, Style: confirm.StylePrimary}
	if buf < 0 {
		send = confirm.Button{ID: "chat", Title: "Put in AI Chat", Style: confirm.StylePrimary}
	}
	m.confirm = confirm.Risky("Send PR feedback to the agent?", b.String(), first, send).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTFeedback
	return nil
}

// wtFeedbackOneLine is a comment as one short line.
func wtFeedbackOneLine(s string) string {
	return runewidth.Truncate(strings.Join(strings.Fields(s), " "), 90, "…")
}

// answerWTFeedback answers the card ("" = Esc = Cancel, or Skip for the
// follow loop).
func (m *Model) answerWTFeedback(id string) tea.Cmd {
	p := m.wt.feedbackAsk
	m.wt.feedbackAsk = nil
	if p == nil {
		return nil
	}
	switch id {
	case "send":
		return m.wtFeedbackSend(p)
	case "chat":
		return m.wtFeedbackChat(p)
	}
	if p.auto { // Skip or Esc: do not ask about these again
		m.wtFeedbackMarkSeen(p)
	}
	return nil
}

// wtFeedbackMarkSeen notes what the card showed.
func (m *Model) wtFeedbackMarkSeen(p *wtFeedbackPending) {
	var run int64
	if p.fb.Run != nil {
		run = p.fb.Run.ID
	}
	ids := p.fb.SeenIDs()
	if err := m.wtTouchMeta(p.path, func(md *worktrees.Meta) { md.MarkFollowSeen(ids, run) }); err != nil {
		recordError("[worktrees] follow seen: " + err.Error())
	}
}

// wtFeedbackSend writes the file and types the line into the agent.
func (m *Model) wtFeedbackSend(p *wtFeedbackPending) tea.Cmd {
	var tab *terminalTab
	for i := range m.terminalTabs {
		if m.terminalTabs[i].BufID == p.buf {
			tab = &m.terminalTabs[i]
		}
	}
	if tab == nil {
		return m.pushToastDetail(toast.Warn, "The agent tab closed", "Nothing was sent; W reads the feedback again.")
	}
	if tab.Hibernated { // AGX-15: the shell is bare; resume first
		m.agentWake(tab)
		return m.pushToastDetail(toast.Info, p.agent+" is starting again", "Nothing was sent yet; press W again in a few seconds.")
	}
	file := m.wt.wtFeedbackFile(p.path, p.fb.PR)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return m.pushToastDetail(toast.Warn, "Could not write the feedback file", err.Error())
	}
	if err := os.WriteFile(file, []byte(p.fb.Prompt()), 0o600); err != nil {
		return m.pushToastDetail(toast.Warn, "Could not write the feedback file", err.Error())
	}
	wtFeedbackType(m, p.buf, ghci.AgentLine(file, p.fb))
	m.wtFeedbackMarkSeen(p)
	return m.pushToastDetail(toast.Info, "Sent PR feedback to "+p.agent, p.fb.Summary())
}

// wtFeedbackChat puts the feedback into the AI chat input.
func (m *Model) wtFeedbackChat(p *wtFeedbackPending) tea.Cmd {
	s := m.aiS()
	if s.streaming || s.chatCancel != nil {
		return m.aiInfo("The AI is still answering: press W again when it is done")
	}
	var open tea.Cmd
	if len(s.conv.Messages) > 0 {
		open = m.newAIChat() // the old chat stays in Chat History
	} else {
		open = m.openAIPanel(true)
	}
	s.input = []rune(p.fb.Prompt())
	s.caret = len(s.input)
	m.wtFeedbackMarkSeen(p)
	return tea.Batch(open, m.pushToastDetail(toast.Info, "Check the message in the chat and press Enter to send it", p.fb.Summary()))
}

// worktreeFollow is w: turn the follow loop on or off.
func (m *Model) worktreeFollow() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	on := !it.e.Meta.Follow
	if on && it.e.Meta.PR <= 0 {
		return m.pushToastDetail(toast.Info, "Link its pull request first", "i links a PR; then w follows its review comments and CI.")
	}
	if err := m.wtTouchMeta(it.e.Path, func(md *worktrees.Meta) { md.Follow = on }); err != nil {
		return m.pushToastDetail(toast.Warn, "The setting could not be saved", err.Error())
	}
	if !on {
		return m.pushToastDetail(toast.Info, "Stopped following PR #"+strconv.Itoa(it.e.Meta.PR), wtTitle(it))
	}
	delete(m.wt.followAt, it.e.Path)
	return m.pushToastDetail(toast.Info, "Following PR #"+strconv.Itoa(it.e.Meta.PR), "New review comments, @termocode mentions and failing CI ask to go to the agent while this view shows.")
}

// wtFollowDue: the follow loop should read path's PR now.
func (st *wtState) wtFollowDue(e worktrees.Entry, now time.Time) bool {
	switch {
	case !e.Meta.Follow || e.Meta.PR <= 0 || e.Gone || st.off,
		e.Meta.State == worktrees.StateArchived || e.Meta.State == worktrees.StateAsleep,
		e.Meta.Link.State == "merged" || e.Meta.Link.State == "closed",
		st.feedbackReading[e.Path]:
		return false
	}
	at, ok := st.followAt[e.Path]
	return !ok || now.Sub(at) >= wtFollowEvery
}

// wtFollowRefresh starts the due follow reads (a few per load).
func (m *Model) wtFollowRefresh(now time.Time) tea.Cmd {
	st := m.wt
	if st == nil || st.off || st.feedbackAsk != nil {
		return nil
	}
	items, _ := m.wtList()
	var cmds []tea.Cmd
	for _, it := range items {
		if len(cmds) == wtFollowMaxReads {
			break
		}
		if st.wtFollowDue(it.e, now) {
			cmds = append(cmds, m.wtFeedbackRead(it, true))
		}
	}
	return tea.Batch(cmds...)
}
