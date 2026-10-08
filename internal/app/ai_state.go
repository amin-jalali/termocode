package app

import (
	"context"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/ai"
	"termocode/internal/statusbar"
	"termocode/internal/toast"
)

// ── AI assistant (Group A) ──────────────────────────────────────────────
//
// File map:
//   ai_state.go     — this file: shared state, config load, palette / key
//                     dispatch, status badge, "no provider" degradation
//   ai_lua.go       — nvim side of ghost text (_G._termocode_ai)
//   ai_inline.go    — debounced inline completion requests
//   ai_panel.go     — right-side chat panel rendering, keys, mouse
//   ai_chat.go      — streaming chat turns through the tool agent
//   ai_actions.go   — Explain / Fix / Edit / Doc proposals in the nvim diff
//   ai_config_ui.go — Configure / Pick Model / OAuth sign-in flows
//   ai_tools_app.go — editor-backed agent tools (LSP, buffers)
//
// Every AI entry point calls m.aiReady() first: with no provider it only
// pushes a toast and changes nothing else.

// aiState is the AI feature state. Held by pointer on Model so Bubble Tea
// value copies and background goroutines share one instance.
type aiState struct {
	settings ai.Settings
	cfg      ai.Config
	provider ai.Provider
	loadErr  string

	// inline ghost text
	inlineCancel context.CancelFunc
	inlineBusy   bool

	// chat panel
	store       ai.ChatStore
	conv        ai.Conversation
	input       []rune
	caret       int
	scroll      int // lines scrolled up from the bottom (0 = follow)
	focused     bool
	streaming   bool
	chatGen     uint64
	chatCancel  context.CancelFunc
	chatCh      chan tea.Msg
	attachments []aiAttachment
	restored    bool

	// pending tool confirmation (agent goroutine waits on reply)
	confirm *aiPendingConfirm

	// one-shot code actions (Explain / Fix / Edit / Doc)
	actionGen    uint64
	actionCancel context.CancelFunc
	actionBusy   string
	proposal     *aiProposal
	editTarget   *aiRangeTarget // Edit-with-AI waiting for its instruction

	// configure / sign-in flow
	cfgKind string
	pkce    *ai.PKCE
}

// aiAttachment is file / selection context added to the next chat message.
type aiAttachment struct {
	Label string
	Text  string
}

// newAIState loads settings + credentials and builds the provider.
func newAIState() *aiState {
	s := &aiState{store: ai.DefaultChatStore(), conv: ai.NewConversation()}
	s.reload()
	s.restoreAIChat() // last chat + scroll from session.json
	return s
}

// reload re-reads config.json + credentials.json + env and rebuilds the
// provider. Safe to call any time (e.g. after Configure).
func (s *aiState) reload() {
	s.loadErr = ""
	cfgPath, err := ai.ConfigPath()
	if err == nil {
		s.settings = ai.LoadSettings(cfgPath)
	}
	credPath, err := ai.CredentialsPath()
	var creds ai.Credentials
	if err == nil {
		creds, err = ai.LoadCredentials(credPath)
		if err != nil {
			s.loadErr = err.Error()
		}
	}
	s.cfg = ai.Resolve(s.settings, creds, os.Getenv)
	if s.cfg.Kind == ai.KindClaudeOAuth && creds.ClaudeOAuth != nil && credPath != "" {
		src := &ai.TokenSource{Path: credPath}
		s.cfg.Token = src.Token
	}
	s.provider = ai.NewProvider(s.cfg)
}

// ai returns the AI state, creating it on first use (tests build Model
// literals without New()).
func (m *Model) aiS() *aiState {
	if m.ai == nil {
		m.ai = newAIState()
	}
	return m.ai
}

// aiConfigured reports whether a provider is ready.
func (m *Model) aiConfigured() bool {
	s := m.aiS()
	return s.provider != nil && s.provider.Configured()
}

// aiReady is the guard every AI entry point calls. Without a provider it
// pushes the "configure AI" toast and returns ok=false — nothing else in
// the editor changes.
func (m *Model) aiReady() (tea.Cmd, bool) {
	if m.aiConfigured() {
		return nil, true
	}
	detail := "Run \"AI: Configure Provider…\" or set ANTHROPIC_API_KEY / OPENAI_API_KEY (or TERMOCODE_AI_PROVIDER=ollama)."
	if s := m.aiS(); s.loadErr != "" {
		detail = ai.Redact(s.loadErr) + "\n" + detail
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "AI is not configured", detail)
	return c, false
}

// aiToastErr shows a redacted AI error toast (also lands in errors.log,
// which redacts again).
func (m *Model) aiToastErr(title string, err error) tea.Cmd {
	if err == nil {
		return nil
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Errr, title, ai.Redact(err.Error()))
	return c
}

// aiModelLabel is the chat model shown in the badge / panel header.
func (s *aiState) modelLabel() string {
	if s.cfg.Model != "" {
		return s.cfg.Model
	}
	return ai.KindLabel(s.cfg.Kind)
}

