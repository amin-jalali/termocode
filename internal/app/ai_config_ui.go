package app

import (
	"context"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/theme"
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
	confirmKindAIUndoTurn
	confirmKindAIOAuth
	confirmKindAIBudget
)

// The Runs view's card and list (runs_state.go).
const (
	confirmKindRunsStop confirmKindEnum = 2400
	pickerKindRunsMore  pickerKindEnum  = 2400
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
	case pickerKindAIHistoryDelete:
		return m.onAIHistoryDeletePicked(id), true
	case pickerKindAIHistoryRename:
		return m.onAIHistoryRenamePicked(id), true
	case pickerKindAIFork: // ai_fork.go
		return m.onAIForkPicked(id), true
	case pickerKindAIAutoApprove:
		return m.onAIAutoApproveToggled(id), true
	case pickerKindMCPServeStatus: // mcp_serve_status.go
		return m.onMCPServeStatusPicked(id), true
	case pickerKindAIAttach:
		return m.onAIAttachPicked(id), true
	case pickerKindMCPInsert: // ai_mcp_insert.go
		return m.onMCPInsertPicked(id), true
	case pickerKindAIMemory: // ai_memory_picker.go
		return m.onAIMemoryPicked(id), true
	case pickerKindAISkills: // ai_skills.go
		return m.onAISkillsPicked(id), true
	case pickerKindAIWebTools: // ai_web_tools.go
		return m.onAIWebToolsPicked(id), true
	case pickerKindAgentHooks: // agent_hooks_ui.go
		return m.onAgentHooksPicked(id), true
	case pickerKindRunsMore: // runs_state.go
		return m.onRunsMorePicked(id), true
	case pickerKindAIWorktrees: // ai_worktree_list.go
		return m.onAIWorktreePicked(id), true
	}
	if cmd, ok := m.handleWebAccessPicker(id); ok { // web_access.go
		return cmd, true
	}
	if cmd, ok := m.handleItermocPickerSelect(id); ok { // itermoc_actions.go
		return cmd, true
	}
	if cmd, ok := m.handleAIProfilePickerSelect(id); ok {
		return cmd, true
	}
	return m.handleMCPPickerSelect(id)
}

// handleAIPromptSubmit routes the AI prompt kinds. ok=false: not ours.
func (m *Model) handleAIPromptSubmit(value string) (tea.Cmd, bool) {
	switch m.promptKind {
	case promptKindAIEdit:
		return m.onAIEditInstruction(value), true
	case promptKindAIKey:
		return m.onAIKeyEntered(value), true
	case promptKindAIWebKey: // ai_web_tools.go
		return m.onAIWebKeyEntered(value), true
	case promptKindAIModel:
		return m.saveAIModel(strings.TrimSpace(value)), true
	case promptKindAIBaseURL:
		return m.onAIBaseURLEntered(value), true
	case promptKindAIOAuthCode:
		return m.onClaudeCodePasted(value), true
	case promptKindAIHistoryRename:
		return m.onAIHistoryRenamed(strings.TrimSpace(value)), true
	case promptKindAISaveAs:
		return m.onAISaveAsPath(value), true
	case promptKindAISide: // OMX-30 (ai_side.go)
		if strings.TrimSpace(value) == "" {
			return nil, true
		}
		return tea.Batch(m.aiInfo("Side question sent (one request to the quick model)"), m.onAISideQuestion(value)), true
	case promptKindAgentsSend: // agents_node_ctl.go
		return m.onAgentsSend(value), true
	}
	return m.handleMCPPromptSubmit(value)
}

