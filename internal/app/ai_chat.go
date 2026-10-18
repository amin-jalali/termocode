package app

import (
	"context"
	"fmt"
	"github.com/amin-jalali/termocode/internal/mcp"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
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
	// step is the title of the pipeline step that asks ("" in a plain
	// chat turn).
	step string
}

type aiChatDoneMsg struct {
	gen uint64
	// run is the final event of a pipeline run (nil for a plain turn).
	run *agentrun.Event
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

// aiChatHistory rebuilds the provider messages from the stored chat. The
// tool lines of a turn ("⚙ Create a.md → created a.md") go into that
// turn's assistant message, so after a step-limit pause "continue" knows
// what was done already.
func (m *Model) aiChatHistory(system string) []ai.Message {
	return m.aiChatHistoryN(system, len(m.aiS().conv.Messages))
}

// aiChatHistoryN is aiChatHistory of the first n stored messages. A
// pipeline run's step tool lines count as the turn's tools.
func (m *Model) aiChatHistoryN(system string, n int) []ai.Message {
	s := m.aiS()
	msgs := []ai.Message{{Role: ai.RoleSystem, Content: system}}
	var tools, text []string
	flush := func() {
		var parts []string
		if len(tools) > 0 {
			parts = append(parts, "[tools I ran]\n"+strings.Join(tools, "\n"))
		}
		if t := strings.TrimSpace(strings.Join(text, "")); t != "" {
			parts = append(parts, t)
		}
		if len(parts) > 0 {
			msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: strings.Join(parts, "\n\n")})
		}
		tools, text = nil, nil
	}
	for _, sm := range s.conv.Messages[:n] {
		switch sm.Role {
		case ai.StoredUser:
			flush()
			c := sm.Text
			if sm.Context != "" {
				c = sm.Context + "\n\n" + sm.Text
			}
			um := ai.Message{Role: ai.RoleUser, Content: c}
			if len(sm.Images) > 0 {
				if s.provider != nil && ai.SupportsImages(s.provider) {
					imgs, note := aiHistoryImages(sm.Images)
					um.Images = imgs
					if note != "" {
						um.Content = note + "\n\n" + um.Content
					}
				} else {
					um.Content = "(images not sent: provider has no image input: " + strings.Join(sm.Images, ", ") + ")\n\n" + um.Content
				}
			}
			msgs = append(msgs, um)
		case ai.StoredTool:
			tools = append(tools, sm.Text)
		case ai.StoredAssistant:
			for _, st := range sm.Steps {
				tools = append(tools, st.Tools...)
			}
			text = append(text, sm.Text)
		}
	}
	flush()
	return msgs
}

// aiChatTools is every agent tool of a chat turn. The docs reference
// (AIToolCatalog, cmd/docgen) is built from this list too, so a new tool
// group goes here, with one appended line.
func (m *Model) aiChatTools(root string, call aiIDECaller) []ai.Tool {
	tools := append(ai.WorkspaceTools(ai.Workspace{Root: root}), m.aiEditorTools()...)
	tools = aiSyncTools(tools, root, call) // the IDE follows file / git changes
	tools = append(tools, aiIDETools(root, call)...)
	// Group K2: settings, layout and IDE state.
	tools = append(tools, aiSettingsTools(root, call)...)
	// K3: the visible integrated terminal.
	tools = append(tools, aiTerminalTools(call)...)
	tools = append(tools, m.aiLSPTools(root, call)...) // Group K5: LSP hover, symbols, rename, code actions
	tools = append(tools, aiGitTools(root, call)...)   // Group K4
	tools = append(tools, aiTasksTools(root, call)...) // Group K6: tasks and tests
	tools = append(tools, aiAdminTools(root, call)...) // Group K8: installers, extensions, workspace folders
	tools = append(tools, aiDebugTools(root, call)...) // Group K7
	tools = append(tools, m.aiServicesTools(call)...)  // the Services view (ai_services_tools.go)
	if m.aiReviewEdits() {                             // Group R2: file changes become diffs to review
		tools = m.aiReviewTools(tools, root, call)
	}
	tools = append(tools, m.aiHTTPTools(root, call, tools)...) // HC-9: HTTP client
	return aiRedactResults(tools)                              // known secrets (.env passwords, API keys) never reach the model
}