// aiStatus fills the status-bar badge fields.
func (m Model) aiStatus() (string, statusbar.AIState) {
	s := m.ai
	if s == nil || m.zenMode {
		return "", statusbar.AIOff
	}
	if s.provider == nil || !s.provider.Configured() {
		return "AI", statusbar.AIUnconfigured
	}
	if s.streaming || s.inlineBusy || s.actionBusy != "" {
		return s.modelLabel(), statusbar.AIStreaming
	}
	return s.modelLabel(), statusbar.AIIdle
}

// hitStatusAIBadge reports whether (x, y) lands on the status-bar badge.
func (m Model) hitStatusAIBadge(x, y int) bool {
	if m.zenMode || y != m.h-1 {
		return false
	}
	bx, bw := m.statusBarRect()
	sb := m.status
	sb.SetWidth(bw)
	x0, x1, ok := sb.AIBadgeSpan(m.statusState())
	return ok && x >= bx+x0 && x < bx+x1
}

// onAIBadgeClick: unconfigured → Configure; otherwise toggle the panel.
func (m *Model) onAIBadgeClick() tea.Cmd {
	if !m.aiConfigured() {
		return m.openAIConfigure()
	}
	return m.toggleAIPanel()
}

// refreshAIHighlights re-pushes the theme's AI colors into nvim.
func (m *Model) refreshAIHighlights() {
	if m.nvim != nil {
		_ = m.nvim.ExecLua(aiColorsLua())
	}
}

// aiInlineEnabled is the effective inline toggle.
func (m *Model) aiInlineEnabled() bool {
	return m.aiS().settings.InlineEnabled() && m.aiConfigured()
}

// syncAIInline mirrors the inline toggle into nvim.
func (m *Model) syncAIInline() {
	if m.nvim != nil {
		_ = m.nvim.ExecLua(aiEnableLua(m.aiInlineEnabled()))
	}
}

// aiAfterAttach runs at the end of attachCmd (nvim goroutine): colors +
// enable flag. Reads only.
func (m Model) aiAfterAttach() {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.ExecLua(aiColorsLua())
	on := false
	if s := m.ai; s != nil {
		on = s.settings.InlineEnabled() && s.provider != nil && s.provider.Configured()
	}
	_ = m.nvim.ExecLua(aiEnableLua(on))
}

// ── Palette ─────────────────────────────────────────────────────────────

// aiPaletteItems are appended to the command palette (Group A).
func aiPaletteItems() []paletteEntry {
	return []paletteEntry{
		{"ai-open-chat", "AI: Open Chat", "Alt+A"},
		{"ai-new-chat", "AI: New Chat", ""},
		{"ai-history", "AI: Chat History…", ""},
		{"ai-explain", "AI: Explain Selection / Line", ""},
		{"ai-fix", "AI: Fix Diagnostic at Cursor", ""},
		{"ai-edit", "AI: Edit Selection…", "Alt+I"},
		{"ai-doc", "AI: Generate Doc Comment", ""},
		{"ai-attach", "AI: Add File / Selection to Chat", ""},
		{"ai-apply-code", "AI: Apply Last Code Block to File", ""},
		{"ai-apply-proposal", "AI: Apply Proposed Edit", ""},
		{"ai-discard-proposal", "AI: Discard Proposed Edit", ""},
		{"ai-trigger-inline", "AI: Trigger Inline Completion", "Alt+\\"},
		{"ai-toggle-inline", "AI: Toggle Inline Completions", "Alt+|"},
		{"ai-configure", "AI: Configure Provider…", ""},
		{"ai-pick-model", "AI: Pick Model…", ""},
		{"ai-auto-approve", "AI: Agent Auto-Approve…", ""},
		{"ai-sign-in-claude", "AI: Sign in with Claude (OAuth, opt-in)…", ""},
		{"ai-sign-out-claude", "AI: Sign out of Claude", ""},
		{"ai-credentials", "Open Config: AI Credentials", ""},
		{"ai-cancel", "AI: Stop Generating", ""},
	}
}

// paletteEntry is a tiny tuple so the list above stays one line per row.
type paletteEntry struct{ id, title, hint string }

