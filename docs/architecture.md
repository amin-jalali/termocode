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
- **[Glamour](https://github.com/charmbracelet/glamour)** — markdown rendering for the `F7` preview overlay.
- **[neovim/go-client](https://github.com/neovim/go-client)** — msgpack-RPC client to the embedded Neovim process.
- **[sahilm/fuzzy](https://github.com/sahilm/fuzzy)** — fuzzy matching for the picker (Quick Open, palette, …).
- **Neovim 0.10+** (0.9.5 mostly works as fallback) — runs as `nvim --embed`; talks to the host via `ext_linegrid`.
- **ripgrep** (`rg`) — workspace search backend (`F8`, live-find overlay).

## The big knot: `internal/app`

`internal/app/model.go` defines `Model`, the master Bubble Tea model. It owns one of every component (activity, explorer, editor, tabs, status, picker, menu, find, confirm, preview, prompt, replace, search, toast) plus an `*nvim.Client`. **There are ~70 files in this package** — each one is a slice of behaviour hanging off the same `Model`: git actions, multi-cursor, settings UI, snippet engine, terminal split, and so on.

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
| `app`        | Master model — owns everything else                                               |
| `clipring`   | Clipboard history ring                                                            |
| `confirm`    | "Are you sure?" modal                                                             |
| `editor`     | Reads nvim grid via `ext_linegrid`, draws cells + scrollbar + cursor              |
| `explorer`   | File tree sidebar; handles expand / collapse / multi-root                         |
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
| `search`     | Workspace ripgrep results sidebar                                                 |
| `setup`      | First-run bootstrapping (plugin clones, lazy.nvim, etc.)                          |
| `statusbar`  | Bottom status bar — branch, diagnostics, ln/col, badges                           |
| `tabbar`     | Top tab strip with close buttons, pin indicator                                   |
| `theme`      | Color palette + lipgloss style helpers (`Bg`, `FgBg`, `LG`)                       |
| `toast`      | Transient notifications (top-right of editor area)                                |

## Overlay rendering (`modal_overlay.go`)

Modal overlays parse the rendered base into ANSI cells, mutate them, and splice back. This is where the "frosted glass" look comes from: cells under the modal rect get read, dimmed, tinted, and used as the modal body background so the editor "shows through".

### The CSI parser quirk we hit

`[` (0x5B) sits in the same range (0x40-0x7E) as SGR final bytes. A naive CSI parser would exit CSI mode on encountering `[` — meaning `\x1b[` (the introducer) would terminate itself after one byte. The fix in `parseANSIRow` / `truncRightVisual` / `dropLeftVisual` skips `\x1b[` as a unit.

### `ModalOpts`

One `modalOverlay` function powers every picker and prompt. Per-call tweaks go through `ModalOpts`; pass `nil` for tuned defaults. Past experiments (frosted-glass dot patterns, noise textures) were rejected in favour of dim+tint on the underlying cells.

## Persistence (`~/.config/termocode/`)

| File                    | Purpose                                                                |
| ----------------------- | ---------------------------------------------------------------------- |
| `session.json`          | Open files, tab order, cursor positions, expanded explorer dirs, roots |
| `config.json`           | Settings (theme, font_delta, auto_save, word_wrap, show_hidden, …)     |
| `recents.json`          | Recent files for the welcome screen                                    |
| `palette_recents.json`  | Frequently-used palette commands (auto-floats them to the top)         |
| `workspaces.json`       | Recent workspace roots for `File: Open Recent Workspace...`            |
| `errors.log`            | Append-only ring of error toasts and `recordError(...)` debug lines    |
| `commands.json`         | Optional — user-defined palette commands surfaced as `User: <Title>`  |

`tail -f ~/.config/termocode/errors.log` is the supported live-debug path.

## Keymap (`internal/keymap`)

Actions are an enum in `keymap.go`; the default `KeyMap` maps key strings (Bubble Tea vocabulary: `ctrl+s`, `alt+shift+t`, `f8`) to actions. User overrides layer on top via `~/.config/termocode/keys.json`.

**Some keys are reserved by terminal emulators.** For example `ctrl+shift+t` opens a new terminal tab in most emulators and never reaches the app. termocode ships multiple fallbacks for the affected actions — Reopen-closed is bound to all of `alt+shift+t`, `alt+T`, `shift+f4`, and `f16`.

## Theme system (`internal/theme`)

Single source of truth: `palette.go` declares `Color256` package vars (`BgEditor`, `BgHover`, `TextPrimary`, …). `SetPalette` rebinds them at runtime when the user picks a theme. `LG()` resolves to a `lipgloss.Color` — truecolor hex when the palette has one mapped, else the generic 256-color name.

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
- **`.github/workflows/release.yml`** — tagged releases (`v*`) cross-compile for linux/darwin × amd64/arm64 and upload tarballs to the GitHub Release with auto-generated release notes.

## Repository layout

```text
cmd/termocode/        entrypoint — sets up nvim, wires the Bubble Tea program
internal/             every package described above (~24 sub-packages)
assets/               vendored runtime assets (snippets, theme JSONs)
scripts/              dev.sh, release.sh, run-with-small-font.sh
docs/                 this folder
.github/workflows/    CI / release pipelines
```
