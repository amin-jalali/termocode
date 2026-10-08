# Termocode — Task List

Roadmap tasks for closing the gaps vs VSCode / LazyVim / Zed / Helix / Oni.
Sizes: **S** ≈ 1–3 days · **M** ≈ 1–2 weeks · **L** ≈ 3–6 weeks.
"Port" = logic ported from `/home/fu/mobocode` (Dart → Go).

## How the groups work

- **Group 0 (Base)** is small and goes first. Some groups need it.
- **Groups A–J** are independent of each other. Any of them can run in parallel, in any order.
- **Glue tasks** (end of file) connect two groups. Do each one after both of its groups are done.

| Group | Needs Base? | Size |
|---|---|---|
| 0 Base | — | S/M |
| A AI | yes (event channel) | L |
| B Search | no | S/M |
| C Problems + Tasks | yes (panel framework) | M |
| D Run & Debug | yes (both) | M |
| E Testing | yes (panel framework) | M |
| F LSP/DAP installer | no | M |
| G Git extras | no | S/M |
| H Settings & editor extras | no | S/M |
| I Extensions | yes (event channel) | M→L |
| J Docs & community | no | S |

---

## Group 0 — Base (do first)

### Event channel nvim → Go (S)
- [ ] `internal/nvim/client.go`: add `RegisterNotify(method, fn)` next to the `"redraw"` handler; emit `NotifyMsg{Method, Args}` through `Next()`
- [ ] Expose `ChannelID()` (from `nvim_get_api_info`) so Lua can `vim.rpcnotify(...)`
- [ ] Add `CursorContext()` helper (path, filetype, line, col, before, after)
- [ ] `internal/app/update.go`: handle `NotifyMsg` in `handleNvimMsg`
- [ ] Go-side debounce helper for noisy events

### Bottom panel framework (M)
- [ ] `shell.go` + new `panel.go`: tab `kind` = terminal | problems | output | tests | debugConsole; Go overpaints non-terminal tabs
- [ ] `Ctrl+Shift+PgUp/PgDn` cycles all tabs; mouse clicks on tabs; drag-resize unchanged
- [ ] Palette: `View: Output`; generic "Output" tab other groups can write to

---

## Group A — AI (L) · needs Base

**Provider layer — new `internal/ai/`**
- [ ] `provider.go`: `Message`, `Request`, `Chunk`, `Provider` interface, `Config`
- [ ] `openai.go` + test: SSE streaming (OpenAI, OpenRouter, Ollama, LM Studio) — port `openai_compatible_provider.dart`
- [ ] `anthropic.go` + test: Messages API streaming with API key — port `anthropic_provider.dart`
- [ ] `config.go`: non-secret config in `config.json`; secrets in `~/.config/termocode/ai/credentials.json` (0600); env-var precedence; `Redact()`
- [ ] `chatstore.go`: chats in `~/.config/termocode/ai/chats/` — port `chat_store.dart`

**Inline ghost-text completion**
- [ ] `internal/ai/inline.go` + test: `ResolveInlineCompletion`, `BuildInlinePrompt`, `ParseInlineResponse` — port `inline_completion.dart` + its tests
- [ ] new `internal/app/ai_lua.go`: `_G._termocode_ai` (context, show/clear/has/accept ghost, extmarks, `TextChangedI` → notify)
- [ ] `internal/app/snippets_lua.go`: Tab chain = ghost → snippet → completion → placeholder → tab
- [ ] new `internal/app/ai_inline.go`: debounce (300 ms), cancel on move, size cap, toggle
- [ ] `internal/app/model.go`: load `aiLua` after `snippetsLua`; add AI state fields

**Chat panel (right side)**
- [ ] new `internal/app/ai_panel.go`: replace body of `actions_panel.go` (keep pin/close/resize/persist)
- [ ] new `internal/app/ai_chat.go`: streaming `tea.Cmd`, `aiChunkMsg`/`aiDoneMsg`, attach file/selection, save chats
- [ ] `update.go`: route keys when `aiPanelFocused`; add panel to `F6` focus cycle
- [ ] `mouse.go`: panel input/scroll + status-bar badge hit-tests
- [ ] Restore last chat + scroll position on reopen (`session.go`: `AILastChat`)

**AI code actions (review-then-apply)**
- [ ] new `internal/app/ai_actions.go`: Explain (preview), Fix diagnostic, Edit selection, Doc comment
- [ ] Proposals open in the side-by-side nvim diff; Apply via `confirm` → one undo step
- [ ] `code_actions_extra.go` + `code_actions.go`: "AI" group after Quick Fix, icon `✦`

