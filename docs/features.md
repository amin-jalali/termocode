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
**Source Control**, **Run** (Run and Debug), **Testing**, **Services** and **Databases**, with **Settings**
at the bottom. Extensions can add their own icons. Click the active icon again
to hide the sidebar. Rest the mouse on an icon button for half a second to see
its name and key.

![Editor with the explorer, git status, tabs and breadcrumbs](assets/screenshots/editor.png)

## Editing

- Real Neovim: motions, registers, macros, your `init.lua`. → [Edit and move around](guides/editing.md)
- Familiar IDE keys: `Ctrl+C/V/Z`, `Ctrl+/`, `Alt+↑↓`, `Shift+Alt+F`.
- Multi-cursor (`Ctrl+D`), clipboard history (`Ctrl+Shift+V`), snippets, bookmarks (`F3`).
- Format on save, auto-save, word wrap, Zen mode (`Alt+Z`), Markdown preview (`F7`).

![Markdown preview](assets/screenshots/md-preview.png)

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

![AI chat with a Markdown answer and a code card](assets/screenshots/ai-chat-answer.png)

![Review AI changes: accept or reject each change](assets/screenshots/diff-review.png)

## Run, test and debug

- Tasks from `tasks.json`, `package.json`, `Makefile`, Cargo, pytest and more, each in a terminal tab. → [Tasks and problems](guides/tasks-problems.md)
- A test explorer for Go, pytest, Rust, Jest and Vitest. → [Run and explore tests](guides/tests.md)
- A Containers view: your project's services on top, then every container, image, registry, network, volume and context on the machine. Logs, shell, attach and stats open as terminal tabs; remove and prune always ask with the exact command; the AI reads the host and asks before it starts, stops or pulls. `/` filters every section; `b` lists an image's public remote tags and `s` searches Docker Hub. Works with Docker and Podman (Podman connections show as contexts). → [Containers](guides/containers.md)
- Project services for Docker Compose and local dev commands: start, stop, logs in a terminal tab, and AI tools that read their state and logs (secrets masked). → [Dev services](guides/services.md)
- A Databases view for PostgreSQL, MySQL / MariaDB, SQLite and Redis / Valkey: schema tree, SQL console, result pages, row edits with a reviewed transaction, export and import, EXPLAIN, SSH tunnels, and AI tools that read in a read-only session and ask before a write. → [Browse databases](guides/databases.md)
- A Run and Debug view with breakpoints, watch, call stack and a Debug Console. → [Debug a program](guides/debug.md)
- An integrated terminal with tabs (`Ctrl+T`). Click `file:line` links in its output.

![Integrated terminal under the editor](assets/screenshots/terminal.png)

## Git

- Stage files or single hunks, commit, amend, sync, stash, blame, history. → [Git guide](guides/git.md)
- Side-by-side diffs, a commit graph, and gutter change bars.
- A merge-conflict resolver and a Clone repository flow.

![Source Control: commit box, change tree and commit graph](assets/screenshots/scm.png)

## Make it yours

- Nine themes (dark and light) plus a custom theme editor. → [Customize](guides/customize.md)
- A settings UI, key rebinding, your own snippets and commands.
- First run: a **Set up termocode** card on the welcome screen sets icons (Nerd Font), AI and theme in a few clicks. → [First 5 minutes](getting-started/first-steps.md#first-run-the-setup-card)
- Export / import your settings (secrets are left out).
- Import settings, keys and theme from another editor you use, with a review, a backup and **Undo Last Import**. → [Import settings from another editor](guides/import-from-another-editor.md)
- Lua extensions: commands, sidebar panels, status items. → [Write an extension](extensions.md)

![Catppuccin Latte light theme](assets/screenshots/theme-catppuccin-latte.png)

## Workspaces

- Several folders in one window, or switch projects in one step. → [Workspaces and sessions](guides/workspaces.md)
- Tabs, cursors, folds, undo history and breakpoints come back after a restart.
- Over SSH, a dropped connection loses nothing: run `termocode` again to get the same editor, terminals and jobs back. → [Remote and SSH](guides/remote-ssh.md)
- Unsaved files survive a crash or a kill: the next start offers to restore them.
- Download a file or folder to your own computer, or upload into a folder. Locally it is a plain copy (Downloads folder; the desktop file chooser for uploads); over SSH it goes through the terminal (kitty, iTerm2, trzsz; else a ready scp command). → [Remote and SSH](guides/remote-ssh.md#download-and-upload-files)

## Help

- Hover over a button's **ⓘ More info** line or press **Shift+F1** to open its guide section. **Help: Open Guide…** browses the built-in guides; **Help: Explain This…** opens help for the focused item. → [Help inside termocode](getting-started/help-doctor.md#help-inside-termocode)
- **Help: Run Doctor** checks your tools. → [Check your setup](getting-started/help-doctor.md)
- **Help: Show Shortcuts** shows a cheat sheet of your current keys (with your `keymap.json` changes).
- The full lists: [Keyboard shortcuts](reference/keys.md) · [Commands](reference/commands.md) · [Settings](reference/settings.md).