// handleAIConfirm routes the AI confirm kinds. ok=false: not ours.
func (m *Model) handleAIConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindBugReport:
		return m.answerBugReport(id), true
	case confirmKindAITool:
		return m.answerAIToolConfirm(id), true
	case confirmKindAIUndoTurn:
		return m.onAIUndoTurnConfirm(id), true
	case confirmKindAIBudget:
		return m.answerAIBudget(id), true
	case confirmKindAIPlan: // G-PLAN (ai_plan_approval.go)
		return m.answerAIPlan(id), true
	case confirmKindAISide: // OMX-30 (ai_side.go)
		return m.answerAISide(id), true
	case confirmKindRunsStop: // runs_state.go
		return m.answerRunsStop(id), true
	case confirmKindAgentsStop: // agents_node_ctl.go
		return m.answerAgentsStop(id), true
	case confirmKindAIOAuth:
		if id == "continue" {
			return m.beginClaudeOAuth(), true
		}
		return nil, true
	case confirmKindAISaveOverwrite:
		return m.onAISaveOverwrite(id), true
	case confirmKindAIHistoryDelete:
		return m.onAIHistoryDeleteConfirm(id), true
	case confirmKindAIHistoryClear:
		return m.onAIHistoryClearConfirm(id), true
	case confirmKindMCPTrust:
		return m.answerMCPTrust(id), true
	case confirmKindMCPImportOther: // mcp_import_other.go
		return m.answerMCPImportOther(id), true
	case confirmKindMCPElicit, confirmKindMCPElicitURL, confirmKindMCPElicitSend: // ai_mcp_elicit.go
		return m.answerMCPElicit(m.confirmKind, id), true
	case confirmKindMCPRemove: // mcp_service_manage.go
		return m.answerMCPRemove(id), true
	case confirmKindMCPUntrust: // ai_mcp_untrust.go
		return m.answerMCPUntrust(id), true
	case confirmKindMCPDisable:
		return m.answerMCPDisable(id), true
	case confirmKindMCPShareStop: // mcp_share.go
		return m.answerMCPShareStop(id), true
	case confirmKindMCPCatalogAdd: // mcp_catalog.go
		return m.answerMCPCatalogAdd(id), true
	case confirmKindMCPBundle: // mcp_bundle.go
		return m.answerMCPBundle(id), true
	case confirmKindMCPUpdate: // mcp_updates.go
		return m.answerMCPUpdate(id), true
	case confirmKindItermocAgentRevoke: // itermoc_agent_page.go
		return m.answerItermocAgentRevoke(id), true
	case confirmKindAISlotRemove: // itermoc_slots.go
		return m.answerItermocSlotRemove(id), true
	case confirmKindAIProviderDisconnect: // itermoc_providers.go
		return m.answerItermocProvDisconnect(id), true
	case confirmKindMCPServe: // an outside agent's call (mcp_serve_confirm.go)
		return m.answerMCPServeConfirm(id), true
	case confirmKindMCPServeStop, confirmKindMCPServeDisconnect: // mcp_serve_status.go
		return m.answerMCPServeStatusConfirm(m.confirmKind, id), true
	case confirmKindAIProfileTrust:
		return m.answerAIProfileTrust(id), true
	case confirmKindAIRulesTrust: // ai_rules.go
		return m.answerAIRulesTrust(id), true
	case confirmKindAISkillsTrust: // ai_skills.go
		return m.answerAISkillsTrust(id), true
	case confirmKindAIHooksTrust: // ai_hooks.go
		return m.answerAIHooksTrust(id), true
	case confirmKindAIWebAdd: // ai_web_presets.go
		return m.answerAIWebPreset(id), true
	case confirmKindAIShipPush, confirmKindAIShipPR: // ai_ship.go
		return m.answerAIShipCard(m.confirmKind, id), true
	case confirmKindAIPlanShip: // ai_plan_worktree.go
		return m.answerAIPlanShip(id), true
	case confirmKindAgentCtlPrompt: // agent_ctl.go
		return m.answerAgentCtlPrompt(id), true
	case confirmKindAgentHooks: // agent_hooks_ui.go
		return m.answerAgentHooks(id), true
	case confirmKindAIMemoryLesson, confirmKindAIMemoryForget, confirmKindAIMemoryForgetAll: // ai_memory_picker.go
		return m.answerAIMemoryCard(m.confirmKind, id), true
	case confirmKindAIProfileSave:
		return m.onAIProfileSaveChoice(id), true
	case confirmKindAIProfileShow, confirmKindBuiltinSkill, confirmKindSharedMemory: // read-only: Close
		return nil, true
	case confirmKindAIProfileFormStep, confirmKindAIProfileFormDelete, confirmKindAIProfileFormDiscard:
		return m.answerAIProfileFormConfirm(id)
	case confirmKindSecretForget: // secrets_store_ui.go
		return m.answerSecretForget(id), true
	case confirmKindSecretsMigrate: // secrets_migrate.go
		return m.answerSecretsMigrate(id), true
	case confirmKindSecretsDeletePlain: // secrets_migrate.go
		return m.answerSecretsDeletePlain(id), true
	case confirmKindSecretsSwitch: // secrets_switch.go
		return m.answerSecretsSwitch(id), true
	case confirmKindSecretsSwitchPlain:
		return m.answerSecretsSwitchPlain(id), true
	case confirmKindSecretsBroken: // secrets_failures.go
		return m.answerSecretsBroken(id), true
	case confirmKindRegistryRemember: // containers_registries.go
		return m.answerRegistryRemember(id), true
	case confirmKindAgentMCP: // agent_mcp_card.go
		return m.answerAgentMCPCard(id), true
	case confirmKindAIWorktreeConflict, confirmKindAIWorktreeDiscard: // ai_worktree_actions.go
		return m.answerAIWorktreeCard(m.confirmKind, id), true
	case confirmKindAIWorktreeLeft: // ai_worktree_list.go
		return m.answerAIWorktreeLeft(id), true
	case confirmKindWebStart: // web_tui.go
		return m.answerWebStart(id), true
	case confirmKindWebAsk: // web_ask.go
		return m.answerWebAsk(id), true
	}
	if cmd, ok := m.handleWebAccessConfirm(id); ok { // web_access.go
		return cmd, true
	}
	return nil, false
}

