package app

// Docs — the user documentation site and the pure, side-effect-free
// catalogs that cmd/docgen turns into docs/reference/*.md. Nothing here
// starts nvim or a UI. When you add a key, palette command, setting or
// config file, update the matching list and run `go run ./cmd/docgen`.

import (
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/rtl"
	"github.com/amin-jalali/termocode/internal/toast"
)

// DocsURL is the user documentation site.
const DocsURL = "https://amin-jalali.github.io/termocode/"

// docsHost is DocsURL without scheme and trailing slash, for short hints.
const docsHost = "amin-jalali.github.io/termocode"

// openDocs opens DocsURL in the system browser ("Help: Open
// Documentation"). If no browser can be started, the URL is shown in a
// toast so the user can copy it.
func (m *Model) openDocs() tea.Cmd {
	var c tea.Cmd
	if err := osOpen(DocsURL); err != nil {
		m.toast, c = m.toast.PushDetail(toast.Warn, "Open the docs in your browser", DocsURL)
		return c
	}
	m.toast, c = m.toast.PushDetail(toast.Info, "Opened", DocsURL)
	return c
}

// ── Palette ─────────────────────────────────────────────────────────────

// PaletteEntry is one static command palette row.
type PaletteEntry struct {
	ID    string
	Title string
	// Hint is the text shown on the right of the palette row.
	Hint string
	// Keys are the default keymap keys (Bubble Tea strings) of the
	// command's action, sorted. Empty when the command has no key.
	Keys []string
}

// paletteActions maps a palette ID to the keymap Action that does the same
// thing, so the reference can show the real default key next to the
// command (the palette Hint is free text and can drift).
var paletteActions = map[string]keymap.Action{
	"save":                        keymap.ActionSave,
	"save-all":                    keymap.ActionSaveAll,
	"close-buffer":                keymap.ActionCloseBuffer,
	"reopen-closed":               keymap.ActionReopenClosed,
	"clipboard-history":           keymap.ActionClipboardHistory,
	"next-buffer":                 keymap.ActionNextBuffer,
	"prev-buffer":                 keymap.ActionPrevBuffer,
	"toggle-explorer":             keymap.ActionToggleExplorer,
	"toggle-actions-panel":        keymap.ActionToggleActionsPanel,
	"focus-explorer":              keymap.ActionFocusExplorer,
	"focus-editor":                keymap.ActionFocusEditor,
	"reveal-file":                 keymap.ActionRevealFile,
	"toggle-terminal":             keymap.ActionToggleTerminal,
	"open-shell":                  keymap.ActionOpenShell,
	"terminal-new-tab":            keymap.ActionTerminalNewTab,
	"terminal-close-tab":          keymap.ActionTerminalCloseTab,
	"terminal-next-tab":           keymap.ActionTerminalNextTab,
	"terminal-prev-tab":           keymap.ActionTerminalPrevTab,
	"terminal-split-right":        keymap.ActionTerminalSplitRight,
	"terminal-split-down":         keymap.ActionTerminalSplitDown,
	"terminal-focus-prev-pane":    keymap.ActionTerminalFocusPrevPane,
	"terminal-focus-next-pane":    keymap.ActionTerminalFocusNextPane,
	"quick-open":                  keymap.ActionQuickOpen,
	"duplicate-line":              keymap.ActionDuplicateLine,
	"toggle-comment":              keymap.ActionToggleComment,
	"preview-md":                  keymap.ActionMarkdownPreview,
	"find-files":                  keymap.ActionWorkspaceSearch,
	"search-next-result":          keymap.ActionNextSearchResult,
	"search-prev-result":          keymap.ActionPrevSearchResult,
	"replace-in-file":             keymap.ActionReplaceInFile,
	"toggle-inlay-hints":          keymap.ActionToggleInlayHints,
	"pick-theme":                  keymap.ActionPickTheme,
	"git-stage-hunk":              keymap.ActionStageHunk,
	"git-unstage-hunk":            keymap.ActionUnstageHunk,
	"git-discard-hunk":            keymap.ActionDiscardHunk,
	"git-stage-lines":             keymap.ActionStageLines,
	"git-unstage-lines":           keymap.ActionUnstageLines,
	"code-actions":                keymap.ActionCodeActions,
	"code-action-apply-preferred": keymap.ActionApplyPreferredCodeAction,
	"goto-symbol-file":            keymap.ActionGotoSymbolInFile,
	"bookmark-toggle":             keymap.ActionToggleBookmark,
	"bookmark-list":               keymap.ActionShowBookmarks,
	"goto-line":                   keymap.ActionGotoLine,
	"split-vertical":              keymap.ActionSplitVertical,
	"split-horizontal":            keymap.ActionSplitHorizontal,
	"close-split":                 keymap.ActionCloseSplit,
	"format-document":             keymap.ActionFormatDocument,
	"open-recent":                 keymap.ActionOpenRecent,
	"toggle-wrap":                 keymap.ActionToggleWordWrap,
	"alternate-file":              keymap.ActionAlternateFile,
	"next-diagnostic":             keymap.ActionNextDiagnostic,
	"prev-diagnostic":             keymap.ActionPrevDiagnostic,
	"toggle-hidden":               keymap.ActionToggleHidden,
	"cheat-sheet":                 keymap.ActionShowCheatSheet,
	"help-context":                keymap.ActionContextHelp,
	"buffer-info":                 keymap.ActionShowBufferInfo,
	"hover":                       keymap.ActionHover,
	"find-references":             keymap.ActionFindReferences,
	"replace-workspace":           keymap.ActionReplaceInWorkspace,
	"goto-type-def":               keymap.ActionGotoTypeDef,
	"goto-impl":                   keymap.ActionGotoImplementation,
	"run-tests":                   keymap.ActionRunTests,
	"testing-run-all":             keymap.ActionRunTests,
	"git-blame":                   keymap.ActionGitBlameLine,
	"git-history":                 keymap.ActionFileHistory,
	"zen-mode":                    keymap.ActionToggleZenMode,
	"reload-window":               keymap.ActionReloadWindow,
	"detach-session":              keymap.ActionDetachSession,
	"services-focus":              keymap.ActionServicesFocus,
	"db-focus":                    keymap.ActionDatabasesFocus,
	"http-send":                   keymap.ActionHTTPSend,
	"toggle-auto-save":            keymap.ActionToggleAutoSave,
	"next-hunk":                   keymap.ActionNextHunk,
	"prev-hunk":                   keymap.ActionPrevHunk,
	"reload-buffer":               keymap.ActionReloadBuffer,
	"open-url":                    keymap.ActionOpenURL,
	"pin-tab":                     keymap.ActionPinTab,
	"copy-file-ref":               keymap.ActionCopyFileRef,
	"snippet-picker":              keymap.ActionShowSnippetPicker,
	"debug-start":                 keymap.ActionDebugStartContinue,
	"debug-stop":                  keymap.ActionDebugStop,
	"debug-toggle-breakpoint":     keymap.ActionDebugToggleBreakpoint,
	"debug-step-over":             keymap.ActionDebugStepOver,
	"debug-step-into":             keymap.ActionDebugStepInto,
	"debug-step-out":              keymap.ActionDebugStepOut,
	"tasks-run":                   keymap.ActionRunTask,
	"tasks-rerun":                 keymap.ActionRerunTask,
	"problems-panel":              keymap.ActionShowProblems,
	"ai-open-chat":                keymap.ActionToggleActionsPanel,
	"ai-trigger-inline":           keymap.ActionAITriggerInline,
	"ai-edit":                     keymap.ActionAIEditSelection,
	"ai-toggle-inline":            keymap.ActionAIToggleInline,
	"ai-profile-next":             keymap.ActionAINextProfile,
	"ai-agents":                   keymap.ActionAIToggleAgents,
	"itermoc-open":                keymap.ActionItermocToggle,
	"git-ci-fix":                  keymap.ActionGitCIFix,
	"layout-open":                 keymap.ActionLayoutOpen,
	"web-stop":                    keymap.ActionWebStop,
	"quit":                        keymap.ActionQuit,
}

