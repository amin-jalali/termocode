package app

import (
	"context"
	"fmt"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/mcp"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

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
	if cwd, err := procRootDir(); err == nil {
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
	users := 0
	for i := range s.conv.Messages[:n] {
		if s.conv.Messages[i].Role == ai.StoredUser {
			users++
		}
	}
	msgs := make([]ai.Message, 1, 2*users+2) // a user and an assistant message per turn
	msgs[0] = ai.Message{Role: ai.RoleSystem, Content: system}
	var tools, text []string // reused from turn to turn (the joins copy)
	flush := func() {
		t := ""
		if len(text) == 1 {
			t = strings.TrimSpace(text[0]) // the usual case: no join
		} else if len(text) > 1 {
			t = strings.TrimSpace(strings.Join(text, ""))
		}
		var c string
		switch {
		case len(tools) > 0 && t != "":
			c = "[tools I ran]\n" + strings.Join(tools, "\n") + "\n\n" + t
		case len(tools) > 0:
			c = "[tools I ran]\n" + strings.Join(tools, "\n")
		default:
			c = t
		}
		if c != "" {
			msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: c})
		}
		tools, text = tools[:0], text[:0]
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
	return m.aiChatToolsBy(root, call, aiRunCaller{})
}

// aiChatToolsBy is aiChatTools for the steps of a run (c says which:
// agent_run_start knows how deep it is and the run's budget).
func (m *Model) aiChatToolsBy(root string, call aiIDECaller, c aiRunCaller) []ai.Tool {
	tools := append(ai.WorkspaceTools(ai.Workspace{Root: root}), m.aiEditorTools()...)
	tools = aiSyncTools(tools, root, call) // the IDE follows file / git changes
	tools = append(tools, aiIDETools(root, call)...)
	// Group K2: settings, layout and IDE state.
	tools = append(tools, aiSettingsTools(root, call)...)
	// K3: the visible integrated terminal.
	tools = append(tools, aiTerminalTools(call)...)
	tools = append(tools, m.aiLSPTools(root, call)...)    // Group K5: LSP hover, symbols, rename, code actions
	tools = append(tools, aiGitTools(root, call)...)      // Group K4
	tools = append(tools, aiTasksTools(root, call)...)    // Group K6: tasks and tests
	tools = append(tools, aiAdminTools(root, call)...)    // Group K8: installers, extensions, workspace folders
	tools = append(tools, aiDebugTools(root, call)...)    // Group K7
	tools = append(tools, m.aiServicesTools(call)...)     // the Services view (ai_services_tools.go)
	tools = append(tools, m.aiContainersTools(call)...)   // CP-9: host containers (ai_containers_tools.go)
	tools = append(tools, aiKubeTools(call)...)           // G-K8S: Kubernetes, read only (ai_kube_tools.go)
	tools = append(tools, aiHelpTools()...)               // U21: the built-in guide (ai_help_tools.go)
	tools = append(tools, aiItermocTools(call)...)        // AH-7: profiles, models, MCP services, outside agents (ai_itermoc_tools.go)
	tools = append(tools, m.aiItermocWriteTools(call)...) // AH-8: switch, drafts, connect, access, start (ai_itermoc_write_tools.go)
	tools = append(tools, m.aiAgentRunTools(call, c)...)  // G-API: agents start agent runs (ai_agent_run_tools.go)
	if m.aiReviewEdits() {                                // Group R2: file changes become diffs to review
		tools = m.aiReviewTools(tools, root, call)
	}
	tools = append(tools, m.aiHTTPTools(root, call, tools)...) // HC-9: HTTP client
	tools = append(tools, aiDBTools(root, call)...)            // DB-5: the Databases view (ai_db_tools.go)
	tools = append(tools, aiSkillTools(root)...)               // OM-4: only when a skill is offered
	tools = append(tools, aiWebTools()...)                     // OMX-13: only when turned on
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
	asked := text
	// "/done <message>" runs this one message with the until-done
	// profile; the active profile stays and the prefix is not stored.
	if rest, ok := strings.CutPrefix(text, aiDonePrefix); ok && strings.TrimSpace(rest) != "" {
		if p, ok := s.profiles.Get(ai.ProfileUntilDone); ok {
			prof, text = p, strings.TrimLeft(rest, " ")
		}
	}
	if prof.Source == ai.ProfileSourceProject && m.aiProfileTrust(prof) != aiTrustUse {
		return m.askAIProfileTrust(prof, aiTrustAfterSend, asked) // F4: a repo's prompts are shown first
	}
	rules := ai.LoadProjectRules(aiWorkspaceRoot()) // OM-1: read once per turn
	if rules.Text != "" && aiRulesTrust(rules) == aiTrustAsk {
		return m.askAIRulesTrust(rules, true, text) // a repo's rules file is shown first
	}
	if sk := aiLoadSkills(aiWorkspaceRoot()); sk.ProjectHash != "" && aiSkillsTrust(sk) == aiTrustAsk {
		return m.askAISkillsTrust(sk, true, text) // OM-4: a repo's skills are shown first
	}
	if hk := aiLoadHooks(aiWorkspaceRoot()); hk.ProjectHash != "" && aiHooksTrust(hk) == aiTrustAsk {
		return m.askAIHooksTrust(hk, true, text) // OMX-12: a repo's commands are shown first
	}
	if out, ok, problem := aiExpandChatCommand(aiWorkspaceRoot(), text); problem != "" {
		m.ai.input = []rune(asked)
		return m.aiInfo(problem)
	} else if ok {
		text = out // OMX-11: a /name command file
	}
	if len(prof.Steps) > 1 || prof.Planned() || prof.HasCLI() { // a CLI step always runs as a run (CS-1)
		return m.startAIRun(text, prof, rules)
	}
	step := prof.Run("").Nodes[0]
	slot, err := s.resolveSlot(step.Slot)
	if err != nil {
		return m.aiProfileProblem(prof.Name, err)
	}
	root := aiWorkspaceRoot()
	memBlock, lessons := m.aiMemoryTurn(root, text) // OM-2: before the attachments are taken
	ctxText, images := s.takeAttachments()
	s.turnMsg = len(s.conv.Messages) // restore points key on the turn's user message
	model := slot.cfg.Model
	if model == s.cfg.Model {
		model = "" // the chat's own model: stored chats stay as they were
	}
	s.conv.Messages = append(s.conv.Messages,
		ai.StoredMessage{Role: ai.StoredUser, Text: text, Context: ctxText, Images: images, Rules: aiRulesLabel(rules), Lessons: lessons},
		ai.StoredMessage{Role: ai.StoredAssistant, Model: model},
	)
	s.scroll = 0

	s.chatGen++
	gen := s.chatGen
	ch := make(chan tea.Msg, 64)

	call := newAIIDECaller(gen, ch)
	tools := aiFilterTools(m.aiChatTools(root, call), step.Tools)
	native := slot.p.SupportsTools()
	base := m.aiChatBasePrompt(root, rules, memBlock) + aiStepSystem(step.System)
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
		Tools:       m.aiRecallTurn(root, text).wrap(aiWithHooks(root, tools, m.output)), // OMX-12, OMX-18
		Native:      native,
		AutoApprove: auto,
		MaxSteps:    s.settings.MaxSteps, // 0: ai.DefaultMaxSteps
		BeforeCalls: aiBeforeCalls(root, call),
		Confirm:     aiChatConfirmFunc(gen, ch, auto, ""),
	}
	crash.GoWith("the AI chat", func() {
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
	}, func() { // the chat turn ends instead of waiting for ever
		ch <- aiChatDoneMsg{gen: gen}
		close(ch)
	})
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
// rules are the project rules read at the turn's start (added only when
// the user trusts them); memory is the turn's lessons block (OM-2), after
// the rules.
func (m *Model) aiChatBasePrompt(root string, rules ai.Rules, memory string) string {
	base := aiChatSystemPrompt + "\nWorkspace root: " + root + m.aiServicesPromptLine(root) + m.aiContainersPromptLine() + aiDBPromptLine(root)
	if p := m.editor.Path(); p != "" {
		base += "\nThe open file is: " + p
	}
	return base + aiRulesPrompt(rules) + memory + aiSkillsPrompt(aiOfferedSkills(aiLoadSkills(root)))
}

