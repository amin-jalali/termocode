# Feature guide

This document walks through every feature termocode exposes — keybindings, palette commands, and the configuration knobs that go with them.

## Layout

```text
┌──┬──────────────────────────────────────────────────┐
│ A│ tabs                                              │
│ c│ ┌──────────────────────────────────────────────┐ │
│ t│ │ breadcrumbs · sticky context                  │ │
│ i│ ├──────────────────────────────────────────────┤ │
│ v│ │                                                │ │
│ i│ │  editor (Neovim under the hood)               │ │
│ t│ │                                                │ │
│ y│ │  ────────────────────────────────────────────  │ │
│ b│ │  terminal split (when toggled)                │ │
│ a│ │                                                │ │
│ r│ └──────────────────────────────────────────────┘ │
├──┴──────────────────────────────────────────────────┤
│ status: branch · diagnostics · ln/col · TERM         │
└────────────────────────────────────────────────────┘
```

The activity bar on the left switches the sidebar between **Files**, **Search**, **Source Control**, **Outline**, and **Run**. Click the active icon again to hide the sidebar.

## Editing

| What you want                            | How                                  |
| ---------------------------------------- | ------------------------------------ |
| Save                                     | `Ctrl+S`                             |
| Save all                                 | Palette → `File: Save All Files`     |
| Quit                                     | `Ctrl+Q`                             |
| Copy / Cut / Paste                       | `Ctrl+C` / `Ctrl+X` / `Ctrl+V`       |
| Clipboard history                        | `Ctrl+Shift+V`                       |
| Toggle line comment                      | `Ctrl+/`                             |
| Move / duplicate line                    | `Alt+↑↓` / `Shift+Alt+↑↓`            |
| Undo / redo                              | `Ctrl+Z` / `Ctrl+Y`                  |
| Selection                                | `Shift+arrow keys`                   |
| Word jump                                | `Ctrl+←→`                            |
| Word jump + select                       | `Ctrl+Shift+←→`                      |
| Multi-cursor (next occurrence)           | `Ctrl+D`                             |
| Add cursor above / below                 | `Ctrl+Alt+↑↓`                        |
| Format document                          | `Shift+Alt+F`                        |
| Quick fix (code actions)                 | `Ctrl+.` / `Alt+Enter`               |
| Sort / reverse / case-convert lines      | Palette                              |
| Join lines                               | Palette                              |
| Trim trailing whitespace                 | Palette                              |
| Collapse extra blank lines               | Palette                              |
| Convert tabs ↔ spaces                    | Palette                              |
| Toggle word wrap                         | `Alt+W`                              |
| Browse snippets (when trigger forgotten) | Palette → `Snippets: Browse...`      |
| Copy `path:line:col` reference           | Palette → `Copy: File:Line...`       |

Press `Esc` any time to drop into Vim's Normal mode if you want to use Vim motions; the editor is real Neovim underneath.

## Files & navigation