// PaletteCatalog returns every static command palette row (the same rows
// the palette shows, minus user commands from commands.json and "Ext:"
// commands from extensions, which only exist at run time), with the
// default keys of each row's action.
func PaletteCatalog() []PaletteEntry {
	km := keymap.Default()
	items := withKeyHints(append(paletteItems(), tasksPaletteItems()...), km)
	out := make([]PaletteEntry, 0, len(items))
	for _, it := range items {
		e := PaletteEntry{ID: it.ID, Title: it.Title, Hint: it.Hint}
		if a, ok := paletteActions[it.ID]; ok {
			e.Keys = km.KeysFor(a)
		}
		out = append(out, e)
	}
	return out
}

// maxHintKeys caps how many keys a palette hint lists, so the row stays short.
const maxHintKeys = 2

// withKeyHints sets the Hint of every palette item that runs a keymap
// Action (paletteActions) from the keys bound to it in km, so hints follow
// the real keymap — including keymap.json overrides — and cannot drift.
// Keys named in the static Hint come first, in that order (the static
// hint says which key is the main one); other bound keys follow. An item
// whose action has no key keeps its static Hint: that is a Neovim-side
// key (for example Shift+Alt+Down), checked by TestPaletteHintsMatchKeymap.
func withKeyHints(items []picker.Item, km keymap.KeyMap) []picker.Item {
	out := make([]picker.Item, len(items))
	copy(out, items)
	for i, it := range out {
		a, ok := paletteActions[it.ID]
		if !ok {
			continue
		}
		if h := keyHint(km.KeysFor(a), it.Hint); h != "" {
			out[i].Hint = h
		}
	}
	return out
}

// keyHint builds a hint from bound key strings: unique human names
// ("alt+T" and "alt+shift+t" are both Alt+Shift+T), keys mentioned in
// static first, then the rest sorted, at most maxHintKeys of them.
func keyHint(keys []string, static string) string {
	var names []string
	seen := map[string]bool{}
	for _, k := range keys {
		h := keymap.HumanKey(k)
		if !seen[h] {
			seen[h] = true
			names = append(names, h)
		}
	}
	if len(names) == 0 {
		return ""
	}
	pos := func(n string) int {
		for i, p := range strings.Split(static, " / ") {
			if strings.EqualFold(strings.TrimSpace(p), n) {
				return i
			}
		}
		return len(static) + 1
	}
	sort.SliceStable(names, func(i, j int) bool {
		pi, pj := pos(names[i]), pos(names[j])
		if pi != pj {
			return pi < pj
		}
		return names[i] < names[j]
	})
	if len(names) > maxHintKeys {
		names = names[:maxHintKeys]
	}
	return strings.Join(names, " / ")
}

// PaletteActionFor returns the keymap Action a palette ID runs, if any.
func PaletteActionFor(id string) (keymap.Action, bool) {
	a, ok := paletteActions[id]
	return a, ok
}

// ── Settings ────────────────────────────────────────────────────────────

// SettingEntry is one config.json key.
type SettingEntry struct {
	ID          string // Settings row id ("setting-theme"); "" for file-only keys
	Key         string
	Type        string
	Default     string
	Description string
	// Category is the Settings modal group ("Appearance", "Editor", …).
	// Keys that only exist in the file use "<group> (file only)".
	Category string
	// Choices are the values of an enum setting.
	Choices []string
}

