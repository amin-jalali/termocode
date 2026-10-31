package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agenthooks"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aimemory"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/profileform"
	"github.com/amin-jalali/termocode/internal/statusbar"
	"github.com/amin-jalali/termocode/internal/toast"
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
//   ai_actions.go   — Explain / Fix / Edit / Doc proposals
//   ai_review.go    — the review card for every proposed change
//   ai_config_ui.go — Configure / Pick Model / OAuth sign-in flows
//   ai_tools_app.go — editor-backed agent tools (LSP, buffers)
//
// Every AI entry point calls m.aiReady() first: with no provider it only
// pushes a toast and changes nothing else.

// aiState is the AI feature state. Held by pointer on Model so Bubble Tea
// value copies and background goroutines share one instance.
type aiState struct {
	answerCleaner    aiStreamCleaner
	cleanAnswerIndex int
	streamTranscript aiStreamTranscript
	warmPending      <-chan struct{} // startup keychain probe; never wait before the first frame
	// historyTarget is the chat a Delete / Rename dialog is about.
	historyTarget string
	// wtTarget is the step ("run:step") a worktree Merge / Discard card
	// is about (G-WT, ai_worktree_actions.go).
	wtTarget string
	// wtLeft are the agent worktrees AI: Agent Worktrees… listed that no
	// card of this chat owns (left over), by id.
	wtLeft map[string]*ai.StoredWorktree
	// streamSteps counts chat events; the status bar's busy comet moves
	// one step per event (no timer).
	streamSteps int
	toolNotes   []string             // notes of the running tool for the user (ai_tool_note.go)
	toolArgs    map[string]any       // the running MCP call's arguments (ai_tool_details.go)
	toolDetails map[int]aiToolDetail // MCP call details by tool line
	toolOpen    map[int]bool         // open details
	// streamText grows the streamed answer in place (aiTextAppender).
	streamText aiTextAppender
	// batch is set while one user action changes many files (aiBatch).
	batch    *aiBatch
	settings ai.Settings
	keep     aiKeepGoingRun // OMX-7: the keep-going chain of the open chat
	// OMX-17: kwOff is set when the user removed the keyword chip of the
	// draft; kwProfile is the profile the next sendAIChat runs.
	kwOff     bool
	kwProfile string
	side      aiSideChat // OMX-30: the side chat (ai_side.go)
	cfg       ai.Config
	provider  ai.Provider
	loadErr   string
	// profiles are the agent profiles (built-ins + profiles.json + the
	// project's ai-profiles.json, with their load errors); slots holds
	// one provider per model slot, built on first use (ai_profiles.go).
	profiles ai.ProfileSet
	slots    *ai.SlotPool
	// slotKnown / slotErrs are the slot names and slot errors of the last
	// loadProfiles (reloadProfiles reuses them).
	slotKnown map[string]bool
	slotErrs  []string
	// providers are the connected AIs by kind (ai/providers.json, else
	// derived from the setup); provider steps take their base URL.
	providers map[string]ai.ProviderEntry
	// modelsCache is the model cache in memory (nil: read it on next use;
	// a reload drops it); provBusy / provErr are Itermoc's refreshes that
	// run and the last one that failed, by kind (itermoc_providers.go).
	modelsCache ai.ModelsCache
	provBusy    map[string]bool
	provErr     map[string]string
	// cliStates is the last check of the subscription CLIs (CS-5; nil:
	// not checked yet). The profile form shows it.
	cliStates map[string]profileform.CLIState
	// profTrust is the open project-profile trust card (ai_profiles_ui.go).
	profTrust   *aiProfileTrustPending
	profileSave *aiProfileSavePending
	// rulesTrust is the open project-rules card (ai_rules.go).
	rulesTrust *aiRulesTrustPending
	// rulesInj adds folder / glob rules to read_file results once per
	// chat (OMX-9); rulesInjChat is the chat it belongs to.
	rulesInj     *ai.RulesInjector
	rulesInjChat string
	// astGrepAsked: the ast-grep install card was shown this session
	// (OMX-28), so a missing ast-grep does not open it again.
	astGrepAsked bool
	// skillsTrust is the open project-skills card (ai_skills.go).
	skillsTrust *aiSkillsTrustPending
	hooksTrust  *aiHooksTrustPending // the open project-hooks card (ai_hooks.go)
	webPreset   string               // the server the open add card is for (ai_web_presets.go)
	recall      *aiRecallState       // lesson recall per chat (ai_recall.go)
	ship        *aiShip              // the running ship loop (ai_ship.go)
	ctl         *agentCtlState       // the agent control socket's UI side (agent_ctl.go)
	// agentHooksPlan is the change the open state-hooks card shows (agent_hooks_ui.go).
	agentHooksPlan *agenthooks.Plan
	shipGen        int
	// memFake replaces lessons.md for the screenshot demo (ai_memory.go).
	memFake *[]aimemory.Lesson
	// skillsFake replaces the skill files in AI: Skills… for the
	// screenshot demo (ai_skills_demo.go).
	skillsFake *ai.Skills
	// memSlotWarned: the "no quick model" toast was shown this session.
	memSlotWarned bool
	// memAsk is the turn the open ask-once memory card is for.
	memAsk *aiMemoryAskPending
	// memLesson is the lesson id of the open memory card (ai_memory_picker.go).
	memLesson string
	// profile is the active profile's name ("" = default); profErrShown
	// is the profile errors already shown as a toast.
	profile      string
	profErrShown string
	// prevProfile is the profile AI: Work Until Done (toggle) switches
	// back to.
	prevProfile string
	// profFake are profiles added after every profile load (the demo
	// shots' project profile, which has no file).
	profFake []ai.Profile
	// keyOnly: the open key prompt came from Itermoc and only stores the key.
	keyOnly bool
	// modelsFake replaces the models cache in the profile form's model
	// picker (the demo shots).
	modelsFake ai.ModelsCache
	// slotFake replaces model slots by name (tests and the demo shots).
	slotFake map[string]aiSlot

	// The running pipeline run (ai_runs.go): its handle, the index of
	// its assistant message, its slots, the restore files counted when
	// each step started, a counter that changes with any card
	// (transcript cache), the card the keyboard selected and the gate
	// its steps share for confirm and review cards (one at a time).
	run      *agentrun.Handle
	runMsg   int
	runSlots map[string]aiSlot
	runFiles map[string]int
	runGen   int
	// runLaunched is when the running run was (re)started; runPausing:
	// Pause was asked and the run has not paused yet (runs_actions.go).
	runLaunched time.Time
	runPausing  bool
	cardSel     string
	reviewGate  agentrun.Gate
	// planWait is the plan the running run waits on and runActs what
	// its steps may do (G-PLAN, ai_plan_approval.go).
	planWait *aiPlanWait
	runActs  func(agentdag.Node) (writes, exec bool)
	// runQueue are the runs the AI started (agent_run_start) that wait
	// for the chat; runInline the AI's own steps of such a run by run id;
	// runNotStarted why a queued run never started (G-API,
	// ai_agent_run_tools.go).
	runQueue      []aiQueuedRun
	runInline     map[string]ai.Profile
	runNotStarted map[string]string
	// bgRuns are the AI-started runs that run beside the chat (OMX-6,
	// ai_bg_runs.go).
	bgRuns []*aiBgRun
	bgMail aiBgMail
	// catErrShown are the ai_categories errors logged already (OMX-16).
	catErrShown map[string]bool
	// runAgain are the running run's steps that start once more (a stall,
	// ESCALATE): their card keeps its notes, time and tokens.
	runAgain map[string]bool
	// planDocs are the plan files of planned runs that go (OMX-14).
	planDocs map[string]*aiPlanDoc
	// agents is the AGENTS view of the chat's runs (agents_panel.go).
	agents aiAgentsState
	// usageLog is every step and plain chat turn that used a model in
	// this UI process, for AI: Usage (memory only, ai_usage_view.go);
	// usageGen changes with it.
	usageLog []aiUsageEntry
	usageGen int

	// inline ghost text
	inlineCancel context.CancelFunc
	inlineBusy   bool
	inlineCache  ai.InlineCache // type-through of the shown ghost
	inlineImp    map[string]any // imports of the cached ghost
	// nes is the next edit suggestion (ai_nextedit.go).
	nes aiNESState

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
	act         aiActivity // the activity line (ai_activity.go)
	attachments []aiAttachment
	// thinkOff: the user removed the input's think / ultrathink chip
	// (OMX-5, ai_reasoning.go); the next send clears it.
	thinkOff bool
	// turnThink is the keyword level of the message being sent
	// (takeTurnKeyword); the turn or run it starts reads and clears it.
	turnThink string
	// turnRoot is the workspace of the next plain turn when it is not
	// the user's tree (a ship's fix in a plan run's worktree; "": none).
	turnRoot string
	planWT   *aiPlanWT // the plan in a worktree (ai_plan_worktree.go)
	restored bool
	// claim holds the shown chat against other termocode windows on the
	// same store (ai_chat_claim.go).
	claim *ai.ChatClaim

	// pending tool confirmation (agent goroutine waits on reply)
	confirm *aiPendingConfirm

	// one-shot code actions (Explain / Fix / Edit / Doc)
	actionGen    uint64
	actionCancel context.CancelFunc
	actionBusy   string
	// review is the open review card's files (ai_review.go).
	review *aiReview
	// preReview holds the proposals made for a step's later calls
	// (aiBeforeCalls), by tool call id.
	preReview map[string]aiProposed
	// Restore points (ai_restore.go): the chat's points, a counter that
	// changes with them (transcript cache), the user message of the
	// running turn and the turn the undo dialog is about.
	restore    *aiRestorePoints
	restoreGen int
	turnMsg    int
	undoTurn   int
	// auditPending is the started tool call the audit log waits for
	// (ai_audit.go).
	auditPending *aiAuditPending
	editTarget   *aiRangeTarget // Edit-with-AI waiting for its instruction

	// MCP (ai_mcp.go): the servers, the open trust dialog and the
	// server the actions picker is about.
	mcp       *mcp.Manager
	mcpTrust  *aiMCPTrustPending
	mcpSel    string
	mcpSecret string         // the ${secret:NAME} being entered
	mcpImport []mcp.Imported // the open Import from Other Apps card (mcp_import_other.go)
	// mcpUntrustTool is the tool the Reset trust card is about ("" = the
	// whole server; ai_mcp_untrust.go).
	mcpUntrustTool string
	// mcpLogServer is the server the MCP log shows ("" = all; ai_mcp_log.go).
	mcpLogServer string

	mcpx aiMCPXState // prompts, completion, elicitation (ai_mcp_prompts.go)
	// mcpProfile is the MCP profile this chat uses ("" all; mcp_profiles.go).
	mcpProfile string
	// mcpCatalog is the catalog list shown; mcpCatalogSel the server the
	// add card is about (mcp_catalog.go).
	mcpCatalog    []mcp.CatalogEntry
	mcpCatalogSel string
	mcpBundle     *mcpBundleFlow // the running bundle install (mcp_bundle.go)
	// mcpWiz is the open Connect-a-service wizard (ai_mcp_connect.go);
	// mcpWizGen drops messages of a closed one; mcpGHAccounts are gh's
	// accounts from the last open.
	mcpWiz        *mcpWizard
	mcpWizGen     int
	mcpGHAccounts []mcp.GHAccount
	mcpNotNow     map[string]bool // "Not now" on a server's sign-in row
	// addAICancel stops the Add AI card's request in flight (ai_add.go).
	addAICancel context.CancelFunc
	// addAIFlow is the card's running sign-in path (ai_add_run.go).
	addAIFlow addAISignInFlow

	// secretsLocked: the last reload found the encrypted secret store
	// locked (secrets_ui.go); the setup card and the "not configured"
	// toast say so.
	secretsLocked bool

	// configure / sign-in flow
	cfgKind string
	pkce    *ai.PKCE

	// mdCache holds rendered answers per message index (ai_panel.go).
	mdCache map[int]aiMDEntry
	// mdStream keeps the drawn start of a long answer (ai_md_stream.go).
	mdStream aiMDStream
	// segCache holds the transcript lines of each finished message, so a
	// streamed token rebuilds only the last answer (ai_panel.go).
	segCache map[int]aiSegEntry
	// transcript keeps the built transcript and its drawn rows between
	// frames (ai_panel.go).
	transcript aiTranscriptCache
	inputCache aiInputCache
	// sel is the transcript text selection (ai_select.go).
	sel aiSelState

	// "Save as…" of a code block (ai_export.go): the code, and the path
	// waiting for the overwrite confirm.
	saveCode string
	savePath string
}

