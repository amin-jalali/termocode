# Architecture

This is the contributor-facing tour of how termocode is built — what each package does, how the pieces talk to each other, and the design decisions worth knowing before changing them.

## The 30-second model

termocode is a single-window **Bubble Tea** application that wraps **a real Neovim process** for the actual editing work. The Go side handles chrome (tabs, sidebar, status bar, picker overlays, themes, keymap dispatch); Neovim handles buffers, cursors, syntax, LSP, and DAP. The two communicate over msgpack-RPC.

```text
┌─────────────────────────────────────────────────────────┐
│                  Go (Bubble Tea)                        │
│  ┌───────────┐  ┌───────────┐  ┌──────────────────┐    │
│  │ activity  │  │ explorer  │  │ tabs / status     │    │
│  └───────────┘  └───────────┘  └──────────────────┘    │
│  ┌──────────────────────────────────────────────────┐  │
│  │                editor view                        │  │
│  │  (reads nvim grid via ext_linegrid, redraws       │  │
│  │   styled cells with our own scrollbar + cursor)   │  │
│  └──────────────────────────────────────────────────┘  │
│           ▲                                ▲             │
│  msgpack-RPC                  Lua chunks (EvalLuaString) │
│           ▼                                ▼             │
│  ┌──────────────────────────────────────────────────┐   │
│  │             nvim --embed (child process)          │   │
│  │   buffers · syntax · LSP · DAP · undo history    │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

## Technology stack

- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** — the Elm-style TUI runtime; everything routes through `Update(msg) → (model, cmd)`.
- **[Lipgloss](https://github.com/charmbracelet/lipgloss)** — string styling, layout primitives (`JoinHorizontal`, `JoinVertical`), truecolor SGR output.
- **[Chroma](https://github.com/alecthomas/chroma)** — fallback syntax highlighting for read-only previews.
- **[Goldmark](https://github.com/yuin/goldmark)** — Markdown parsing for the `F7` reader view (`internal/mdview` draws it).
- **[neovim/go-client](https://github.com/neovim/go-client)** — msgpack-RPC client to the embedded Neovim process.
- **[sahilm/fuzzy](https://github.com/sahilm/fuzzy)** — fuzzy matching for the picker (Quick Open, palette, …).
- **Neovim 0.10+** (0.9.5 mostly works as fallback) — runs as `nvim --embed`; talks to the host via `ext_linegrid`.
- **ripgrep** (`rg`) — workspace search backend (`F8`, live-find overlay). Optional: without it, search falls back to a slower pure-Go walker (literal text only, no `.gitignore`). See [ADR 0004](adr/0004-pure-go-search-fallback.md).

## The big knot: `internal/app`

`internal/app/model.go` defines `Model`, the master Bubble Tea model. It owns one of every component (activity, explorer, editor, tabs, status, picker, menu, find, confirm, preview, prompt, replace, search, toast) plus an `*nvim.Client`. **There are ~75 files in this package** — each one is a slice of behaviour hanging off the same `Model`: git actions, multi-cursor, settings UI, snippet engine, terminal split, and so on.

### Render flow

```text
View()
  └── renderBase()
        └── compose activity bar + sidebar + editor + status bar
            (lipgloss.JoinHorizontal / JoinVertical)
  └── normalizeFrame()
        └── pad each row to exactly m.w × m.h with BgEditor fill
  └── overlay phase
        ├── overlayToastsInEditor (transient notifications)
        └── modalOverlay (pickers, prompts, confirms — pseudo-transparent)