// sendAIChat appends a user message and starts a streamed agent turn.
// The active profile decides how: one step is the plain chat turn below
// (the default profile is exactly today's chat); more steps run as a
// pipeline (ai_runs.go).
func (m *Model) sendAIChat(text string) tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c
	}
	s := m.aiS()
	if s.streaming {
		return m.aiInfo("AI is still answering — Ctrl+C to stop")
	}
	prof := s.activeProfile()
	if prof.Source == ai.ProfileSourceProject && m.aiProfileTrust(prof) != aiTrustUse {
		return m.askAIProfileTrust(prof, aiTrustAfterSend, text) // F4: a repo's prompts are shown first
	}
	if len(prof.Steps) > 1 || prof.Planned() {
		return m.startAIRun(text, prof)
	}
	step := prof.Run("").Nodes[0]
	slot, err := s.resolveSlot(step.Slot)
	if err != nil {
		return m.aiProfileProblem(prof.Name, err)
	}
	ctxText, images := s.takeAttachments()
	s.turnMsg = len(s.conv.Messages) // restore points key on the turn's user message
	s.conv.Messages = append(s.conv.Messages,
		ai.StoredMessage{Role: ai.StoredUser, Text: text, Context: ctxText, Images: images},
		ai.StoredMessage{Role: ai.StoredAssistant},
	)
	s.scroll = 0

	s.chatGen++
	gen := s.chatGen
	ch := make(chan tea.Msg, 64)

	root := aiWorkspaceRoot()
	call := newAIIDECaller(gen, ch)
	tools := aiFilterTools(m.aiChatTools(root, call), step.Tools)
	native := slot.p.SupportsTools()
	base := m.aiChatBasePrompt(root) + aiStepSystem(step.System)
	history := m.aiChatHistory(ai.SystemPrompt(base, tools, native))
	mcpTools := m.aiMCPTurn(gen, ch) // MCP servers' tools, built on the goroutine
	mcpLines := ""
	if mcpTools != nil {
		mcpLines = m.mcpPromptLines() // "GitHub is connected as … (read only)."
	}

	auto := s.autoApproveSet()
	ctx, cancel := context.WithCancel(context.Background())
	s.chatCancel = cancel
	s.streaming = true
	s.act.begin(clock.Now())
	s.chatCh = ch
	send := func(msg tea.Msg) {
		select {
		case ch <- msg:
		case <-ctx.Done():
		}
	}
	agent := &ai.Agent{
		Provider:    slot.p,
		Tools:       tools,
		Native:      native,
		AutoApprove: auto,
		MaxSteps:    s.settings.MaxSteps, // 0: ai.DefaultMaxSteps
		BeforeCalls: aiBeforeCalls(root, call),
		Confirm:     aiChatConfirmFunc(gen, ch, auto, ""),
	}
	go func() {
		defer cancel()
		if mcpTools != nil {
			if extra := aiFilterTools(mcpTools(ctx), step.Tools); len(extra) > 0 {
				agent.Tools = append(agent.Tools, extra...)
				b := base
				if mcpLines != "" {
					b += "\nConnected services (MCP tools mcp__<server>__*):\n" + mcpLines
				}
				history[0].Content = ai.SystemPrompt(b, agent.Tools, native)
			}
		}
		agent.Run(ctx, history, func(ev ai.Event) { send(aiChatEventMsg{gen: gen, ev: ev, ch: ch}) })
		ch <- aiChatDoneMsg{gen: gen}
		close(ch)
	}()
	return waitAIChat(ch)
}

// takeAttachments returns the attachments as the user message's context
// and image paths, and clears them.
func (s *aiState) takeAttachments() (string, []string) {
	var ctxParts, images []string
	for _, a := range s.attachments {
		if a.Image != "" {
			images = append(images, a.Image)
			ctxParts = append(ctxParts, "Attached image "+a.Label+" (sent as an image).")
			continue
		}
		ctxParts = append(ctxParts, "Attached "+a.Label+":\n```\n"+a.Text+"\n```")
	}
	s.attachments = nil
	return strings.Join(ctxParts, "\n\n"), images
}

// aiChatBasePrompt is the chat's system prompt before the tool protocol.
func (m *Model) aiChatBasePrompt(root string) string {
	base := aiChatSystemPrompt + "\nWorkspace root: " + root + m.aiServicesPromptLine(root)
	if p := m.editor.Path(); p != "" {
		base += "\nThe open file is: " + p
	}
	return base
}

// autoApproveSet is ai_auto_approve as a set (one per agent: "Always
// allow" adds to it while the agent runs).
func (s *aiState) autoApproveSet() map[string]bool {
	auto := map[string]bool{}
	for _, c := range s.settings.AutoApprove {
		auto[c] = true
	}
	return auto
}