// fileOnlySettings are config.json keys with no row in the Settings modal.
// They are set by other UI (named in the description) or by hand.
var fileOnlySettings = []SettingEntry{
	{Key: "db_max_rows", Type: "int", Default: "10000", Category: "Databases (file only)", Description: "Maximum loaded rows per SQL result (1–100000). Larger results are cut and marked."},
	{Key: "db_page_rows", Type: "int", Default: "100", Category: "Databases (file only)", Description: "Rows per SQL result page (1–1000)."},
	{Key: "db_timeout", Type: "int", Default: "30", Category: "Databases (file only)", Description: "Maximum seconds for a console statement. The session also enforces its own timeout."},
	{Key: "db_history_max", Type: "int", Default: "500", Category: "Databases (file only)", Description: "Maximum SQL history entries kept in db/history.jsonl."},
	{Key: "db_ai_max_kb", Type: "int", Default: "32", Category: "Databases (file only)", Description: "Maximum size in KB of one AI database tool answer (1–1024). Longer results are cut and marked."},
	{Key: "db_redis_separator", Type: "string", Default: ":", Category: "Databases (file only)", Description: "Separator that groups Redis keys into namespaces in the tree (session:ab12 is under session:). Empty: no grouping."},
	{Key: "db_redis_max_keys", Type: "int", Default: "10000", Category: "Databases (file only)", Description: "Most Redis keys one tree row loads with SCAN (100–1000000). Past it the row says so; open a namespace to see more."},
	{Key: "containers_engine", Type: "enum", Default: "auto", Category: "Containers (file only)", Choices: []string{"auto", "docker", "podman"},
		Description: "Container engine of the Containers view. auto tries Docker, then Podman; docker or podman uses only that engine. Press R in the view after changing it."},
	{Key: "containers_sections", Type: "string[]", Default: "all", Category: "Containers (file only)",
		Description: "Sections shown by the Containers view, across workspaces. The Containers menu writes this list; collapse state is kept in the workspace session."},
	{Key: "ai_inline_model", Type: "string", Default: "(provider default)", Category: "AI (file only)",
		Description: "Model for inline ghost-text completions. Empty uses a fast model of the provider (for example Claude Haiku or gpt-4.1-mini)."},
	{Key: "ai_auto_approve", Type: "string[]", Default: "[]", Category: "AI (file only)",
		Description: "Agent tool classes that run without asking: " + strings.Join(ai.ToolClasses, ", ") + ". Set with AI: Agent Auto-Approve…."},
	{Key: "ai_edit_review", Type: "enum", Default: "review", Category: "AI (file only)", Choices: []string{"review", "auto"},
		Description: "review: the agent's file changes (write_file, create_file, LSP rename, code actions, format) open as a diff where you accept or reject each change; the agent waits and is told what was written. auto: the old way (a confirm dialog, or no question when fileWrite is auto-approved). Auto-approving fileWrite also skips the review. Toggle with AI: Toggle Edit Review."},
	{Key: "ai_max_steps", Type: "int", Default: strconv.Itoa(ai.DefaultMaxSteps), Category: "AI (file only)",
		Description: "Most tool steps the AI agent takes for one chat message. At the limit it stops and asks you to send \"continue\"."},
	{Key: "ai_prices", Type: "object", Default: "{}", Category: "AI (file only)",
		Description: "Model prices in US dollars per million tokens, by model id prefix, over the built-in table (checked " + ai.PricesDate + "): for example `{\"my-model\": {\"in\": 3, \"out\": 15, \"cache_read\": 0.3, \"cache_write\": 3.75}}`. A missing cache price is the input price. Your entry wins over a built-in one. Ollama and LM Studio cost $0; a model with no price shows tokens only."},
	{Key: "ai_context_windows", Type: "object", Default: "{}", Category: "AI (file only)",
		Description: "Context window in tokens, by model id prefix, over the built-in table: for example `{\"qwen2.5-coder\": 32768}`. Used for the context % of a turn; a model with no window shows none."},
	{Key: "ai_prompt_cache", Type: "enum", Default: "5m", Category: "AI (file only)", Choices: ai.PromptCacheModes,
		Description: "Prompt cache of Claude (Anthropic key or Claude sign-in). 5m: the tools, the system prompt and the chat so far are cached for 5 minutes, so each next step reads them at a small part of the input price. 1h: the cache lasts an hour (writing it costs more; good when you pause between messages). off: nothing is cached."},
	{Key: "ai_reasoning", Type: "enum", Default: "(model default)", Category: "AI (file only)", Choices: ai.ReasoningLevels,
		Description: "Reasoning level of each new chat and of the main model slot: low, medium, high, xhigh or max (more thinking: better answers on hard problems, slower, more tokens). Empty: the model's default. A slot sets its own with ai_models.<slot>.reasoning. \"think\" / \"ultrathink\" in a message raise one turn to high / max; AI: Next Reasoning Level changes it for one chat."},
	{Key: "ai_fallback", Type: "string[]", Default: "[]", Category: "AI (file only)",
		Description: "Backup models of the main slot, in order: slot names or <provider>/<model>, for example `[\"gpt\", \"openrouter/anthropic/claude-sonnet-5-5\"]` (at most " + strconv.Itoa(ai.MaxFallbacks) + "). When a request fails before any text — a busy server after 3 retries, a rate, usage or credit limit, a refused key or an expired sign-in — the turn moves to the next one and the chat shows a line \"Now using …\". A slot sets its own list with ai_models.<slot>.fallback. A backup with no key is skipped."},
	{Key: "ai_concurrency", Type: "object", Default: "{}", Category: "AI (file only)",
		Description: "Most AI requests that run at the same time, per provider or per model: for example `{\"anthropic\": 2, \"openai/gpt-5\": 1}` (1 to " + strconv.Itoa(ai.MaxConcurrency) + "). Chat, agent steps and ghost text all count. A request over the limit waits before it is sent; Stop ends the wait. Use it when parallel agent steps get \"rate limit\" (HTTP 429) answers. No entry: no limit."},
	{Key: "ai_models", Type: "object", Default: "{}", Category: "AI (file only)",
		Description: "Model slots for agent profiles (ai/profiles.json): slot name → `{\"provider\", \"model\", \"base_url\"}`, for example `{\"gpt\": {\"provider\": \"openai\", \"model\": \"gpt-5-mini\"}}`; an optional `\"reasoning\"` sets the slot's reasoning level (low … max) and `\"fallback\"` its backup models (see ai_fallback). No keys here: a slot uses the key of its provider kind (environment variable, then AI: Configure Provider…). Built-in slots: `main` (ai_provider / ai_model, can not be replaced), `quick` (the main provider with ai_inline_model) and `local` (Ollama); a slot with the name quick or local replaces the built-in one. A bad entry is left out and named in the profile errors."},
	{Key: "ai_parallel_max", Type: "int", Default: "1", Category: "AI (file only)",
		Description: "How many steps of an agent profile may run at the same time (at most " + strconv.Itoa(ai.MaxParallel) + "). With 1, steps run one after the other and profiles may not use `deps`. When it is not set, a planned run (a profile with a `plan` step, like the built-in plan profile) runs up to " + strconv.Itoa(ai.DefaultPlannedParallel) + " steps at once."},
	{Key: "ai_categories", Type: "object", Default: "{}", Category: "AI (file only)",
		Description: "Step categories by name, over the built-in ones (quick, deep, visual, writing, architect, ultrabrain): `{\"ultrabrain\": {\"slot\": \"opus\", \"reasoning\": \"max\"}, \"docs\": {\"like\": \"quick\", \"tools\": \"read-only\", \"temperature\": 0.3, \"prompt_append\": \"Write for beginners.\", \"description\": \"user docs\"}}`. slot is the model slot (else the slot of the category it is like: quick or deep, default deep); tools may narrow a step's tools (none, read-only, all); temperature is 0 to 2 (sent only to models that take it); reasoning is the level of its steps (low, medium, high, xhigh, max; the think keyword can still raise it); prompt_append joins the step's prompt; description tells the planner what it is for. A profile's own categories map wins. A bad entry is left out and named in the errors log."},
	{Key: "ai_step_escalate", Type: "bool", Default: "true", Category: "AI (file only)",
		Description: "A run step that is not on the deep model may answer `ESCALATE: <why>`; it then runs once more on the deep category's model, and its card says so. false: steps are not told, and such an answer stands."},
	{Key: "ai_plan_review_rounds", Type: "int", Default: "0", Category: "AI (file only)",
		Description: "0: off. 1 to 5: a reviewer on the deep model reads a planner's plan and approves it, or sends its reasons back so the planner plans again, at most this many times; then the last plan goes on (plan approval still asks). Each round is one more model request."},
	{Key: "ai_plan_file", Type: "bool", Default: "false", Category: "AI (file only)",
		Description: "true: a planned run writes its plan to .termocode/plans/<slug>.md in the workspace: the steps with their tasks, the review rounds, the verify step's evidence and how the run ended (secrets hidden). The file shows as untracked in git unless you ignore it. false (the default): no file."},
	{Key: "ai_step_stall_minutes", Type: "int", Default: "10", Category: "AI (file only)",
		Description: "An agent step whose model sends nothing for this many minutes is stopped and started once more; a second stall fails the step. A running tool call (a build, a card that waits for you) is no stall. 0 turns it off; at most 120."},
	{Key: "ai_worktrees", Type: "enum", Default: "auto", Category: "AI (file only)", Choices: []string{"auto", "off"},
		Description: "auto: in a git repository, each agent step that runs at the same time as another writing step works in its own git worktree (branch termocode/agent-<id>), so they never change the same files. When it ends, its card shows the changed files with Review, Merge and Discard. off: every step works in your tree. Toggle with AI: Toggle Agent Worktrees."},
	{Key: "ai_plan_approval", Type: "enum", Default: "ask", Category: "AI (file only)", Choices: []string{"ask", "auto", "always"},
		Description: "ask: before an agent run starts a step that changes files or runs commands, the chat shows the plan (steps, models, budget) with Deny, Approve and Edit; a read-only plan runs at once. auto: plans run without asking. always: every plan asks, read-only ones too. AI: Toggle Plan Approval turns it on or off for one chat."},
	{Key: "ai_compact", Type: "enum", Default: "auto", Category: "AI (file only)", Choices: []string{"auto", "off"},
		Description: "auto: when a chat fills about 80% of the model's context window, or the provider says the context is too long, the older turns are summarised by the quick model (one extra request) and the chat shows \"Earlier chat summarised\". The model then gets the summary instead of those turns; rules, memory and todos are kept. off: never summarise (a long chat can fail with \"context too long\")."},
	{Key: "ai_memory_recall", Type: "enum", Default: "on", Category: "AI (file only)", Choices: []string{"on", "check", "off"},
		Description: "While memory is on: after each AI tool call of a chat, a saved lesson about the call's file or words that this chat has not seen yet is added to the tool result and the chat shows recalled: <title> (local matching, no extra request). check: the quick model is asked first if the lesson applies (one small request per new match). off: no recall."},
	{Key: "ai_web_fetch", Type: "bool", Default: "false", Category: "AI (file only)",
		Description: "true gives the AI the web_fetch tool: it reads one http(s) page as text (GET only, at most 2 MB read and 20000 characters returned, no private or local addresses, secrets blanked). Turn it on in AI: Web Tools…."},
	{Key: "ai_web_search", Type: "enum", Default: "off", Category: "AI (file only)", Choices: []string{"off", "brave", "tavily", "exa"},
		Description: "Gives the AI the web_search tool with this provider. The key is the secret web_search_key (AI: Web Tools… → Set the search key…), else BRAVE_API_KEY, TAVILY_API_KEY or EXA_API_KEY. A search sends the query to the provider."},
	{Key: "secret_store", Type: "enum", Default: "auto", Category: "AI (file only)", Choices: []string{"auto", "keyring", "age", "plain"},
		Description: "Where AI keys, sign-ins, MCP secrets and database passwords are kept. auto: the OS keyring when one answers (not over SSH, not on Termux), else an encrypted file (secrets.age, a passphrase asked once while termocode runs); never the plain file. keyring or age uses only that one (keyring with no keyring is an error, not a fallback). plain: the old ai/credentials.json (mode 0600), by choice only. An existing plain ai/credentials.json stays in use under auto until you move it (the card at start, or AI: Move Secrets to a Safer Store…). The choice is recorded in secrets.json."},
	{Key: "secret_lock_minutes", Type: "int", Default: "0", Category: "AI (file only)",
		Description: "Lock the encrypted secret file after this many idle minutes (0 = never; the passphrase stays until AI: Lock Secrets or quit). Each use of a secret starts the count again. Only while the file is unlocked does one timer run; none with 0, for the OS keyring or for the plain file. Read when the file is unlocked."},
	{Key: "mcp_trusted", Type: "object", Default: "{}", Category: "AI (file only)",
		Description: "Project MCP servers you allowed with \"Always allow\": server name → hash of its mcp.json entry. A changed entry asks again. Written by the trust dialog and AI: MCP Servers…."},
	{Key: "mcp_disabled_tools", Type: "object", Default: "{}", Category: "AI (file only)",
		Description: "MCP tools you turned off: server name → tool names. Off tools are not sent to the AI. Turn them off or on with Space in AI: MCP Servers… → a server → Show tools."},
	{Key: "services_on_quit", Type: "enum", Default: "ask", Category: "Services (file only)", Choices: []string{"ask", "keep", "stop"},
		Description: "On quit, the Docker Compose services termocode started in this session (never the ones that were already running): ask shows a card (Keep running and quit / Stop and quit / Cancel), keep leaves them running, stop runs docker compose stop for them first. Local services always stop with the editor."},
	{Key: "services_max_log_kb", Type: "int", Default: "16", Category: "Services (file only)",
		Description: "Cut the log text the AI's services_logs tool returns to this many KB (the newest lines stay)."},
	{Key: "containers_max_inspect_kb", Type: "int", Default: "64", Category: "Containers (file only)",
		Description: "Cut the JSON the AI's containers_inspect tool returns to this many KB. Env values, auth fields and secret-looking labels are masked before the cut."},
	{Key: "mcp_server", Type: "enum", Default: "auto", Category: "AI (file only)", Choices: []string{"auto", "on", "off"},
		Description: "termocode as an MCP server: outside agents (Claude Code, Codex, …) reach a short list of IDE tools (open files, diagnostics, tests, debugger, terminal and git reads) over a private socket in the session folder, open only to your own user. Tools that change something ask on a card first; files, settings, git writes and your own MCP servers are never offered. auto: serve once you start an agent tab (F1 → Agent: Start …). on: serve from start. off: never. Applied when you save config.json."},
	{Key: "mcp_server_auto_approve", Type: "object", Default: "{}", Category: "AI (file only)",
		Description: "The \"Always allow\" answers you gave on an outside agent's card (see mcp_server): agent name → tool names that run without asking for that agent. Separate from ai_auto_approve, which never applies to outside agents. The name is the agent's own claim. See and revoke them in AI: Agent Auto-Approve…."},
	{Key: "web_panel", Type: "enum", Default: "auto", Category: "Web (file only)", Choices: []string{"auto", "on", "off"},
		Description: "The web panel (experimental): your files, chats and terminals in a web browser. auto: Web: Open in Browser (or termocode web typed in a terminal) opens it on this computer in one step; it listens only on 127.0.0.1, with a one-time link. on: the same, and also over termocode ssh --web, and a script's termocode web --yes skips the card. off: nothing listens and the commands say it is off. Anyone who controls the browser tab can run any command as you. The AI can never open it. Applied when you save config.json."},
	{Key: "web_listen", Type: "enum", Default: "loopback", Category: "Web (file only)", Choices: []string{"loopback", "tailnet"},
		Description: "Where the web panel can be reached. loopback: only on this computer (127.0.0.1), or over termocode ssh --web. tailnet: also from your own devices on your Tailscale network, at https://<this computer>.<tailnet>.ts.net/; termocode runs tailscale serve while the panel runs and refuses to start when the port is public (Funnel). Needs Tailscale with HTTPS turned on."},
	{Key: "web_require_totp", Type: "bool", Default: "false", Category: "Web (file only)",
		Description: "A paired phone or tablet also types a 6-digit code from an authenticator app after its passkey. Set the code up first with Web: Set Up Sign-in Code… (its key is kept in your secret store, which must be unlocked when a device signs in). The one-time link from this computer does not need it."},
	{Key: "web_port", Type: "int", Default: "0", Category: "Web (file only)",
		Description: "The port the web panel listens on, on 127.0.0.1 only. 0: a free port each time. Else 1024..65535."},
	{Key: "web_idle_minutes", Type: "int", Default: "15", Category: "Web (file only)",
		Description: "The web panel stops after this many minutes with no use from any browser (1..120)."},
	{Key: "web_max_hours", Type: "int", Default: "12", Category: "Web (file only)",
		Description: "The web panel stops this many hours after it started, in use or not (1..24)."},
	{Key: "web_open_browser", Type: "bool", Default: "true", Category: "Web (file only)",
		Description: "Open the one-time link in the browser of this computer when the panel starts. false: only Web: Copy Link."},
	{Key: "web_max_viewers", Type: "int", Default: "4", Category: "Web (file only)",
		Description: "How many browsers may be connected at once (1..8). One more is turned away."},
	{Key: "mcp_trusted_tools", Type: "object", Default: "{}", Category: "AI (file only)",
		Description: "The tool list you trusted per MCP server: server name → hash of its tools. The first list is kept by itself; when a server's tools change, termocode asks again and, until you trust the new list, every tool of that server asks first."},
	{Key: "mouse_hover", Type: "enum", Default: "auto", Category: "Appearance (file only)", Choices: []string{"auto", "off"},
		Description: "auto: the terminal reports plain pointer motion (xterm mode 1003) while termocode has focus and no text input is open, for hover labels and row highlights. off: only clicks, drags and the wheel (mode 1002), for terminals or links where motion is slow or dropped. Read at start."},
	{Key: "rtl", Type: "enum", Default: "auto", Category: "Appearance (file only)", Choices: rtl.Choices,
		Description: "Right-to-left text (Persian, Arabic, Hebrew) in the AI chat and the Markdown preview. termocode: termocode joins the letters and puts them in reading order itself, right-aligns RTL paragraphs, and asks the terminal not to reorder (BDSM, CSI 8 l). terminal: termocode sends the text unchanged and the terminal does it. off: unchanged text, no request to the terminal. auto: terminal on Konsole and mlterm (they always reorder), else termocode. Read at start."},
	{Key: "terminal_skip_shell", Type: "string[]", Default: "[]", Category: "Terminal (file only)",
		Description: "Actions (keymap.json names) whose keys reach termocode while the terminal has focus. Other Ctrl keys go to the shell. Quit (Ctrl+Q) and ContextHelp (Shift+F1) are in the list by default; add a name, or remove one with \"-Name\" (for example `[\"QuickOpen\", \"-Quit\"]`)."},
	{Key: "terminal_restore", Type: "bool", Default: "true", Category: "Terminal (file only)",
		Description: "After a restart of termocode, the terminal tabs of the project come back: each tab and split pane gets a fresh shell in the folder it was in, and the same tab and pane have focus. Output, scrollback and environment values are never kept. A pane that was running a program shows its name. A folder that is gone starts in the project folder. false: the panel starts empty. Read at start."},
	{Key: "terminal_restore_commands", Type: "bool", Default: "false", Category: "Terminal (file only)",
		Description: "With terminal_restore: also keep the last command line of a pane that was running a program (needs shell integration), so after a restart Enter in that pane runs it again. Command lines can hold secrets, so this is off by default; off, only the program name is kept. Read at start."},
	{Key: "terminal_shell_integration", Type: "bool", Default: "true", Category: "Terminal (file only)",
		Description: "bash, zsh and fish terminal tabs load a small script (after your own rc file) that tells termocode each command, its exit code and the folder (OSC 133 / OSC 7). It gives the tab its folder name, the \"✦ Fix?\" chip on a failed command and the CLI agent marks. false starts the shell exactly as before. Read when a terminal tab opens; needs Neovim 0.10+."},
}