```

The `normalizeFrame` step exists because terminal default background leaks through any cell that isn't styled. Every row must end up exactly `m.w` cells wide, all of them styled with `theme.BgEditor` (or whatever the component owns).

### Update flow

`Update()` is a giant switch that dispatches on message type **first** (overlay control messages like "close prompt" must work regardless of state), then routes to per-overlay handlers (`routeToPicker`, `routeToPrompt`, `routeToConfirm`, …) when one of `m.{picker,prompt,…}Open` is true. Global keybindings only reach `handleGlobalKey` when no overlay is open.

This ordering is deliberate: it keeps the modal stack closeable from anywhere, and avoids the classic "I pressed Esc but the picker stayed open because the editor swallowed it" bug.

## Neovim interop (`internal/nvim`)

`Client` speaks msgpack-RPC to a child `nvim --embed` process. termocode calls `vim.api.nvim_*` and `vim.fn.*` to manipulate buffers; nvim's grid is mirrored into our own `internal/grid` and rendered as styled cells. Lua side-of-the-wire helpers live in `internal/app/*_lua.go` files — Go raw-string heredocs containing Lua source, executed via `EvalLuaString` / `ExecLua`.

### Version floor

nvim 0.9.5 is what tests run against, even though README says ≥0.10. Some helpers fall back when modern API isn't available:

- `_expand_plain` substitutes for `vim.snippet.expand`
- `_toggle_comment_*` substitutes for `gc` / `gcc` motions

### Heredoc Lua gotcha

Go raw-string syntax uses backticks. A backtick anywhere inside a Lua heredoc — even in a comment — closes the Go string and breaks compilation. Use `--` Lua comments and avoid backtick characters entirely.

## Per-feature packages (`internal/<name>`)

Each is a self-contained Bubble Tea sub-component with its own `Model` / `Update` / `View`. They communicate with the parent via custom message types (e.g. `picker.SelectMsg`, `prompt.SubmitMsg`) emitted from their `Update`.

| Package      | What it does                                                                      |
| ------------ | --------------------------------------------------------------------------------- |
| `activity`   | Vertical icon bar on the left; switches the sidebar view                          |
| `ai`         | AI layer: streaming providers, config / secrets, chats, inline helpers, agent     |
| `app`        | Master model — owns everything else                                               |
| `clipring`   | Clipboard history ring                                                            |
| `clock`      | Wall-clock seam for user-visible dates (golden tests freeze it)                   |
| `confirm`    | "Are you sure?" modal                                                             |
| `editor`     | Reads nvim grid via `ext_linegrid`, draws cells + scrollbar + cursor              |
| `explorer`   | File tree sidebar; handles expand / collapse / multi-root                         |
| `ext`        | Extension discovery, registry JSON, `{{token}}` markup, scaffold ([extensions](extensions.md)) |
| `findbar`    | Inline find/replace bar with case/word/regex toggles                              |
| `git`        | Git commands, status parsing, diff rendering                                      |
| `grid`       | Cell-grid data structure shared with nvim                                         |
| `keymap`     | Action enum + default `KeyMap` + JSON override loader                             |
| `menu`       | Right-click context menus                                                         |
| `nvim`       | RPC client wrapping `neovim/go-client`                                            |
| `picker`     | The universal modal list — used for files, palette, settings, themes, bookmarks  |
| `preview`    | Scrollable read-only overlay (test output, command output, log, diff)             |
| `prompt`     | Single-line input modal                                                           |
| `recents`    | Welcome-screen recents pane                                                       |
| `replacebar` | Inline replace bar; pairs with `findbar`                                          |
| `session`    | Persistent sessions: socket server owning the UI's PTY, thin client, mode replay ([ADR 0007](adr/0007-persistent-sessions.md)) |
| `search`     | Workspace search sidebar — ripgrep, or a pure-Go walker when `rg` is missing      |
| `setup`      | `termocode setup` subcommand (Nerd Font, gopls) + read-only `Doctor()` checks     |
| `statusbar`  | Bottom status bar — branch, diagnostics, ln/col, badges                           |
| `tabbar`     | Top tab strip with close buttons, pin indicator                                   |
| `tasks`      | `.termocode/tasks.json` loader, task auto-detection, `file:line` links + problem matchers |
| `theme`      | Palette tokens, built-in themes (`Spec` → palette), nvim colours, floating-card / pill / gradient helpers, icon sets |
| `anim`       | Spring animations (harmonica); the `animations` setting                           |
| `termimg`    | Terminal images: kitty / sixel / half-block detection, encoding, caching          |
| `devtools`   | Developer tools: `shotkit` (scripted app driver + golden screen tests), `shots` (screenshot renderer), `ansipng` (ANSI → PNG) |
| `toast`      | Transient notifications (top-right of editor area)                                |

## AI assistant (`internal/ai` + `internal/app/ai_*.go`)

AI is optional: with no provider, every AI entry only shows a "configure" toast.

- **Providers** (`internal/ai`): one `Provider` interface (`Stream(ctx, Request) <-chan Chunk`) with two wire implementations — OpenAI-compatible SSE (OpenAI, OpenRouter, Ollama, LM Studio, any `/v1` URL) and the Anthropic Messages API (API key, or opt-in Claude OAuth). Both support native tool calling. No SDKs; tests use `httptest` only.
- **Config**: non-secret `ai_*` keys live in `config.json`; keys and OAuth tokens in `ai/credentials.json` (mode 0600). Environment variables win (`TERMOCODE_AI_PROVIDER`, `TERMOCODE_AI_MODEL`, `TERMOCODE_AI_BASE_URL`, `TERMOCODE_AI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `OPENROUTER_API_KEY`, `OLLAMA_HOST`). `ai.Redact` scrubs secrets from every AI error, and `recordError` runs it on everything written to `errors.log`.
- **Ghost text**: `ai_lua.go` installs `_G._termocode_ai`. `TextChangedI` → `termocode_ai_changed` notify → 300 ms debounce → request (fast model) → `ResolveInlineCompletion` (continuation vs rewrite, never blind insert) → extmark virtual text. `Tab` tries `accept()` first, then snippet → completion → placeholder → tab.
- **Chat**: the right-side panel (`actions_panel.go` frame, `ai_panel.go` body). A turn runs `ai.Agent` on a goroutine; events reach `Update` through a channel (`aiChatEventMsg` … `aiChatDoneMsg`), tagged with a generation so New Chat / Stop drops late events. Mutating tools wait on a confirm dialog (per-class auto-approve: fileWrite / git / run). Chats are saved in `ai/chats/<id>.json`.
- **Code actions**: Explain goes to the chat; Fix / Edit / Doc build a range proposal that opens in a side-by-side nvim diff, and Apply replaces the range with one `nvim_buf_set_lines` call (one undo step), after checking the buffer did not change.

## Built-in guide

`docs/embed.go` embeds the getting-started pages, guides, reference and feature
pages into the binary. `internal/help` prepares those pages for terminal
reading and resolves their headings and links. The read-only guide viewer uses
the `internal/preview` reader, so help works offline and shares its scrolling,
link navigation and Markdown rendering with the file preview.

## Overlay rendering (`modal_overlay.go`)

Floating chrome (pickers, prompts, confirms, menus, toasts, the image preview) is composited by `modalOverlay`: the rendered base is parsed into ANSI cells, everything outside the card is dimmed (the backdrop), the opaque card (`theme.FloatBox`, `BgFloat`, rounded border) is spliced in with its border glyphs sitting on the backdrop, and a drop shadow is blended toward the theme's `Shadow` token. Colour math only runs on truecolor / 256-colour profiles; on 16 colours the backdrop uses the SGR "faint" attribute, so no 24-bit sequence is ever written (`render_invariants_test.go` checks this).

Animations (`internal/anim`, harmonica springs) drive the overlay open (slide + fade), toast slide in / out and smooth list scrolling. One `anim.TickMsg` steps everything and is only scheduled while something moves; with `animations: off` values snap and no tick runs. The nvim grid is never animated.

Images (`internal/termimg`) have two layers: half-blocks as ordinary cells, plus — on kitty / sixel terminals — the real image, drawn by a carrier sequence appended after the image's last row at the very end of `View()` (after every overlay), so Bubble Tea's line diff rewrites it whenever the area changes.

### The CSI parser quirk we hit

`[` (0x5B) sits in the same range (0x40-0x7E) as SGR final bytes. A naive CSI parser would exit CSI mode on encountering `[` — meaning `\x1b[` (the introducer) would terminate itself after one byte. The fix in `parseANSIRow` / `truncRightVisual` / `dropLeftVisual` skips `\x1b[` as a unit.

### `ModalOpts`

One `modalOverlay` function powers every picker and prompt. Per-call tweaks go through `ModalOpts`; pass `nil` for tuned defaults. Past experiments (frosted-glass dot patterns, noise textures) were rejected in favour of dim+tint on the underlying cells.

## Persistence (`~/.config/termocode/`)

The full list of files termocode reads and writes — config dir, data dir and
per-workspace `.termocode/` — with their format and whether each is safe to
edit, is generated from `ConfigFileCatalog()` in `internal/app/docs.go`:
**[reference/config-files.md](reference/config-files.md)**. Add new files
there (and run `go run ./cmd/docgen`), not here, so the two can not drift.

What a contributor should know about the layout:

- **Settings vs. state.** `config.json`, `keymap.json`, `commands.json`,
  `snippets.json`, `user_theme.json` are user settings (exported by
  *Preferences: Export termocode Settings File…*). Session, recents, palette recents and
  workspaces files are state and never exported.
- **Sessions are per workspace.** `sessions/<hash>.json` holds open files,
  cursors, explorer state and extra multi-root roots; the hash is of the
  absolute cwd (`loadSession` / `saveSession` in `internal/app/session.go`).
  `session.json` only keeps layout preferences shared by all workspaces (and
  an un-migrated session from older versions).
- **Secrets** (`ai/credentials.json`) are written with mode 0600.
- **Per-workspace files** live in `<workspace>/.termocode/`: `tasks.json`
  (tasks) and `launch.json` (debug / run configurations, VSCode shape,
  comments allowed).
- **Data dir** (`~/.local/share/termocode`, `$XDG_DATA_HOME/termocode`):
  `plugins/` and `tools/` (language servers and debug adapters installed by
  termocode, plus `tools/asked.json`).

Plugins that termocode clones on first launch (nvim-dap, vim-visual-multi) live outside the config dir, in `~/.local/share/termocode/plugins` (`$XDG_DATA_HOME/termocode/plugins`, or `$TERMOCODE_PLUGINS_DIR`). See [ADR 0003](adr/0003-auto-bootstrapped-nvim-plugins.md).

`tail -f ~/.config/termocode/errors.log` is the supported live-debug path.

## Keymap (`internal/keymap`)

Actions are an enum in `keymap.go`; the default `KeyMap` maps key strings (Bubble Tea vocabulary: `ctrl+s`, `alt+shift+t`, `f8`) to actions. User overrides layer on top via `~/.config/termocode/keymap.json` (or `$XDG_CONFIG_HOME/termocode/keymap.json`) — see `overrides.go`.

The file is a flat map of key string → action name, e.g. `{"alt+s": "Save"}`. Action names come from the `actionNames` table in `overrides.go`. `Preferences: Customize Keybindings` writes this file for you (read-modify-write, so other entries stay). Any key string Bubble Tea can produce is accepted (`keymap.ValidateKey`: optional `ctrl+` / `alt+` / `shift+`, then one character or a named key such as `f1`–`f24`, `enter`, `pgdown`). On load, entries with an unknown action name or a malformed key are dropped — a broken file can never block startup — and `LoadOverridesReport` lists them, so the app shows one warning toast after start and logs each entry to `errors.log`.

**Some keys are reserved by terminal emulators.** For example `ctrl+shift+t` opens a new terminal tab in most emulators and never reaches the app. termocode ships fallbacks for the affected actions — Reopen-closed is bound to `ctrl+shift+t` (best effort), `alt+shift+t` and `alt+T` (how xterm-style terminals report Alt+Shift+T).

## Theme system (`internal/theme`)

Single source of truth: `palette.go` declares `Color256` package vars (`BgEditor`, `BgHover`, `TextPrimary`, …). `SetPalette` rebinds them at runtime when the user picks a theme. `LG()` resolves to a `lipgloss.Color` — truecolor hex when the palette has one mapped, else the generic 256-color name.

New themes are one `Spec` literal (`spec.go`: the upstream base / surface / overlay / text ladder plus accents and syntax roles); `FromSpec` derives every chrome token and `NvimColors` derives the editor colourscheme (`theme_lua.go`), so chrome and editor always match. Views never write hex: derived shades come from `theme.Mix(a, b, t)` / `theme.Float()`.

Most components style via `theme.Bg(...)` and `theme.FgBg(...)`. A theme switch is a global swap — no component holds its own copy.

## Render-width contract

Each component fills its allotted column count **exactly**. Short rows leak terminal default background through overlay splices. If you change a render path, verify with `lipgloss.Width(line) == expected`.

Specifically:

- **No plain spaces inside an overlay.** Unstyled spaces show terminal default background, not editor background. Wrap pad whitespace in a styled `Render(...)` — e.g. `theme.Bg(theme.BgEditor).Render(strings.Repeat(" ", N))`.
- **Don't add new noise / texture to overlays** without confirming visually first. The working approach is dim + tint of the underlying cells; previous attempts at frosted-glass dot patterns produced unreadable backgrounds.

## Build pipeline

```sh
go build -o termocode ./cmd/termocode    # build
go test ./...                            # all tests
./scripts/dev.sh                         # vet + test + build, the dev loop
./scripts/release.sh                     # cross-compile to dist/ for linux/darwin × amd64/arm64
```

Tests cache aggressively; if a test wrongly reports stale, use `go clean -testcache`.

## CI

- **`.github/workflows/test.yml`** — every push & PR runs `go vet ./...` + `go test ./... -race -count=1` on Ubuntu and macOS.
- **`.github/workflows/release.yml`** — tagged releases (`v*`) cross-compile for linux/darwin × amd64/arm64, upload tarballs + `checksums.txt` to the GitHub Release with auto-generated release notes, then render the Homebrew formula and AUR `PKGBUILD` from `packaging/` and push them (each push is skipped when its secret is missing — see [CONTRIBUTING.md](https://github.com/amin-jalali/termocode/blob/main/CONTRIBUTING.md#releases)).
- **Update signature (maintainer step)** — `internal/update/signature.go` can check a minisign signature of `checksums.txt`. It is off while `releasePublicKey` is empty (a placeholder), so updates work today. To turn it on: make a key with `minisign -G` (keep the secret key offline), paste the base64 line of the `.pub` file into `releasePublicKey`, and add `minisign -Sm checksums.txt` (upload `checksums.txt.minisig` to the release) to the release job. After that, an update without a valid signature is refused. Ship the key and the signing step in the same release.

## Repository layout

```text
cmd/termocode/        entrypoint — sets up nvim, wires the Bubble Tea program
extensions/           sample Lua extensions, embedded in the binary
internal/             every package described above (~23 sub-packages)
assets/screenshots/   README images + the vhs demo script
packaging/            Homebrew formula + AUR PKGBUILD templates
scripts/              dev.sh, install.sh (one-line installer), release.sh, build-release.sh, render-packaging.sh, run-with-small-font.sh
docs/                 user guides, reference, concepts and adr/ (decisions)
.github/workflows/    CI / release pipelines
```

## Architecture decisions

- **[`docs/adr/`](adr/README.md)** — Architecture Decision Records: why the big choices were made. Add one when you make a choice that is hard to undo.