// dispatchAIPalette runs an AI palette / menu ID. ok=false when not ours.
func (m *Model) dispatchAIPalette(id string) (tea.Cmd, bool) {
	if !strings.HasPrefix(id, "ai-") {
		return nil, false
	}
	// Without a provider every AI entry only shows the "configure" toast;
	// the configure / sign-in / credentials entries are the way out.
	switch id {
	case "ai-configure", "ai-sign-in-claude", "ai-sign-out-claude", "ai-credentials",
		"ai-cancel", "ai-discard-proposal", "ai-history", "ai-pick-model", "ai-auto-approve":
	default:
		if c, ok := m.aiReady(); !ok {
			return c, true
		}
	}
	switch id {
	case "ai-open-chat":
		return m.openAIPanel(true), true
	case "ai-new-chat":
		return m.newAIChat(), true
	case "ai-history":
		return m.openAIHistory(), true
	case "ai-explain":
		return m.aiExplain(), true
	case "ai-fix":
		return m.aiFixDiagnostic(), true
	case "ai-edit":
		return m.aiEditSelection(), true
	case "ai-doc":
		return m.aiDocComment(), true
	case "ai-attach":
		return m.aiAttachCurrent(), true
	case "ai-apply-code":
		return m.aiApplyLastCodeBlock(), true
	case "ai-apply-proposal":
		return m.applyAIProposal(), true
	case "ai-discard-proposal":
		return m.discardAIProposal(true), true
	case "ai-trigger-inline":
		return m.aiTriggerInline(), true
	case "ai-toggle-inline":
		return m.aiToggleInline(), true
	case "ai-configure":
		return m.openAIConfigure(), true
	case "ai-pick-model":
		return m.openAIModelPicker(), true
	case "ai-auto-approve":
		return m.openAIAutoApprovePicker(), true
	case "ai-sign-in-claude":
		return m.startClaudeSignIn(), true
	case "ai-sign-out-claude":
		return m.claudeSignOut(), true
	case "ai-credentials":
		return m.editAICredentials(), true
	case "ai-cancel":
		return m.aiCancelAll(), true
	}
	return nil, false
}

// aiCancelAll stops chat streaming, one-shot actions and inline requests.
func (m *Model) aiCancelAll() tea.Cmd {
	s := m.aiS()
	stopped := s.streaming || s.actionBusy != ""
	if s.chatCancel != nil {
		s.chatCancel()
	}
	if s.actionCancel != nil {
		s.actionCancel()
		s.actionBusy = ""
		s.actionGen++
	}
	m.cancelAIInline()
	if !stopped {
		return nil
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, "AI stopped")
	return c
}

// ── Settings rows (settings_ui.go reads settingsRows) ──────────────────

func init() {
	settingsRows = append(settingsRows,
		settingsRow{ID: "setting-ai-inline", Label: "ai_inline", Kind: "bool"},
		settingsRow{ID: "setting-ai-provider", Label: "ai_provider", Kind: "string"},
		settingsRow{ID: "setting-ai-model", Label: "ai_model", Kind: "string"},
		settingsRow{ID: "setting-ai-base-url", Label: "ai_base_url", Kind: "string"},
	)
}

// aiSettingsCategories puts the AI rows in their own Settings group.
func aiSettingsCategories(cats map[string]string) map[string]string {
	for _, id := range []string{"setting-ai-inline", "setting-ai-provider", "setting-ai-model", "setting-ai-base-url"} {
		cats[id] = "AI"
	}
	return cats
}

// aiSettingValue renders an AI row's current value. ok=false: not ours.
func aiSettingValue(id string) (string, bool) {
	path, _ := ai.ConfigPath()
	s := ai.LoadSettings(path)
	switch id {
	case "setting-ai-inline":
		if s.InlineEnabled() {
			return "true", true
		}
		return "false", true
	case "setting-ai-provider":
		if s.Provider == "" {
			return "(auto)", true
		}
		return s.Provider, true
	case "setting-ai-model":
		if s.Model == "" {
			return "(default)", true
		}
		return s.Model, true
	case "setting-ai-base-url":
		if s.BaseURL == "" {
			return "(default)", true
		}
		return s.BaseURL, true
	}
	return "", false
}

// applyAISetting persists one AI row and reloads the provider. ok=false
// when the row is not an AI one.
func (m *Model) applyAISetting(id, value string) (tea.Cmd, bool) {
	path, err := ai.ConfigPath()
	if err != nil {
		return nil, false
	}
	s := ai.LoadSettings(path)
	value = strings.TrimSpace(value)
	if value == "(auto)" || value == "(default)" {
		value = ""
	}
	var label string
	switch id {
	case "setting-ai-inline":
		b, err := parseBoolLoose(value)
		if err != nil {
			var c tea.Cmd
			m.toast, c = m.toast.Push(toast.Errr, "ai_inline: expected true/false")
			return c, true
		}
		s.Inline = &b
		label = "ai_inline = " + value
	case "setting-ai-provider":
		v := strings.ToLower(value)
		if v != "" && v != "none" && ai.KindLabel(v) == "None" {
			var c tea.Cmd
			m.toast, c = m.toast.PushDetail(toast.Errr, "ai_provider: unknown provider", strings.Join(ai.Kinds, ", ")+", none")
			return c, true
		}
		s.Provider = v
		label = "ai_provider = " + orAuto(v)
	case "setting-ai-model":
		s.Model = value
		label = "ai_model = " + orAuto(value)
	case "setting-ai-base-url":
		s.BaseURL = value
		label = "ai_base_url = " + orAuto(value)
	default:
		return nil, false
	}
	if err := ai.SaveSettings(path, s); err != nil {
		return m.aiToastErr("Settings error", err), true
	}
	m.aiS().reload()
	m.syncAIInline()
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, label)
	return c, true
}

func orAuto(s string) string {
	if s == "" {
		return "(default)"
	}
	return s
}
