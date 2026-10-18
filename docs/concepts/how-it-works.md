# How termocode works

Most terminal tools make you choose. You get a powerful editor with a bare
screen, or a friendly IDE that does not run in the terminal. termocode does
not make you choose.

It runs a real `nvim --embed` process as its editing engine. Around it, it
draws IDE chrome: tabs, an explorer, a command palette, a git panel, a
terminal, a debugger view and a test explorer. You get an IDE that is easy
to **discover** and an editor with real **power**, in one window — over
SSH, in any terminal.

## The three layers

Almost every feature lives in one of three layers.

```text
┌───────────────────────────────────────────────┐
│ 3. The ecosystem — plugins, servers, tools    │
│  ┌─────────────────────────────────────────┐  │
│  │ 2. The chrome — Go / Bubble Tea UI       │  │
│  │  ┌───────────────────────────────────┐  │  │
│  │  │ 1. The core — embedded Neovim      │  │  │
│  │  └───────────────────────────────────┘  │  │
│  └─────────────────────────────────────────┘  │
└───────────────────────────────────────────────┘
```

### 1. The Neovim core (the brain)

Editing, motions, syntax, folds, marks, LSP, snippets and the debugger all
**are** Neovim. termocode drives Neovim over RPC and draws its screen grid.

This is why bookmarks are Neovim global marks, why multi-cursor is a real
plugin, and why `Esc` drops you into Normal mode. Your own `init.lua` loads
too.

### 2. The Go / TUI chrome (the face)

Everything around the text — tabs, the activity bar, the git panel,
pickers, dialogs, toasts, the welcome screen, mouse support — is drawn by
termocode on top of the editor grid, with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

The chrome **mirrors** core state: gutter signs come from `git diff`,
breadcrumbs from LSP symbols, the dirty dot from the buffer. The chrome also
**commands** the core: a click stages a hunk, a palette entry runs a motion.

Neovim talks back through an event channel. For example, when diagnostics
change, Neovim notifies termocode, and the Problems panel updates.

### 3. The ecosystem (the reflexes)

Some things termocode sets up for you, with no plugin manager:

- **Auto-cloned plugins** on first start: `nvim-dap` (debugging),
  `vim-visual-multi` (multi-cursor), `rainbow-delimiters` (bracket colors).
- **Language servers and debug adapters**: found on your `PATH`, or installed
  into termocode's own tools folder with **LSP: Manage Language Servers...**.
- **Extensions**: small Lua programs that run inside the same Neovim. See
  [Write an extension](../extensions.md).
- **AI providers**: optional, over plain HTTP. No SDKs.

If a tool is missing, its feature turns off quietly. The rest keeps working.

## Design rules you will notice

- **Three ways to do everything.** Most actions have a key, a palette entry
  and a mouse path. Beginners use the palette. Experts learn the keys.
- **Degrade, never block.** No language server? You get a short message. No
  `rg`? A built-in search runs. No network? The editor still starts. A broken
  config file is ignored, not fatal.
- **The UI does not lie.** Status bar, gutter signs and git marks are read
  from real state and refreshed often (git every 2 seconds).
- **Disk is sacred.** Destructive actions ask first. Replace in Workspace
  shows how many files will change. AI edits open as a diff before they are
  applied.
- **It remembers you.** Tabs, cursors, folders, undo history, theme and
  breakpoints survive a restart.
- **One visual language.** Every color comes from theme tokens. A custom
  theme recolors the whole app, editor included, with no restart.

## Learn more

- [Architecture](../architecture.md) — the code layout, for contributors.
- [Decisions (ADRs)](../adr/README.md) — why things are built this way.
- [Find in files, in depth](find-in-files.md) — one feature, end to end.
