package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/ai"
	"termocode/internal/confirm"
	"termocode/internal/picker"
	"termocode/internal/toast"
)

// Streaming chat (Group A). A turn runs the tool agent on a goroutine;
// every agent event is sent over a channel as a tea.Msg and Update
// re-arms waitAIChat after each one. Messages carry the chat generation
// so a New Chat / Stop drops late events (the goroutine is drained, never
// leaked).

type aiChatEventMsg struct {
	gen uint64
	ev  ai.Event
	ch  chan tea.Msg
}

type aiChatConfirmMsg struct {
	gen   uint64
	tool  ai.Tool
	human string
	reply chan bool
	ch    chan tea.Msg
}

type aiChatDoneMsg struct {
	gen uint64
}

// aiPendingConfirm is a tool call waiting for the user.
type aiPendingConfirm struct {
	tool  ai.Tool
	human string
	reply chan bool
}

func waitAIChat(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

const aiChatSystemPrompt = "You are termocode's coding assistant, running inside a terminal IDE built on Neovim. Be concise and concrete.\n" +
	"When the user asks you to change the OPEN FILE, reply with the COMPLETE updated file in ONE fenced code block " +
	"(real code, not json) — the user reviews it in a side-by-side diff and applies it with one click. " +
	"For questions, answer in prose (short code snippets are fine)."

// aiWorkspaceRoot is the root the agent tools operate on.
func aiWorkspaceRoot() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

// aiChatHistory rebuilds the provider messages from the stored chat (user
// and assistant lines only — tool lines are display-only).
func (m *Model) aiChatHistory(system string) []ai.Message {
	s := m.aiS()
	msgs := []ai.Message{{Role: ai.RoleSystem, Content: system}}
	for _, sm := range s.conv.Messages {
		switch sm.Role {
		case ai.StoredUser:
			c := sm.Text
			if sm.Context != "" {
				c = sm.Context + "\n\n" + sm.Text
			}
			msgs = append(msgs, ai.Message{Role: ai.RoleUser, Content: c})
		case ai.StoredAssistant:
			if strings.TrimSpace(sm.Text) != "" {
				msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: sm.Text})
			}
		}
	}
	return msgs
}

// sendAIChat appends a user message and starts a streamed agent turn.
func (m *Model) sendAIChat(text string) tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c
	}
	s := m.aiS()
	if s.streaming {
		return m.aiInfo("AI is still answering — Ctrl+C to stop")
	}
	var ctxParts []string
	for _, a := range s.attachments {
		ctxParts = append(ctxParts, "Attached "+a.Label+":\n```\n"+a.Text+"\n```")
	}
	s.attachments = nil
	s.conv.Messages = append(s.conv.Messages,
		ai.StoredMessage{Role: ai.StoredUser, Text: text, Context: strings.Join(ctxParts, "\n\n")},
		ai.StoredMessage{Role: ai.StoredAssistant},
	)
	s.scroll = 0

	root := aiWorkspaceRoot()
	tools := append(ai.WorkspaceTools(ai.Workspace{Root: root}), m.aiEditorTools()...)
	native := s.provider.SupportsTools()
	sys := aiChatSystemPrompt + "\nWorkspace root: " + root
	if p := m.editor.Path(); p != "" {
		sys += "\nThe open file is: " + p
	}
	sys = ai.SystemPrompt(sys, tools, native)
	history := m.aiChatHistory(sys)

	auto := map[string]bool{}
	for _, c := range s.settings.AutoApprove {
		auto[c] = true
	}
	s.chatGen++
	gen := s.chatGen
	ctx, cancel := context.WithCancel(context.Background())
	s.chatCancel = cancel
	s.streaming = true
	ch := make(chan tea.Msg, 64)
	s.chatCh = ch
	send := func(msg tea.Msg) {
		select {
		case ch <- msg:
		case <-ctx.Done():
		}
	}
	agent := &ai.Agent{
		Provider:    s.provider,
		Tools:       tools,
		Native:      native,
		AutoApprove: auto,
		Confirm: func(ctx context.Context, t ai.Tool, human string) bool {
			reply := make(chan bool, 1)
			select {
			case ch <- aiChatConfirmMsg{gen: gen, tool: t, human: human, reply: reply, ch: ch}:
			case <-ctx.Done():
				return false
			}
			select {
			case ok := <-reply:
				return ok
			case <-ctx.Done():
				return false
			}
		},
	}
	go func() {
		defer cancel()
		agent.Run(ctx, history, func(ev ai.Event) { send(aiChatEventMsg{gen: gen, ev: ev, ch: ch}) })
		ch <- aiChatDoneMsg{gen: gen}
		close(ch)
	}()
	return waitAIChat(ch)
}

