package app

import (
	"context"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Group A overlay kinds. Defined here with a high base (instead of in the
// shared enums) so parallel groups adding kinds never collide.
const (
	pickerKindAIProvider pickerKindEnum = 900 + iota
	pickerKindAIModel
	pickerKindAIHistory
	pickerKindAIAutoApprove
)

const (
	promptKindAIEdit promptKindEnum = 900 + iota
	promptKindAIKey
	promptKindAIModel
	promptKindAIBaseURL
	promptKindAIOAuthCode
)

const (
	confirmKindAITool confirmKindEnum = 900 + iota
	confirmKindAIProposal
	confirmKindAIOAuth
)

// handleAIPickerSelect routes the AI picker kinds. ok=false: not ours.
func (m *Model) handleAIPickerSelect(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindAIProvider:
		return m.onAIProviderPicked(id), true
	case pickerKindAIModel:
		return m.onAIModelPicked(id), true
	case pickerKindAIHistory:
		return m.onAIHistoryPicked(id), true
	case pickerKindAIAutoApprove:
		return m.onAIAutoApproveToggled(id), true
	}
	return nil, false
}

// handleAIPromptSubmit routes the AI prompt kinds. ok=false: not ours.
func (m *Model) handleAIPromptSubmit(value string) (tea.Cmd, bool) {
	switch m.promptKind {
	case promptKindAIEdit:
		return m.onAIEditInstruction(value), true
	case promptKindAIKey:
		return m.onAIKeyEntered(value), true
	case promptKindAIModel:
		return m.saveAIModel(strings.TrimSpace(value)), true
	case promptKindAIBaseURL:
		return m.onAIBaseURLEntered(value), true
	case promptKindAIOAuthCode:
		return m.onClaudeCodePasted(value), true
	}
	return nil, false
}

// handleAIConfirm routes the AI confirm kinds. ok=false: not ours.
func (m *Model) handleAIConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindAITool:
		return m.answerAIToolConfirm(id), true
	case confirmKindAIProposal:
		return m.onAIProposalConfirm(id), true
	case confirmKindAIOAuth:
		if id == "continue" {
			return m.beginClaudeOAuth(), true
		}
		return nil, true
	}
	return nil, false
}

// onAIConfirmClosed handles Esc on an AI dialog: a tool call is denied,
// a proposal stays open for review.
func (m *Model) onAIConfirmClosed() tea.Cmd {
	switch m.confirmKind {
	case confirmKindAITool:
		return m.answerAIToolConfirm("deny")
	case confirmKindAIProposal:
		return m.onAIProposalConfirm("review")
	}
	return nil
}

// ── Configure provider ─────────────────────────────────────────────────

