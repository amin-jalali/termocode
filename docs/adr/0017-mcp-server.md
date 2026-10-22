# ADR 0017 — termocode is an MCP server for outside agents

Status: **Accepted** (2026-10-22)

## Context
Agent CLIs such as Claude Code, Codex, Gemini CLI and OpenCode run in a
terminal tab of termocode. In a plain terminal they see files and a shell,
and nothing else. termocode knows more: the editor state, language server
diagnostics, hover and symbols, test results, the debugger, the visible
terminal and git. These agents all speak MCP (Model Context Protocol) over
stdio, so MCP is the common door.

Constraints:
- The user stays in charge. An outside agent must not get more power than
  the built-in chat has, and not silently.
- The session already has a private socket (ADR 0007). The IDE state lives
  in the UI process, on one goroutine (Bubble Tea).
- No TCP port, ever. Nothing should listen unless the user wants it.
- CI runs on Linux and macOS; short socket paths are needed.
- Each agent CLI has its own way to add an MCP server. Editing the user's
  agent config files is not our job.

Alternatives on the table:
- **A separate daemon** that owns the tools: it would need its own copy of
  the editor and the policy, and could not show a card.
- **A TCP or HTTP MCP server**: reachable by other users and by web pages,
  and it needs tokens.
- **A unix socket in the UI process, with a thin stdio relay** (chosen).

## Decision
- **The listener lives in the UI process**, on its own unix socket in the
  session folder (mode `0600`, folder `0700`). Every call becomes a normal
  IDE tool call on the UI goroutine: the same `ai.Tool`s, policy and audit
  log as the built-in chat.
- **Same-user peer check on every connection**: `SO_PEERCRED` on Linux,
  `LOCAL_PEERCRED` on macOS. A peer with another uid is closed before it
  can say a word. There is no other login.
- **`termocode mcp --dir <folder>` is the stdio server** the agent starts.
  It relays to the socket. When no UI listens it runs in *static mode*:
  `initialize` and `tools/list` still answer, every `tools/call` fails with
  a clear `-32001` message. The agent CLI then sees a healthy server and the
  user sees why nothing happens.
- **One allow-list table** decides which tools are offered. It is the only
  source for `tools/list`, for `tools/call` and for the docs. The gate of
  each tool is `never`, `confirm` or `auto`:
  reads run at once; tools that run or change something ask on a card;
  everything else is not offered. Left out on purpose: file writes,
  `run_command`, typing into a terminal, git writes, settings, credentials,
  databases, containers, service starts and HTTP sends. The agent has its
  own shell and editor; termocode adds the IDE, not a second agent.
  `execute_action` is filtered too: only commands that read, or that stop
  and clear things in the IDE.
- **Cards are first in, first out**, one at a time, over all connections.
  A call whose agent went away, or that waited two minutes, leaves the
  queue and gets an error; nothing runs. A declined call gets `-32000`.
- **The client name is a claim.** The card shows the name the agent sent in
  `initialize`, cleaned of control characters. Any process of the same user
  can send any name, so the name is never an identity check.
- **Always allow is per agent name and per tool**, saved as
  `mcp_server_auto_approve` in `config.json`. It is separate from the
  chat's `ai_auto_approve`; neither applies to the other. The AI cannot
  change it. Tools with no class and `debug_evaluate` never offer it.
  `execute_action` is remembered per command id. The user lists and revokes
  entries in **AI: Agent Auto-Approve…**.
- **No re-export (MC-11).** Tools of the user's own MCP servers
  (`mcp__*`) are never offered to an outside agent, whatever the table says.
  Otherwise termocode would become a bridge that hands one server's
  credentials to another process.
- **The `mcp_server` setting** is `auto` (listen once an agent tab starts),
  `on` (from start) or `off` (never). It is applied when `config.json` is
  saved.
- **Launch recipes are data** in `internal/agents`. For Claude Code and
  Codex, termocode adds flags to that one launch, so nothing on disk of the
  user's changes (Claude Code gets a private temp file in the session
  folder, removed when the tab is gone). OpenCode can only read a project
  file: termocode shows what it would write to `opencode.json` and writes it
  only after a yes. Gemini CLI and Aider get a one-line tip. Nothing else is
  touched.
- **The user sees it.** A status bar chip shows connected agents (updated on
  events only, no timer), **MCP: Server Status** shows the socket, agents
  and recent calls, and every call goes to the audit log as `mcp:<name>`.

## Consequences
- **+** Agents get the IDE's eyes (diagnostics, tests, debugger) without a
  new port, a new login or a second copy of the policy.
- **+** The server speaks the handshake revisions (`2025-03-26` to
  `2025-11-25`) and the stateless revision `2026-07-28`: no `initialize`,
  the revision and the client name sit in each request's `_meta`, and
  `server/discover` lists what is supported. One read-only resource,
  `termocode://editor/state` (file, cursor, buffers, branch, diagnostics
  counts), needs no card. Static mode lists no resources.
- **+** One table drives the list, the gate and the generated docs, so they
  cannot drift apart.
- **+** A same-user check plus a private folder is the same trust line as
  the session socket: a process that can reach it can already act as you.
- **−** A process running as the same user can connect and pretend to be
  any agent. Reads then run without a card. This is accepted: it can read
  those files itself. Tools that run something always ask unless the user
  chose Always allow for that name.
- **−** Only Linux and macOS have the peer check. Other unix systems rely on
  the private folder and socket mode. Windows has no MCP server.
- **−** The recipe flags (`--mcp-config`, `-c mcp_servers...`, the
  `opencode.json` keys) belong to other projects and can change. They are
  data in one file, and a manual checklist row per agent CLI keeps them
  honest.
- **−** The allow-list is short on purpose. Adding a tool is a code change
  with a golden test, not a setting.
