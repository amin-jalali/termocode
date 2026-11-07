package app

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// Diff comments → agent (CMP-51). In a changes card (an agent
// worktree's changes, the AI review, a file diff) c leaves a comment on
// the focused change and S sends every comment of the card in ONE
// message: to the built-in AI chat, or pasted into an outside agent's
// terminal tab (Claude Code, Codex, …), or copied. The send card is
// Cancel first and shows the whole message. Esc on the card keeps the
// comments; Agent: Send Diff Comments… sends them later. The AI never
// sends them itself (policy never).

const (
	promptKindDiffComment   promptKindEnum  = 5151
	confirmKindDiffComments confirmKindEnum = 5152
	diffCommentsPaletteID                   = "agent-diff-comments"
	// diffCommentsTabsMax caps the agent tab buttons on the send card.
	diffCommentsTabsMax = 3
)

// diffNotesState is the UI side: the change the open prompt is for, the
// comments of a hidden card, and the open send card.
type diffNotesState struct {
	ask       diffview.CommentAskMsg
	kept      []diffview.Comment
	keptTitle string
	send      *diffCommentsSend
}

// diffCommentsSend is the open send card: the message and where each
// tab button goes.
type diffCommentsSend struct {
	text     string
	n        int
	fromCard bool           // the comments are on the open card (else kept)
	tabs     map[string]int // button id → the tab's buffer
}

func (m *Model) diffNotesS() *diffNotesState {
	s := m.aiS()
	if s.diffNotes == nil {
		s.diffNotes = &diffNotesState{}
	}
	return s.diffNotes
}

func init() {
	aiActionPolicies[diffCommentsPaletteID] = aiNever("it sends the user's own review comments to an agent: only the user sends them, after the preview")
}

// onDiffCommentAsk opens the comment prompt for one change.
func (m *Model) onDiffCommentAsk(msg diffview.CommentAskMsg) tea.Cmd {
	m.diffNotesS().ask = msg
	m.prompt = prompt.New("Comment · "+msg.Where, "Your comment on this change:", msg.Text)
	m.prompt.SetAllowEmpty(true)
	m.prompt.SetNote("Enter saves it on the card. Empty removes it. S on the card sends every comment to an agent in one message.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindDiffComment
	return nil
}

// onDiffCommentEntered saves the prompt's text on the card.
func (m *Model) onDiffCommentEntered(value string) tea.Cmd {
	s := m.aiS()
	if s.review == nil {
		return m.aiInfo("The changes card is closed; the comment was not saved")
	}
	a := m.diffNotesS().ask
	s.review.card.SetComment(a.File, a.Hunk, value)
	m.reviewOpen = true
	return nil
}

// diffCommentsHidden keeps the comments of a read-only card that goes
// away (Esc), so they can still be sent. It returns the toast.
func (m *Model) diffCommentsHidden(card diffview.Model) tea.Cmd {
	cs := card.Comments()
	if len(cs) == 0 {
		return nil
	}
	st := m.diffNotesS()
	st.kept, st.keptTitle = cs, card.Title()
	return m.aiInfo(fmt.Sprintf("%d diff comment%s kept · Agent: Send Diff Comments… sends them", len(cs), plural(len(cs))))
}

// openDiffCommentsSend opens the send card for the open card's comments
// (S), or for the kept ones (palette).
func (m *Model) openDiffCommentsSend(fromCard bool) tea.Cmd {
	var cs []diffview.Comment
	title := ""
	if r := m.aiS().review; fromCard && r != nil {
		cs, title = r.card.Comments(), r.card.Title()
	} else {
		fromCard = false
		st := m.diffNotesS()
		cs, title = st.kept, st.keptTitle
	}
	if len(cs) == 0 {
		return m.aiInfo("No diff comments yet: press c on a change in a changes card")
	}
	text := ai.Redact(diffview.FormatComments(title, cs))
	snd := &diffCommentsSend{text: text, n: len(cs), fromCard: fromCard, tabs: map[string]int{}}
	buttons := []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "chat", Title: "AI chat", Style: confirm.StylePrimary},
	}
	for _, t := range m.agentCtlTabs() {
		if t.t.Agent == "" || len(snd.tabs) == diffCommentsTabsMax {
			continue
		}
		id := "tab:" + strconv.Itoa(t.t.BufID)
		snd.tabs[id] = t.t.BufID
		name := t.t.agentTitle()
		if t.t.Name != "" && t.t.Name != name {
			name += " · " + t.t.Name
		}
		buttons = append(buttons, confirm.Button{ID: id, Title: runewidth.Truncate(name, 24, "…")})
	}
	buttons = append(buttons, confirm.Button{ID: "copy", Title: "Copy"})
	m.diffNotesS().send = snd
	where := "the AI chat"
	if len(snd.tabs) > 0 {
		where += " or an agent tab (pasted, you press Enter there)"
	}
	var list strings.Builder
	for i, c := range cs {
		if i == 5 {
			fmt.Fprintf(&list, "  … %d more\n", len(cs)-i)
			break
		}
		line := c.Path + " (" + c.Lines + "): " + strings.Join(strings.Fields(ai.Redact(c.Text)), " ")
		list.WriteString("  • " + runewidth.Truncate(line, 100, "…") + "\n")
	}
	body := fmt.Sprintf("Send %d comment%s in one message to %s:\n%s\nThe message:\n\n%s", snd.n, plural(snd.n), where, list.String(), text)
	m.confirm = confirm.New(fmt.Sprintf("Send %d diff comment%s to an agent?", snd.n, plural(snd.n)), body, buttons).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindDiffComments
	return nil
}