// openAIConfigure lists the provider kinds.
func (m *Model) openAIConfigure() tea.Cmd {
	cur := m.aiS().cfg.Kind
	items := make([]picker.Item, 0, len(ai.Kinds)+1)
	for _, k := range ai.Kinds {
		hint := ai.DefaultBaseURL(k)
		if k == cur {
			hint = "active"
		}
		items = append(items, picker.Item{ID: k, Title: ai.KindLabel(k), Hint: hint})
	}
	items = append(items, picker.Item{ID: "none", Title: "Turn AI off", Hint: ""})
	m.picker = picker.NewItems(" ✦ AI Provider ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIProvider
	return nil
}

func (m *Model) saveAISettings(mut func(*ai.Settings)) error {
	path, err := ai.ConfigPath()
	if err != nil {
		return err
	}
	s := ai.LoadSettings(path)
	mut(&s)
	if err := ai.SaveSettings(path, s); err != nil {
		return err
	}
	m.aiS().reload()
	m.syncAIInline()
	return nil
}

func (m *Model) onAIProviderPicked(kind string) tea.Cmd {
	s := m.aiS()
	if kind == "none" {
		if err := m.saveAISettings(func(st *ai.Settings) { st.Provider = "none" }); err != nil {
			return m.aiToastErr("Settings error", err)
		}
		return m.aiInfo("AI turned off")
	}
	if kind == ai.KindClaudeOAuth {
		return m.startClaudeSignIn()
	}
	s.cfgKind = kind
	if err := m.saveAISettings(func(st *ai.Settings) {
		if st.Provider != kind {
			st.Model, st.BaseURL, st.InlineModel = "", "", ""
		}
		st.Provider = kind
	}); err != nil {
		return m.aiToastErr("Settings error", err)
	}
	switch kind {
	case ai.KindOllama, ai.KindLMStudio:
		return m.openAIModelPicker()
	case ai.KindCompatible:
		m.prompt = prompt.New("✦ AI base URL", "OpenAI-compatible base URL (…/v1):", s.settings.BaseURL)
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindAIBaseURL
		return nil
	}
	return m.openAIKeyPrompt(kind)
}

func (m *Model) openAIKeyPrompt(kind string) tea.Cmd {
	m.aiS().cfgKind = kind
	m.prompt = prompt.New("✦ "+ai.KindLabel(kind)+" API key", "API key (stored 0600 in ai/credentials.json; empty = keep):", "")
	m.prompt.SetMasked(true)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAIKey
	return nil
}

func (m *Model) onAIBaseURLEntered(v string) tea.Cmd {
	v = strings.TrimSpace(v)
	if err := m.saveAISettings(func(st *ai.Settings) { st.BaseURL = v }); err != nil {
		return m.aiToastErr("Settings error", err)
	}
	return m.openAIKeyPrompt(ai.KindCompatible)
}

func (m *Model) onAIKeyEntered(v string) tea.Cmd {
	s := m.aiS()
	kind := s.cfgKind
	v = strings.TrimSpace(v)
	if v != "" {
		path, err := ai.CredentialsPath()
		if err != nil {
			return m.aiToastErr("Credentials error", err)
		}
		creds, _ := ai.LoadCredentials(path)
		if creds.Keys == nil {
			creds.Keys = map[string]string{}
		}
		creds.Keys[kind] = v
		if err := ai.SaveCredentials(path, creds); err != nil {
			return m.aiToastErr("Credentials error", err)
		}
		s.reload()
		m.syncAIInline()
	}
	return m.openAIModelPicker()
}

// openAIModelPicker offers the suggested models + Custom….
func (m *Model) openAIModelPicker() tea.Cmd {
	s := m.aiS()
	kind := s.cfg.Kind
	if kind == "" {
		kind = s.settings.Provider
	}
	if kind == "" || kind == "none" {
		return m.openAIConfigure()
	}
	var items []picker.Item
	for _, id := range ai.SuggestedModels(kind) {
		hint := ""
		if id == s.cfg.Model {
			hint = "active"
		} else if id == ai.DefaultModel(kind) {
			hint = "default"
		}
		items = append(items, picker.Item{ID: id, Title: id, Hint: hint})
	}
	items = append(items, picker.Item{ID: "__custom", Title: "Custom model id…"})
	m.picker = picker.NewItems(" ✦ AI Model ("+ai.KindLabel(kind)+") ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIModel
	return nil
}

func (m *Model) onAIModelPicked(id string) tea.Cmd {
	if id == "__custom" {
		m.prompt = prompt.New("✦ AI model", "Model id:", m.aiS().cfg.Model)
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindAIModel
		return nil
	}
	return m.saveAIModel(id)
}

func (m *Model) saveAIModel(id string) tea.Cmd {
	if err := m.saveAISettings(func(st *ai.Settings) { st.Model = id }); err != nil {
		return m.aiToastErr("Settings error", err)
	}
	if !m.aiConfigured() {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "AI model saved", "The provider still needs a key — run AI: Configure Provider…")
		return c
	}
	return m.aiInfo("✦ AI ready: " + m.aiS().modelLabel())
}

// ── Auto-approve ───────────────────────────────────────────────────────

func (m *Model) openAIAutoApprovePicker() tea.Cmd {
	s := m.aiS()
	desc := map[string]string{
		ai.ClassFileWrite: "write / create / delete / rename files",
		ai.ClassGit:       "stage / commit / checkout / push / pull",
		ai.ClassRun:       "run shell commands",
	}
	var items []picker.Item
	for _, c := range ai.ToolClasses {
		state := "ask"
		if contains(s.settings.AutoApprove, c) {
			state = "AUTO"
		}
		items = append(items, picker.Item{ID: c, Title: c + " — " + desc[c], Hint: state})
	}
	m.picker = picker.NewItems(" ✦ Agent auto-approve (Enter toggles) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIAutoApprove
	return nil
}

func (m *Model) onAIAutoApproveToggled(class string) tea.Cmd {
	on := false
	err := m.saveAISettings(func(st *ai.Settings) {
		var next []string
		for _, c := range st.AutoApprove {
			if c != class {
				next = append(next, c)
			}
		}
		if len(next) == len(st.AutoApprove) {
			next = append(next, class)
			on = true
		}
		st.AutoApprove = next
	})
	if err != nil {
		return m.aiToastErr("Settings error", err)
	}
	if on {
		return m.aiInfo("AI agent: " + class + " runs without asking")
	}
	return m.aiInfo("AI agent: " + class + " asks first")
}

// ── Credentials file ───────────────────────────────────────────────────

func (m *Model) editAICredentials() tea.Cmd {
	path, err := ai.CredentialsPath()
	if err != nil {
		return m.aiToastErr("Credentials error", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := ai.SaveCredentials(path, ai.Credentials{Keys: map[string]string{}}); err != nil {
			return m.aiToastErr("Credentials error", err)
		}
	}
	_ = os.Chmod(path, 0o600)
	if m.nvim != nil {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`vim.cmd('edit ' .. vim.fn.fnameescape(...))
vim.bo.undofile = false
vim.bo.swapfile = false
vim.api.nvim_create_autocmd('BufWritePost', { buffer = 0, once = false, callback = function(ev) pcall(vim.loop.fs_chmod, ev.file, 384) end })`, path)
	}
	m.focus = FocusEditor
	return m.aiInfo("Edit keys, save, then run AI: Configure Provider… (or restart)")
}

// ── Claude OAuth (opt-in) ──────────────────────────────────────────────

// aiOAuthDoneMsg ends the code exchange.
type aiOAuthDoneMsg struct {
	tokens ai.OAuthTokens
	err    error
}

// startClaudeSignIn shows the ToS note first.
func (m *Model) startClaudeSignIn() tea.Cmd {
	m.confirm = confirm.New("✦ Sign in with Claude?", ai.ClaudeOAuthToSNote, []confirm.Button{
		{ID: "continue", Title: "Continue", Style: confirm.StylePrimary},
		{ID: "cancel", Title: "Use an API key instead"},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIOAuth
	return nil
}

// beginClaudeOAuth opens the browser and asks for the pasted code.
func (m *Model) beginClaudeOAuth() tea.Cmd {
	p, err := ai.NewPKCE()
	if err != nil {
		return m.aiToastErr("Sign-in failed", err)
	}
	m.aiS().pkce = &p
	u := p.AuthorizeURL()
	_ = defaultOpen(u)
	m.copyToClipboard(u)
	m.prompt = prompt.New("✦ Sign in with Claude", "Browser opened (URL also copied). Paste the code here:", "")
	m.prompt.SetMasked(true)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAIOAuthCode
	return nil
}

func (m *Model) onClaudeCodePasted(v string) tea.Cmd {
	s := m.aiS()
	p := s.pkce
	if p == nil {
		return m.aiInfo("No sign-in in progress")
	}
	code, state, ok := ai.ParseCallback(v)
	if !ok {
		return m.aiInfo("That does not look like an authorization code")
	}
	if state != "" && state != p.State {
		s.pkce = nil
		return m.aiInfo("State mismatch — start the sign-in again")
	}
	if state == "" {
		state = p.State
	}
	verifier := p.Verifier
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		t, err := ai.OAuthClient{}.Exchange(ctx, code, state, verifier)
		return aiOAuthDoneMsg{tokens: t, err: err}
	}
}

func (m *Model) onAIOAuthDone(msg aiOAuthDoneMsg) tea.Cmd {
	s := m.aiS()
	s.pkce = nil
	if msg.err != nil {
		return m.aiToastErr("Claude sign-in failed", msg.err)
	}
	path, err := ai.CredentialsPath()
	if err != nil {
		return m.aiToastErr("Credentials error", err)
	}
	creds, _ := ai.LoadCredentials(path)
	t := msg.tokens
	creds.ClaudeOAuth = &t
	if err := ai.SaveCredentials(path, creds); err != nil {
		return m.aiToastErr("Credentials error", err)
	}
	if err := m.saveAISettings(func(st *ai.Settings) {
		if st.Provider != ai.KindClaudeOAuth {
			st.Model = ""
		}
		st.Provider = ai.KindClaudeOAuth
	}); err != nil {
		return m.aiToastErr("Settings error", err)
	}
	return m.aiInfo("✦ Signed in with Claude — " + s.modelLabel())
}

func (m *Model) claudeSignOut() tea.Cmd {
	path, err := ai.CredentialsPath()
	if err != nil {
		return m.aiToastErr("Credentials error", err)
	}
	creds, _ := ai.LoadCredentials(path)
	if creds.ClaudeOAuth == nil {
		return m.aiInfo("Not signed in to Claude")
	}
	creds.ClaudeOAuth = nil
	if err := ai.SaveCredentials(path, creds); err != nil {
		return m.aiToastErr("Credentials error", err)
	}
	_ = m.saveAISettings(func(st *ai.Settings) {
		if st.Provider == ai.KindClaudeOAuth {
			st.Provider = ""
		}
	})
	return m.aiInfo("Signed out of Claude")
}