| What you want                       | How                                    |
| ----------------------------------- | -------------------------------------- |
| Open a file (fuzzy)                 | `Ctrl+P`                               |
| Toggle file explorer                | `Ctrl+B`                               |
| Switch focus (sidebar ↔ editor)     | `F6`                                   |
| Reveal current file in explorer     | `Ctrl+Shift+E` or palette              |
| Next / previous tab                 | `Ctrl+PgDn` / `Ctrl+PgUp`              |
| Close tab                           | `Ctrl+W`                               |
| Reopen closed tab                   | `Alt+Shift+T` (also `Shift+F4`)        |
| Go to line                          | `Ctrl+G`                               |
| Bookmark current line               | `F3`                                   |
| Show all bookmarks                  | `Shift+F3`                             |
| Clear all bookmarks                 | Palette                                |
| Open recent file                    | `Ctrl+R`                               |
| Open recent workspace               | Palette                                |
| Split editor right                  | `Ctrl+\`                               |
| Split editor down / close split     | Palette                                |
| Switch to last file                 | `Ctrl+Tab`                             |
| Pin / unpin tab (kept at front)     | `Alt+P`                                |
| Toggle hidden files (dotfiles)      | Palette                                |
| Reveal in file system               | Palette                                |
| Show keyboard shortcuts             | Palette → `Help: Show Shortcuts`       |
| Show error log                      | Palette → `Help: Show Error Log`       |
| Show buffer info                    | `Ctrl+Alt+I`                           |
| Open URL on current line            | Palette                                |
| Compare two files                   | Palette → `File: Compare Two Files...` |
| Revert / reload buffer from disk    | Palette → `File: Revert / Reload...`   |
| Fold all / unfold all / toggle fold | Palette (`zM` / `zR` / `za` in vim)    |

The recents modal (`Ctrl+R`) is mouse-friendly — click an entry or scroll-wheel through the list.

## Search

| What you want                    | How                                              |
| -------------------------------- | ------------------------------------------------ |
| Find in current file             | `Ctrl+F`                                         |
| Replace in current file          | `Ctrl+H`                                         |
| Find in workspace (ripgrep)      | `F8` or `Alt+F`                                  |
| Find in workspace (live overlay) | Palette → `Search: Find in Files (Live Overlay)` |
| Replace in workspace             | `Ctrl+Shift+H`                                   |

The find/replace bar exposes three click-toggles in the right cluster — `Aa` (case-sensitive), `ab` (whole word), `.*` (regex). When you open Find/Replace with text selected in the editor, that selection auto-prefills the search input.

## Code intelligence (LSP)

Works out of the box for **Go**, **Python**, **TypeScript / JavaScript**, **Rust**, **C / C++**, and **Lua**. The corresponding language server has to be installed on your `PATH` (`gopls`, `pyright`, `ts_ls`, `rust-analyzer`, `clangd`, `lua-language-server`).

| What you want                   | How                          |
| ------------------------------- | ---------------------------- |
| Go to definition                | `F12`                        |
| Go to type definition           | `Ctrl+F12`                   |
| Go to implementation            | Palette                      |
| Find references                 | `Shift+F12`                  |
| Hover documentation             | `K` (Normal mode) or palette |
| Rename symbol                   | `F2`                         |
| Quick fix / code actions        | `Ctrl+.` or `Alt+Enter`      |
| Go to symbol in file            | `Ctrl+Shift+O`               |
| Go to symbol in workspace       | Palette                      |
| Show all problems (diagnostics) | Palette → `View: Problems`   |
| Next / previous problem         | `F5` / `Shift+F5`            |
| Toggle inlay hints              | `F4`                         |
| Manual completion               | `Ctrl+Space`                 |
| Format document                 | `Shift+Alt+F`                |

Format-on-save is on. Auto-completion pops up while you type. `Tab` accepts completions; `Enter` accepts too.

## Snippets

Type a trigger word, then `Tab`. Inside an active snippet, `Tab` and `Shift+Tab` jump between placeholders. `Snippets: Browse...` in the palette lists every available trigger for the current filetype.

- **Go**: `iferr`, `iferrf`, `funcm`, `fori`, `forr`, `pkg`, `test`, `bench`, `lg`, `doc`
- **Python**: `defm`, `deff`, `cls`, `main`, `forr`, `pr`, `try`
- **JS / TS**: `cl`, `fn`, `afn`, `forr`, `fori`, `iml`, `tryc` (+ `iface` / `type` for TS)
- **Rust**: `fnm`, `fnn`, `forr`, `matchc`, `impl`, `test`, `pl`
- **Lua**: `fn`, `forr`, `forp`, `iff`
- **Markdown**: `code`, `link`, `img`

`F7` opens the markdown preview overlay — only enabled on `.md` / `.markdown` / `.mdx` / `.mdown` / `.mkd` files. Output is rendered with glamour, themed to match the editor.

## Debugger

`F9` toggles a breakpoint, `F10` / `F11` / `Shift+F11` step over / into / out. The palette has `Debug: Start`, `Debug: Stop`, `Debug: Continue`, `Debug: Show Call Stack`, `Debug: Show Variables`. Status bar shows a `● DEBUG` badge while a session is active.

Adapters: **Go** (delve — `go install github.com/go-delve/delve/cmd/dlv@latest`), **Python** (`pip install debugpy`), **Node.js** (`node --inspect`). The `nvim-dap` plugin is auto-cloned on first run, just like vim-visual-multi.

> Most terminal emulators bind `F11` to fullscreen, so `Step Into` may not reach the app from your keyboard. The palette commands always work.

## Workspaces

You can have several project folders open at once. Palette → `Workspace: Add Folder...` adds a folder; the explorer stacks roots vertically and Quick Open / workspace search index every root. `Workspace: Remove Folder...` drops one. The list persists in `session.json`.

To open a different recent project entirely, use `File: Open Recent Workspace...` which re-execs termocode with the chosen cwd.

## Settings, themes, keybindings

All three are palette-driven and persist to `~/.config/termocode/`:

- **`Preferences: Open Settings (UI)`** — exposes every persisted setting with live preview:
  - `theme` — color theme id (also driven by `Preferences: Color Theme`)
  - `font_delta` — terminal font-size delta (negative = smaller; restart to apply)
  - `auto_save` — write modified buffers automatically after one second of idle time
  - `word_wrap` — soft-wrap long lines on startup
  - `show_hidden` — show dotfiles in the explorer
  - `tab_size` — `tabstop` / `shiftwidth` (1..16)
- **`Preferences: Color Theme`** — pick from the bundled themes (and your custom one if you've saved any overrides).
- **`Preferences: Edit Custom Theme`** — every palette key (44 colors) editable as hex. The Theme Picker gains a "Custom" entry once any override is saved.
- **`Preferences: Customize Keybindings`** — every action lists its current binding; Enter to retype. Validation rejects unknown key names. Loaded at startup, layered on top of the defaults.
- **`Open Config: Theme` / `Open Config: User Commands`** — open the raw JSON config files in the editor.

## Git

The Source Control sidebar (second activity-bar icon) shows the current branch, ahead/behind counts, and every changed file. Status letters appear next to filenames in the file explorer too.

Inside the editor, the leftmost column shows colored bars for added (`┃` green), modified (`┃` yellow), and deleted (`▁` red) lines vs HEAD. Updates on save and after a moment of idle. Jump between changes with `Alt+]` (next) and `Alt+[` (previous).

When the Source Control sidebar has focus:

| Key       | Action                |
| --------- | --------------------- |
| `s`       | Stage / unstage       |
| `d`       | Show diff             |
| `c`       | Commit (opens prompt) |
| `x`       | Discard changes       |
| `r`       | Refresh               |
| `Enter`   | Open the file         |
| `j` / `k` | Move cursor           |
| `g` / `G` | Top / bottom          |

Higher-level commands live in the palette: `Git: Push`, `Git: Pull`, `Git: Switch Branch...` (creates new branches too), `Git: Stage All Changes`, `Git: Show Diff for Current File`, `Git: Refresh Status`, `Git: Focus Source Control`, `Git: Stash Changes`, `Git: Pop Latest Stash`, `Git: Show Stash List...`, `Git: Show Log...` (Enter on any commit shows its full diff), `Git: Compare with Revision...` (diff vs branch / `HEAD~3` / SHA), `Git: Show File History...` (commits that touched the active file), `Git: Blame Current Line` (`Alt+B`, shows hash · author · time · subject in a toast), `Git: Show Tags...` (Enter checks the tag out as a detached HEAD).

Compare any two files with `File: Compare Two Files...` in the palette — pick LEFT, then RIGHT, see the colored unified diff.

## Run tests

`Alt+T` (or palette → `Run: Tests`) runs the project's test suite and shows the output in a scrollable preview overlay. Project type is detected from a marker file in the current directory:

| Marker           | Command             |
| ---------------- | ------------------- |
| `go.mod`         | `go test ./...`     |
| `Cargo.toml`     | `cargo test`        |
| `pyproject.toml` | `pytest`            |
| `package.json`   | `npm test --silent` |

PASS/FAIL lines are highlighted green/red so failures are easy to spot.

## Terminal

`Ctrl+T` opens (or closes) an integrated shell in a 10-row split at the bottom. Your `$SHELL` is used. Status bar shows `● TERM` while it's open. ``Ctrl+` `` and ``Alt+` `` are alternate bindings if `Ctrl+T` clashes with something. `F11` is **not** used because most terminal emulators reserve it for fullscreen.

`Terminal: Open External Shell` (palette) launches your `$SHELL` in a fresh terminal window outside termocode.

## Custom commands

Drop a `commands.json` into `~/.config/termocode/` with your project's build / lint / deploy / whatever scripts and they'll show up in the palette as `User: <Title>`. Output is captured into a scrollable preview so you don't have to leave the editor.

```json
[
  { "id": "build", "title": "Build",   "cmd": "go build ./..." },
  { "id": "lint",  "title": "Lint",    "cmd": "golangci-lint run" },
  { "id": "deploy","title": "Deploy",  "cmd": "./scripts/deploy.sh staging" }
]
```

`cmd` runs through `sh -c` so pipes / chains / env vars all work.

## Zen mode & power moves

`Alt+Z` (or palette → `View: Toggle Zen Mode`) hides every chrome element so just the editor fills the window — no tabs, breadcrumbs, sidebar, or status bar. Toggle again to bring them back. Terminal-emulator fullscreen (e.g. `F11`) is best-effort and not toggled by termocode itself.

Other quality-of-life commands in the palette:

- `Developer: Reload Window` — restart termocode in the same folder (useful after editing a config file or installing a new LSP).
- `Developer: Reset Session` — clear tabs / cursor history / undo state for the current workspace.
- `File: Toggle Auto Save` — write modified buffers automatically after one second of idle time.
- `Help: Show Shortcuts` — a quick scrollable reference of every binding.
- `Help: Show Error Log` — tail the rolling `errors.log` ring without leaving the editor.
- `View: Open URL on Current Line` — find an `http(s)://...` URL in the line and open it in your browser.
- `File: Revert / Reload from Disk` — re-read the active buffer from disk (autoread is on too, but useful for "I just changed this in another tool").

## Themes

Four built-in themes: VSCode Dark+ (default), GitHub Dark, One Dark, Solarized Dark. Open the palette and run `Preferences: Color Theme`. Your choice is saved.

## Command palette

`F1` opens the palette — every command in the IDE is reachable from there. When you forget a shortcut, just type a couple of words. Frequently-used commands float to the top automatically (tagged `recent`), so muscle memory works.

Edit your config files without leaving the editor: `Open Config: Theme`, `Open Config: User Commands`. The files live under `~/.config/termocode/`.

## Welcome screen

When termocode opens with no file, it shows a welcome screen with four Quick Action cards — `Open File` (`Ctrl+P`), `Command Palette` (`F1`), `Find in Files` (`F8`), `Open Shell` (`Ctrl+T`) — plus your recently-opened files and recent workspaces. Click any card / entry to dispatch it; the cards are also keyboard-navigable with arrow keys + `Enter`.

## Persistence

Your session sticks around between runs:

- Every open tab reopens.
- The cursor lands where you left it in each file.
- Expanded folders in the sidebar stay expanded.
- Recent files are remembered for the welcome screen.
- Recent workspaces are remembered for `File: Open Recent Workspace...`.
- Undo history persists per file across runs.
- Your theme choice is saved.

## Mouse

Click anywhere to position the cursor. Drag to select. Scroll the wheel. Click tabs to switch, the X to close. Click activity-bar icons to switch sidebars. Drag the splitter between sidebar and editor to resize. Right-click a file in the explorer for more actions. The recents modal supports wheel scrolling.

## Optional tweaks

- **Icon mode** — set `TERMOCODE_ICON_MODE=nerd_font|unicode|ascii` to override the auto-detected glyph set. Default is `unicode`; nerd-font fonts get prettier file-type / git glyphs; `ascii` is the safest fallback for terminals that mis-render BMP geometry.
- **Smaller font wrapper** — `scripts/run-with-small-font.sh` launches termocode in a fresh terminal at a slightly smaller font (uses kitty / wezterm / xterm font-size flags depending on `$TERM`). Handy if your default terminal font is too large for a comfortable IDE layout.
