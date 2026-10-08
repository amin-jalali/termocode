# Termocode — Feature Document

> A terminal-native IDE that pairs a **real embedded Neovim** editing core with **VSCode-style IDE chrome** (tabs, sidebars, command palette, git panel, integrated terminal).
> This document describes **what the product does** from a user/product perspective — not the tech stack.

**Target user:** terminal-native developers, especially Vim-proficient ones, who want IDE conveniences without leaving the terminal.

---

## The Story of the System

Read this part first — the sections that follow are a feature-by-feature reference, but the product only makes sense as a whole once you see the idea underneath it.

### The thesis

Most "terminal IDEs" force a choice: either a *real* editor (Vim/Neovim) with a bare interface, or a friendly IDE (VSCode) that isn't really in the terminal. **Termocode refuses the trade-off.** It runs an actual `nvim --embed` process as its editing brain — every motion, plugin, Lua config, undo tree, and register is the genuine article — and wraps it in IDE chrome (tabs, an explorer, a command palette, a source-control panel, an integrated terminal) rendered with a TUI framework. You get VSCode's *discoverability* and Vim's *power* in the same window, over SSH, on any terminal.

### The three layers

The whole system is three concentric rings, and almost every feature in this document lives in one of them:

1. **The Neovim core (the brain).** Editing, motions, syntax, folding, marks, LSP, snippets, and the debugger all *are* Neovim — termocode drives them over RPC and renders the grid. This is why bookmarks are global marks, why multi-cursor is a real plugin, why "drop to Normal mode with `Esc`" just works.
2. **The Go/TUI chrome (the face).** Everything VSCode-shaped — tabs, the activity bar, the git sidebar, pickers, modals, toasts, the welcome dashboard, mouse handling — is drawn by termocode on top of the editor grid. The chrome *reflects* core state (gutter signs from `git diff`, breadcrumbs from LSP symbols, the dirty dot from buffer state) and *commands* the core (a click stages a hunk, a palette entry runs a motion).
3. **The auto-provisioned ecosystem (the reflexes).** Things termocode bootstraps for you on first launch with no plugin manager: `nvim-dap` (debugging), `vim-visual-multi` (multi-cursor), `rainbow-delimiters` (bracket colors). Language servers and debug adapters are *detected* on `PATH`, not installed — present ones light up, absent ones degrade quietly.

### The design principles that recur everywhere

Once you notice these, the rest of the document reads as variations on a theme:

- **Three ways to do everything.** Nearly every action is reachable by **keyboard shortcut**, the **command palette** (`F1`), *and* the **mouse** (and often a context menu or the overflow `⋮`). Beginners discover via the palette; power users graduate to keys; nobody is stuck.
- **Graceful degradation, never a wall.** No language server? Features no-op or show a polite toast. No `ripgrep`? A pure-Go search fallback runs. No `git`? The plugin clone is skipped and the editor still starts. Offline, over SSH, on a fresh machine — it keeps working.
- **The chrome is a faithful mirror.** The status bar, gutter signs, breadcrumbs, tab dirty-dots, and git decorations are all *derived* from real core state and refreshed on a heartbeat (git every 2 s, signs on idle), so the UI never lies about what's on disk or in the buffer.
- **Disk is sacred, and honest.** Destructive actions (discard, delete) confirm first; diff scratch buffers are kept unnamed so they can't leak phantom files; copies forward to your *local* clipboard via OSC 52 even over SSH; external file changes are detected and reloaded.
- **It remembers you.** Open tabs, cursor positions, expanded folders, recent files, recent workspaces, extra workspace roots, undo history, theme, and tree/flat preference all persist across sessions; a re-exec ("Reload Window") survives them.
- **One visual language.** Every surface shares the same palette tokens (no hard-coded colors), the same modal styling, the same dark glass — so a custom theme restyles the *entire* app, chrome and editor alike, with no restart.

### A day in the tool (the user journey)

You launch in a project directory. With nothing open you land on the **dashboard** — logo, four quick-action cards, recent files, recent workspaces. You `Ctrl+P` to a file, or `F8` to ripgrep the codebase. You edit with full Neovim under your fingers while LSP completion, inlay hints, and diagnostics ride along; `F12` jumps to definitions, `Ctrl+.` offers fixes. You open the **integrated terminal** (`Ctrl+T`) to run something, or `Alt+T` to run the test suite in an overlay. When it's time to commit, the **Source Control** panel shows staged/changed files and a commit graph; you stage a hunk from the side-by-side **diff**, type a message in the always-visible box, and hit **Sync**. Need focus? `Alt+Z` for Zen mode. Need another project in view? *Add Folder* for multi-root, or *Open Recent Workspace* to switch entirely. Close the app and reopen it tomorrow — every tab, cursor, and fold is exactly where you left it.

> **How to read the rest:** sections 1–3 are the editing/navigation foundation; 4–9 are the intelligence layer (search, LSP, snippets, debug, git, markdown); 10–13 are environment and customization; 14–21 are the power-user surface area and reference tables. Each section is self-contained, so you can also treat this as a lookup.

---

## Table of Contents

