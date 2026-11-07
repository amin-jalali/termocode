package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// AI git helpers (GITX-8). Generate Commit Message and Stash with AI
// Message ask the AI once and put its answer where the user checks it (the
// message box, the stash prompt): nothing is committed or stashed
// without the user. Explain Changes, Explain Commit, Explain Line's
// Commit and Write PR Description fill in the chat box; the user reads
// it and presses Enter to send.

const (
	promptKindGitAIStash promptKindEnum = 3660

	gitAIDiffMax   = 48 << 10 // bytes of diff sent for a message
	gitAIChatMax   = 24 << 10 // bytes of diff put in the chat box
	gitAITimeout   = 60 * time.Second
	gitAIMaxTokens = 400
)

// gitAIReplyMsg is the AI's answer for kind "commit" or "stash".
type gitAIReplyMsg struct {
	kind, text string
	err        error
}

func (m *Model) gitAINeedsSetup() tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Warn, "Set up AI first", "Run AI: Configure Provider…, then try again")
	return c
}

// gitAIMessagePrompt asks for a commit (or stash) message for diff, in
// the style of the recent subjects.
func gitAIMessagePrompt(kind, diff string, cut bool, recent []string) []ai.Message {
	sys := "You write git commit messages. Answer with the message only: no quotes, no code fence, no comments. " +
		"First line: a short summary in the imperative mood, at most 72 characters. " +
		"If the change needs it, add a blank line and a short body that says why. Follow the style of the recent subjects."
	if kind == "stash" {
		sys = "You name git stashes. Answer with one short line (at most 60 characters) that says what the work in progress is. " +
			"No quotes, no code fence."
	}
	var b strings.Builder
	if len(recent) > 0 {
		b.WriteString("Recent subjects in this repository:\n")
		for _, s := range recent {
			subject, _, _ := strings.Cut(s, "\n")
			b.WriteString("- " + subject + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("The changes (git diff):\n")
	b.WriteString(diff)
	if cut {
		b.WriteString("\n[the diff was cut here: it is longer]\n")
	}
	return []ai.Message{{Role: ai.RoleSystem, Content: sys}, {Role: ai.RoleUser, Content: b.String()}}
}

// gitAICleanReply drops a code fence, quotes and leading "Commit message:"
// text around the answer.
func gitAICleanReply(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if i := strings.IndexByte(s, '\n'); i >= 0 && !strings.Contains(s[:i], " ") {
			s = s[i+1:] // a language tag
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	s = strings.TrimSpace(s)
	for _, p := range []string{"Commit message:", "commit message:", "Message:"} {
		s = strings.TrimSpace(strings.TrimPrefix(s, p))
	}
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '`' && s[len(s)-1] == '`') {
		s = s[1 : len(s)-1]
	}
	return git.CleanMessage(s)
}

// gitAIAsk sends the diff (staged first when staged is true and there
// is a staged change) and returns the reply as a gitAIReplyMsg.
func (m *Model) gitAIAsk(kind string, staged bool) tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	if !m.aiConfigured() {
		return m.gitAINeedsSetup()
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	n, changed := m.gitFileCounts()
	if n+changed == 0 {
		return m.aiInfo("No changes to describe")
	}
	useStaged := staged && n > 0
	diff, cut, err := git.ChangesDiff(cwd, useStaged, gitAIDiffMax)
	if err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Errr, "Could not read the changes", err.Error())
		return c
	}
	recent, _ := git.RecentMessages(cwd, 10)
	s := m.aiS()
	prov, model := s.provider, s.cfg.InlineModel
	msgs := gitAIMessagePrompt(kind, diff, cut, recent)
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, "Asking the AI…")
	return tea.Batch(c, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gitAITimeout)
		defer cancel()
		text, last := ai.Collect(prov.Stream(ctx, ai.Request{Messages: msgs, Model: model, MaxTokens: gitAIMaxTokens}))
		return gitAIReplyMsg{kind: kind, text: text, err: last.Err}
	})
}

// onGitAIReply puts the answer where the user checks it.
func (m *Model) onGitAIReply(msg gitAIReplyMsg) tea.Cmd {
	var c tea.Cmd
	text := gitAICleanReply(msg.text)
	if msg.err != nil || text == "" {
		detail := "The answer was empty"
		if msg.err != nil {
			detail = msg.err.Error()
		}
		m.toast, c = m.toast.PushDetail(toast.Errr, "The AI did not answer", detail)
		return c
	}
	switch msg.kind {
	case "commit":
		m.gitSetCommitMsg(text)
		m.gitRevealCommitBox()
		m.toast, c = m.toast.PushDetail(toast.Info, "The AI wrote a message", "Check it, then commit. Up brings your old text back")
	case "stash":
		subject, _, _ := strings.Cut(text, "\n")
		m.prompt = prompt.New("Stash with AI Message", "Stash message (Enter stashes, Esc keeps the changes):", subject)
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindGitAIStash
	}
	return c
}

