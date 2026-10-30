# Architecture Decision Records

Short, immutable records of the significant architectural decisions in termocode.
Format: Status · Context · Decision · Consequences. Start from
[`0000-template.md`](0000-template.md).

**Supersede, don't edit.** When a decision changes, write a new ADR, set the old
one's status to `Superseded by NNNN`, and link both ways. Typo fixes are fine.

Write an ADR when a choice is hard to undo, surprises a new contributor, or was
picked over a clear alternative. A normal feature does not need one.

| # | Title | Status |
|---|---|---|
| [0001](0001-embedded-neovim-editor-engine.md) | Embedded `nvim --embed` as the editor engine | Accepted |
| [0002](0002-lua-chunks-in-go-raw-strings.md) | Neovim-side logic as Lua chunks in Go raw strings | Accepted |
| [0003](0003-auto-bootstrapped-nvim-plugins.md) | Auto-bootstrap a few nvim plugins by `git clone` on first launch | Accepted |
| [0004](0004-pure-go-search-fallback.md) | Pure-Go search fallback when ripgrep is missing | Accepted |
| [0005](0005-config-never-breaks-startup.md) | Config files can never break startup | Accepted |
| [0006](0006-lua-extension-host.md) | Extensions run as Lua inside the embedded Neovim | Accepted |
| [0007](0007-persistent-sessions.md) | Persistent sessions: the UI runs behind a socket, on over SSH | Accepted |
| [0008](0008-go-rendered-diff-viewer.md) | A Go-rendered diff card for reviewing changes | Accepted |
| [0009](0009-process-extensions.md) | Process extensions (Tier 2) over JSON-RPC on stdio | Accepted |
| [0010](0010-patched-bubbletea-renderer.md) | A patched copy of Bubble Tea's renderer | Accepted |
| [0011](0011-android-spawn-trampoline.md) | Start child processes through Android's shell | Accepted |
| [0012](0012-http-client-engine.md) | HTTP client engine in Go; `.http` grammar shared with mobocode | Accepted |
| [0013](0013-secret-store.md) | Secret store: OS keyring, else an age-encrypted file; plain only by opt-in | Accepted |
| [0014](0014-database-access.md) | Database drivers and the user's SQLite command | Accepted |
| [0015](0015-agent-pipelines-dag-in-ui-process.md) | Agent pipelines run as a DAG inside the UI process | Accepted |
| [0016](0016-container-engine-cli.md) | Container engines are driven through their CLI, not the Engine API | Accepted |
| [0017](0017-mcp-server.md) | termocode is an MCP server for outside agents | Accepted |
| [0018](0018-workspace-host-abstraction.md) | A workspace is reached through a Host; termocode stays local | Accepted |
| [0019](0019-ai-providers.md) | Connected AIs are providers; a step picks a provider and a model | Accepted |
| [0020](0020-web-ui.md) | Web UI: a second UI over one structured WebSocket | Accepted |

> The folder was started after the project was already running, so 0001–0005
> backfill decisions that were made earlier. Dates are the best guess from git
> history and code comments.