// answerDiffComments runs the send card's answer ("" or cancel: nothing
// happens, the comments stay).
func (m *Model) answerDiffComments(id string) tea.Cmd {
	st := m.diffNotesS()
	snd := st.send
	st.send = nil
	if snd == nil || id == "" || id == "cancel" {
		return nil
	}
	switch {
	case id == "copy":
		m.diffCommentsSent(snd, false)
		return m.copyWithToast(snd.text, fmt.Sprintf("%d diff comment%s copied", snd.n, plural(snd.n)))
	case id == "chat":
		return m.diffCommentsToChat(snd)
	}
	buf, ok := snd.tabs[id]
	if !ok {
		return nil
	}
	return m.diffCommentsToTab(snd, buf)
}

// diffCommentsToChat sends the message to the AI chat; while the AI
// answers it waits in the chat input instead.
func (m *Model) diffCommentsToChat(snd *diffCommentsSend) tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c // the comments stay
	}
	m.diffCommentsSent(snd, true)
	open := m.openAIPanel(true)
	if m.aiS().streaming {
		m.aiSetInput(snd.text)
		return tea.Batch(open, m.aiInfo("The AI is still answering: the comments wait in the chat input"))
	}
	return tea.Batch(open, m.sendAIChat(snd.text))
}

// diffCommentsToTab pastes the message into an agent tab (bracketed
// paste, no Enter: the user reads it in the agent and sends it).
func (m *Model) diffCommentsToTab(snd *diffCommentsSend, buf int) tea.Cmd {
	var tab *terminalTab
	for _, t := range m.agentCtlTabs() {
		if t.t.BufID == buf {
			tab = t.t
		}
	}
	if tab == nil {
		return m.aiInfo("That agent tab closed; the comments stay")
	}
	if tab.Hibernated { // AGX-15: wake it first (agent_hibernate.go)
		m.agentWake(tab)
		return m.aiInfo("The agent was hibernated and is starting again: send the comments again in a few seconds")
	}
	if m.nvim == nil {
		return m.aiInfo("The editor is not running; the comments stay")
	}
	var tick int
	if err := m.nvim.ExecLuaResult(aiTermSendLua, &tick, buf, diffCommentsPaste(snd.text)); err != nil || tick < 0 {
		return m.aiInfo("Could not type into the agent tab; the comments stay")
	}
	m.diffCommentsSent(snd, true)
	name := tab.agentTitle()
	return m.aiInfo(fmt.Sprintf("%d comment%s pasted into %s: press Enter there to send", snd.n, plural(snd.n), name))
}

// diffCommentsPaste wraps text in a bracketed paste, so a multi-line
// message lands in the agent's input box as one paste.
func diffCommentsPaste(text string) string {
	text = strings.ReplaceAll(text, "\x1b", "") // no escape can end the paste early
	return "\x1b[200~" + text + "\x1b[201~"
}

// diffCommentsSent drops the sent comments; hide also closes a
// read-only card (the agent's answer is the next thing to look at).
func (m *Model) diffCommentsSent(snd *diffCommentsSend, hide bool) {
	st := m.diffNotesS()
	r := m.aiS().review
	if !snd.fromCard || r == nil {
		st.kept, st.keptTitle = nil, ""
		return
	}
	r.card.ClearComments()
	if hide {
		m.reviewOpen = false
		if !r.card.Review() {
			m.aiS().review = nil
		}
	}
}