// onAIChatEvent applies one streamed agent event to the transcript.
func (m *Model) onAIChatEvent(msg aiChatEventMsg) tea.Cmd {
	s := m.aiS()
	if msg.gen != s.chatGen {
		return waitAIChat(msg.ch) // stale: keep draining
	}
	last := func() *ai.StoredMessage {
		if n := len(s.conv.Messages); n > 0 {
			return &s.conv.Messages[n-1]
		}
		return nil
	}
	switch msg.ev.Kind {
	case ai.EventText:
		if l := last(); l != nil && l.Role == ai.StoredAssistant {
			l.Text += msg.ev.Text
		} else {
			s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredAssistant, Text: msg.ev.Text})
		}
	case ai.EventToolStart:
		if l := last(); l != nil && l.Role == ai.StoredAssistant && strings.TrimSpace(l.Text) == "" {
			s.conv.Messages = s.conv.Messages[:len(s.conv.Messages)-1]
		}
		s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ " + msg.ev.Detail})
	case ai.EventToolResult:
		if l := last(); l != nil && l.Role == ai.StoredTool {
			l.Text += " → " + aiShortResult(msg.ev.Detail)
		}
	case ai.EventError:
		s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredError, Text: ai.Redact(msg.ev.Detail)})
		recordError("[ai] chat: " + ai.Redact(msg.ev.Detail))
	}
	return waitAIChat(msg.ch)
}

// aiShortResult is a one-line summary of a tool result.
func aiShortResult(s string) string {
	s = strings.TrimSpace(s)
	n := strings.Count(s, "\n") + 1
	first := strings.SplitN(s, "\n", 2)[0]
	if r := []rune(first); len(r) > 48 {
		first = string(r[:48]) + "…"
	}
	if n > 1 {
		return fmt.Sprintf("%s (%d lines)", first, n)
	}
	return first
}

// onAIChatConfirm opens the confirm gate for a mutating tool.
func (m *Model) onAIChatConfirm(msg aiChatConfirmMsg) tea.Cmd {
	s := m.aiS()
	if msg.gen != s.chatGen {
		msg.reply <- false
		return waitAIChat(msg.ch)
	}
	s.confirm = &aiPendingConfirm{tool: msg.tool, human: msg.human, reply: msg.reply}
	buttons := []confirm.Button{{ID: "allow", Title: "Allow", Style: confirm.StylePrimary}}
	if msg.tool.Class != "" {
		buttons = append(buttons, confirm.Button{ID: "always", Title: "Always allow " + msg.tool.Class})
	}
	buttons = append(buttons, confirm.Button{ID: "deny", Title: "Deny", Style: confirm.StyleDestructive})
	m.confirm = confirm.New("✦ AI wants to: "+msg.tool.Name, msg.human, buttons)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAITool
	return waitAIChat(msg.ch)
}