// handleGitAIPromptSubmit stashes with the checked message.
func (m *Model) handleGitAIPromptSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindGitAIStash {
		return nil, false
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil, true
	}
	var c tea.Cmd
	if err := git.Stash(cwd, strings.TrimSpace(value)); err != nil {
		m.toast, c = m.toast.PushDetail(toast.Warn, "Stash failed", err.Error())
		return c, true
	}
	m.toast, c = m.toast.PushDetail(toast.Info, "Stashed changes", strings.TrimSpace(value))
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	return tea.Batch(fetchGitCmd(), c), true
}

// gitAIPrefillChat opens the chat (a new one when the current has
// messages) with text in the input box, unsent.
func (m *Model) gitAIPrefillChat(text string) tea.Cmd {
	s := m.aiS()
	if s.streaming || s.chatCancel != nil {
		return m.aiInfo("The AI is still answering: try again when it is done")
	}
	var open tea.Cmd
	if len(s.conv.Messages) > 0 {
		open = m.newAIChat() // the old chat stays in Chat History
	} else {
		open = m.openAIPanel(true)
	}
	s.input = []rune(text)
	s.caret = len(s.input)
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, "Check the message in the chat and press Enter to send it")
	return tea.Batch(open, c)
}

func gitAIFence(body string, cut bool) string {
	if cut {
		body += "\n[cut here: the rest is longer]"
	}
	return "```diff\n" + strings.TrimRight(body, "\n") + "\n```"
}

// gitAIExplainChanges asks the AI to explain the working-tree changes.
func (m *Model) gitAIExplainChanges() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	diff, cut, err := git.ChangesDiff(cwd, false, gitAIChatMax)
	if err != nil || strings.TrimSpace(diff) == "" {
		return m.aiInfo("No changes to explain")
	}
	return m.gitAIPrefillChat("Explain my uncommitted changes: what they do, and anything that looks wrong or unfinished.\n\n" + gitAIFence(diff, cut))
}

// gitAIExplainCommit asks the AI to explain one commit.
func (m *Model) gitAIExplainCommit(hash string) tea.Cmd {
	cwd, err := m.gitRootDir()
	if err != nil || hash == "" {
		return nil
	}
	body, err := git.ShowCommit(cwd, hash)
	if err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Errr, "Could not read the commit", err.Error())
		return c
	}
	cut := len(body) > gitAIChatMax
	if cut {
		body = body[:gitAIChatMax]
	}
	return m.gitAIPrefillChat(fmt.Sprintf("Explain commit %s: what it changes and why, in a few short points.\n\n%s", hash, gitAIFence(body, cut)))
}

// gitAIExplainLineCommit explains the commit that last changed the
// cursor line.
func (m *Model) gitAIExplainLineCommit() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, rel, ok := m.gitLineTarget()
	if !ok {
		return m.aiInfo("Open a file of the repository first")
	}
	bl, err := git.BlameLineAt(cwd, rel, max(m.cursorLine, 1))
	if err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Errr, "Blame failed", err.Error())
		return c
	}
	if bl.Hash == "" || strings.Trim(bl.Hash, "0") == "" {
		return m.aiInfo("This line is not committed yet")
	}
	return m.gitAIExplainCommit(bl.Hash)
}

// gitAIBase is the branch a PR would go to: main, master, trunk or
// develop, the first one that exists.
func gitAIBase(local []string, current string) string {
	for _, b := range []string{"main", "master", "trunk", "develop"} {
		for _, l := range local {
			if l == b && b != current {
				return b
			}
		}
	}
	return ""
}

// gitAIPRText asks the AI for a PR title and description of the branch.
func (m *Model) gitAIPRText() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	local, _ := git.Branches(cwd)
	base := gitAIBase(local, m.gitBranch.Name)
	if base == "" {
		return m.aiInfo("Switch to a feature branch first: no main, master, trunk or develop branch to compare with")
	}
	subjects, err := git.BranchSubjects(cwd, base)
	if err != nil || len(subjects) == 0 {
		return m.aiInfo("No commits on " + m.gitBranch.Name + " that " + base + " does not have")
	}
	stat, _ := runGitOut(cwd, "diff", "--stat", "--no-color", base+"...HEAD")
	var b strings.Builder
	fmt.Fprintf(&b, "Write a pull request title and description for branch %s into %s. Title on the first line (at most 72 characters), then a blank line, then the description: what changes, why, and how to test it.\n\nCommits:\n", m.gitBranch.Name, base)
	for _, s := range subjects {
		b.WriteString("- " + s + "\n")
	}
	if strings.TrimSpace(stat) != "" {
		b.WriteString("\nFiles:\n" + gitAIFence(stat, false))
	}
	return m.gitAIPrefillChat(b.String())
}