// aiChatConfirmFunc asks the user through the chat channel to allow a
// mutating tool call. auto is the agent's auto-approve set: "Always
// allow" adds the class for the rest of the agent's turn (it is only
// read on the agent's goroutine). step names the pipeline step that asks
// ("" in a plain turn).
func aiChatConfirmFunc(gen uint64, ch chan tea.Msg, auto map[string]bool, step string) ai.ConfirmFunc {
	return func(ctx context.Context, t ai.Tool, human string) bool {
		reply := make(chan bool, 1)
		select {
		case ch <- aiChatConfirmMsg{gen: gen, tool: t, human: human, reply: reply, ch: ch, step: step}:
		case <-ctx.Done():
			return false
		}
		select {
		case ok := <-reply:
			// "Always allow" saved the class before replying.
			if ok && t.Class != "" {
				if p, err := ai.ConfigPath(); err == nil && contains(ai.LoadSettings(p).AutoApprove, t.Class) {
					auto[t.Class] = true
				}
			}
			return ok
		case <-ctx.Done():
			return false
		}
	}
}

// onAIChatEvent applies one streamed agent event to the transcript.
func (m *Model) onAIChatEvent(msg aiChatEventMsg) tea.Cmd {
	s := m.aiS()
	if msg.gen != s.chatGen {
		return waitAIChat(msg.ch) // stale: keep draining
	}
	s.streamSteps++
	s.act.onEvent(msg.ev, clock.Now())
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
		s.aiAuditStart(msg.ev)
	case ai.EventToolResult:
		m.aiAuditResult(msg.ev)
		if l := last(); l != nil && l.Role == ai.StoredTool {
			l.Text += " → " + aiShortResult(msg.ev.Detail)
		}
	case ai.EventError:
		s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredError, Text: ai.Redact(msg.ev.Detail)})
		recordError("[ai] chat: " + ai.Redact(msg.ev.Detail))
		m.aiAuditUsage(msg.ev)
	case ai.EventDone:
		m.aiAuditUsage(msg.ev)
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
	buttons = append(buttons, aiServicesConfirmButton(msg.tool.Class)...) // "Always in this project"
	buttons = append(buttons, confirm.Button{ID: "deny", Title: "Deny", Style: confirm.StyleDestructive})
	who := "AI"
	if msg.step != "" {
		who = aiOneLine(aiCleanText(msg.step)) // which step of the run asks
	}
	m.confirm = confirm.New("✦ "+who+" wants to: "+msg.tool.Label(), msg.human, buttons)
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
	ok := id == "allow" || id == "always" || id == "project"
	if id == "project" { // services: "Always in this project" (ai_services_tools.go)
		m.aiServicesAllowProject()
	}
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
	if msg.run != nil {
		m.finishAIRun(*msg.run)
	}
	s.streaming = false
	s.chatCancel = nil
	if n := len(s.conv.Messages); n > 0 && s.conv.Messages[n-1].Role == ai.StoredAssistant && s.conv.Messages[n-1].Run == nil && strings.TrimSpace(s.conv.Messages[n-1].Text) == "" {
		s.conv.Messages = s.conv.Messages[:n-1]
	}
	if err := s.store.Save(&s.conv); err != nil {
		recordError("[ai] save chat: " + err.Error())
	}
	m.aiRestoreTurnDone()
	m.persistAIState()
	_, cmd := m.aiSyncIDE("", aiSyncDisk) // each tool synced already; this catches the rest
	return cmd
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
	s.profile = sess.AIProfile // the workspace's agent profile (U20 AP-5)
	if sess.AILastChat == "" {
		return
	}
	if c, err := s.store.Load(sess.AILastChat); err == nil {
		aiMarkInterrupted(&c)
		s.conv = c
		s.scroll = sess.AIChatScroll
	}
}

// openAIPanel shows (and optionally focuses) the chat panel.
func (m *Model) openAIPanel(focus bool) tea.Cmd {
	s := m.aiS()
	s.restoreAIChat()
	if mcp.LoadSettings(mcpConfigPath()).Enabled {
		m.aiMCP() // reads mcp.json (starts nothing): the header chip knows the services
	}
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
		m.cancelAIReviewTurn()
	}
	s.chatGen++
	s.streaming = false
	s.run = nil
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
	d := clock.Since(time.UnixMilli(ms))
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
		m.cancelAIReviewTurn()
	}
	s.chatGen++
	s.streaming = false
	s.run = nil
	aiMarkInterrupted(&c)
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
	if len(text) > aiMaxAttach {
		text = text[:aiMaxAttach] + "\n… [truncated]"
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