1. [Editor Core & Editing](#1-editor-core--editing)
2. [File Navigation & Explorer](#2-file-navigation--explorer)
3. [Tabs, Splits & Layout](#3-tabs-splits--layout)
4. [Search & Find/Replace](#4-search--findreplace)
5. [Code Intelligence (LSP)](#5-code-intelligence-lsp)
6. [Snippets](#6-snippets)
7. [Debugger (DAP)](#7-debugger-dap)
8. [Source Control (Git)](#8-source-control-git)
9. [Markdown Support](#9-markdown-support)
10. [Integrated Terminal](#10-integrated-terminal)
11. [Command Palette](#11-command-palette)
12. [Themes & Appearance](#12-themes--appearance)
13. [Settings & Configuration](#13-settings--configuration)
14. [Bookmarks](#14-bookmarks)
15. [Clipboard Management](#15-clipboard-management)
16. [Multi-Cursor Editing](#16-multi-cursor-editing)
17. [Run Tests](#17-run-tests)
18. [UI Chrome: Activity Bar, Status Bar, Breadcrumbs, Actions Panel](#18-ui-chrome)
19. [Mouse Support](#19-mouse-support)
20. [Sessions, Welcome & Power Moves](#20-sessions-welcome--power-moves)
21. [Keyboard Shortcuts Reference](#21-keyboard-shortcuts-reference)

---

## 1. Editor Core & Editing

The editing core is a **real embedded Neovim** (`nvim --embed` over RPC) — not an emulation. All native Vim capabilities (motions, undo tree, marks, registers), user keymaps, Lua config, and the plugin ecosystem work as-is. Press `Esc` to drop into Normal mode for native motions.

| Capability | How to use |
|---|---|
| Save current file | `Ctrl+S` |
| Save all files | Palette → *File: Save All Files* |
| Undo / Redo | `Ctrl+Z` / `Ctrl+Y` |
| Select with arrows | `Shift+←↑↓→` |
| Word jump / word select | `Ctrl+←→` / `Ctrl+Shift+←→` |
| Move line up/down | `Alt+↑↓` |
| Duplicate line | `Shift+Alt+↑↓` |
| Copy / Cut / Paste | `Ctrl+C` / `Ctrl+X` / `Ctrl+V` (system clipboard) |
| Toggle line comment | `Ctrl+/` |
| Format document | `Shift+Alt+F` (LSP-powered where available) |
| Toggle word wrap | `Alt+W` |
| Copy file:line reference | Palette → *Copy: File:Line…* (outputs `path:line:col`, OSC 52 for SSH) |

**Line transformations** (palette-driven): sort ascending/descending, reverse lines, UPPER/lowercase, join lines, trim trailing whitespace, collapse blank lines, convert tabs ↔ spaces.

### Code folding
- **Fold all**: `zM` (palette → *View: Fold All*, overflow → *Collapse All*).
- **Unfold all**: `zR` (palette → *View: Unfold All*, overflow → *Expand All*).
- **Toggle fold under cursor**: `za` (palette → *View: Toggle Fold Under Cursor*).
- Folds start fully open (`foldlevelstart=99`); the fold gutter is hidden to keep the left margin compact.

### Editor visuals (always on)
- **Syntax highlighting** via Neovim `syntax on` plus Tree-sitter, with every highlight group remapped to the active theme.
- **Rainbow bracket-pair colorization** — a Tree-sitter colorizer (`rainbow-delimiters.nvim`, auto-cloned on first run) cycles `()`/`[]`/`{}` through gold → magenta → azure by nesting depth.
- **Indent guides** — a faint `│` rendered at each indentation level via extmarks.
- **Current-line highlight** (`cursorline`), **line numbers** (toggleable — see the overflow menu), 4-space scroll-off margin, and a mode-aware cursor shape (block in Normal, bar in Insert).
- **External-change detection** — `autoread` + a 1 s idle `checktime` silently reloads buffers whose file changed on disk (e.g. after `git pull` or an edit from another tool).

### Supported file types
Termocode opens text/code files only. Opening a binary, image, or archive is **refused with a polite toast** rather than dumping garbage into Neovim. The allow-list spans ~80 extensions across programming languages, prose/docs (`.md`, `.txt`, `.rst`, `.org`, `.tex`, …), and config/data formats.

---

## 2. File Navigation & Explorer

### File Explorer Sidebar
- Directory tree of one or more workspace roots; toggle with `Ctrl+B`.
- **Multi-root workspaces** — add/remove folders via Palette → *Workspace: Add Folder…* / *Remove Folder…*. Files from extra roots are prefixed with the root basename.
- **Git status glyphs** next to filenames (modified, staged, untracked…).
- **Multi-select & bulk ops** — `Shift+Click` (range), `Ctrl+Click` (toggle), bulk delete with confirmation.
- **Right-click context menu** — new file, new folder, rename, delete, reveal in file system.
- **Reveal active file** with `Ctrl+Shift+E`.
- **Toggle hidden files** via Palette → *Files: Toggle Hidden Files*.
- **Drag-resize splitter** between sidebar and editor (20–60 cols).

### Quick Open (fuzzy file picker)
- `Ctrl+P` — fuzzy search across all project files with live filtering. The file list is **rebuilt fresh each time** the picker opens.
- Smart-skips `.git`, `node_modules`, `vendor`, `dist`, `build`, `target`, `__pycache__`, `.idea`, `.vscode`, `.cache`.
- **Multi-root aware** (see [Multi-Root Workspaces](#multi-root-workspaces--multi-workspace) below): files from the primary root show as plain relative paths; files from added folders are prefixed with the folder's basename (e.g. `otherproj/src/main.go`) so you always know which project a hit belongs to. Files that exist in the primary root are not duplicated from extra roots.

### Recent Files & Workspaces
- `Ctrl+R` — recent files modal (separate from quick open), persisted across sessions in `~/.config/termocode/recents.json` (up to **20** entries).
- Palette → *File: Open Recent Workspace…* — see the distinction in [Multi-Root Workspaces](#multi-root-workspaces--multi-workspace).

### Multi-Root Workspaces & Multi-Workspace

Termocode distinguishes two *different* concepts that are easy to confuse — one adds a folder to the **current window**, the other **switches projects entirely**.

#### Add Folder to Workspace (multi-root, same window — VSCode-style)
- **Open:** Palette → *Workspace: Add Folder…*
- A text **prompt** ("Folder path:") opens, pre-filled with your home directory. (It's a path prompt, not a directory picker.) `~` is expanded and the path is resolved to absolute and validated as an existing directory.
- The folder is added as an **additional top-level root** in the explorer, rendered as its own depth-0 section *below* the primary root (roots are **not** nested under a shared parent). Each root keeps its own expand/collapse state.
- **Deduplication:** adding a path that's already the primary root or an existing extra is silently ignored.
- **Remove:** Palette → *Workspace: Remove Folder…* opens a picker of all roots. The **primary root is shown but cannot be removed** (labeled *"primary (cannot remove)"*); only added folders can be removed. If there are no extra folders you get a *"No additional folders to remove"* toast.
- **What sees all roots:** Quick Open (`Ctrl+P`), Find/Replace in workspace, and the live search overlay all operate across **every** root.
- **Persistence:** extra roots are saved to `SessionState.Roots` in `~/.config/termocode/session.json`. On the next launch they're re-added *before* restoring the explorer's expanded-folder state (so an expansion living under an extra root is reopened too). Roots that no longer exist on disk are **silently dropped and the list re-saved**.
- **Note:** the *primary* root is always the directory termocode was launched in (cwd) and is intentionally **not** persisted — only the extra roots are.

#### Open Recent Workspace (switch project — shell-style)
- **Open:** Palette → *File: Open Recent Workspace…*
- This is a **full workspace switch**, not multi-root: it **re-execs** the termocode process (`syscall.Exec`) in the chosen directory. The new instance starts with that folder as its cwd/primary root and loads *its own* `session.json`. Your current session is saved on the way out.
- **Recent list:** every launch records the cwd to `~/.config/termocode/workspaces.json` (each entry is a `{Path, OpenedAt}`), keeping up to the most-recent entries and moving an existing folder to the top rather than duplicating it.

| | Add Folder | Open Recent Workspace |
|---|---|---|
| Mental model | VSCode "Add Folder to Workspace" | `cd` + relaunch |
| Process | same running instance | re-execs a new process |
| Primary root | unchanged | becomes the chosen folder |
| Stored in | `session.json` → `Roots[]` | `workspaces.json` |

### Go to Line
- `Ctrl+G` — jump to a line number.

---

## 3. Tabs, Splits & Layout

### Tabs
- Tab bar above the editor for open buffers.
- Next / previous tab: `Ctrl+PgDn` / `Ctrl+PgUp`; last file: `Ctrl+Tab`.
- Close tab: `Ctrl+W` (or click the **X**).
- **Reopen closed** tab: `Alt+Shift+T` / `Shift+F4` (ring of last 10 closed files).
- **Pin tab**: `Alt+P` (pinned tabs stay at the front).

### Splits
- Vertical split: `Ctrl+\`; horizontal split & close split via palette.
- Focus swap editor ↔ sidebar: `F6`; focus editor `Ctrl+1`, focus explorer `Ctrl+0`.
- Native nvim `:split` / `:vsplit` window management works.

---

## 4. Search & Find/Replace

Termocode ships **four distinct search surfaces**, each tuned to a different scope. They do *not* share one engine — current-file search is driven by Neovim's own search engine, while workspace search shells out to **ripgrep** (`rg`) with a built-in pure-Go fallback when `rg` is absent.

| Scope | Open | Surface | Engine |
|---|---|---|---|
| Find in current file | `Ctrl+F` | Floating findbar | Neovim `@/` + `hlsearch` |
| Replace in current file | `Ctrl+H` | Same findbar, expanded | Neovim `:s` |
| Find in workspace | `F8` / `Alt+F` | Prompt → results buffer | ripgrep `--json` |
| Find in files (live overlay) | Palette → *Search: Find in Files (Live Overlay)* | Floating picker, debounced | ripgrep `--json` |
| Replace in workspace | `Ctrl+Shift+H` / palette | 2-step prompt | `rg -l` + `sed -i` |

### 4.1 Find in current file (`Ctrl+F`)

- Opens a **compact floating widget** anchored top-right of the editor (it overlays content rather than pushing it).
- **Auto-prefills** from the current Visual-mode selection if one exists.
- On every keystroke it sets Neovim's search register (`let @/ = …`), enables `hlsearch`, and **all matches highlight live** in the buffer.
- **Three filter toggles** (clickable glyphs, each 2 cells wide):
  - `Aa` — **case-sensitive** (injects `\C` into the pattern).
  - `ab` — **whole word** (wraps the pattern with `\<…\>`).
  - `.*` — **regex** mode. When OFF, the query is treated literally via `\V` with backslashes escaped; when ON the pattern is passed through raw.
- **Live match count** shown as `current/total` (e.g. `3/17`), computed via Neovim's `searchcount()` (capped at 9999, 50 ms timeout). Shows in a warning color when zero.
- **Navigate**: `Enter` / ↓ button → next match (`n`); `Shift+Tab` / ↑ button → previous (`N`).
- **Close**: `Esc` (also runs `nohlsearch` to clear the highlight).

### 4.2 Replace in current file (`Ctrl+H`)

- Expands the **same findbar** into a two-row find + replace layout (the chevron flips to point down).
- `Tab` / `Shift+Tab` move focus between the *Find* and *Replace* inputs.
- **Replace one**: `Enter` in the replace field (or the `↵` button) — jumps to the next match and replaces it at the cursor, leaving you positioned so the next `Enter` continues.
- **Replace all**: `Ctrl+Enter` / `Alt+Enter` (or the `⇒` button) — runs `:%s/…/…/g` across the whole buffer.
- Replace buttons are **disabled** (dimmed) when there is no active query or zero matches.
- Replacement text is escaped for the `:s` command (`/`, `&`, `\`).

### 4.3 Find in workspace (`F8` / `Alt+F`)

- Opens a **text prompt** ("Find:") — prefilled from the Visual selection if present — *not* a live picker.
- On submit, searches **every workspace root** and writes results into a dedicated **"Find Results" scratch buffer** (Sublime-style) with syntax highlighting of the query, rather than an interactive list.
- Under the hood it runs:
  ```
  rg --json --smart-case --max-count=100 --max-columns=200 -- <query>
  ```
  - `--smart-case`: case-insensitive unless the query contains an uppercase letter.
  - `--max-count=100` per file, `--max-columns=200` to tame minified lines.
  - The query is a **literal string** (no `--`-escaping needed; not regex by default).
- **Multi-root**: each root is searched independently and result paths are rewritten to absolute so any hit opens correctly regardless of which root it came from.
- **Pure-Go fallback** when `rg` isn't on `PATH`: walks the tree with smart-case literal matching, skips `.git`, `node_modules`, `vendor`, `target`, `dist`, `build`, `.next`, `.cache`, `__pycache__`, `.venv`, `venv` and any dotdir, skips files > 5 MiB, and detects binaries via a NUL-byte probe in the first 8 KiB.
- **Result format**: grouped by file, `line:    preview` rows, previews truncated to 250 chars. Footer summarizes `M matches across N files`. Capped at **100 results total**.
- Click a result row to jump to that `file:line`.

### 4.4 Find in files — live overlay (palette)

- A **floating search picker** that updates results *as you type* (150 ms debounce), distinct from the prompt-and-buffer flow above.
- Same ripgrep engine and 100-result cap; matched spans are highlighted within each preview line (offsets come from ripgrep `submatches`).
- Layout: title ` Find in Files`, a `> query` input, two rows per hit (`path:line:col` then the indented preview), and a footer `↑↓ navigate · enter open · esc close`.
- Reachable via the palette (it is wired through `SetRoots` to all workspace roots); there is no default keybinding bound to it.

### 4.5 Replace in workspace (`Ctrl+Shift+H`)

A fast, **two-step prompt** flow (no picker, no preview):

1. **Find:** prompt — the literal string to search for.
2. **Replace `<find>` with:** prompt — the replacement text.

On submit it:
- Lists matching files with `rg -l --null -F <find>` (the `-F` makes it a **literal** search — there is no case/regex/whole-word toggle here).
- Applies the change in place per file with `sed -i 's#<find>#<replace>#g'` (delimiter `#`; `\`, `#`, `&` escaped in both sides).
- Runs `:checktime` so Neovim reloads the modified buffers.
- Reports the outcome via toast: *"Replaced in N file(s)"*, *"No matches found"*, or *"No files modified"*.

> ⚠️ **Important behavior:** this is a direct, **literal, in-place** edit with **no confirmation dialog and no built-in undo** across files — changes hit disk immediately. (Single-file find/replace, by contrast, is fully undoable through Neovim.)

### 4.6 Shared limits

- Workspace search/replace cap: **100 results**, **200 columns** per line, **5 MiB** per file (fallback), **1 MiB** JSON scanner buffer per event.
- Match-count timeout in current-file find: **50 ms**.

---

## 5. Code Intelligence (LSP)

Language servers attach automatically per filetype when their executable is on `PATH` (auto-detected, not auto-installed). Configured servers and the filetypes they bind to:

| Server | Languages | Root markers |
|---|---|---|
| `gopls` | Go (go, gomod, gowork, gotmpl) | `go.mod`, `go.work`, `.git` |
| `pyright` / `pylsp` | Python | `pyproject.toml`, `setup.py`, … |
| `ts_ls` | JS/TS (+ JSX/TSX) | `package.json`, `tsconfig.json`, … |
| `rust_analyzer` | Rust | `Cargo.toml`, `.git` |
| `clangd` | C / C++ / Obj-C | `compile_commands.json`, … |
| `lua_ls` | Lua | `.luarc.json`, … |

Every LSP feature degrades gracefully when no server is attached (no-op or a polite toast).

### 5.1 Navigation
| Action | Binding(s) | Result |
|---|---|---|
| Go to definition | `F12`, `gd` | jump |
| Go to declaration | `gD` | jump |
| Go to type definition | `Ctrl+F12`, `gt` | jump (toast if none) |
| Go to implementation | `gi`, palette | jump (toast if none) |
| Find all references | `Shift+F12`, `gr` | **picker** of `file · preview · Lline` |
| Go to symbol in file | `Ctrl+Shift+O` | indented picker with kind glyphs (`C`/`M`/`ƒ`/`I`/`S`/`E`/…) |
| Go to symbol in workspace | palette | picker `name · container`, fuzzy |

### 5.2 Diagnostics
- **Problems picker** (palette → *View: Problems*) — all diagnostics across loaded buffers, sorted by severity then file then line, with icons `✘`/`⚠`/`ℹ`/`💡`.
- Navigate within the buffer: `F5` (next) / `Shift+F5` (previous).
- Inline display: virtual-text `●` prefix, sign-column glyphs (Nerd Font or `E`/`W`/`I`/`H`), underlines; severity-sorted, not updated while in Insert mode.

### 5.3 Editing intelligence
- **Hover docs**: `K` — opens a preview overlay; renders markdown when the body looks like markdown, plain text otherwise.
- **Rename symbol**: `F2` — LSP-wide rename.
- **Format**: `Shift+Alt+F` on demand, plus **format-on-save** (async, 2 s timeout, silently skipped if no formatter).
- **Inlay hints**: auto-enabled per buffer (type hints / parameter names) in a muted italic; `F4` is the documented toggle.
- **Completion**: auto-pops while typing (80 ms debounce, on word chars / `.` / `:`); `Tab`/`Enter` accept, `Shift+Tab` previous, `Ctrl+Space` manual trigger (falls back to keyword completion without LSP). **Signature help** on `Ctrl+Space`.

### 5.4 Code actions / Quick Fix
- Open with `Ctrl+.` or `Alt+Enter` (palette: *Edit: Quick Fix*). The picker groups actions in a stable order: **Quick Fix → Source → Refactor → Generate → Test → Git → Other**, with section headers and per-kind icons (`🔧`/`✎`/`✦`).
- It blends **real LSP actions** with **termocode-synthesized actions** for the current context:
  - **Run Test: `<Func>`** and **Run Package Tests** — runs `go test` (suspends/streams/resumes), with a pass/fail toast.
  - **Copy go test command** — puts `go test -run ^Func$ -v <pkg>` on the clipboard.
  - **Git: Show Blame for Line**, **Git: Show Diff for File**, **Git: Copy Commit Hash for Line**.
  - **Generate: Doc Comment for `<Name>`** — inserts a `// Name …` comment above an exported declaration.
  - **Generate: Table-Driven Test Skeleton for `<Func>`** — scaffolds (or appends to) the matching `*_test.go`.
- **Preferred action**: when the LSP flags a preferred fix, an inline **💡 hint** appears at the cursor row (`💡 <title>  Ctrl+.  Apply: Ctrl+Shift+.`); `Ctrl+Shift+.` applies it directly without opening the picker. The hint clears on cursor move or any other key.

---

## 6. Snippets

- **Expand**: type a trigger then `Tab`; jump placeholders with `Tab` / `Shift+Tab` (uses Neovim 0.10+ `vim.snippet`; falls back to plain insertion on older nvim). Tab precedence: expand trigger → accept completion popup → jump placeholder → plain tab.
- **Browse**: Palette → *Snippets: Browse…* — picker of all triggers for the current filetype (insert without remembering the name).
- Bundled triggers per language:
  - **Go**: `iferr`, `iferrf`, `funcm`, `fori`, `forr`, `pkg`, `test`, `bench`, `lg`, `doc`
  - **Python**: `defm`, `deff`, `cls`, `main`, `forr`, `pr`, `try`
  - **JavaScript**: `cl`, `fn`, `afn`, `forr`, `fori`, `iml`, `tryc`
  - **TypeScript**: adds `iface`, `type` (+ all JS ones)
  - **Rust**: `fnm`, `fnn`, `forr`, `matchc`, `impl`, `test`, `pl`
  - **Lua**: `fn`, `forr`, `forp`, `iff`
  - **Markdown**: `code`, `link`, `img`
  - (`*react` filetypes alias to JS/TS.)

---

## 7. Debugger (DAP)

Debug Adapter Protocol via auto-bootstrapped `nvim-dap` (shallow-cloned to `~/.local/share/termocode/plugins` on first launch; clone failure is non-fatal and degrades gracefully).

| Action | Binding |
|---|---|
| Toggle breakpoint | `F9` |
| Step over | `F10` |
| Step into | `F11` (terminal may hijack — use palette) |
| Step out | `Shift+F11` |
| Start / Stop / Continue | Palette |
| Show call stack / variables | Palette |

- **Adapters** (auto-registered when their toolchain is present): **Go** = Delve (`dlv dap`), **Python** = debugpy, **Node/TS** = `node --inspect`. Each ships two-ish launch configs (current file / package).
- **Start** calls nvim-dap's `continue()`, which auto-launches a single matching config or **prompts you to pick** when several match. If the adapter tool is missing you get an actionable toast (e.g. *"install dlv (go install …)"*).
- **Breakpoints** show in the sign column: `●` breakpoint, `◆` conditional, `▶` current stop line (with line highlight). They are **session-only** (not persisted).
- **Call stack** and **Variables** render into a scrollable **preview overlay** (formatted by Lua helpers; values truncated to 80 chars) — there is no dedicated debug panel.
- A bold red **`● DEBUG`** badge appears in the status bar while a session is active. Every step/continue/toggle pushes a small status toast.

---

## 8. Source Control (Git)

A deep, VSCode-class git experience reached from the **Source Control** activity-bar icon. The whole panel **auto-refreshes every 2 seconds** (and on save/idle), so status, branch, graph, explorer decorations, status-bar branch, and editor gutter signs stay live.

### 8.1 Panel anatomy

Fixed chrome sits above a scrollable accordion:

```
S O U R C E   C O N T R O L            (title)
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
⎇ main ↑2 ↓1                       t   (branch + ahead/behind + tree/flat toggle)
▌ Message…                             (always-visible commit box)
✓ Commit (3)                    ⟳  ▾   (action bar)
─────────────────────────────────────
▾  S T A G E D  3                      (accordion section, collapsible)
▾  C H A N G E S  2
▾  G R A P H
```

- **Three accordion sections**, each collapsible with ←/→ or `Enter` on its header (chevron `▾`/`▸`):
  - **STAGED** — files with index-side changes (only shown when non-empty).
  - **CHANGES** — working-tree modifications + untracked files.
  - **GRAPH** — the current branch's commit graph (up to 200 commits).
- A file with status `MM` legitimately appears in **both** STAGED and CHANGES (matches VSCode).
- Section collapse states are **not** persisted; tree/flat mode **is**.

### 8.2 Tree vs Flat view

- Toggle with `t` (or click the glyph at the right of the branch row). The choice is **persisted** in `session.json`.
- **Flat**: every file at depth 0, basename + dim parent dir.
- **Tree**: a real directory hierarchy with per-folder collapse (←/→ or `Enter` on a folder) and `│` indent guides. View-mode icons distinguish the two states.

### 8.3 File status display

- Two-char git codes (`M`/`A`/`D`/`R`/`C`/`U`/`??`).
- A colored status dot per file: untracked = hollow ○ green, modified = ● amber, added = ● green, deleted = ● red, renamed = ● blue, conflict = ● bright red.
- **Editor gutter signs** (left margin) computed from `git diff -U0 HEAD`: added `┃` green, modified `┃` amber, deleted `▁` red. Refreshed on save/read/enter and on 1 s idle, throttled to ≤ once per 500 ms. Tracked files only.

### 8.4 Keyboard (git sidebar focused)

| Key | Action |
|---|---|
| `j`/`k`, ↑/↓ | move cursor (skips connector/spacer rows) |
| `g` / `G` | jump to top / bottom |
| `Enter` | section/folder → toggle collapse; file → open; commit → show its diff |
| ←/→ | collapse / expand section or folder |
| `s` | **stage toggle** — stages an unstaged/untracked file, or unstages a staged one (direction chosen automatically) |
| `d` | **diff** — on a file: side-by-side diff vs HEAD; on a commit: that commit's full diff |
| `c` | focus the commit message box |
| `x` | **discard** (opens a confirmation dialog) |
| `r` | refresh git state |
| `t` | toggle tree/flat view |

### 8.5 Commit box + action bar

- The **commit message box** is always visible (sunken input with a left rail that turns cyan when focused, "Message…" placeholder, horizontal scroll). `Enter` commits; `Esc`/`Tab` blur back to the list. Empty message → *"Enter a commit message"* toast; no staged changes → confirm *"Stage all changes?"*. On success the box clears and a toast shows the subject (≤ 50 chars).
- The **action bar** below it:
  - **✓ Commit (N)** — primary button; shows the staged count; lit when there are staged files or a non-empty message.
  - **⟳ Sync** — pull then push (or first `push -u` if no upstream).
  - **▾ Overflow** — dropdown: Commit, **Commit (Amend)**, **Commit (Sign-off)**, Commit Message…, ─, Sync, Push, Pull, Fetch, ─, Refresh.

### 8.6 Side-by-side diff (the real centerpiece)

Opened with `d` on a file, palette *Git: Show Diff for Current File*, or right-click → *Open Changes*. It builds a **two-pane Neovim diff**:

- **Left pane** = HEAD revision (read-only scratch buffer); **right pane** = working tree (editable). Both in native `diffthis` mode with line numbers and folds disabled so they stay aligned.
- **Per-pane winbar headers**: right shows a live **`current/total` hunk counter** + filename + `· working`; left shows clickable **`‹ ›` nav arrows** + filename + `· HEAD`.
- **Per-side coloring**: the HEAD side tints changes red, the working side tints them green; changed *words* are bolded; deleted-line gaps fill with `·` dots.
- HEAD pane uses **Tree-sitter** syntax (not regex) to avoid scroll lag.
- **Navigation**: `]c`/`[c` or click the `‹ ›` arrows to jump hunks (wraps at ends). Mouse **wheel scrolls both panes together**; pointer motion switches focus between panes.
- **Hunk staging from anywhere** (palette / context): **Stage Hunk**, **Unstage Hunk**, **Discard Hunk** — each isolates the single hunk under the cursor and applies it with `git apply` (`--cached` to stage, `--reverse` to unstage/discard); discard reloads the buffer via `checktime`.
- Diff buffers are deliberately **unnamed** (label lives in the winbar) and `bufhidden=wipe`, with temp files deleted immediately and stray `[No Name]` buffers swept — this is the fix for "phantom files" leaking into the explorer.
- **New/untracked files** synthesize a `/dev/null → file` diff (all-added). Deleted files show the inverse. Binary files fall back to git's "Binary files differ".

### 8.7 Commit diff (GitLab-style)

Selecting a commit (in GRAPH, log, or file-history) opens its **full `git show` diff** as a single scrollable `filetype=diff` buffer with green/red/cyan/yellow syntax coloring — not side-by-side.

### 8.8 Compare arbitrary things

- **File: Compare Two Files…** — pick file A then file B; renders `diff -u` in a preview overlay (any two files, not just tracked).
- **Git: Compare with Revision…** — prompts for a ref (branch / `HEAD~N` / SHA), shows `git diff <ref>` colorized in a preview overlay.

### 8.9 Commit GRAPH

- Rendered from `git log --graph --decorate` with lane glyphs (`│ ╱ ╲`) and commit nodes (`● ◉`).
- Each commit row: a **type-colored ribbon `▌`** (feat=green, fix=amber, docs=blue, refactor=magenta, perf=teal, test=cyan, revert=orange, chore/ci=muted; HEAD=teal, merge=magenta), a branch/tag pill, the cleaned subject (bold if HEAD), and a compact relative age (`2h`, `3d`, `now`).
- **Commit hover card** (on mouse hover): a floating card with author · relative date, absolute date, bold subject, wrapped body (≤ 12 lines), and `+adds −dels  N files   <hash>`. Details are fetched once per commit and cached.

### 8.10 Branches, remotes, stash, history, blame

- **Switch/Create branch** — fuzzy picker of local branches; first entry creates a new branch (`checkout -b`). Reloads buffers + toast.
- **Checkout tag** — picker of tags → detached HEAD.
- **Push / Pull / Fetch / Sync** — push auto-sets upstream on first push (`-u`); pull/sync reload buffers via `checktime`; fetch uses `--prune`; each reports a toast.
- **Stash** — Stash Changes, Pop Latest Stash, Show Stash List… (apply a chosen stash).
- **Log** (`git log -n200`) and **File History** (`git log --follow -n200 -- <file>`) → pickers; selecting shows the commit diff.
- **Blame current line** (`Alt+B`) → toast `hash · author · when · subject` (uncommitted lines show `uncommitted · you · just now`).
- **Ahead/behind** counts (`↑N ↓N`) and the current branch (or `(detached <sha>)`) show in both the panel header and the status bar.

### 8.11 Git via Code Actions

The Quick Fix menu (`Ctrl+.`) also synthesizes git actions for the current line/file: **Show Blame for Line**, **Show Diff for File**, **Copy Commit Hash for Line** (see §5.4).

---

## 9. Markdown Support

- **Preview**: `F7` for `.md/.markdown/.mdx/.mdown/.mkd` — glamour-rendered, theme-matched, opens in a new tab, refreshes on save.
- Markdown snippets: `code`, `link`, `img`.

---

## 10. Integrated Terminal

- Toggle bottom panel: `Ctrl+T` (alt: `` Ctrl+` `` / `` Alt+` ``). `● TERM` badge while open.
- **Multi-tab**: new `` Ctrl+Shift+` ``, close `Ctrl+Shift+W`, next/prev `Ctrl+Shift+PgDn`/`PgUp`. Each tab keeps its own PTY/shell state.
- Maximize / minimize panel; drag-resize the panel height.
- **Extra ops** (overflow ⋮ menu): *Clear Terminal*, *Show Scrollback* (dumps the buffer into a scrollable preview), and *Open Terminal Here* (new tab rooted at the active file's directory).
- **External shell**: suspends the TUI, runs `$SHELL` on the real TTY, resumes on exit.
- In terminal insert mode, control keys (`Ctrl+C`, `Tab`, `Ctrl+D`, `Ctrl+R`) pass straight to the shell; `Esc` returns to normal mode.

---

## 11. Command Palette

- Open with `F1`; fuzzy search across 100+ items.
- **Recent commands** float to the top; each item shows its keyboard hint.
- Categories: File, View/Layout, Search, Editor, Code intelligence, Git, Debug, Terminal, Run, Settings/Preferences, Help, Developer.

---

## 12. Themes & Appearance

- **Built-in themes**: **VSCode Dark+** (default), **GitHub Dark**, **One Dark**, **Solarized Dark**.
- **Theme picker**: Palette → *Preferences: Color Theme* (VSCode Dark+ always listed first; a "Custom" entry appears once you save overrides).
- **Custom theme editor**: Palette → *Preferences: Edit Custom Theme* — a picker over **~60 named palette tokens** across categories (backgrounds, foregrounds, accents, borders, **syntax**, **diagnostics**, **git**, and a 3-color **bracket-pair** set). Pick a token, enter a hex (`#rgb`/`#rrggbb`), and it applies **live** with no restart. Saved to `user_theme.json` and layered over VSCode Dark+.
- The theme drives Neovim highlight groups too (traditional syntax, Tree-sitter `@…` groups, LSP semantic tokens, rainbow brackets, indent guides) so the editor body matches the chrome.
- **Visual tweaks**: font delta at launch (`TERMOCODE_FONT_DELTA`), icon mode (`TERMOCODE_ICON_MODE=nerd_font|unicode|ascii`), cursor shape changes with Vim mode.

---

## 13. Settings & Configuration

- **Settings UI**: Palette → *Preferences: Open Settings (UI)* — a grouped, **searchable** modal with live preview (mouse + keyboard, `Ctrl+P`/`Ctrl+N` skip headers). Settings and defaults:

  | Key | Type | Default | Notes |
  |---|---|---|---|
  | `theme` | string | `vscode-dark-plus` | theme ID |
  | `font_delta` | int | `-2` | launch font-size delta |
  | `auto_save` | bool | `false` | write after ~1 s idle |
  | `word_wrap` | bool | `false` | soft-wrap on startup |
  | `show_hidden` | bool | `false` | dotfiles in explorer |
  | `tab_size` | int | `4` | 1–16 |

- **Keybinding customization**: Palette → *Preferences: Customize Keybindings* — picker of every action with its current binding; selecting prompts for a new key string (e.g. `ctrl+x`, `f5`, `alt+enter`). Validated against known keys, saved to `keymap.json`, merged over defaults at startup (read-modify-write preserves your other rebinds).
- **User commands**: define `{id, title, cmd, cwd?}` entries in `commands.json`; run via `sh -c` (pipes/chains/env supported); combined output shown in a scrollable overlay (capped at 200 KB, test-status words colorized); surfaced as Palette → *User: <Title>*.
- **Open Config** commands edit the raw JSON in the editor (seeding a default template for new files): *Open Config: Theme*, *Open Config: User Commands*.
- All config lives under `~/.config/termocode/` (or `$XDG_CONFIG_HOME`): `config.json`, `user_theme.json`, `keymap.json`, `commands.json`, `session.json`, `recents.json`, `workspaces.json`, `errors.log`.

---

## 14. Bookmarks

- Toggle bookmark on the current line: `F3`.
- Show all bookmarks (picker across files): `Shift+F3`.
- Clear all via palette.
- Backed by nvim global marks (A–Z) — cross-file and survive reloads; `'A` jump works natively.

---

## 15. Clipboard Management

- **Clipboard ring**: `Ctrl+Shift+V` opens a picker of the last **20** clipboard entries (newest-first; entries > 64 KB and consecutive duplicates are skipped). Sources are **both** the OS clipboard (watched in the background) and in-editor yanks (`TextYankPost`). Multi-line entries preview as *"first line · N lines"*. Selecting one pastes it at the cursor.
- **OSC 52**: copy operations also emit an OSC 52 escape so the copy reaches the *local* clipboard even over SSH (used by Copy File:Line Reference; payloads over ~6 KB are skipped, unsupported terminals ignore it).

---

## 16. Multi-Cursor Editing

| Action | Binding |
|---|---|
| Select next occurrence | `Ctrl+D` |
| Add cursor above / below | `Ctrl+Alt+↑` / `Ctrl+Alt+↓` |
| Select all occurrences | `Ctrl+Alt+D` |
| Skip current region | `Ctrl+X` |
| Remove current region | `Ctrl+Shift+X` |
| Exit multi-cursor | `Esc` |

- Powered by auto-bootstrapped `vim-visual-multi`; all native motions apply inside the multi-selection.

---

## 17. Run Tests

- `Alt+T` or Palette → *Run: Tests*.
- Auto-detects the project type (first match wins): `go.mod` → `go test ./...`, `Cargo.toml` → `cargo test`, `pyproject.toml`/`setup.py`/`pytest.ini` → `pytest`, `package.json` → `npm test`.
- Output in a scrollable overlay with PASS/FAIL coloring; capped at 200 KB.

---

## 18. UI Chrome

### Activity Bar (left)
A 4-cell icon strip with a top group (**Files**, **Source Control**, **Run**) and a bottom group (**Settings** ⚙); an accent bar marks the active view. Clicking an inactive icon switches view; clicking the active icon toggles the sidebar. Default sidebar width 30 cols (drag-resizable 20–60, persisted).

### Status Bar
- **Left**: dirty dot (●) + project name — or a red error message if one is set, or `[no file]`.
- **Center**: git branch (with `↑N`/`↓N` ahead/behind) and LSP diagnostic counts (errors red, warnings amber).
- **Right**: `Ln N, Col N` · `Spaces: N` · `UTF-8` · filetype · `● TERM` (green, when terminal open) · `● DEBUG` (red, when debugging).
- Truncates center first, then right, then left, as width shrinks.

### Tab Bar
- One row of tabs; each shows an accent bar (active), a green `●` dirty dot, the (truncated) filename, and a `×` close button. **Pinned** tabs float to the front in pin order.
- Horizontal **overflow** with `◀`/`▶` indicators; the active tab is always scrolled into view; wheel scrolls by 2 tabs.

### Breadcrumbs & Outline
- Breadcrumb row above the editor: file path + enclosing LSP symbol, clickable.
- Outline via *Go to Symbol* pickers (file: `Ctrl+Shift+O`, workspace: palette).

### Context menus (right-click)
Right-click targets get tailored menus:
- **Explorer file**: Open, Open to the Side, Rename, Delete, Copy Path, Reveal in Terminal.
- **Explorer folder**: New File, New Folder, Rename, Delete, Open in Terminal, Copy Path.
- **Tab**: Close, Close Others, Close All to the Right, Copy Path.
- **Editor**: Cut, Copy, Paste, Format Document, Toggle Line Comment, Go to Definition.
- **Git file / commit**: Open Changes, Stage/Unstage, Discard, Commit…, Copy Commit Hash, Refresh.

Menus support hover-highlight, wheel scroll, click-to-select, and click-outside-to-close; separators and headers are skipped by keyboard nav.

### Toasts (notifications)
Bottom-right stack of up to 3, three severities — **INFO** (blue), **WARN** (amber), **ERR** (red) — auto-dismissing after ~3 s, with an optional second detail line. Warnings and errors are also mirrored into the Error Log.

### Overflow (⋮) menu
- A `⋮` glyph sits at the right edge of the tab-bar row (always visible, muted until hovered/opened); clicking it opens a **context-aware dropdown**.
- Its contents adapt to what's focused:
  - **Editor**: Go to Definition, Find References, Outline, Hover, Rename Symbol, Format Document, Find Implementations, Run Tests, Preview Markdown, Collapse/Expand All (folding), Toggle Sidebar, Word Wrap, **Line Numbers**, *Source Control: View as Tree*, Zen Mode, Auto Save, Pick Theme, Settings.
  - **Terminal**: New Terminal Tab, Clear Terminal, Show Scrollback, Close/Next/Previous Terminal Tab, Open External Shell, Open Terminal Here.
- It's a discoverability surface — every item is also reachable via the palette or a shortcut.

### Actions Panel (right)
- Toggle with `Alt+A`. Two states: **pinned** (right column, ~32 cols) or **unpinned** (floating `▸ Actions` chip). Drag-resizable (24–60 cols). Currently a placeholder for future expansion.

---

## 19. Mouse Support

- **Editor**: click to place cursor, drag to select, wheel to scroll.
- **Splitters**: drag to resize explorer width, terminal height, actions-panel width.
- **Tabs**: click to switch, click **X** to close.
- **Activity bar**: click to switch sidebars.
- **Explorer**: right-click context menu; shift/ctrl-click multi-select.
- **Pickers & modals**: click to select, wheel to scroll, click buttons in confirmations.
- **Git GRAPH**: hover a commit for a metadata card.
- Mouse tracking adapts (cell-motion vs all-motion) based on whether a text input is focused.

---

## 20. Sessions, Welcome & Power Moves

### Session persistence
Auto-saved to `session.json`: open tabs + cursor positions, expanded folders, recent files, recent workspaces, per-file undo history, theme, custom theme overrides. Reset via Palette → *Developer: Reset Session* (backs up old session as `.bak`).

### Welcome screen / Dashboard

The dashboard is the landing surface shown in the editor pane **only when all of these hold**: no file is open, at most one buffer exists, the integrated terminal is closed, and no git-diff view is active. Open any file and it disappears.

**Layout (centered, max 88 cells wide), top to bottom:**

1. **Logo block** — an ASCII "Termocode" banner (in accent blue) with a letter-spaced tagline *"A TERMINAL-NATIVE EDITOR"* (muted). On very narrow panes the banner degrades to a plain `Termocode` wordmark.
2. **START — quick-action cards.** A 2×2 grid on wide panes (≥80 cells), collapsing to a single column when narrow:

   | Card | Shortcut |
   |---|---|
   | `+` **Open file** — browse your filesystem | `^P` |
   | `>` **Command palette** — run any action | `F1` |
   | `?` **Find in files** — project-wide search | `F8` |
   | `$` **Open shell** — integrated terminal | `^T` |

   Each card shows an icon, title, subtitle, and a keycap. The **focused** card gets a highlighted background, a lavender title, and a `▌` accent bar. Subtitles are dropped on very narrow panes (<50 cells).
3. **RECENT FILES** (up to **3** shown) — each entry shows a colored 2-char file-type tag (e.g. `go` lavender, `py` amber, `rs` coral, `md` green), the bold filename, a relative timestamp (`just now`, `5m`, `3h`, `2d`, or `Mar 4`), and the directory path below. A *"view all NN →"* link opens the full recents picker when there are more than 3. Empty state: *"No files yet."*
4. **WORKSPACES** (up to **3** shown) — recent workspaces with a `>` pin glyph (amber for the most recent), name, timestamp, and path. A *"browse all →"* link opens the workspace picker. Empty state: *"No recent workspaces."*
5. **Footer** — a subtle rule, then a version line like `v0.1.2 · go 1.22` (pulled from Go build info) and an `F1 for help` keycap.

**Keyboard navigation:** `←` `→` `↑` `↓` `Tab` / `Shift+Tab` cycle focus across the four cards (wrapping); `Enter` runs the focused card's action.

**Mouse:** clicking a card runs its action *and* moves focus there; clicking a recent file opens it; clicking a workspace re-execs termocode in that folder; clicking *"view all"* / *"browse all"* opens the respective picker.

> Rendering note: the dashboard fills every line to the exact pane width with theme-styled spaces, so the terminal's default background never leaks through — and it uses **only** theme color tokens (no hard-coded hex).

### Zen mode

`Alt+Z` toggles Zen mode (VSCode's `Ctrl+K Z` chord isn't representable in a terminal, so `Alt+Z` is used). It strips the UI down to **just the editor** — hidden elements are: tabs, breadcrumbs, sidebar, status bar, activity bar, the overflow (`⋮`) menu glyph, the 💡 preferred-action hint, and the actions panel.

**Host fullscreen (best-effort).** Because terminals expose fullscreen inconsistently, Zen mode tries two mechanisms at once:
- **Escape sequences** — on: `CSI 10;1 t` + `CSI 10;2 t`; off: `CSI 10;0 t` + `CSI 10;2 t`. These are honored by xterm, WezTerm, urxvt, Konsole, mintty.
- **OS-level F11** — for terminals that ignore the escapes (kitty, iTerm2, Ghostty, Alacritty, GNOME Terminal, Terminal.app): macOS sends F11 via `osascript`; Linux/BSD try `xdotool key F11` and `wtype -k F11` (whichever exists).

A toast confirms the toggle — turning on shows *"Zen mode on"* with the hint *"press F11 if window didn't fullscreen"*; turning off shows *"Zen mode off"*.

**State:** Zen mode is *not* persisted — every launch starts with it off. Toggling off doesn't restore a remembered layout; it simply re-enables the sidebar and reapplies the standard layout.

### Auto-save
`Ctrl+Shift+A` — writes modified buffers after ~1 s idle.

### File operations (explorer / context menu)
- **New File** / **New Folder** — path prompt; creates intermediate dirs; new file opens in the editor (won't overwrite an existing file).
- **Rename** — prompt pre-filled with the current name; `os.Rename`.
- **Delete** — confirmation dialog (destructive styling); recursive; closes the buffer if open. **Bulk delete** lists up to 5 names + "…and N more" and warns it can't be undone.

### Reopen closed editor
A **ring of the last 10 closed files**; `Alt+Shift+T` (also `Shift+F4`) reopens the most recent (LIFO), with a toast — or *"No recently closed editors"* when empty.

### Help & diagnostics
- **Show Shortcuts** (palette) — a scrollable cheat sheet grouped by FILE / EDIT / SEARCH / NAVIGATION / LSP / VIEW / GIT / TERMINAL / DEBUG / WORKSPACE / PREFERENCES / DEVELOPER, plus env-var tips.
- **Show Error Log** (palette) — an in-memory ring of the last 100 errors/warnings (auto-captured from toasts), timestamped newest-first, also appended to `~/.config/termocode/errors.log`.
- **Buffer info** (`Ctrl+Alt+I`): path, size, line count, filetype, encoding, indent, and attached LSP servers.

### Developer & misc
- *Developer: Reload Window* — re-execs (useful after config/LSP changes).
- *Developer: Reset Session* — clears tabs/cursor history (backs up the old session as `.bak`).
- Open URL on current line, Reveal in file system, Revert/reload from disk.

---

## 21. Keyboard Shortcuts Reference

| Action | Binding |
|---|---|
| Quit | `Ctrl+Q` |
| Save / Save all | `Ctrl+S` / palette |
| Copy / Cut / Paste | `Ctrl+C` / `Ctrl+X` / `Ctrl+V` |
| Undo / Redo | `Ctrl+Z` / `Ctrl+Y` |
| Toggle explorer | `Ctrl+B` |
| Focus swap (editor ↔ sidebar) | `F6` |
| Quick open (files) | `Ctrl+P` |
| Command palette | `F1` |
| Close / reopen tab | `Ctrl+W` / `Alt+Shift+T` |
| Next / prev tab | `Ctrl+PgDn` / `Ctrl+PgUp` |
| Last file | `Ctrl+Tab` |
| Pin tab | `Alt+P` |
| Split vertical | `Ctrl+\` |
| Go to line | `Ctrl+G` |
| Find / Replace in file | `Ctrl+F` / `Ctrl+H` |
| Find in workspace | `F8` / `Alt+F` |
| Replace in workspace | `Ctrl+Shift+H` |
| Go to definition / type def | `F12` / `Ctrl+F12` |
| Find references | `Shift+F12` |
| Symbol in file | `Ctrl+Shift+O` |
| Rename | `F2` |
| Hover (normal mode) | `K` |
| Code actions / quick fix | `Ctrl+.` / `Alt+Enter` |
| Inlay hints | `F4` |
| Format document | `Shift+Alt+F` |
| Next / prev problem | `F5` / `Shift+F5` |
| Toggle breakpoint | `F9` |
| Step over / out | `F10` / `Shift+F11` |
| Toggle terminal | `Ctrl+T` |
| Terminal new / close tab | `` Ctrl+Shift+` `` / `Ctrl+Shift+W` |
| Open recent files | `Ctrl+R` |
| Bookmark toggle / list | `F3` / `Shift+F3` |
| Next / prev git hunk | `Alt+]` / `Alt+[` |
| Blame line | `Alt+B` |
| Zen mode | `Alt+Z` |
| Clipboard history | `Ctrl+Shift+V` |
| Toggle comment | `Ctrl+/` |
| Move / duplicate line | `Alt+↑↓` / `Shift+Alt+↑↓` |
| Multi-cursor next | `Ctrl+D` |
| Add cursor above/below | `Ctrl+Alt+↑↓` |
| Markdown preview | `F7` |
| Run tests | `Alt+T` |
| Toggle actions panel | `Alt+A` |
| Word wrap | `Alt+W` |
| Buffer info | `Ctrl+Alt+I` |
| Toggle auto-save | `Ctrl+Shift+A` |

---

*Document generated as a product-level feature inventory of Termocode. For setup/installation and architecture, see the project README and `internal/` package layout.*
