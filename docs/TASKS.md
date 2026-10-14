# Termocode — Task List

Roadmap tasks for closing the gaps vs VSCode / LazyVim / Zed / Helix / Oni.
Sizes: **S** ≈ 1–3 days · **M** ≈ 1–2 weeks · **L** ≈ 3–6 weeks.
"Port" = logic ported from `/home/fu/mobocode` (Dart → Go).

**Status:** groups 0 and A–J are merged and checked against the code. Still
open: Tier 2 process extensions (I), screenshots (J), and the I + C glue task
(left out on purpose, see its note).

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
- [x] `internal/nvim/client.go`: add `RegisterNotify(method, fn)` next to the `"redraw"` handler; emit `NotifyMsg{Method, Args}` through `Next()`
- [x] Expose `ChannelID()` (from `nvim_get_api_info`) so Lua can `vim.rpcnotify(...)`
- [x] Add `CursorContext()` helper (path, filetype, line, col, before, after)
- [x] `internal/app/update.go`: handle `NotifyMsg` in `handleNvimMsg`
- [x] Go-side debounce helper for noisy events

### Bottom panel framework (M)
- [x] `shell.go` + new `panel.go`: tab `kind` = terminal | problems | output | tests | debugConsole; Go overpaints non-terminal tabs
- [x] `Ctrl+Shift+PgUp/PgDn` cycles all tabs; mouse clicks on tabs; drag-resize unchanged
- [x] Palette: `View: Output`; generic "Output" tab other groups can write to

---

## Group A — AI (L) · needs Base

**Provider layer — new `internal/ai/`**
- [x] `provider.go`: `Message`, `Request`, `Chunk`, `Provider` interface, `Config`
- [x] `openai.go` + test: SSE streaming (OpenAI, OpenRouter, Ollama, LM Studio) — port `openai_compatible_provider.dart`
- [x] `anthropic.go` + test: Messages API streaming with API key — port `anthropic_provider.dart`
- [x] `config.go`: non-secret config in `config.json`; secrets in `~/.config/termocode/ai/credentials.json` (0600); env-var precedence; `Redact()`
- [x] `chatstore.go`: chats in `~/.config/termocode/ai/chats/` — port `chat_store.dart`

**Inline ghost-text completion**
- [x] `internal/ai/inline.go` + test: `ResolveInlineCompletion`, `BuildInlinePrompt`, `ParseInlineResponse` — port `inline_completion.dart` + its tests
- [x] new `internal/app/ai_lua.go`: `_G._termocode_ai` (context, show/clear/has/accept ghost, extmarks, `TextChangedI` → notify)
- [x] `internal/app/snippets_lua.go`: Tab chain = ghost → snippet → completion → placeholder → tab
- [x] new `internal/app/ai_inline.go`: debounce (300 ms), cancel on move, size cap, toggle
- [x] `internal/app/model.go`: load `aiLua` after `snippetsLua`; add AI state fields

**Chat panel (right side)**
- [x] new `internal/app/ai_panel.go`: replace body of `actions_panel.go` (keep pin/close/resize/persist)
- [x] new `internal/app/ai_chat.go`: streaming `tea.Cmd`, `aiChunkMsg`/`aiDoneMsg`, attach file/selection, save chats
- [x] `update.go`: route keys when `aiPanelFocused`; add panel to `F6` focus cycle
- [x] `mouse.go`: panel input/scroll + status-bar badge hit-tests
- [x] Restore last chat + scroll position on reopen (`session.go`: `AILastChat`)

**AI code actions (review-then-apply)**
- [x] new `internal/app/ai_actions.go`: Explain (preview), Fix diagnostic, Edit selection, Doc comment — Explain answers in the chat panel instead of a preview
- [x] Proposals open in the side-by-side nvim diff; Apply via `confirm` → one undo step
- [x] `code_actions_extra.go` + `code_actions.go`: "AI" group after Quick Fix, icon `✦`