// autoApproveSet is ai_auto_approve as a set (one per agent: "Always
// allow" adds to it while the agent runs).
func (s *aiState) autoApproveSet() map[string]bool {
	auto := map[string]bool{}
	for _, c := range s.settings.AutoApprove {
		if aiAutoApproveSaved(c) { // containers: session only (CP-9)
			auto[c] = true
		}
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
			l.Text = s.streamText.add(l.Text, msg.ev.Text)
		} else {
			s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredAssistant, Text: msg.ev.Text})
		}
	case ai.EventToolStart:
		if l := last(); l != nil && l.Role == ai.StoredAssistant && strings.TrimSpace(l.Text) == "" {
			s.conv.Messages = s.conv.Messages[:len(s.conv.Messages)-1]
		}
		line := "⚙ " + msg.ev.Detail
		if msg.ev.Tool == "skill_load" { // OM-4: a small chip, not a tool line
			line = aiSkillChip + aiSkillArgName(msg.ev.Args)
		}
		s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredTool, Text: line})
		s.aiAuditStart(msg.ev)
	case ai.EventToolResult:
		m.aiAuditResult(msg.ev)
		if l := last(); l != nil && l.Role == ai.StoredTool {
			if !strings.HasPrefix(l.Text, aiSkillChip) || strings.HasPrefix(msg.ev.Detail, "error:") {
				l.Text += " → " + aiShortResult(msg.ev.Detail)
			}
		}
		for _, c := range s.aiTakeRecallChips() { // OMX-18
			s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredTool, Text: c})
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

