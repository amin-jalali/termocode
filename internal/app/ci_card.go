package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// G-CI: the "CI and Pull Request" card (Git: CI and Pull Request…, a
// click on the CI chip). It shows the branch's pull request, its review
// state and checks, and offers Fix with AI (on red: the failing log goes
// into the chat input for the user to send), Open Log (a read-only
// page) and Open in Browser (copied over SSH).

const confirmKindCI confirmKindEnum = 3100

// ciGHHint is the one quiet hint when gh can not be used.
const ciGHHint = "Install and sign in to gh to see CI."

func ciPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "git-ci", Title: "Git: CI and Pull Request…"},
		{ID: "git-ci-fix", Title: "Git: Fix CI with AI"},
	}
}

// dispatchCIPalette runs the CI palette ids.
func (m *Model) dispatchCIPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "git-ci":
		return m.openCICard(), true
	case "git-ci-fix":
		return m.ciFixWithAI(), true
	}
	return nil, false
}

// ciCardText is the card body.
func ciCardText(w *ciWatch) string {
	var b strings.Builder
	head := "Branch " + w.branch
	if w.snap.HeadSHA != "" {
		head += " · commit " + shortSHA(w.snap.HeadSHA)
	}
	b.WriteString(head + "\n\n")
	switch {
	case w.err != nil && ghci.Quiet(w.err):
		if w.err == ghci.ErrNoGitHub {
			b.WriteString("This repository has no GitHub remote.")
		} else {
			b.WriteString(ciGHHint)
		}
		return b.String()
	case !w.loaded:
		b.WriteString("Checking CI…")
		return b.String()
	case w.err != nil:
		b.WriteString("Could not reach GitHub: " + w.err.Error() + "\n\n")
	}
	if pr := w.snap.PR; pr != nil {
		fmt.Fprintf(&b, "Pull request #%d · %s\n", pr.Number, pr.Title)
		state := ciTitle(pr.State)
		if pr.Draft {
			state = "Draft"
		}
		if r := ciReviewText(pr.Review); r != "" {
			state += " · " + r
		}
		b.WriteString(state + "\n")
	} else {
		b.WriteString("No pull request for this branch.\n")
	}
	checks := ciChecks(w.snap)
	if len(checks) == 0 {
		b.WriteString("\nNo CI runs for this branch yet.")
		return b.String()
	}
	b.WriteString("\nChecks\n")
	for _, c := range checks {
		b.WriteString(ciMark(c.State) + " " + c.Name + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ciChecks are the PR's checks, or the runs when there is no PR.
func ciChecks(s ghci.Snapshot) []ghci.Check {
	if s.PR != nil && len(s.PR.Checks) > 0 {
		return s.PR.Checks
	}
	var out []ghci.Check
	for _, r := range s.Runs {
		st := ghci.StatePass
		switch {
		case r.Pending():
			st = ghci.StatePending
		case r.Failed():
			st = ghci.StateFail
		case r.Conclusion != "success":
			st = ghci.StateNone
		}
		out = append(out, ghci.Check{Name: r.Workflow, State: st, URL: r.URL})
	}
	return out
}

func ciMark(s ghci.State) string {
	switch s {
	case ghci.StatePass:
		return "✓"
	case ghci.StateFail:
		return "✗"
	case ghci.StatePending:
		return "…"
	}
	return "–"
}

func ciTitle(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func ciReviewText(r string) string {
	switch r {
	case "APPROVED":
		return "Approved"
	case "CHANGES_REQUESTED":
		return "Changes requested"
	case "REVIEW_REQUIRED":
		return "Review required"
	}
	return ""
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// ciURL is the page Open in Browser shows: the PR, else the first
// failed run, else the newest run.
func ciURL(s ghci.Snapshot) string {
	if s.PR != nil && s.PR.URL != "" {
		return s.PR.URL
	}
	if f := s.FailedRuns(); len(f) > 0 {
		return f[0].URL
	}
	if len(s.Runs) > 0 {
		return s.Runs[0].URL
	}
	return ""
}

// openCICard opens the card and asks gh for fresh data; the card
// updates when the answer comes.
func (m *Model) openCICard() tea.Cmd {
	m.gitEnsureLoaded()
	w := m.ciW()
	if !m.gitIsRepo || m.gitBranch.Name == "" {
		return m.aiInfo("CI needs a git branch: open a folder with a git repository")
	}
	var cmd tea.Cmd
	if w.branch != m.gitBranch.Name {
		w.branch, w.snap, w.err, w.loaded = m.gitBranch.Name, ghci.Snapshot{}, nil, false
	}
	cmd = m.ciTrigger()
	m.showCICard()
	return cmd
}

// showCICard (re)draws the card from the watcher state.
func (m *Model) showCICard() {
	w := m.ciW()
	var buttons []confirm.Button
	red := w.snap.State() == ghci.StateFail
	if red {
		buttons = append(buttons, confirm.Button{ID: "fix", Title: "Fix with AI", Style: confirm.StylePrimary},
			confirm.Button{ID: "log", Title: "Open Log"})
	}
	if ciURL(w.snap) != "" {
		buttons = append(buttons, confirm.Button{ID: "browser", Title: "Open in Browser"})
	}
	buttons = append(buttons, confirm.Button{ID: "close", Title: "Close"})
	m.confirm = confirm.New("CI and Pull Request", ciCardText(w), buttons).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindCI
}

// ciCardOpen reports the card on screen.
func (m *Model) ciCardOpen() bool { return m.confirmOpen && m.confirmKind == confirmKindCI }

// handleCIConfirm answers the card's buttons.
func (m *Model) handleCIConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindCI {
		return nil, false
	}
	switch id {
	case "fix":
		return m.ciFixWithAI(), true
	case "log":
		return m.ciFetchLog(false), true
	case "browser":
		return m.ciOpenBrowser(), true
	}
	return nil, true
}

// ciLogMsg is a fetched failed log: for the chat (fix) or the preview.
type ciLogMsg struct {
	fix    bool
	branch string
	run    ghci.Run
	log    ghci.FailedLog
	err    error
}

// ciFetchLog reads the failed log of the first failed run.
func (m *Model) ciFetchLog(fix bool) tea.Cmd {
	fetch := m.ciLogCmd(fix)
	if fetch == nil {
		return m.aiInfo("CI is not red on " + m.ciW().branch)
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, "Reading the CI log…")
	return tea.Batch(c, fetch)
}

// ciLogCmd runs `gh run view --log-failed` off the main loop; nil when
// no run failed.
func (m *Model) ciLogCmd(fix bool) tea.Cmd {
	w := m.ciW()
	failed := w.snap.FailedRuns()
	dir, err := m.rootDir()
	if len(failed) == 0 || err != nil {
		return nil
	}
	r, branch, run := failed[0], w.branch, w.runner()
	return func() tea.Msg {
		fl, err := ghci.FetchFailedLog(context.Background(), run, dir, r.ID)
		return ciLogMsg{fix: fix, branch: branch, run: r, log: fl, err: err}
	}
}

// ciLogPage is the text of the log preview.
func ciLogPage(msg ciLogMsg) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s", msg.run.Workflow, msg.log.Job)
	if msg.log.Step != "" {
		b.WriteString(" · " + msg.log.Step)
	}
	if msg.run.URL != "" {
		b.WriteString("\n" + msg.run.URL)
	}
	b.WriteString("\n\n" + strings.Join(msg.log.All, "\n"))
	return b.String()
}

// ciFixWithAI fills the chat with the failing log (Alt+C, the card's
// Fix with AI). The user reviews it and presses Enter.
func (m *Model) ciFixWithAI() tea.Cmd {
	w := m.ciW()
	if w.snap.State() != ghci.StateFail {
		return m.openCICard()
	}
	return m.ciFetchLog(true)
}

// onCILog shows a fetched log.
func (m *Model) onCILog(msg ciLogMsg) tea.Cmd {
	if msg.err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Errr, "Could not read the CI log", msg.err.Error())
		return c
	}
	if !msg.fix {
		m.focus = FocusEditor
		m.ensureEditorWindowCurrent()
		if err := m.openReadOnlyPage("CI log: "+msg.run.Workflow+" #"+strconv.FormatInt(msg.run.ID, 10), ciLogPage(msg), "", "log"); err != nil {
			m.err = "CI log: " + err.Error()
		}
		return nil
	}
	s := m.aiS()
	if s.streaming || s.chatCancel != nil {
		return m.aiInfo("The AI is still answering: try Fix with AI again when it is done")
	}
	var open tea.Cmd
	if len(s.conv.Messages) > 0 {
		open = m.newAIChat() // the old chat stays in Chat History
	} else {
		open = m.openAIPanel(true)
	}
	s.input = []rune(ghci.FixPrompt(msg.branch, msg.run, msg.log))
	s.caret = len(s.input)
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, "Check the message in the chat and press Enter to send it")
	return tea.Batch(open, c)
}

// ciOpenBrowser opens the PR (or run) page; over SSH it copies the link.
func (m *Model) ciOpenBrowser() tea.Cmd { return m.openLink(ciURL(m.ciW().snap)) }

// openLink opens a page in the browser; over SSH (or with no browser)
// it copies the link instead.
func (m *Model) openLink(url string) tea.Cmd {
	if url == "" {
		return nil
	}
	var c tea.Cmd
	if mcpOverSSH() {
		m.copyToClipboard(url)
		m.toast, c = m.toast.PushDetail(toast.Info, "Copied the link", url)
		return c
	}
	if err := osOpen(url); err != nil {
		m.copyToClipboard(url)
		m.toast, c = m.toast.PushDetail(toast.Info, "No browser: copied the link", url)
		return c
	}
	m.toast, c = m.toast.PushDetail(toast.Info, "Opened", url)
	return c
}