**Wiring & polish**
- [x] `palette.go`: AI: Open Chat / Explain / Fix / Edit / Toggle Inline / Configure / Pick Model / New Chat / History
- [x] `keymap.go` + `overrides.go`: `Alt+A` panel, `Alt+\` trigger, `Alt+I` edit, toggle inline
- [x] `overflow_menu.go`, `menu.go`: AI entries
- [x] Settings: `ai_inline`, `ai_provider`, `ai_model`, `ai_base_url`; "Open Config: AI Credentials"
- [x] `statusbar/model.go`: `✦ <model>` badge (unconfigured / idle / streaming)
- [x] Theme tokens `AIGhost`, `AIAccent` in all themes + custom theme list
- [x] `error_log.go`: pass AI errors through `ai.Redact()`
- [x] Docs: `FEATURE.md` §22 "AI Assistant", `docs/architecture.md` — `FEATURE.md` now points to the docs site; AI is in `docs/guides/ai-setup.md` + `ai-use.md`

**Milestone check (AI foundation)**
- [x] With a key/Ollama set, ghost text shows in ~1 s and Tab accepts without duplicating the prefix — no-duplicate accept is tested (`ai_nvim_test.go`); the ~1 s latency is not measured
- [x] With no key, every AI entry shows a toast and nothing else changes
- [x] "✦ Fix with AI" applies as one `u`-undoable step
- [x] Theme switch recolors ghost text live — `refreshAIHighlights` on theme change; no automated test

**AI agent (after the milestone above)**
- [x] `internal/ai/agent.go` + `tools.go`: port `ai_tools.dart` / `ai_agent.dart` (6-step cap)
- [x] Tools on top of `internal/git`, `internal/search`, file ops, LSP, run command
- [x] Confirm gate for writes + per-class auto-approve (fileWrite / git / run)
- [x] Native tool calling for OpenAI / Anthropic
- [x] Claude OAuth sign-in (opt-in, with ToS note) — port `claude_oauth.dart`

---

## Group B — Search (S/M) · independent
- [x] `search.go`: `MaxResults` → setting `search_max_results` (default 2000, 0 = unlimited)
- [x] `find_results.go`: stream results in batches; footer "Showing N of M"
- [x] `search/model.go`: `Aa` / `ab` / `.*` toggles, include/exclude globs, context lines
- [x] Replace-in-workspace: confirm with file count before writing

---

## Group C — Problems + Tasks (M) · needs Base

### Problems panel
- [x] new `problems_panel.go`: bottom tab, grouped by file, severity filters, `PROBLEMS (n)` title, Enter/click jumps
- [x] `lsp_lua.go`: `DiagnosticChanged` → notify (live refresh)
- [x] Status-bar error/warning counts open Problems
- [x] Palette: `View: Problems`

### Tasks system
- [x] new `internal/tasks/`: `.termocode/tasks.json` (label, command, args, cwd, env, group, problemMatcher)
- [x] Auto-detect: go, `package.json` scripts, Makefile, cargo, pytest, justfile — port `run_targets.dart`
- [x] Move `commands.json` user commands onto tasks
- [x] Palette: Run Task… / Build / Test / Rerun Last / Terminate / Configure Tasks
- [x] Keys: `Alt+R` run task, `Alt+Shift+R` rerun last
- [x] Run each task in a PTY terminal tab; exit status → tab dot
- [x] `matchers.go`: `file:line(:col)` parser (port `terminal_link_parser.dart`) → `vim.diagnostic.set` → gutter + Problems
- [x] Clickable `file:line` links in any terminal tab (reuses `matchers.go`)

---

## Group D — Run & Debug (M) · needs Base
- [x] Run view (`view.go` placeholder) → RUN & DEBUG accordion: toolbar, Configurations, Breakpoints, Call Stack, Variables, Watch
- [x] `dap_lua.go`: JSON helpers (`frames`, `scopes`, `variables`, `threads`, `evaluate`) replace text formatters
- [x] `dap.listeners` stopped/terminated/initialized → notify; make `dapSessionActive` real state
- [x] Debug Console as a bottom-panel tab (output + REPL)
- [x] `.termocode/launch.json` + auto-detected configs — port `run_config.dart`
- [x] Persist breakpoints in `~/.config/termocode/breakpoints.json`; restore on `BufReadPost`, re-check line numbers
- [x] Keys: `Alt+F5` start/continue, `Shift+Alt+F5` stop; `● DEBUG` badge clickable

---

## Group E — Testing (M) · needs Base
- [x] New Testing activity icon + TESTS tree (package → file → test) with status glyphs and counts
- [x] new `internal/tests/`: discovery for Go, pytest, Rust, JS/TS — port `test_discovery.dart`
- [x] Parsers: `go test -json` (port `test_result_parser.dart`), pytest junitxml, cargo, `jest --json`
- [x] Actions: Run All, Rerun Failed, Refresh, Collapse; row keys `Enter` / `r` / `o`
- [x] Inline failure message; click jumps to failure line
- [x] TEST RESULTS bottom tab; status bar `✓ n ✗ n`
- [x] Move `test_runner.go` and `Ctrl+.` "Run Test" onto this runner

---

## Group F — LSP/DAP installer (M) · independent
- [x] new `internal/lspinstall/`: registry `{Name, Filetypes, Bin, InstallKind, Spec, Version}` — port `language_registry.dart` / `toolchain_catalog.dart`
- [x] Install kinds: go, npm, pip, cargo, rustup, github-release → `~/.local/share/termocode/tools/<name>/`
- [x] Generalize `download()` / `unzipTTF()` in `internal/setup/setup.go`
- [x] Add tools bin dir to the `nvim --embed` PATH; let `lsp_lua.go` accept absolute paths
- [x] Palette: `LSP: Manage Language Servers…`, `DAP: Install Adapter…`
- [x] One-time toast for a missing server on first open of a filetype
- [x] Status-bar LSP chip (`{} gopls` / `{} none`)
- [x] Merge `reportOtherLSPs` into the registry; `termocode setup` uses the same table

---

## Group G — Git extras (S/M) · independent
- [x] Merge-conflict resolver: CONFLICTS section, `]x` / `[x`, Accept Ours/Theirs/Both (port `conflict_parser.dart`)
- [x] Dashboard "Clone repository" card + HTTPS token prompt

---

## Group H — Settings & editor extras (S/M) · independent
- [x] Settings export/import (minus secrets) — port `settings_sync.dart` idea
- [x] Keymap conflict warning in `keybinding_ui.go`
- [x] Check built-in EditorConfig is on; show it in Buffer Info
- [x] User snippets manager (`snippets.json`, create/edit/delete, scope filter)

---

## Group I — Extensions (M→L) · needs Base
Best started after A/C/D/E so the API is shaped by real panels — but not blocked by them.
- [x] new `internal/app/ext_lua.go`: `_G.termocode` API — `register_command`, `register_panel`, `register_status_item`, `on`, `notify`, `prompt`, `pick`, `preview`
- [x] new `internal/ext/`: load `~/.config/termocode/extensions/<name>/init.lua` (+ optional `extension.json`)
- [x] Palette items `Ext: <Title>`; extension activity-bar items (`activity.SetExtraItems`)
- [x] Generic text-panel renderer with theme-token markup
- [x] `keymap.json` support for `"ext:<id>"`
- [x] Palette: Extensions: Reload / Open Folder / Show Log / New Extension
- [x] A broken extension never blocks startup (pcall + toast + Error Log)
- [x] Sample extensions: TODO tree, word count, Open in GitHub
- [x] REST client (`.http` files) as an extension — port `http_file_parser.dart`
- [ ] Later: Tier 2 process extensions over JSON-RPC (reuse `mobocode/agent/rpc.go`) — not started; only a follow-up note in ADR 0006

---

## Group J — Docs & community (S) · independent
- [x] Fix `keys.json` → `keymap.json` in `docs/architecture.md`
- [ ] Add screenshots / asciinema to `assets/screenshots/` — only the vhs script (`demo.tape`) and a README; no images or casts committed yet
- [x] Seed `docs/adr/` and `docs/qc/` from mobocode templates
- [x] `CONTRIBUTING.md`
- [x] Homebrew tap / AUR via `release.yml`
- [x] `Help: Run Doctor` palette entry (in-app `setup.Run` checks)

---

## Glue tasks (do after both groups are done)
- [x] **D + E:** "Debug this test" (`d` key in TESTS tree) uses Run & Debug configs — also `Ctrl+.` "Debug Test: X" and **Testing: Debug Test at Cursor**; Go (dlv test mode) and pytest (debugpy); Rust / Jest / Vitest show a toast (`test_debug.go`)
- [x] **D + F:** Run view "install dlv / debugpy" hint opens `DAP: Install Adapter…` — opens the install dialog of the missing adapter in one step
- [x] **C + E:** Tasks "Test" group runs through the Testing runner — a `test` task in tasks.json still runs as a task; detected test tasks are the fallback when no test framework is found
- [x] **A + C:** "✦ Fix with AI" action on rows in the Problems panel — key `a`, a ✦ button on the selected row, and a right-click menu
- [ ] **I + C:** move `commands.json` user commands into a built-in extension — not done on purpose: today edits to `commands.json` show up at once and work without nvim; through the extension host they would need **Extensions: Reload**, would be missing until extensions finish loading, and Go would still need a fallback reader. No user gain, so the current Go path (palette `User: <Title>` + Tasks) stays

---

## Android (APK) — later
- [ ] Clipboard on Android: copy / paste uses X11 today (`golang.design/x/clipboard`, cgo) and does not work on Android. Connect it to Android's own clipboard (through the APK shell, e.g. a small bridge the shell exposes), keep OSC 52 as fallback.
- Size estimate (2026-10-11): termocode arm64 binary 19 MB, ~6.5 MB compressed in an APK; plus Neovim for Android (~4–6 MB) and the terminal shell (Kotlin ~1–2 MB, Flutter ~8–10 MB) → APK ~12–20 MB.

---

## Group K — AI controls the IDE (2026-10-14)

The user asked that the AI chat can manage every feature of the editor.
Plan by Fable (inventory: the AI covered about a third of the features).
Rules: every action is `auto`, `confirm` or `never` (with a reason);
never: quit, reload, detach/reset session, API keys/credentials, the
AI's own auto-approve, settings import/export. Each step gets a
testkit test that runs the tool through the real app and checks the
screen.

- [x] K0 IDE tools: open_file, get_editor_state, notify, show_panel, reveal_in_explorer, execute_action (allow-list)
- [x] K0 IDE follows AI changes: explorer, open tabs (write/rename/delete), git, checktime; step limit 25 + continue
- [x] K1 (S) Policy map for every palette action (auto/confirm/never + reason), `Gate` on ai.Tool, `list_actions(query)`, coverage test (every palette id and setting is exposed or listed "not for AI"), table-driven testkit harness
- [x] K2 (M) `settings_list` / `settings_set` (theme, tab size, icons, word wrap…; never credentials or ai_auto_approve), `layout_set` (sidebar width, panel size, zen, panels), `get_ide_state`, one-line capability summary in the system prompt — settings_set asks (class `settings`); layout_set runs at once and never hides the AI panel
- [x] K3 (M) Terminal: `terminal_send`, `terminal_read`, `terminal_tabs` (visible integrated terminal)
- [x] K4 (M) Git: hunks (stage/unstage/discard), blame, log, stash, unstage, fetch
- [x] K5 (M) LSP: `lsp_hover`, `lsp_symbols` (file outline / workspace query), `lsp_nav` type_definition + implementation (auto); `lsp_rename` and `lsp_code_actions` apply (fileWrite, through nvim so undo works; save only files with no unsaved edits; no new tabs); clear "no language server" error with the installer hint
- [x] K6 (S) Tasks/tests typed tools + `tests_results`
- [x] K7 (L) Debugger: `debug_breakpoint` (list / set / remove auto, clear asks), `debug_state`, `debug_start`, `debug_control` (continue, pause, steps, stop, restart; waits for the next stop), `debug_evaluate`; the editor follows each stop
- [x] K8 (S) LSP/DAP installer, extensions, workspace folders, docs reference page
- [x] K9 Leftovers from the sync audit: search results (Find Results buffer + overlay) search again after AI file changes; rename / delete (agent and explorer) update the closed-tabs list, recents and hot-exit backups; the language server stays attached after a rename (it was dropped); palette / `execute_action` save refreshes git

## Group L — Upload / download (2026-10-14)

- [x] L1 (S) File → chat: dropped/pasted paths become attachment chips (text + images), attach button with file picker, `@file`
- [x] L2 (S) Chat → file: "Save as…" on code blocks, export chat as .md (the AI writes files with `write_file`; no extra `save_file` tool)
- [x] L3 (M) Local machine ↔ IDE over SSH: explorer "Download" / "Upload Here..." + palette (internal/transfer). kitty: download + upload (OSC 5113, kitty asks to allow); iTerm2: download (OSC 1337 File, ≤256 MB) + upload (RequestUpload tgz); trzsz: `tsz` / `trz` when installed; WezTerm, Ghostty, tmux, others: dialog with a ready scp command (Copy Command, OSC 52). Folders as .tar.gz; uploads never replace a file and stay in the workspace

## Group P — Performance (2026-10-14)

The user wants termocode "super fast and light". Measure with
TERMOCODE_CPUPROFILE / TERMOCODE_MEMPROFILE and a pty key-repeat test
(hold ↓ 30 keys/s in 240×62, with and without the AI chat). Baseline
after the frame gate: 30% CPU editor only, 38% with the AI chat;
live heap ~10 MB, RSS ~50 MB, ~50 MB/s short-lived allocations.

- [x] P0 Frame gate (≤1 draw per 16 ms), fast cellWidth/joins, activity bar cache, GOGC 300 (now 200, see P4), profile switches
- [x] P1 Cut allocations: cache the AI transcript lines between frames, fewer copies in handleNvimMsg, stop Model copies escaping to the heap; target ≥2× fewer bytes/s and lower CPU per key
  — done: transcript / input / header / tab bar / status bar / breadcrumbs caches, frame built as rows (one join), row-width memo, Model boxed once per message (theme.Theme by pointer). Soak 108 → 29 MB/s; ↓ held: editor 30% → 15% CPU (10.3 → 5.0 ms/key), AI chat 34% → 15% (11.5 → 5.4 ms/key)
- [x] P2 Cache the explorer / sidebar render by an exact state key (tree version counter) — randomized test compares cached View with a fresh draw
- [x] P3 A perf guard in CI-safe form: allocation-count benchmarks with limits (not wall-clock), so regressions fail tests — perf_guard_test.go (frame 5487 → 352 allocs, AI frame 7470 → 457)
- [x] P4 RAM: measure RSS of termocode + nvim (idle, 11 files, long AI chat, after soak); GOGC 300 → 200 and soft limit 256 → 128 MB (100/200/300 compared); image cache bounded by bytes (16 MB); hidden chat panel drops its drawn rows; nvim already runs with `-i NONE -n` (no ShaDa / swap); live-heap guard test. termocode after soak 56 → 50 MB, total with nvim 80 → 74 MB

## Group R — AI edit review (port from mobocode) (2026-10-14)

The user wants mobocode's diff viewer and its AI change-suggestion flow
in termocode (`~/mobocode`: lib/features/diff/diff_viewer.dart,
lib/features/scm/hunk_diff_view.dart, lib/core/diff/text_diff.dart,
lib/features/ai/ai_surface.dart, lib/features/compare/compare_sheet.dart).

- [x] R1 Port the diff engine and viewer (text_diff → Go; side-by-side / inline view, word-level highlights, hunk navigation)
- [x] R2 AI change suggestions go through review: the agent's file edits become pending proposals shown in the diff viewer; accept / reject per hunk and all; one undo step; agent waits for the decision and is told the result
- [x] R3 Restore points: snapshot before an AI turn's first change; "AI: Undo last turn" restores
- [x] R4 AI audit log: Output channel "AI" with every tool call, args, result, who approved

## Group N — Next loop (2026-10-15)

The user: "do them in a loop; after finishing each item push it".

- [x] N1 (S) Less CPU while scrolling: save session.json less often (debounce / save on idle + on exit), keep crash safety
- [x] N2 (S) Search results update after a rename/delete in the explorer (not only after AI changes)
- [x] N3 (S/M) First-run setup card on the welcome screen: icons, terminal font, AI provider in a few clicks
- [x] N4 (M) Easy install: release binaries (linux/macOS, amd64/arm64) and a one-line install script
- [x] N5 (M) Persian/Arabic (right-to-left) text in the AI chat and the Markdown preview (bidi + shaping)
- [x] N6a README screenshots: hero + 8-image gallery in README (160x44 PNGs from the shots tool, best compression, each < 300 KB); images also in docs/features.md. Animated `demo.gif` still waits for vhs (not installed)
- [ ] N6b Tier 2 process extensions
- [ ] N6c Android clipboard
