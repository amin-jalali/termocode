package app

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Review in chat (MOB-14). The phone shows an agent worktree's diff
// (ai.worktrees, ai.worktree_text); the user adds notes on lines and
// sends them all at once with ai.worktree_notes. The laptop turns them
// into one chat message: the branch, then per note the file, the line,
// the line's text from the agent's file (quoted, cut and redacted; never
// from a file that may hold secrets) and the note (redacted). The
// message is sent like chat.send, so the agent that did the work reads
// the review in the same chat. A note on a file the agent did not change
// refuses the whole send, so nothing half-sent reaches the AI. git runs
// off the UI thread.

func init() {
	webOps[webproto.OpAIWorktreeNotes] = (*Model).webAIWorktreeNotes
}

// webReviewQuote is the most bytes of a quoted line.
const webReviewQuote = 200

func (m *Model) webAIWorktreeNotes(call *webCall) tea.Cmd {
	a := call.args.(*webproto.AIWorktreeNotesArgs)
	s := m.aiS()
	if !m.webReviewCanSend(call) {
		return nil
	}
	st, mine := s.webAIWorktreeSteps()[a.ID]
	if mine && st.running {
		call.fail(webproto.CodeBusy, "Its step still runs. Send the notes when it ends.")
		return nil
	}
	root := aiWorkspaceRoot()
	call.later()
	return func() tea.Msg {
		fs, err := git.AgentFiles(root, a.ID)
		if err != nil {
			call.fail(webproto.CodeNotFound, "That worktree is gone.")
			return nil
		}
		text, missing := webReviewPrompt(git.AgentBranchPrefix+a.ID, st.title, a.Notes, fs)
		if missing != "" {
			call.fail(webproto.CodeNotFound, "The agent did not change "+missing+".")
			return nil
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if !m.webReviewCanSend(call) {
				return nil
			}
			s := m.aiS()
			if a.ChatID != "" && a.ChatID != s.conv.ID && !m.webOpenStoredChat(call, a.ChatID) {
				return nil
			}
			cmd := m.sendAIChat(text)
			call.reply(webproto.ChatRef{ID: s.conv.ID})
			return cmd
		})
	}
}

// webReviewCanSend: the AI is set up and not answering (as chat.send).
func (m *Model) webReviewCanSend(call *webCall) bool {
	switch {
	case m.aiS().streaming:
		call.fail(webproto.CodeBusy, "The AI is still answering. Stop it or wait.")
		return false
	case !m.aiConfigured():
		call.fail(webproto.CodeConflict, "AI is not set up on the laptop.")
		return false
	}
	return true
}

// webReviewPrompt is the chat message of the notes on the agent's files
// fs (branch, and owner: the step's title or ""); missing names the
// first note's file the agent did not change (then there is no message).
// Pure.
func webReviewPrompt(branch, owner string, notes []webproto.ReviewNote, fs []git.AgentFile) (text, missing string) {
	files := make(map[string]git.AgentFile, len(fs))
	for _, f := range fs {
		files[f.Path] = f
	}
	var b strings.Builder
	b.WriteString("Review notes on the agent's work on branch " + branch)
	if owner = strings.TrimSpace(owner); owner != "" {
		b.WriteString(" (step: " + webReviewOneLine(owner) + ")")
	}
	b.WriteString(":\n")
	for i, n := range notes {
		f, ok := files[n.Path]
		if !ok {
			return "", n.Path
		}
		side, text := "after the agent", f.After
		if n.Side == "old" {
			side, text = "before the agent", f.Before
		}
		fmt.Fprintf(&b, "\n%d. %s line %d (%s)\n", i+1, n.Path, n.Line, side)
		switch line, ok := webReviewLine(text, n.Line); {
		case ai.IsSecretFileName(n.Path):
			b.WriteString("   (the file may hold secrets; the line is not quoted)\n")
		case webAIBinary(f.Before) || webAIBinary(f.After):
			b.WriteString("   (a binary file; the line is not quoted)\n")
		case !ok:
			b.WriteString("   (the file has no line " + strconv.Itoa(n.Line) + ")\n")
		default:
			b.WriteString("   > " + line + "\n")
		}
		for _, l := range strings.Split(strings.TrimSpace(ai.Redact(n.Text)), "\n") {
			b.WriteString("   " + strings.TrimRight(l, " \t\r") + "\n")
		}
	}
	b.WriteString("\nWork on each note in that worktree, then say in short what you changed for each one.")
	return b.String(), ""
}

// webReviewLine is line n (1-based) of text, redacted, on one line and
// cut to webReviewQuote bytes.
func webReviewLine(text string, n int) (string, bool) {
	for i := 1; ; i++ {
		line, rest, more := strings.Cut(text, "\n")
		if i == n {
			if !more && line == "" {
				return "", false // after the last line
			}
			return webReviewOneLine(ai.Redact(line)), true
		}
		if !more {
			return "", false
		}
		text = rest
	}
}

// webReviewOneLine is s as one line of at most webReviewQuote bytes.
func webReviewOneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimRight(s, "\r"))
	out, cut := webChatCut(s, webReviewQuote)
	if cut {
		out += "…"
	}
	return out
}