// aiTextAppender appends streamed tokens to a message text without
// copying the whole text for each token: it keeps one growing buffer
// whose String() is the text. When the text was changed elsewhere (it is
// no longer the buffer's string) it starts over from that text.
type aiTextAppender struct{ buf []byte }

// add returns cur+delta. The bytes of a returned string are never
// written again (only bytes after its end are), so it stays valid.
func (a *aiTextAppender) add(cur, delta string) string {
	if len(a.buf) != len(cur) || unsafe.StringData(cur) != unsafe.SliceData(a.buf) {
		a.buf = make([]byte, len(cur), len(cur)+len(delta)+256)
		copy(a.buf, cur)
	}
	a.buf = append(a.buf, delta...)
	return unsafe.String(unsafe.SliceData(a.buf), len(a.buf))
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
	buttons := aiToolCardButtons(msg.tool)
	who := "AI"
	if msg.step != "" {
		who = aiOneLine(aiCleanText(msg.step)) // which step of the run asks
	}
	m.confirm = confirm.New("✦ "+who+" wants to: "+msg.tool.Label(), msg.human, buttons)
	if strings.HasPrefix(msg.tool.Name, "db_") {
		m.confirm = m.confirm.WithScrollText() // the whole SQL, Up/Down scroll
	}
	if msg.tool.Name == "profile_draft" || msg.tool.Name == "model_slot_draft" || msg.tool.Name == "agent_run_start" {
		m.confirm = m.confirm.WithScrollText().WithWideText() // AH-8: the whole draft, indents kept, Up/Down scroll
	}
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAITool
	return waitAIChat(msg.ch)
}

// aiToolCardButtons are the buttons of an agent tool's card. Deny is the
// safe answer: it is never red, it comes first and it has the focus, so
// Enter alone never lets the AI act (AH-1). A database write is styled as
// the danger.
func aiToolCardButtons(t ai.Tool) []confirm.Button {
	allow := confirm.Button{ID: "allow", Title: "Allow", Style: confirm.StylePrimary}
	if title, ok := aiItermocWriteAllow[t.Name]; ok { // AH-8: the button names the action
		allow.Title = title
	}
	if title, ok := aiAgentRunAllow[t.Name]; ok { // G-API
		allow.Title = title
	}
	deny := confirm.Button{ID: "deny", Title: "Deny"}
	var more []confirm.Button
	if t.Class != "" {
		more = append(more, confirm.Button{ID: "always", Title: "Always allow " + t.Class})
	}
	more = append(more, aiServicesConfirmButton(t.Class)...) // "Always in this project"
	if strings.HasPrefix(t.Name, "db_") {
		allow.Style = confirm.StyleDestructive // the card only opens for a write or an analyze
	}
	return append([]confirm.Button{deny, allow}, more...)
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
	if id == "always" && p.tool.Class == ai.ClassContainers {
		// "Always allow containers": this session only, never config.json
		// (a host action can touch another project's containers).
		m.aiContainersAllowSession()
	} else if id == "always" && p.tool.Class != "" {
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
	_, cmd := m.aiSyncIDE("", aiSyncDisk)            // each tool synced already; this catches the rest
	mem := m.aiMemoryAfterTurn()                     // OM-2: lessons, beside the chat
	return tea.Batch(cmd, mem, m.aiStartQueuedRun()) // G-API: a run the AI started waits for this turn
}

// persistAIState stores the open chat id + scroll in session.json.
func (m *Model) persistAIState() {
	s := m.aiS()
	sess := loadSession()
	if len(s.conv.Messages) > 0 {
		sess.AILastChat = s.conv.ID
		s.holdChat(s.conv.ID) // other windows now leave it alone
	} else {
		sess.AILastChat = ""
		s.claim.Release()
		s.claim = nil
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
	if sess.AILastChat == "" || !s.holdChat(sess.AILastChat) {
		return // none, or another window shows it: start a new chat
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
	m.startNewAIChat()
	return m.openAIPanel(true)
}

// startNewAIChat stops the running turn and opens an empty conversation,
// without touching the panel (the web's chat.new keeps the laptop's
// focus where it is).
func (m *Model) startNewAIChat() {
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
	if !s.holdChat(id) {
		return m.aiInfo("This chat is open in another termocode window")
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
