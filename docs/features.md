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
**Source Control**, **Run** (Run and Debug), **Testing**, **Services**, **Databases** and **Workflows** (once the folder has `.termocode/workflows`), with **Settings**
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

- Ghost-text completion, next edit suggestions, a chat panel and an agent with tools. → [Use AI](guides/ai-use.md)
- Explain / Fix / Edit / Doc actions that open as a diff before they apply.
- Agent steps that run at the same time work in their own git worktree; review, merge or discard each one. → [Own worktrees](guides/ai-profiles.md#own-worktrees-for-parallel-steps)
- Anthropic, OpenAI, OpenRouter, Ollama, LM Studio or any OpenAI-compatible server. → [Set up AI](guides/ai-setup.md)
- Outside agents (Claude Code, Codex, …) in terminal tabs: usage-limit readout, opt-in auto-continue, named accounts in their own folders, and hibernation of idle agents. → [Agents](guides/agents.md#usage-limits-and-auto-continue)
- Race one task across 2 to 4 outside agents, each in its own worktree; compare their diffs, verify them and merge the winner. → [Agents](guides/agents.md#race-agents-on-one-task)

![AI chat with a Markdown answer and a code card](assets/screenshots/ai-chat-answer.png)

![Review AI changes: accept or reject each change](assets/screenshots/diff-review.png)

## Workflows

- A small `.jsonc` file reads items (issues, files, an MCP tool, a URL, a database), keeps the ones that matter, lets the AI or an outside agent work on each one and writes the result back. Build one by chat, try it with a dry run, then run it by hand or on a schedule. → [Workflows](guides/workflows.md)
- Every write asks first on its card (Deny first); **Allow for this workflow** holds only for that version of the file. → [Trust and writes](guides/workflows.md#trust-and-writes)
- Fan out: each item in its own worktree with an agent step, tests and a pull request. → [Fan out](guides/workflows.md#fan-out-worktrees-agents-tests-pull-requests)
- Schedules keep going after a restart; `termocode service install` starts the session at login. Results come as a desktop note, on the phone, to a webhook or to Slack. → [Schedules](guides/workflows.md#schedules) · [Notifications](guides/workflows.md#notifications-and-history)
- Undo a write from the run page, day / run / item AI budgets, an audit file per run and an admin policy file. → [Undo and budgets](guides/workflows.md#undo-budgets-and-the-audit-file)
- Follow a run and answer its cards from the phone. → [On your phone](guides/workflows.md#on-your-phone)
- Let one machine serve agent tasks: `termocode runner serve` takes queued tasks (`termocode runner add`, also over SSH) and runs a few at once, each in its own worktree. → [Serve tasks from this machine](guides/runner.md)

## Run, test and debug

- Tasks from `tasks.json`, `package.json`, `Makefile`, Cargo, pytest and more, each in a terminal tab. → [Tasks and problems](guides/tasks-problems.md)
- A test explorer for Go, pytest, Rust, Jest and Vitest. → [Run and explore tests](guides/tests.md)
- A Containers view: your project's services on top, then every container, image, registry, network, volume and context on the machine. Live CPU and memory graphs for the selected container; logs, shell, attach and stats open as terminal tabs; remove and prune always ask with the exact command; the AI reads the host and asks before it starts, stops or pulls. `/` filters every section; `b` lists an image's public remote tags and `s` searches Docker Hub. Works with Docker and Podman (Podman connections show as contexts). → [Containers](guides/containers.md)
- Project services for Docker Compose and local dev commands: start, stop, logs in a terminal tab, and AI tools that read their state and logs (secrets masked). → [Dev services](guides/services.md)
- A Databases view for PostgreSQL, MySQL / MariaDB, SQLite and Redis / Valkey: schema tree, SQL console, result pages, row edits with a reviewed transaction, export and import, EXPLAIN, SSH tunnels, and AI tools that read in a read-only session and ask before a write. → [Browse databases](guides/databases.md)
- A Run and Debug view with breakpoints, watch, call stack and a Debug Console. → [Debug a program](guides/debug.md)
- An integrated terminal with tabs (`Ctrl+T`) and splits: up to four shells side by side or stacked in one tab (`Alt+/`, `Alt+-`). Click `file:line` links in its output. → [The terminal and its splits](guides/terminal.md)

![Integrated terminal under the editor](assets/screenshots/terminal.png)

## Git

- Stage files or single hunks, commit, amend, sync, stash, blame, history. → [Git guide](guides/git.md)
- Side-by-side diffs, a commit graph, and gutter change bars.
- A merge-conflict resolver and a Clone repository flow.
- A WORKTREES view for every worktree of the repository: make one with its agent and first task, pin, link an issue or PR, and see each agent's state and notes. → [Worktrees](guides/worktrees.md)
- Sleep a worktree (its agent tabs end, its files stay), archive it (the branch stays; restore it later), delete it in safe steps, or clean up merged and old ones at once. → [Worktrees](guides/worktrees.md#clean-up)

![Source Control: commit box, change tree and commit graph](assets/screenshots/scm.png)

## Make it yours

- Nine themes (dark and light) plus a custom theme editor. → [Customize](guides/customize.md)
- A settings UI, key rebinding, your own snippets and commands.
- First run: a **Set up termocode** card on the welcome screen sets icons (Nerd Font), AI and theme in a few clicks. → [First 5 minutes](getting-started/first-steps.md#first-run-the-setup-card)
- Export / import your settings (secrets are left out).
- Import settings, keys, theme and (from VS Code) snippets from another editor you use, with a review, a backup and **Undo Last Import**. → [Import settings from another editor](guides/import-from-another-editor.md)
- Lua extensions: commands, sidebar panels, status items. → [Write an extension](extensions.md)
- Updates from inside: a quiet ⬆ chip when a new release is out, then a checked download (checksums.txt), a confirm card and Restart. Package-manager installs get their command instead. → [Update termocode](getting-started/install.md#update-termocode)

![Catppuccin Latte light theme](assets/screenshots/theme-catppuccin-latte.png)

## Workspaces

- Several folders in one window, or switch projects in one step. → [Workspaces and sessions](guides/workspaces.md)
- Tabs, cursors, folds, undo history and breakpoints come back after a restart.
- Save the window as a named layout (panels, sizes, splits, terminals) and switch with `Alt+Y`: built-in `debug`, `review` and `focus`, your own, or one the project shares. → [Layouts](guides/workspaces.md#layouts)
- Over SSH, a dropped connection loses nothing: run `termocode` again to get the same editor, terminals and jobs back. → [Remote and SSH](guides/remote-ssh.md)
- `termocode ssh host` reconnects by itself after a network change, a Wi-Fi switch or sleep: the screen stays, typing pauses, and a toast says when it is back. → [Remote and SSH](guides/remote-ssh.md#reconnect-by-itself-termocode-ssh)
- The web panel (experimental): the same session in a browser tab, for chat and cards, files, terminals and git, on this computer or from a server with `termocode ssh --web`. → [The web panel](guides/web.md)
- Unsaved files survive a crash or a kill: the next start offers to restore them.
- Download a file or folder to your own computer, or upload into a folder. Locally it is a plain copy (Downloads folder; the desktop file chooser for uploads); over SSH it goes through the terminal (kitty, iTerm2, trzsz; else a ready scp command). → [Remote and SSH](guides/remote-ssh.md#download-and-upload-files)

## Help

- Hover over a button's **ⓘ More info** line or press **Shift+F1** to open its guide section. **Help: Open Guide…** browses the built-in guides; **Help: Explain This…** opens help for the focused item. → [Help inside termocode](getting-started/help-doctor.md#help-inside-termocode)
- **Help: Run Doctor** checks your tools. → [Check your setup](getting-started/help-doctor.md)
- **Help: Show Shortcuts** shows a cheat sheet of your current keys (with your `keymap.json` changes).
- The full lists: [Keyboard shortcuts](reference/keys.md) · [Commands](reference/commands.md) · [Settings](reference/settings.md).