// onAIConfirmClosed handles Esc on an AI dialog: a tool call is denied.
func (m *Model) onAIConfirmClosed() tea.Cmd {
	switch m.confirmKind {
	case confirmKindBugReport:
		return m.answerBugReport("cancel")
	case confirmKindAITool:
		return m.answerAIToolConfirm("deny")
	case confirmKindAIPlan: // G-PLAN: Esc denies the plan
		return m.answerAIPlan("deny")
	case confirmKindAIWorktreeConflict, confirmKindAIWorktreeDiscard:
		return m.answerAIWorktreeCard(m.confirmKind, "") // Esc: Cancel
	case confirmKindAIWorktreeLeft:
		return m.answerAIWorktreeLeft("")
	case confirmKindMCPTrust:
		return m.answerMCPTrust("deny")
	case confirmKindMCPImportOther:
		return m.answerMCPImportOther("cancel")
	case confirmKindMCPElicit, confirmKindMCPElicitURL, confirmKindMCPElicitSend:
		return m.answerMCPElicit(m.confirmKind, "") // Esc: cancel
	case confirmKindMCPRemove:
		return m.answerMCPRemove("keep")
	case confirmKindMCPUntrust:
		return m.answerMCPUntrust("cancel")
	case confirmKindMCPDisable:
		return m.answerMCPDisable("keep")
	case confirmKindMCPShareStop:
		return m.answerMCPShareStop("cancel")
	case confirmKindItermocAgentRevoke:
		return m.answerItermocAgentRevoke("keep")
	case confirmKindAISlotRemove:
		return m.answerItermocSlotRemove("keep")
	case confirmKindAIProviderDisconnect:
		return m.answerItermocProvDisconnect("keep")
	case confirmKindMCPServe:
		return m.answerMCPServeConfirm("deny")
	case confirmKindMCPServeStop, confirmKindMCPServeDisconnect:
		return m.openMCPServeStatus() // Esc: back to the status screen
	case confirmKindAIProfileTrust:
		return m.answerAIProfileTrust("") // Esc: no answer, ask again next time
	case confirmKindAIRulesTrust:
		return m.answerAIRulesTrust("") // Esc: no answer, the message goes back
	case confirmKindAISkillsTrust:
		return m.answerAISkillsTrust("") // Esc: no answer, the message goes back
	case confirmKindAIHooksTrust:
		return m.answerAIHooksTrust("") // Esc: no answer, the message goes back
	case confirmKindAIShipPush, confirmKindAIShipPR:
		return m.answerAIShipCard(m.confirmKind, "") // Esc: the ship stops
	case confirmKindAIPlanShip:
		return m.answerAIPlanShip("") // Esc: Not now
	case confirmKindAgentCtlPrompt:
		return m.answerAgentCtlPrompt("") // Esc: Deny
	case confirmKindAIMemoryLesson, confirmKindAIMemoryForget, confirmKindAIMemoryForgetAll:
		return m.answerAIMemoryCard(m.confirmKind, "") // Esc: Keep
	case confirmKindAIProfileSave:
		return m.onAIProfileSaveChoice("cancel")
	case confirmKindSecretForget:
		return m.answerSecretForget("cancel")
	case confirmKindSecretsMigrate:
		return m.secretsMigrateClosed() // Esc: ask later
	case confirmKindSecretsDeletePlain:
		return m.answerSecretsDeletePlain("keep") // Esc: keep
	case confirmKindSecretsSwitch, confirmKindSecretsSwitchPlain:
		return m.openSecretsStore() // Esc: back to the store screen
	case confirmKindRegistryRemember:
		return m.registryPromptClosed()
	case confirmKindAgentMCP:
		return m.answerAgentMCPCard("skip")
	case confirmKindWebStart:
		return m.answerWebStart("") // Esc: Not now
	case confirmKindWebAsk:
		return m.answerWebAsk("") // Esc: Cancel
	}
	if cmd, ok := m.handleWebAccessConfirm(""); ok { // web_access.go: Esc is the safe answer
		return cmd
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
	s.keyOnly = false
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
	// The key is saved in the secret store: open it first (a new
	// encrypted store asks for its passphrase twice).
	if c, wait := m.secretsOpenFirst(func(m *Model) tea.Cmd { return m.openAIKeyPrompt(kind) }); wait {
		return c
	}
	m.aiS().cfgKind = kind
	m.prompt = prompt.New("✦ "+ai.KindLabel(kind)+" API key", "API key ("+m.aiKeyHome()+"; empty = keep):", "")
	m.prompt.SetMasked(true)
	m.prompt.SetNote(secretPromptNote(""))
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
	if s.keyOnly { // from Itermoc: the key is all that was asked
		s.keyOnly = false
		m.itermocRefreshNow()
		return m.aiInfo("Key saved for " + ai.KindLabel(kind))
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
		ai.ClassIDE:       "IDE commands (execute_action)",
		ai.ClassSettings:  "change settings (settings_set)",
		ai.ClassServices:  "start / stop / restart project services",
	}
	var items []picker.Item
	for _, c := range ai.ToolClasses {
		state := "ask"
		if contains(s.settings.AutoApprove, c) {
			state = "AUTO"
		}
		items = append(items, picker.Item{ID: c, Title: c + " — " + desc[c], Hint: state})
	}
	for _, c := range m.mcpAutoApproveClasses() { // one row per MCP server
		state := "ask"
		if contains(s.settings.AutoApprove, c) {
			state = "AUTO"
		}
		items = append(items, picker.Item{ID: c, Title: c + " — mutating tools of MCP server " + strings.TrimPrefix(c, mcp.ClassPrefix), Hint: state})
	}
	for _, c := range s.settings.AutoApprove { // HTTP hosts allowed with "Always allow"
		if strings.HasPrefix(c, "http:") {
			what := "GET / HEAD / OPTIONS"
			if strings.HasSuffix(c, "/write") {
				what = "all methods"
			}
			items = append(items, picker.Item{ID: c, Title: c + " — HTTP requests (" + what + ") to " + strings.TrimSuffix(strings.TrimPrefix(c, "http:"), "/write"), Hint: "AUTO"})
		}
	}
	items = append(items, m.dbAutoApproveItems(s.settings.AutoApprove)...)
	items = append(items, mcpServeAllowItems()...) // outside agents: their own list
	// The title is the palette title (with the theme's AI glyph, "*" in ASCII);
	// the box follows the window so the rows are not cut at 80 columns.
	m.picker = picker.NewItems(" "+theme.GlyphAI.String()+" AI: Agent Auto-Approve… ", items)
	m.picker.SetWidth(mcpServeStatusWidth(m.w))
	m.picker.SetNote([]string{"Enter switches ask and AUTO; on an agent it revokes."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIAutoApprove
	return nil
}

func (m *Model) onAIAutoApproveToggled(class string) tea.Cmd {
	if cmd, ok := m.onMCPServeAllowRow(class); ok { // an outside agent's Always allow
		return cmd
	}
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
	// An encrypted store or the keyring has no file to edit: the store
	// screen is where the keys are seen and forgotten.
	if s, err := m.secretStore(); err == nil && s != nil && (s.Backend() == "age" || s.Backend() == "keyring") {
		return tea.Batch(m.openSecretsStore(),
			m.secretsToast(toast.Info, "Your keys are in the "+secretsBackendLabel(s), "It is not a file you can edit. This screen lists and forgets them."))
	}
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
	_ = osOpen(u)
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