// SettingsCatalog returns every config.json setting in Settings modal
// order (grouped by category), then the file-only keys.
func SettingsCatalog() []SettingEntry {
	cats := settingsCategories()
	var order []string
	byCat := map[string][]SettingEntry{}
	for _, r := range settingsRows {
		c, ok := cats[r.ID]
		if !ok {
			c = "Editor" // same fallback as newSettingsModal
		}
		if _, seen := byCat[c]; !seen {
			order = append(order, c)
		}
		byCat[c] = append(byCat[c], SettingEntry{
			ID: r.ID, Key: r.Label, Type: r.Kind, Default: r.Default, Description: r.Desc, Category: c,
			Choices: settingChoices(r.ID),
		})
	}
	var out []SettingEntry
	for _, c := range order {
		out = append(out, byCat[c]...)
	}
	return append(out, fileOnlySettings...)
}

// EnvVar is one environment variable termocode reads.
type EnvVar struct{ Name, Description string }

// EnvCatalog lists the environment variables users can set, sorted.
func EnvCatalog() []EnvVar {
	out := []EnvVar{
		{"XDG_CONFIG_HOME", "Moves the config folder to `$XDG_CONFIG_HOME/termocode`."},
		{"XDG_DATA_HOME", "Moves the data folder to `$XDG_DATA_HOME/termocode`."},
		{"TERMOCODE_FRAME_PACING", "on / off: wait for the terminal before sending more frames, so a held key stops at once on a slow line (default: on over SSH)."},
		{"TERMOCODE_FONT_DELTA", "Font size change asked of the terminal at launch (kitty with remote control only); wins over font_delta in config.json (default 0 = off)."},
		{"TERMOCODE_PROBE", "`off`: never ask the terminal what it can do at start (images, cell size, kitty keyboard); the environment decides. A terminal that did not answer is not asked again for 7 days (`$XDG_STATE_HOME/termocode/termcaps.json`)."},
		{"TERMOCODE_SYNC", "on / off: draw each frame as one (synchronized output, mode 2026), so no half-drawn frame shows on a slow link (default: on when the terminal says it supports it)."},
		{"TERMOCODE_ICON_MODE", "Icon set: `nerd_font`, `unicode` or `ascii`."},
		{"TERMOCODE_NERD_FONT", "Legacy: `1` is the same as `TERMOCODE_ICON_MODE=nerd_font`."},
		{"TERMOCODE_PLUGINS_DIR", "Folder for the Neovim plugins termocode clones on first launch."},
		{"TERMOCODE_TOOLS_DIR", "Folder for language servers and debug adapters installed by termocode."},
		{"TERMOCODE_AI_PROVIDER", "AI provider; wins over `ai_provider` in config.json."},
		{"TERMOCODE_AI_MODEL", "AI chat model; wins over `ai_model`."},
		{"TERMOCODE_AI_BASE_URL", "AI API base URL; wins over `ai_base_url`."},
		{"TERMOCODE_AI_API_KEY", "AI API key for the selected provider; wins over saved keys."},
		{"ANTHROPIC_API_KEY", "Anthropic API key. Also selects Anthropic when no provider is set."},
		{"OPENAI_API_KEY", "OpenAI API key. Also selects OpenAI when no provider is set."},
		{"OPENROUTER_API_KEY", "OpenRouter API key."},
		{"OLLAMA_HOST", "Ollama host, used for the base URL of the ollama provider."},
		{"XDG_STATE_HOME", "Moves the hot-exit backups of unsaved files to `$XDG_STATE_HOME/termocode/backup`."},
		{"TERMOCODE_NO_SESSION", "Any value: run without a persistent session, like `--no-session`."},
		{"XDG_RUNTIME_DIR", "Folder for the persistent-session sockets (`$XDG_RUNTIME_DIR/termocode`); without it they go to `~/.cache/termocode/sessions`."},
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ── Config files ────────────────────────────────────────────────────────

// ConfigFile is one file or folder termocode reads or writes.
type ConfigFile struct {
	// Location is "config", "data", "workspace" or "other".
	Location string
	// Path is relative to the location's folder (or absolute-ish for
	// "other", e.g. "~/termocode-settings.json").
	Path    string
	Purpose string
	Format  string
	// Edit says whether hand edits are safe, and how else to change it.
	Edit string
}

// ConfigFileCatalog lists every file termocode reads or writes, in a
// fixed order (location, then path).
func ConfigFileCatalog() []ConfigFile {
	out := []ConfigFile{
		{"config", "config.json", "Settings (theme, editor, search, AI). See the settings reference.",
			"JSON object. Unknown keys are kept on save.", "Yes. Also Preferences: Open Settings (UI)."},
		{"config", "keymap.json", "Key binding overrides. See the keys reference.",
			"JSON object: key string → action name, or `\"ext:<command id>\"` for an extension command.",
			"Yes. Also Preferences: Customize Keybindings."},
		{"config", "commands.json", "User commands shown in the palette as `User: <title>`.",
			"JSON array of `{\"id\", \"title\", \"cmd\", \"cwd\"}`.", "Yes. Open with Open Config: User Commands."},
		{"config", userSnippetsFile, "User snippets, layered over the built-in ones.",
			"JSON object: scope (filetype or `*`) → trigger → body (string or array of lines).",
			"Yes. Also Snippets: Manage…."},
		{"config", "user_theme.json", "Custom color theme saved by the theme editor.",
			"JSON object: palette key (for example `BgEditor`) → hex color. Used by the `custom` theme.",
			"Yes, but Preferences: Edit Custom Theme is easier."},
		{"config", "breakpoints.json", "Saved debugger breakpoints per file (line and line text).",
			"JSON `{\"version\", \"breakpoints\": {file: [...]}}`.", "Not needed. termocode rewrites it when breakpoints change."},
		{"config", "sessions/<hash>.json", "Session of one workspace: open files, tab order, cursor positions, expanded folders, extra workspace roots, last AI chat. The name is a hash of the workspace path; the `workspace` key says which folder.",
			"JSON, written by termocode.", "No (state). Developer: Reset Session clears the current workspace's file and keeps a `.bak`."},
		{"config", "session.json", "Layout kept across all workspaces: terminal height, Actions panel, Source Control view. A session.json from before per-workspace sessions moves to `sessions/` the first time its workspace opens.",
			"JSON, written by termocode.", "No (state). Safe to delete."},
		{"config", "recents.json", "Recent files for the welcome screen and File: Open Recent….",
			"JSON array.", "No (state). Safe to delete."},
		{"config", "workspaces.json", "Recent workspace folders for File: Open Recent Workspace….",
			"JSON array.", "No (state). Safe to delete."},
		{"config", "palette_recents.json", "Recently used palette commands (shown first).",
			"JSON array of command IDs.", "No (state). Safe to delete."},
		{"config", "errors.log", "Log of error and warning toasts. `tail -f` it while debugging.",
			"Plain text, one timestamped line per entry; secrets are redacted.", "Safe to delete."},
		{"config", "settings-backup-<time>.json", "Backup written before Preferences: Import termocode Settings File….",
			"Settings bundle (same shape as an export).", "Safe to delete."},
		{"config", "extensions/<name>/", "Lua extensions: `extension.json` manifest and `init.lua`.",
			"Folder per extension.", "Yes. Extensions: New Extension… creates one."},
		{"config", "secrets.json", "Index of your saved secrets: each name, whether it is a value or a reference, the backend in use and when it changed. It never holds a secret value, only names and reference text such as `${env:NAME}`.",
			"JSON `{\"version\", \"backend\": \"keyring\"|\"age\"|\"plain\", \"entries\": {name: {\"kind\", \"ref\", \"backend\", \"updated\"}}}`.",
			"Not needed. Use AI: Secret Store… to see and forget names."},
		{"config", "secrets.age", "Your secrets in an encrypted file (age, passphrase), used when there is no OS keyring, for example over SSH or on Termux. File mode 0600. termocode asks for the passphrase once while it runs.",
			"One age file holding a JSON object `{name: value}`. Binary, not for editing.",
			"No. Back it up with care: without the passphrase it can not be read."},
		{"config", "web/devices.json", "The phones and tablets paired with the web panel (Web: Pair a Device…): name, role, and the passkey's public key and counter. No secret: a public key cannot sign in. File mode 0600 (folder 0700).",
			"JSON `{\"version\", \"devices\": [{\"id\", \"name\", \"role\", \"guest\", \"cred_id\", \"public_key\", \"sign_count\", \"user_id\", \"rp_id\", \"created\", \"last_seen\"}]}`.",
			"Use Web: Devices… to remove one. Delete the file to forget every device."},
		{"config", "ai/credentials.json", "The old plain secret file: AI API keys, Claude OAuth tokens, MCP secrets and MCP sign-ins, not encrypted. File mode 0600 (folder 0700). Used only while secret_store is plain, or until AI: Move Secrets to a Safer Store… moves it to the OS keyring or secrets.age; the file is deleted only after you confirm on a card.",
			"JSON `{\"keys\": {provider: key}, \"claude_oauth\": {...}, \"mcp_secrets\": {name: value}, \"mcp_oauth\": {server: {issuer, resource, tokens…, account}}, \"mcp_accounts\": {server: account}}`.",
			"With care. Prefer AI: Configure Provider…. Never exported by Preferences: Export termocode Settings File…."},
		{"config", "ai/credentials.json.migrated", "Empty marker written after the secrets were moved out of ai/credentials.json. While it exists termocode does not offer the move again.",
			"Empty file, mode 0600.", "Yes. Delete it to be asked again when a plain ai/credentials.json comes back."},
		{"config", "ai/profiles.json", "Your agent profiles: named pipelines of AI steps (built in: default, review, fast, local-only, plan; yours win by name, except default).",
			"JSON `{\"profiles\": {name: {\"title\", \"categories\": {\"quick\"|\"deep\": slot}, \"budget_usd\", \"steps\": [{\"id\", \"title\", \"slot\", \"category\", \"system\", \"tools\": {\"preset\": \"none\"|\"read-only\"|\"all\", \"allow\", \"deny\"}, \"output\": \"shown\"|\"folded\"|\"hidden\", \"max_steps\", \"deps\", \"plan\", \"cli\", \"provider\", \"model\"}]}}}`. At most 12 steps, system prompts up to 8 KB; `deps` needs ai_parallel_max 2 or more; at most one `plan` step; `budget_usd` pauses a run at that cost. Slots come from ai_models in config.json; a step with `provider` + `model` needs no slot; no keys in this file.",
			"Yes. A broken file is skipped (the built-in profiles stay) and the error names the line."},
		{"config", "ai/providers.json", "The AIs you connected, one per provider kind: how (an API key in the secret store, a CLI's own sign-in, a server on this machine, a server URL) and where. Profile steps with `provider` take the server URL from here. Without the file, termocode reads the list from your setup (stored keys, ai_provider).",
			"JSON `{\"providers\": {kind: {\"method\": \"key\"|\"cli\"|\"local\"|\"url\", \"cli\", \"base_url\", \"title\", \"added_at\"}}}`. No keys or passwords: a `base_url` with a user or password is refused. Unknown keys are kept.",
			"Yes. An entry with an unknown kind or method is skipped."},
		{"config", "ai/chats/*.json", "Saved AI chats (newest 50 kept).",
			"JSON, one file per chat.", "Safe to delete."},
		{"config", "mcp.json", "Your MCP servers: their tools are offered to the AI chat. See the MCP guide.",
			"The shape other apps write: `{\"mcpServers\": {name: {\"command\", \"args\", \"env\", \"cwd\"}}}` for local servers, `{\"type\": \"http\", \"url\", \"headers\"}` for remote ones, plus `readOnly`, `autoApprove`, `deny`, `timeout`, `disabled`, `oauth` (`client_id`, `client_secret`, `scopes` of a pre-registered app) and `auth` (`{\"gh\": \"<login>\"}`: the token comes from the GitHub CLI; only in this file, not in a project's). Secrets as `${secret:NAME}` (from your secret store) or `${VAR}`.",
			"Yes. Open with Open Config: MCP Servers."},
		{"data", "plugins/", "Neovim plugins cloned in the background on first launch (nvim-dap, vim-visual-multi, rainbow-delimiters).",
			"Git checkouts. `$TERMOCODE_PLUGINS_DIR` moves it.", "Delete to re-clone on next launch."},
		{"data", "tools/", "Language servers and debug adapters installed by LSP: Manage Language Servers… and DAP: Install Adapter….",
			"One folder per tool; `tools/bin` holds shims on Neovim's PATH. `$TERMOCODE_TOOLS_DIR` moves it.",
			"Use the palette commands. Delete a tool folder to remove it."},
		{"data", "tools/asked.json", "Filetypes already asked about in the \"no language server\" toast.",
			"JSON `{\"filetypes\": [...]}`.", "Safe to delete (the toasts come back)."},
		{"workspace", ".termocode/tasks.json", "Workspace tasks for Tasks: Run Task….",
			"VS Code style tasks: `label`, `command`, `args`, `cwd`, `env`, `group`, `problemMatcher`.",
			"Yes. Tasks: Configure Tasks creates it."},
		{"workspace", ".termocode/services.json", "Local dev services for the Services view (and the compose file / profiles to use).",
			"JSONC: `compose` {`file`, `profiles`, `project`}, `services` [{`name`, `command`, `cwd`, `env`, `port`, `health` {`tcp` | `http`}}].",
			"Yes. Containers: Configure Services creates it. A local command asks once before its first start."},
		{"workspace", launchJSONRel, "Debug and run configurations.",
			"VS Code `launch.json` shape; comments allowed.", "Yes. Debug: Open launch.json creates it."},
		{"workspace", ".mcp.json", "Project MCP servers in the common file other apps read too. Read before .termocode/mcp.json, which wins on a name clash.",
			"Same shape as the user mcp.json.", "Yes. Same trust question as .termocode/mcp.json; `${cmd:}`, `auth.gh` and a `headersCommand` are refused."},
		{"workspace", ".termocode/mcp.json", "Project MCP servers. An entry with the same name as one in your mcp.json replaces it; `\"disabled\": true` hides it in this project.",
			"Same shape as the user mcp.json.", "Yes. termocode asks before it starts a project server the first time (and again when the entry changes)."},
		{"workspace", ".termocode/ai-profiles.json", "Project agent profiles. A profile with the same name as one of yours replaces it in this project.",
			"Same shape as ai/profiles.json; a `models` key is ignored (a project can not point you at another server).", "Yes. A broken file is skipped and the error names the line."},
		{"workspace", "AGENTS.md", "Project rules for the AI chat: they go into its system prompt (see Project rules file in the Use AI guide).",
			"Plain text or Markdown; at most 16 KB (together with .termocode/rules.md) is used.", "Yes. termocode asks once per workspace before it uses the rules, and again when the text changes."},
		{"workspace", ".termocode/rules.md", "More project rules for the AI chat, after AGENTS.md when both exist.",
			"Same as AGENTS.md.", "Yes. Same question as AGENTS.md."},
		{"workspace", "CLAUDE.md", "Project rules for the AI chat, read like AGENTS.md. In a sub folder (also AGENTS.md there) they apply to the files below it and are added when the AI reads one.",
			"Same as AGENTS.md; at most 4 KB of a folder file is added per read.", "Yes. Same question as AGENTS.md."},
		{"workspace", ".claude/rules/*.md", "Rules for some files: `globs:` or `paths:` in the front matter choose the files; `alwaysApply: true` (or no front matter) puts them in the system prompt.",
			"Markdown or .mdc with an optional `---` front matter.", "Yes. Same question as AGENTS.md."},
		{"workspace", ".termocode/memory/lessons.md", "AI memory: lessons the AI wrote after earlier chats in this project (see Memory in the Use AI guide).",
			"Markdown; one `## <date> · <title> {#id}` section per lesson, an optional `paths:` line; at most 200 lessons and 64 KB.",
			"Yes. Written only after you turn memory on; AI: Memory… lists and forgets lessons. termocode adds `.termocode/memory/` to .gitignore."},
		{"workspace", ".editorconfig", "Indent and whitespace rules, read by Neovim's EditorConfig support.",
			"EditorConfig INI.", "Yes (read only for termocode)."},
		{"other", "~/termocode-settings.json", "Default target of Preferences: Export termocode Settings File….",
			"Settings bundle: `{\"termocode_settings\": 1, \"files\": {...}}`. State files and secrets are left out.",
			"Yes, but re-export is easier."},
		{"other", "~/.local/share/fonts/JetBrainsMonoNF/", "Nerd Font installed by `termocode setup` (Linux).",
			"Font files.", "Safe to delete."},
		{"other", "~/.docker/config.json", "Registry hosts and credential helper names for the Containers view's REGISTRIES section (`$DOCKER_CONFIG/config.json`; Podman: `$REGISTRY_AUTH_FILE` or `$XDG_RUNTIME_DIR/containers/auth.json`). termocode reads host and helper names only, never the stored logins, and never writes it.",
			"The engine's own JSON (`auths`, `credsStore`, `credHelpers`).", "Yes, with `docker login` / `docker logout`; read only for termocode."},
		{"other", "~/.cache/termocode/mcp/<name>.tools.json", "Tool list of each MCP server, so a chat turn does not start the server just to list its tools. `$XDG_CACHE_HOME` moves it.",
			"JSON, written by termocode; keyed by a hash of the server's mcp.json entry.", "Safe to delete."},
	}
	out = append(out, ConfigFile{"config", "db/history.jsonl", "Redacted SQL history, capped at 4 KB per entry; mode 0600.", "JSON lines; db_history_max trims the list.", "Databases: History… → Clear history…."}, ConfigFile{"config", "db/consoles/", "SQL consoles per workspace and connection, mode 0600.", "SQL with a -- @conn <id> header.", "Yes. Press n on a database connection."})
	return append(out, httpConfigFiles()...) // HTTP client (http_settings.go)
}

// ── Neovim-side keys ────────────────────────────────────────────────────

// EditorKey is a key handled inside Neovim (lsp_lua.go, multi_cursor.go,
// git_conflicts.go, …) rather than by the keymap package. These can not
// be changed in keymap.json.
type EditorKey struct {
	Key, Mode, Description string
}

// EditorKeyCatalog lists the Neovim-side keys termocode sets up.
func EditorKeyCatalog() []EditorKey {
	return []EditorKey{
		{"F12 / gd", "Normal", "Go to definition (LSP)."},
		{"gD", "Normal", "Go to declaration (LSP)."},
		{"gi", "Normal", "Go to implementation (LSP)."},
		{"gr / Shift+F12", "Normal", "List references (LSP)."},
		{"gt", "Normal", "Go to type definition (LSP)."},
		{"K", "Normal", "Hover documentation (LSP)."},
		{"F2", "Normal", "Rename symbol (LSP)."},
		{"Ctrl+Space", "Normal", "Signature help (LSP)."},
		{"Ctrl+Space", "Insert", "Open completion."},
		{"<leader> (wait)", "Normal", "Key hints: a box lists the next keys after the leader key (setting key_hints; Esc closes it)."},
		{"<leader>f", "Normal", "Format the buffer (LSP)."},
		{"<leader>x / <leader>X", "Normal", "In a SQL console: show the query plan of the statement at the cursor / EXPLAIN ANALYZE it (asks first, the statement runs)."},
		{"Tab / Enter", "Insert", "Accept the completion item. Tab also accepts AI ghost text and jumps to the next snippet field."},
		{"Shift+Tab", "Insert", "Previous completion item, or previous snippet field."},
		{"Tab", "Normal, Insert", "AI next edit (marker \"⇥ next edit\"): the first Tab goes to the suggested edit and shows it, the next Tab applies it (one undo step)."},
		{"Esc", "Normal", "Dismiss the AI next edit suggestion."},
		{"Ctrl+Z / Ctrl+Y", "Insert", "Undo / redo."},
		{"Alt+Up / Alt+Down", "All", "Move the line or selection up / down."},
		{"Shift+Alt+Up / Shift+Alt+Down", "All", "Duplicate the line or selection up / down."},
		{"Ctrl+D", "Normal", "Multi-cursor: select the next match of the word."},
		{"Ctrl+Alt+Up / Ctrl+Alt+Down", "Normal, Insert", "Multi-cursor: add a cursor above / below."},
		{"]x / [x", "Normal", "Next / previous merge conflict."},
		{"Esc", "Insert", "Back to Normal mode — the editor is real Neovim."},
	}
}
