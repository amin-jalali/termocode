# Changelog

All notable changes to termocode. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/).

There is no release tag yet. This is the first entry, written for the
first release, `v0.1.0`. It covers everything since the first commit
(2026-05-13). The release notes on GitHub are the short version of it.

## [Unreleased]

Nothing yet. After the `v0.1.0` tag, new changes go here.

## [0.1.0] - not released yet

First public release. Status: early. Linux, macOS and Android (Termux)
builds; no Windows build (use WSL).

### The editor and the screen

- A full IDE in the terminal: explorer, tabs, breadcrumbs, command palette
  (`F1`), go to file, recent files, multi-cursor, an integrated terminal
  with tabs and splits, project-wide search with ripgrep, tasks and a
  Problems panel. The mouse works everywhere.
- The editor is a real `nvim --embed`: your motions, registers, macros and
  `init.lua` work as they do in Neovim.
- Language servers for Go, Python, JS/TS, Rust, C/C++, Lua, Shell and
  TOML, with a built-in installer for servers and debug adapters.
- Nine themes (dark and light) and a custom theme editor, a settings UI,
  key rebinding, snippets, your own commands and Lua extensions.
- Import settings, keys, themes and snippets from another editor, with a
  review, a backup and **Undo Last Import**.
- Persian and Arabic text shows right to left in the AI chat and in the
  Markdown preview.
- First-run **Set up termocode** card (icons, AI, theme), **Help: Run
  Doctor**, in-app guides (`Shift+F1`).

### Disconnect, keep working

- Over SSH a dropped connection loses nothing: run `termocode` again in
  the same folder to get the same editor, terminals and running jobs back.
- `termocode ssh host` reconnects by itself after a network change, a
  Wi-Fi switch or sleep. The screen stays, typing pauses, a toast says when
  it is back.
- Unsaved files survive a crash or a kill; the next start offers to
  restore them. Tabs, cursors, folds, undo history and breakpoints come
  back after a restart.
- Download and upload files between the far machine and your own computer
  (kitty, iTerm2, trzsz; else a ready `scp` command).
- Copy works over SSH through the terminal (OSC 52) in terminals that take it.

### AI

- Chat panel, ghost-text completion, next-edit suggestions, and an agent
  that can open files, run commands and tests, stage hunks and set
  breakpoints inside the IDE.
- Every file edit shows as a diff you accept or reject per hunk; one
  command undoes a whole AI turn. Risky steps ask first, with the safe
  answer focused.
- Providers: Anthropic, OpenAI, OpenRouter, Ollama, LM Studio or any
  OpenAI-compatible server. Local models work offline.
- Agent steps that run at the same time work in their own git worktree.
- Outside coding agents run in terminal tabs, with launch profiles, a
  usage readout, opt-in auto-continue, and a race mode that runs one task
  in 2 to 8 lanes and lets you compare and merge the winner.
- MCP: local and remote servers and a **Connect a service** wizard
  (GitHub, GitLab, Linear, Notion, Sentry, Slack, ClickUp, Atlassian).
  termocode can also serve its tools to outside agents over MCP.
- Workflows: small `.jsonc` files that read items, let the AI or an agent
  work on each one and write results back, with dry runs, schedules,
  budgets, an audit file per run and an undo for writes.
- `termocode runner serve` takes queued agent tasks on one machine.

### Git, run, test and debug

- Stage files or single hunks, commit, amend, sync, stash, blame, history,
  side-by-side diffs, a commit graph, a merge-conflict resolver, clone.
- A WORKTREES view: make a worktree with its agent, pin, link an issue or
  PR, sleep, archive, delete in safe steps.
- Run and Debug view: breakpoints, watch, call stack and a Debug Console
  for Go, Python and Node.
- Test explorer for Go, pytest, Rust, Jest and Vitest.

### Your dev stack

- Services view for Docker Compose and local dev commands.
- Containers view for every container, image, network and volume on the
  machine, with live CPU and memory graphs; removing and pruning always
  ask with the exact command.
- Kubernetes view: pods, deployments, logs, events, Helm releases, apply
  and delete with a card first. New: **Add Cluster to Kubeconfig** writes
  the kubeconfig only after you see the exact diff and say OK, with a
  timestamped backup first.
- Databases view for PostgreSQL, MySQL / MariaDB, SQLite, Redis / Valkey
  and SQL Server / Azure SQL: schema tree, SQL console, row edits in a
  reviewed transaction, export and import, EXPLAIN, SSH tunnels. AI reads
  in a read-only session and asks before a write.
- HTTP client for `.http` files.

### The web panel (experimental)

- The same session in a browser tab, on this computer or from a server
  with `termocode ssh --web`: chat and cards, files, terminals, git. On a
  phone it has its own layout. One-time links and no open network port.

### Install and update

- One-line installer with a checksum check (`scripts/install.sh`), tarballs
  on the Releases page, `go install`.
- In-app update check with a checksummed download and a confirm card.
- Lite mode (`termocode --lite`) for a machine without Neovim: terminals,
  agents, git and worktrees only.

### Speed

- One binary. About 60 MB of RAM with its editor engine and near-zero CPU
  when idle. Numbers and method: [docs/benchmarks.md](docs/benchmarks.md).

### Known limits

- No Windows build yet.
- The web panel is experimental.
- On the first start the editor plugins (multi-cursor, debugger, bracket
  colours) download in the background; until then those features are off.
