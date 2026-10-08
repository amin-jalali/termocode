# Feature tour

A short list of what termocode can do. Each line links to the guide that shows
you how.

## The screen

```text
┌──┬──────────────────────────────────────────┬────────────┐
│ A│ tabs                                  ⋮  │            │
│ c│ breadcrumbs                              │  AI chat   │
│ t│                                          │  (Alt+A)   │
│ i│  editor — real Neovim                    │            │
│ v│                                          │            │
│ i├──────────────────────────────────────────┤            │
│ t│ TERMINAL · PROBLEMS · OUTPUT · TEST      │            │
│ y│ RESULTS · DEBUG CONSOLE  (bottom panel)  │            │
├──┴──────────────────────────────────────────┴────────────┤
│ status: branch · errors · LSP · tests · ✦ AI · Ln/Col    │
└──────────────────────────────────────────────────────────┘
```

The **activity bar** on the left switches the sidebar: **Explorer**,
**Source Control**, **Run** (Run and Debug) and **Testing**, with **Settings**
at the bottom. Extensions can add their own icons. Click the active icon again
to hide the sidebar.

## Editing

- Real Neovim: motions, registers, macros, your `init.lua`. → [Edit and move around](guides/editing.md)
- VSCode-style keys: `Ctrl+C/V/Z`, `Ctrl+/`, `Alt+↑↓`, `Shift+Alt+F`.
- Multi-cursor (`Ctrl+D`), clipboard history (`Ctrl+Shift+V`), snippets, bookmarks (`F3`).
- Format on save, auto-save, word wrap, Zen mode (`Alt+Z`), Markdown preview (`F7`).

## Find your way

- Go to file (`Ctrl+P`), recent files (`Ctrl+R`), command palette (`F1`). → [First 5 minutes](getting-started/first-steps.md)
- Go to definition, references, symbols, rename, hover, quick fix. → [Edit and move around](guides/editing.md#code-intelligence-lsp)
- Find and replace in one file or across the whole workspace, with ripgrep. → [Find and replace](guides/search.md)

## Code intelligence

- Language servers for Go, Python, JS/TS, Rust, C/C++, Lua, Shell and TOML.
- A built-in installer for servers and debug adapters. → [Install language servers](guides/language-servers.md)
- A Problems panel for every error and warning. → [Tasks and problems](guides/tasks-problems.md)

## AI assistant

- Ghost-text completion, a chat panel and an agent with tools. → [Use AI](guides/ai-use.md)
- Explain / Fix / Edit / Doc actions that open as a diff before they apply.
- Anthropic, OpenAI, OpenRouter, Ollama, LM Studio or any OpenAI-compatible server. → [Set up AI](guides/ai-setup.md)

## Run, test and debug

- Tasks from `tasks.json`, `package.json`, `Makefile`, Cargo, pytest and more, each in a terminal tab. → [Tasks and problems](guides/tasks-problems.md)
- A test explorer for Go, pytest, Rust, Jest and Vitest. → [Run and explore tests](guides/tests.md)
- A Run and Debug view with breakpoints, watch, call stack and a Debug Console. → [Debug a program](guides/debug.md)
- An integrated terminal with tabs (`Ctrl+T`). Click `file:line` links in its output.

## Git

- Stage files or single hunks, commit, amend, sync, stash, blame, history. → [Git guide](guides/git.md)
- Side-by-side diffs, a commit graph, and gutter change bars.
- A merge-conflict resolver and a Clone repository flow.

## Make it yours

- Four themes plus a custom theme editor. → [Customize](guides/customize.md)
- A settings UI, key rebinding, your own snippets and commands.
- Export / import your settings (secrets are left out).
- Lua extensions: commands, sidebar panels, status items. → [Write an extension](extensions.md)

## Workspaces

- Several folders in one window, or switch projects in one step. → [Workspaces and sessions](guides/workspaces.md)
- Tabs, cursors, folds, undo history and breakpoints come back after a restart.

## Help

- **Help: Run Doctor** checks your tools. → [Check your setup](getting-started/help-doctor.md)
- **Help: Show Shortcuts** shows a cheat sheet.
- The full lists: [Keyboard shortcuts](reference/keys.md) · [Commands](reference/commands.md) · [Settings](reference/settings.md).