// aiAttachment is file / selection context added to the next chat message.
type aiAttachment struct {
	Label string
	Text  string
	// Image is the path of an attached image (sent as an image, not as
	// text); MediaType its type ("image/png").
	Image     string
	MediaType string
}

// newAIState loads settings + credentials and builds the provider.
func newAIState() *aiState {
	s := &aiState{store: ai.DefaultChatStore(), conv: ai.NewConversation(), turnMsg: -1, undoTurn: -1}
	s.warmPending = secretStoreWarmPending()
	if s.warmPending == nil {
		s.reload()
	}
	s.restoreAIChat() // last chat + scroll from session.json
	return s
}

// reload re-reads config.json + credentials.json + env and rebuilds the
// provider. Safe to call any time (e.g. after Configure).
func (s *aiState) reload() {
	s.warmPending = nil
	s.loadErr = ""
	_, _ = configureSecretStore() // credentials go through the app's one store
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
	s.secretsLocked = creds.Locked() && secretsAgeFileExists()
	s.cfg = ai.Resolve(s.settings, creds, os.Getenv)
	if s.cfg.Kind == ai.KindClaudeOAuth && creds.ClaudeOAuth != nil && credPath != "" {
		src := &ai.TokenSource{Path: credPath}
		s.cfg.Token = src.Token
	}
	s.provider = ai.NewProvider(s.cfg)
	s.loadProfiles(creds)
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
	// A locked secret store asks for its passphrase on the first need.
	if c, asked := m.secretsAskOnNeed(); asked {
		return c, false
	}
	detail := "Run \"AI: Configure Provider…\" or set ANTHROPIC_API_KEY / OPENAI_API_KEY (or TERMOCODE_AI_PROVIDER=ollama)."
	if m.aiS().secretsLocked {
		detail = "Your secrets are locked: F1 → AI: Unlock Secrets.\n" + detail
	}
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

// profileModelLabel is the model a profile answers with: the slot model
// of its last step (the step whose answer is the reply; a planned
// profile starts with its planner). "" when the slot has none.
func (s *aiState) profileModelLabel(p ai.Profile) string {
	if len(p.Steps) == 0 {
		return ""
	}
	st := p.Steps[len(p.Steps)-1]
	if p.Planned() {
		st = p.Steps[0]
	}
	if st.CLI != "" && st.Model != "" {
		return ai.CLITitle(st.CLI) + " · " + st.Model
	}
	if st.CLI != "" {
		return ai.CLITitle(st.CLI)
	}
	m, _ := s.aiSlotModel(p.SlotFor(st))
	return m
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
	label := s.modelLabel()
	if prof := s.activeProfile(); prof.Name != ai.ProfileDefault {
		if mod := s.profileModelLabel(prof); mod != "" {
			label = mod
		}
		label = prof.Name + " · " + label // U20 AP-5: a non-default profile shows in the badge
	}
	if s.streaming || s.inlineBusy || s.actionBusy != "" {
		return label, statusbar.AIStreaming
	}
	return label, statusbar.AIIdle
}

// hitStatusAIBadge reports whether (x, y) lands on the status-bar badge.
func (m Model) hitStatusAIBadge(x, y int) bool {
	return m.statusHitAt(x, y) == "status:ai"
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

// syncAIInline mirrors the inline and next edit toggles into nvim.
func (m *Model) syncAIInline() {
	if m.nvim != nil {
		_ = m.nvim.ExecLua(aiEnableLua(m.aiInlineEnabled()))
		_ = m.nvim.ExecLua(aiNESEnableLua(m.aiNESEnabled()))
	}
}

// aiAfterAttach runs at the end of attachCmd (nvim goroutine): colors +
// enable flag. Reads only.
func (m Model) aiAfterAttach() {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.ExecLua(aiColorsLua())
	on, nes := false, false
	if s := m.ai; s != nil {
		ready := s.provider != nil && s.provider.Configured()
		on = s.settings.InlineEnabled() && ready
		nes = s.settings.NextEditEnabled() && ready
	}
	_ = m.nvim.ExecLua(aiEnableLua(on))
	_ = m.nvim.ExecLua(aiNESEnableLua(nes))
}

// ── Palette ─────────────────────────────────────────────────────────────

// aiPaletteItems are appended to the command palette (Group A).
func aiPaletteItems() []paletteEntry {
	return []paletteEntry{
		{"ai-open-chat", "AI: Open Chat", "Alt+A"},
		{"ai-new-chat", "AI: New Chat", ""},
		{"ai-history", "AI: Chat History…", ""},
		{"ai-history-rename", "AI: Rename Chat…", ""},
		{"ai-fork-chat", "AI: Fork Chat from Message…", ""},
		{"ai-history-delete", "AI: Delete Chat…", ""},
		{"ai-history-clear", "AI: Clear Chat History", ""},
		{"ai-explain", "AI: Explain Selection / Line", ""},
		{"ai-fix", "AI: Fix Diagnostic at Cursor", ""},
		{"ai-edit", "AI: Edit Selection…", "Alt+I"},
		{"ai-doc", "AI: Generate Doc Comment", ""},
		{"ai-attach", "AI: Add File / Selection to Chat", ""},
		{"ai-apply-code", "AI: Apply Last Code Block to File", ""},
		{"ai-export-chat", "AI: Export Chat as Markdown", ""},
		{"ai-apply-proposal", "AI: Apply Proposed Edit", ""},
		{"ai-discard-proposal", "AI: Discard Proposed Edit", ""},
		{"ai-review", "AI: Review Pending Changes", ""},
		{"ai-undo-turn", "AI: Undo Last Turn", ""},
		{"ai-edit-review", "AI: Toggle Edit Review (diff / auto)", ""},
		{"ai-trigger-inline", "AI: Trigger Inline Completion", "Alt+\\"},
		{"ai-toggle-inline", "AI: Toggle Inline Completions", "Alt+|"},
		{"ai-toggle-next-edit", "AI: Toggle Next Edit Suggestions", ""},
		{"ai-configure", "AI: Configure Provider…", ""},
		{"ai-pick-model", "AI: Pick Model…", ""},
		{"ai-auto-approve", "AI: Agent Auto-Approve…", ""},
		{"ai-profile-switch", "AI: Switch Profile…", ""},
		{"ai-profile-next", "AI: Next Profile", "Alt+Shift+P"},
		{"ai-profile-show", "AI: Show Profile", ""},
		{"ai-until-done", "AI: Work Until Done (toggle)", ""},
		{"ai-plan-approval", "AI: Toggle Plan Approval", ""},
		{"ai-reasoning-next", "AI: Next Reasoning Level", ""},
		{"ai-keep-going", "AI: Toggle Keep Going While Todos Are Open", ""},
		{"ai-handoff", "AI: Handoff to a New Chat", ""},
		{"ai-side", "AI: Side Question…", ""},
		{"ai-plan-review", "AI: Review Waiting Plan", ""},
		{"ai-agents", "AI: Toggle Agents Panel", "Alt+Shift+A"},
		{"ai-worktrees", "AI: Agent Worktrees…", ""},
		{"ai-worktrees-toggle", "AI: Toggle Agent Worktrees", ""},
		{"ai-usage", "AI: Usage", ""},
		{"ai-runs", "AI: Runs", ""},
		{"ai-profile-edit", "AI: Edit Profiles", ""},
		{"ai-profile-new", "AI: New Profile from Current…", ""},
		{"ai-profile-form", "AI: Edit Profile…", ""},
		{"ai-profile-trust", "AI: Project Profiles Trust…", ""},
		{"ai-rules", "AI: Project Rules…", ""},
		{"ai-memory", "AI: Memory…", ""},
		{"help-bug-report", "Help: Build Bug Report…", ""},
		{"ai-doctor", "AI: Doctor", ""},
		{"ai-skills", "AI: Skills…", ""},
		{"ai-hooks", "AI: Hooks…", ""},
		{"ai-web-tools", "AI: Web Tools… (fetch, search, docs, browser)", ""},
		{"ai-ship", "AI: Ship Branch to Merge-Ready (push, PR, CI, fixes) / Stop", ""},
		{"ai-plan-worktree", "AI: Run Next Plan in a Worktree, Then Ship… / Off", ""},
		{"ai-agent-hooks", "Agent: State Hooks… (exact working / waiting / done)", ""},
		{"ai-memory-toggle", "AI: Memory: Turn On/Off", ""},
		{"ai-models-edit", "AI: Edit Model Slots", ""},
		{"ai-sign-in-claude", "AI: Sign in with Claude (OAuth, opt-in)…", ""},
		{"ai-sign-out-claude", "AI: Sign out of Claude", ""},
		{"ai-credentials", "Open Config: AI Credentials", ""},
		{"ai-mcp-connect", "AI: Connect a Service… (GitHub, Linear, Slack, Notion, Sentry)", ""},
		{"ai-mcp-servers", "AI: MCP Servers…", ""},
		{"ai-mcp-insert", "AI: MCP Prompts and Resources…", ""},
		{"ai-mcp-catalog", "AI: MCP: Browse Catalog… (find and add servers)", ""},
		{"ai-mcp-updates", "AI: MCP: Check for Updates (catalog servers)", ""},
		{"ai-mcp-bundle", "AI: MCP: Install Bundle (.mcpb)…", ""},
		{"ai-mcp-profile", "AI: MCP: Switch Profile… (servers and tools for this chat)", ""},
		{"ai-mcp-log", "AI: MCP: Show Log", ""},
		{"ai-mcp-reconnect-all", "AI: MCP: Reconnect All Servers", ""},
		{"ai-mcp-reset-cache", "AI: MCP: Reset Tool Lists", ""},
		{"ai-mcp-config", "Open Config: MCP Servers", ""},
		{"ai-mcp-background", "AI: Move MCP Call to Background (Ctrl+B in the chat)", ""},
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
		"ai-cancel", "ai-discard-proposal", "ai-review", "ai-edit-review", "ai-undo-turn", "ai-history", "ai-pick-model", "ai-auto-approve",
		"ai-history-rename", "ai-history-delete", "ai-history-clear", "ai-fork-chat",
		"ai-mcp-servers", "ai-mcp-log", "ai-mcp-config", "ai-mcp-connect", "ai-mcp-reconnect-all", "ai-mcp-profile", "ai-mcp-catalog", "ai-mcp-bundle", "ai-mcp-updates", "ai-mcp-reset-cache",
		"ai-agent-hooks": // agent CLI hook files need no provider
	default:
		if c, ok := m.aiReady(); !ok {
			return c, true
		}
	}
	switch id {
	case "ai-open-chat":
		m.aiS().agents.on = false // the slot shows the chat
		return m.openAIPanel(true), true
	case "ai-new-chat":
		return m.newAIChat(), true
	case "ai-history":
		return m.openAIHistory(), true
	case "ai-worktrees": // G-WT (ai_worktree_list.go)
		return m.openAIWorktrees(), true
	case "ai-worktrees-toggle":
		return m.toggleAIWorktrees(), true
	case "ai-history-rename":
		return m.openAIHistoryFor(pickerKindAIHistoryRename, " Rename Chat "), true
	case "ai-fork-chat":
		return m.openAIForkPicker(), true
	case "ai-history-delete":
		return m.openAIHistoryFor(pickerKindAIHistoryDelete, " Delete Chat "), true
	case "ai-history-clear":
		return m.confirmAIHistoryClear(), true
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
	case "ai-export-chat":
		return m.aiExportChat(), true
	case "ai-review":
		return m.showAIReview(), true
	case "ai-undo-turn":
		return m.aiUndoTurn(-1), true
	case "ai-edit-review":
		return m.toggleAIEditReview(), true
	case "ai-apply-proposal":
		return m.reviewDecideCurrent(true), true
	case "ai-discard-proposal":
		return m.reviewDecideCurrent(false), true
	case "ai-trigger-inline":
		return m.aiTriggerInline(), true
	case "ai-toggle-inline":
		return m.aiToggleInline(), true
	case "ai-toggle-next-edit": // ai_nextedit.go
		return m.aiToggleNextEdit(), true
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
	case "ai-mcp-connect":
		return m.openMCPConnect(), true
	case "ai-mcp-servers":
		return m.openMCPServers(), true
	case "ai-mcp-background": // ai_mcp_background.go
		if cmd, ok := m.aiMCPDetach(); ok {
			return cmd, true
		}
		return m.aiInfo("No MCP call is running"), true
	case "ai-mcp-insert": // ai_mcp_insert.go
		return m.openMCPInsertPicker(), true
	case "ai-mcp-profile":
		return m.openMCPProfiles(), true
	case "ai-mcp-catalog":
		return m.openMCPCatalog(), true
	case "ai-mcp-bundle":
		return m.openMCPBundle(), true
	case "ai-mcp-updates":
		return tea.Batch(m.aiInfo("Checking the MCP catalog for updates…"), checkMCPUpdates(false)), true
	case "ai-mcp-log":
		return m.showMCPLog(), true
	case "ai-mcp-reconnect-all":
		return m.mcpReconnectAll(), true
	case "ai-mcp-reset-cache":
		return m.resetMCPToolLists(), true // ai_mcp_reset_cache.go
	case "ai-mcp-config":
		return m.editMCPConfig(), true
	case "ai-cancel":
		return m.aiCancelAll(), true
	case "ai-profile-switch":
		return m.openAIProfilePicker(), true
	case "ai-profile-next":
		return m.cycleAIProfile(), true
	case "ai-profile-show":
		return m.showAIProfile(), true
	case "ai-until-done":
		return m.toggleAIUntilDone(), true
	case "ai-reasoning-next": // OMX-5 (ai_reasoning.go)
		return m.cycleAIReasoning(), true
	case "ai-side": // OMX-30 (ai_side.go)
		return m.openAISide(), true
	case "ai-handoff": // OMX-21 (ai_session_tools.go)
		return m.aiHandoff(), true
	case "ai-keep-going": // OMX-7 (ai_keep_going.go)
		return m.toggleAIKeepGoing(), true
	case "ai-plan-approval": // G-PLAN (ai_plan_approval.go)
		return m.toggleAIPlanApproval(), true
	case "ai-plan-review":
		return m.openAIPlanCard(), true
	case "ai-agents":
		return m.toggleAgentsView(), true
	case "ai-usage":
		return m.showAIUsage(""), true
	case "ai-runs": // runs_state.go
		return m.revealRuns(true), true
	case "ai-profile-edit":
		return m.editAIProfiles(), true
	case "ai-profile-new":
		return m.openAIProfileNewForm(), true
	case "ai-profile-form":
		return m.openAIProfileEditPicker(), true
	case "ai-profile-trust":
		return m.openAIProfileTrustPicker(), true
	case "ai-rules":
		return m.openAIRules(), true
	case "ai-memory":
		return m.openAIMemory(), true
	case "help-bug-report":
		return m.openBugReport(false), true
	case "ai-doctor":
		return m.openAIDoctor(), true
	case "ai-skills":
		return m.openAISkills(), true
	case "ai-hooks":
		return m.openAIHooks(), true
	case "ai-web-tools":
		return m.openAIWebTools(), true
	case "ai-ship":
		return m.toggleAIShip(), true
	case "ai-plan-worktree":
		return m.toggleAIPlanWorktree(), true
	case "ai-agent-hooks":
		return m.openAgentHooks(), true
	case "ai-memory-toggle":
		return m.toggleAIMemory(), true
	case "ai-models-edit":
		return m.editAIModelSlots(), true
	}
	return nil, false
}

// aiCancelAll stops chat streaming, one-shot actions and inline requests.
func (m *Model) aiCancelAll() tea.Cmd {
	s := m.aiS()
	stopped := s.streaming || s.actionBusy != ""
	s.keep.stopped = true                                    // OMX-45: Stop ends a keep-going chain at once
	dropped := m.aiDropQueuedRuns("the user stopped the AI") // G-API: Stop stops what the AI queued too
	if m.aiBgStopAll() > 0 {                                 // OMX-6: and its background runs
		stopped = true
	}
	if s.chatCancel != nil {
		s.chatCancel()
		m.cancelAIReviewTurn()
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
	text := "AI stopped"
	if dropped > 0 {
		text += " · " + strconv.Itoa(dropped) + " queued run" + plural(dropped) + " not started"
	}
	m.toast, c = m.toast.Push(toast.Info, text)
	return c
}

// ── Settings rows (settings_ui.go reads settingsRows) ──────────────────

func init() {
	settingsRows = append(settingsRows,
		settingsRow{ID: "setting-ai-inline", Label: "ai_inline", Kind: "bool", Default: "true",
			Desc: "Show AI ghost-text completions while typing (needs a configured provider)."},
		settingsRow{ID: "setting-ai-next-edit", Label: "ai_next_edit", Kind: "bool", Default: "true",
			Desc: "After an edit, show where the AI thinks the next related edit goes (Tab: go there, Tab: apply, Esc: dismiss). Not set: same as ai_inline."},
		settingsRow{ID: "setting-ai-provider", Label: "ai_provider", Kind: "string", Default: "(auto)",
			Desc: "AI provider: " + strings.Join(ai.Kinds, ", ") + " or none. Empty picks anthropic or openai from the API keys it finds. TERMOCODE_AI_PROVIDER overrides it."},
		settingsRow{ID: "setting-ai-model", Label: "ai_model", Kind: "string", Default: "(default)",
			Desc: "Chat model ID. Empty uses the provider's default model."},
		settingsRow{ID: "setting-ai-base-url", Label: "ai_base_url", Kind: "string", Default: "(default)",
			Desc: "API base URL, for local or OpenAI-compatible servers. Empty uses the provider's default."},
		settingsRow{ID: "setting-ai-until-done-rounds", Label: "ai_until_done_rounds", Kind: "int", Default: strconv.Itoa(ai.DefaultUntilDoneRounds),
			Desc: fmt.Sprintf("Work until done (the until-done profile, /done, AI: Work Until Done): how many times verify runs at most (1 to %d). A profile's loop.max_rounds wins over it.", agentdag.MaxRounds)},
		settingsRow{ID: "setting-ai-until-done-budget", Label: "ai_until_done_budget_usd", Kind: "number", Default: fmt.Sprintf("%.2f", ai.DefaultUntilDoneBudgetUSD),
			Desc: "Work until done: the run pauses and asks once its steps have cost this many US dollars. 0 means no budget. A profile's budget_usd wins over it."},
	)
}

// aiSettingsCategories puts the AI rows in their own Settings group.
func aiSettingsCategories(cats map[string]string) map[string]string {
	httpSettingsCategories(cats)   // the HTTP client's group (http_settings.go)
	updateSettingsCategories(cats) // G-UPD (update_settings.go)
	for _, id := range []string{"setting-ai-inline", "setting-ai-next-edit", "setting-ai-provider", "setting-ai-model", "setting-ai-base-url",
		"setting-ai-until-done-rounds", "setting-ai-until-done-budget"} {
		cats[id] = "AI"
	}
	return mcpSettingsCategories(cats)
}

// aiSettingValue renders an AI row's current value. ok=false: not ours.
func aiSettingValue(id string) (string, bool) {
	if v, ok := mcpSettingValue(id); ok {
		return v, true
	}
	if v, ok := httpSettingValue(id); ok { // HTTP client (http_settings.go)
		return v, true
	}
	if v, ok := updateSettingValue(id); ok { // G-UPD (update_settings.go)
		return v, true
	}
	path, _ := ai.ConfigPath()
	s := ai.LoadSettings(path)
	switch id {
	case "setting-ai-inline":
		if s.InlineEnabled() {
			return "true", true
		}
		return "false", true
	case "setting-ai-next-edit":
		return strconv.FormatBool(s.NextEditEnabled()), true
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
	case "setting-ai-until-done-rounds":
		r, _, _ := s.UntilDone()
		return strconv.Itoa(r), true
	case "setting-ai-until-done-budget":
		if _, b, _ := s.UntilDone(); b > 0 {
			return fmt.Sprintf("%.2f", b), true
		}
		return "0 (no budget)", true
	}
	return "", false
}

// applyAISetting persists one AI row and reloads the provider. ok=false
// when the row is not an AI one.
func (m *Model) applyAISetting(id, value string) (tea.Cmd, bool) {
	if cmd, ok := m.applyMCPSetting(id, value); ok {
		return cmd, true
	}
	if c, ok := m.applyHTTPSetting(id, value); ok { // HTTP client (http_settings.go)
		return c, true
	}
	if c, ok := m.applyUpdateSetting(id, value); ok { // G-UPD (update_settings.go)
		return c, true
	}
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
	case "setting-ai-next-edit":
		b, err := parseBoolLoose(value)
		if err != nil {
			var c tea.Cmd
			m.toast, c = m.toast.Push(toast.Errr, "ai_next_edit: expected true/false")
			return c, true
		}
		s.NextEdit = &b
		label = "ai_next_edit = " + value
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
	case "setting-ai-until-done-rounds":
		n, err := strconv.Atoi(value)
		if err == nil {
			err = ai.CheckUntilDoneRounds(n)
		} else {
			err = fmt.Errorf("ai_until_done_rounds must be a whole number from 1 to %d", agentdag.MaxRounds)
		}
		if err != nil {
			var c tea.Cmd
			m.toast, c = m.toast.Push(toast.Errr, err.Error())
			return c, true
		}
		s.UntilDoneRounds = n
		label = "ai_until_done_rounds = " + value
	case "setting-ai-until-done-budget":
		usd, err := strconv.ParseFloat(strings.TrimPrefix(strings.TrimSuffix(value, " (no budget)"), "$"), 64)
		if err == nil {
			err = ai.CheckUntilDoneBudget(usd)
		} else {
			err = errors.New("ai_until_done_budget_usd must be a dollar amount like 2 or 0.50, or 0 for no budget")
		}
		if err != nil {
			var c tea.Cmd
			m.toast, c = m.toast.Push(toast.Errr, err.Error())
			return c, true
		}
		s.UntilDoneBudgetUSD = &usd
		label = fmt.Sprintf("ai_until_done_budget_usd = %.2f", usd)
		if usd == 0 {
			label = "ai_until_done_budget_usd = 0 (no budget)"
		}
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