// answerAIToolConfirm replies to the waiting agent.
func (m *Model) answerAIToolConfirm(id string) tea.Cmd {
	s := m.aiS()
	p := s.confirm
	s.confirm = nil
	if p == nil {
		return nil
	}
	ok := id == "allow" || id == "always"
	if id == "always" && p.tool.Class != "" {
		if path, err := ai.ConfigPath(); err == nil {
			cur := ai.LoadSettings(path)
			if !contains(cur.AutoApprove, p.tool.Class) {
				cur.AutoApprove = append(cur.AutoApprove, p.tool.Class)
				_ = ai.SaveSettings(path, cur)
			}
			s.settings.AutoApprove = cur.AutoApprove
		}
	}
	p.reply <- ok
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// onAIChatDone finalises a turn: saves the chat and refreshes anything
// the tools may have changed on disk.
func (m *Model) onAIChatDone(msg aiChatDoneMsg) tea.Cmd {
	s := m.aiS()
	if msg.gen != s.chatGen {
		return nil
	}
	s.streaming = false
	s.chatCancel = nil
	if n := len(s.conv.Messages); n > 0 && s.conv.Messages[n-1].Role == ai.StoredAssistant && strings.TrimSpace(s.conv.Messages[n-1].Text) == "" {
		s.conv.Messages = s.conv.Messages[:n-1]
	}
	if err := s.store.Save(&s.conv); err != nil {
		recordError("[ai] save chat: " + err.Error())
	}
	m.persistAIState()
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	m.explorer.Reload()
	return fetchGitCmd()
}

// persistAIState stores the open chat id + scroll in session.json.
func (m *Model) persistAIState() {
	s := m.aiS()
	sess := loadSession()
	if len(s.conv.Messages) > 0 {
		sess.AILastChat = s.conv.ID
	} else {
		sess.AILastChat = ""
	}
	sess.AIChatScroll = s.scroll
	saveSession(sess)
}

// restoreAIChat loads the last chat + scroll position from session.json.
func (s *aiState) restoreAIChat() {
	if s.restored {
		return
	}
	s.restored = true
	sess := loadSession()
	if sess.AILastChat == "" {
		return
	}
	if c, err := s.store.Load(sess.AILastChat); err == nil {
		s.conv = c
		s.scroll = sess.AIChatScroll
	}
}

// openAIPanel shows (and optionally focuses) the chat panel.
func (m *Model) openAIPanel(focus bool) tea.Cmd {
	s := m.aiS()
	s.restoreAIChat()
	if !m.actionsPanelVisible() {
		m.actionsOpen = true
		m.actionsPinned = true
		m.persistActionsState()
		m.applyLayout()
	}
	if focus {
		s.focused = true
	}
	return nil
}

// toggleAIPanel is the status-badge click.
func (m *Model) toggleAIPanel() tea.Cmd {
	if m.actionsPanelVisible() {
		m.closeActionsPanel()
		m.aiS().focused = false
		m.applyLayout()
		return nil
	}
	return m.openAIPanel(true)
}

// newAIChat starts an empty conversation (the old one stays in history).
func (m *Model) newAIChat() tea.Cmd {
	s := m.aiS()
	if s.chatCancel != nil {
		s.chatCancel()
	}
	s.chatGen++
	s.streaming = false
	s.conv = ai.NewConversation()
	s.attachments = nil
	s.scroll = 0
	s.restored = true
	m.persistAIState()
	return m.openAIPanel(true)
}

// openAIHistory lists stored chats.
func (m *Model) openAIHistory() tea.Cmd {
	s := m.aiS()
	list := s.store.List()
	if len(list) == 0 {
		return m.aiInfo("No saved AI chats yet")
	}
	items := make([]picker.Item, 0, len(list))
	for _, c := range list {
		items = append(items, picker.Item{ID: c.ID, Title: c.Title, Hint: aiAge(c.UpdatedAtMs)})
	}
	m.picker = picker.NewItems(" AI Chat History ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIHistory
	return nil
}

func aiAge(ms int64) string {
	d := time.Since(time.UnixMilli(ms))
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// onAIHistoryPicked loads a stored chat.
func (m *Model) onAIHistoryPicked(id string) tea.Cmd {
	s := m.aiS()
	c, err := s.store.Load(id)
	if err != nil {
		return m.aiToastErr("Could not open chat", err)
	}
	if s.chatCancel != nil {
		s.chatCancel()
	}
	s.chatGen++
	s.streaming = false
	s.conv = c
	s.scroll = 0
	s.restored = true
	m.persistAIState()
	return m.openAIPanel(true)
}

// aiInfo pushes an info toast.
func (m *Model) aiInfo(text string) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, text)
	return c
}

// aiAttachCurrent attaches the selection (or the whole file) to the next
// chat message and opens the panel.
func (m *Model) aiAttachCurrent() tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c
	}
	t, ok := m.aiCaptureTarget(false)
	if !ok {
		return m.aiInfo("Open a file first")
	}
	s := m.aiS()
	label := filepath.Base(t.path)
	var text string
	if t.hadSelection {
		label += fmt.Sprintf(":%d-%d", t.start+1, t.end)
		text = strings.Join(t.full[t.start:t.end], "\n")
	} else {
		text = strings.Join(t.full, "\n")
	}
	const maxAttach = 60 * 1024
	if len(text) > maxAttach {
		text = text[:maxAttach] + "\n… [truncated]"
	}
	s.attachments = append(s.attachments, aiAttachment{Label: label, Text: text})
	m.openAIPanel(true)
	return m.aiInfo("Attached " + label + " to the AI chat")
}

// aiApplyLastCodeBlock proposes the newest code block as the whole file.
func (m *Model) aiApplyLastCodeBlock() tea.Cmd {
	s := m.aiS()
	for i := len(s.conv.Messages) - 1; i >= 0; i-- {
		if s.conv.Messages[i].Role != ai.StoredAssistant {
			continue
		}
		if code, ok := ai.FirstCodeBlock(s.conv.Messages[i].Text); ok {
			return m.proposeWholeFile(code, "chat code block")
		}
	}
	return m.aiInfo("No code block in this chat")
}