**Wiring & polish**
- [ ] `palette.go`: AI: Open Chat / Explain / Fix / Edit / Toggle Inline / Configure / Pick Model / New Chat / History
- [ ] `keymap.go` + `overrides.go`: `Alt+A` panel, `Alt+\` trigger, `Alt+I` edit, toggle inline
- [ ] `overflow_menu.go`, `menu.go`: AI entries
- [ ] Settings: `ai_inline`, `ai_provider`, `ai_model`, `ai_base_url`; "Open Config: AI Credentials"
- [ ] `statusbar/model.go`: `✦ <model>` badge (unconfigured / idle / streaming)
- [ ] Theme tokens `AIGhost`, `AIAccent` in all themes + custom theme list
- [ ] `error_log.go`: pass AI errors through `ai.Redact()`
- [ ] Docs: `FEATURE.md` §22 "AI Assistant", `docs/architecture.md`

**Milestone check (AI foundation)**
- [ ] With a key/Ollama set, ghost text shows in ~1 s and Tab accepts without duplicating the prefix
- [ ] With no key, every AI entry shows a toast and nothing else changes
- [ ] "✦ Fix with AI" applies as one `u`-undoable step
- [ ] Theme switch recolors ghost text live

**AI agent (after the milestone above)**
- [ ] `internal/ai/agent.go` + `tools.go`: port `ai_tools.dart` / `ai_agent.dart` (6-step cap)
- [ ] Tools on top of `internal/git`, `internal/search`, file ops, LSP, run command
- [ ] Confirm gate for writes + per-class auto-approve (fileWrite / git / run)
- [ ] Native tool calling for OpenAI / Anthropic
- [ ] Claude OAuth sign-in (opt-in, with ToS note) — port `claude_oauth.dart`

---

## Group B — Search (S/M) · independent
- [ ] `search.go`: `MaxResults` → setting `search_max_results` (default 2000, 0 = unlimited)
- [ ] `find_results.go`: stream results in batches; footer "Showing N of M"
- [ ] `search/model.go`: `Aa` / `ab` / `.*` toggles, include/exclude globs, context lines
- [ ] Replace-in-workspace: confirm with file count before writing

---

## Group C — Problems + Tasks (M) · needs Base

### Problems panel
- [ ] new `problems_panel.go`: bottom tab, grouped by file, severity filters, `PROBLEMS (n)` title, Enter/click jumps
- [ ] `lsp_lua.go`: `DiagnosticChanged` → notify (live refresh)
- [ ] Status-bar error/warning counts open Problems
- [ ] Palette: `View: Problems`

### Tasks system
- [ ] new `internal/tasks/`: `.termocode/tasks.json` (label, command, args, cwd, env, group, problemMatcher)
- [ ] Auto-detect: go, `package.json` scripts, Makefile, cargo, pytest, justfile — port `run_targets.dart`
- [ ] Move `commands.json` user commands onto tasks
- [ ] Palette: Run Task… / Build / Test / Rerun Last / Terminate / Configure Tasks
- [ ] Keys: `Alt+R` run task, `Alt+Shift+R` rerun last
- [ ] Run each task in a PTY terminal tab; exit status → tab dot
- [ ] `matchers.go`: `file:line(:col)` parser (port `terminal_link_parser.dart`) → `vim.diagnostic.set` → gutter + Problems
- [ ] Clickable `file:line` links in any terminal tab (reuses `matchers.go`)

---

## Group D — Run & Debug (M) · needs Base
- [ ] Run view (`view.go` placeholder) → RUN & DEBUG accordion: toolbar, Configurations, Breakpoints, Call Stack, Variables, Watch
- [ ] `dap_lua.go`: JSON helpers (`frames`, `scopes`, `variables`, `threads`, `evaluate`) replace text formatters
- [ ] `dap.listeners` stopped/terminated/initialized → notify; make `dapSessionActive` real state
- [ ] Debug Console as a bottom-panel tab (output + REPL)
- [ ] `.termocode/launch.json` + auto-detected configs — port `run_config.dart`
- [ ] Persist breakpoints in `~/.config/termocode/breakpoints.json`; restore on `BufReadPost`, re-check line numbers
- [ ] Keys: `Alt+F5` start/continue, `Shift+Alt+F5` stop; `● DEBUG` badge clickable

---

## Group E — Testing (M) · needs Base
- [ ] New Testing activity icon + TESTS tree (package → file → test) with status glyphs and counts
- [ ] new `internal/tests/`: discovery for Go, pytest, Rust, JS/TS — port `test_discovery.dart`
- [ ] Parsers: `go test -json` (port `test_result_parser.dart`), pytest junitxml, cargo, `jest --json`
- [ ] Actions: Run All, Rerun Failed, Refresh, Collapse; row keys `Enter` / `r` / `o`
- [ ] Inline failure message; click jumps to failure line
- [ ] TEST RESULTS bottom tab; status bar `✓ n ✗ n`
- [ ] Move `test_runner.go` and `Ctrl+.` "Run Test" onto this runner

---

## Group F — LSP/DAP installer (M) · independent
- [ ] new `internal/lspinstall/`: registry `{Name, Filetypes, Bin, InstallKind, Spec, Version}` — port `language_registry.dart` / `toolchain_catalog.dart`
- [ ] Install kinds: go, npm, pip, cargo, rustup, github-release → `~/.local/share/termocode/tools/<name>/`
- [ ] Generalize `download()` / `unzipTTF()` in `internal/setup/setup.go`
- [ ] Add tools bin dir to the `nvim --embed` PATH; let `lsp_lua.go` accept absolute paths
- [ ] Palette: `LSP: Manage Language Servers…`, `DAP: Install Adapter…`
- [ ] One-time toast for a missing server on first open of a filetype
- [ ] Status-bar LSP chip (`{} gopls` / `{} none`)
- [ ] Merge `reportOtherLSPs` into the registry; `termocode setup` uses the same table

---

## Group G — Git extras (S/M) · independent
- [ ] Merge-conflict resolver: CONFLICTS section, `]x` / `[x`, Accept Ours/Theirs/Both (port `conflict_parser.dart`)
- [ ] Dashboard "Clone repository" card + HTTPS token prompt

---

## Group H — Settings & editor extras (S/M) · independent
- [ ] Settings export/import (minus secrets) — port `settings_sync.dart` idea
- [ ] Keymap conflict warning in `keybinding_ui.go`
- [ ] Check built-in EditorConfig is on; show it in Buffer Info
- [ ] User snippets manager (`snippets.json`, create/edit/delete, scope filter)

---

## Group I — Extensions (M→L) · needs Base
Best started after A/C/D/E so the API is shaped by real panels — but not blocked by them.
- [ ] new `internal/app/ext_lua.go`: `_G.termocode` API — `register_command`, `register_panel`, `register_status_item`, `on`, `notify`, `prompt`, `pick`, `preview`
- [ ] new `internal/ext/`: load `~/.config/termocode/extensions/<name>/init.lua` (+ optional `extension.json`)
- [ ] Palette items `Ext: <Title>`; extension activity-bar items (`activity.SetExtraItems`)
- [ ] Generic text-panel renderer with theme-token markup
- [ ] `keymap.json` support for `"ext:<id>"`
- [ ] Palette: Extensions: Reload / Open Folder / Show Log / New Extension
- [ ] A broken extension never blocks startup (pcall + toast + Error Log)
- [ ] Sample extensions: TODO tree, word count, Open in GitHub
- [ ] REST client (`.http` files) as an extension — port `http_file_parser.dart`
- [ ] Later: Tier 2 process extensions over JSON-RPC (reuse `mobocode/agent/rpc.go`)

---

## Group J — Docs & community (S) · independent
- [ ] Fix `keys.json` → `keymap.json` in `docs/architecture.md`
- [ ] Add screenshots / asciinema to `assets/screenshots/`
- [ ] Seed `docs/adr/` and `docs/qc/` from mobocode templates
- [ ] `CONTRIBUTING.md`
- [ ] Homebrew tap / AUR via `release.yml`
- [ ] `Help: Run Doctor` palette entry (in-app `setup.Run` checks)

---

## Glue tasks (do after both groups are done)
- [ ] **D + E:** "Debug this test" (`d` key in TESTS tree) uses Run & Debug configs
- [ ] **D + F:** Run view "install dlv / debugpy" hint opens `DAP: Install Adapter…`
- [ ] **C + E:** Tasks "Test" group runs through the Testing runner
- [ ] **A + C:** "✦ Fix with AI" action on rows in the Problems panel
- [ ] **I + C:** move `commands.json` user commands into a built-in extension
