# Give your agent the IDE's tools

An agent CLI such as Claude Code or Codex runs in a terminal tab. In a plain
terminal it sees your files and a shell. termocode can give it more: the
editor state, diagnostics, hover and symbols, test results, the debugger,
the visible terminal and git reads. termocode acts as an
[MCP](mcp.md) server for the agent, and stays in charge of what is allowed.

The tools are the same ones the built-in AI chat has, with the same
gates. The full list, with a column **MCP server**, is in
[AI tools](../reference/ai-tools.md). The design is in
[ADR 0017](../adr/0017-mcp-server.md).

[Itermoc](itermoc.md) shows this in one place: its OUTSIDE AGENTS section says
whether termocode listens, who is connected, their last calls, and has
buttons to start an agent and copy the config. Click the `⇄` in the status
bar to open it.

## What happens when you start an agent

Run **Agent: Start Claude Code** (or Codex, Gemini CLI, OpenCode) from the
command palette (`F1`). termocode opens a terminal tab and types the
agent's command. What it adds depends on the agent:

| Agent | What termocode does | Your own files |
|---|---|---|
| Claude Code | Adds `--mcp-config <file>` to the command. The file is private (mode `0600`) in the session folder. It is removed when the tab is gone. | Not touched. |
| Codex | Adds two `-c mcp_servers.termocode…` options to the command. | Not touched. |
| OpenCode | Can only read MCP servers from `opencode.json` in your project. A card shows the file and the entry. **Add** writes it, **Not now** (or `Esc`) starts OpenCode without it; **Not now** has the focus, so a stray `Enter` writes nothing. | Written only after you say yes. The file holds absolute paths of this machine (the `termocode` program and your folder): check them before you commit it. |
| Gemini CLI | Shows a tip. Add termocode by hand (next section). | Not touched. |
| Aider | Shows a tip: Aider has no MCP support. | Not touched. |

If a tab for that agent is already open, termocode shows it. The flags
count only when the agent starts, so close the tab and start it again to
get them.

The OpenCode entry has the full path of the termocode program and of your
folder. If you commit `opencode.json`, other people get your paths. Add
the entry by hand to your own OpenCode config instead if that is a problem.

The flags and key names come from each agent's own documentation. If an
agent changes them, use the hand setup below.

## Add it by hand

Use this for Gemini CLI, for your own settings, or for any agent that
speaks MCP over stdio.

1. Run **MCP: Copy termocode Server Config** (`F1`). It copies a JSON block
   with the `mcpServers` entry.
2. Paste it into the agent's MCP settings.

The entry starts `termocode mcp --dir <your folder>`. The agent starts that
program; it passes the agent's messages to the termocode that runs in that
folder.

- `--dir` names the workspace. Without it, `termocode mcp` looks at the
  current folder and then up to seven parent folders for one with a
  running termocode.
- If no termocode runs there, the server still answers the tool list, but
  every call fails with a message: open termocode in that folder, then
  restart the agent.

## When termocode listens

The setting `mcp_server` in `config.json` decides:

| Value | Meaning |
|---|---|
| `auto` (default) | Listen once you start an agent tab. |
| `on` | Listen from the start of termocode. |
| `off` | Never listen. The flags and the OpenCode card are skipped too. |

The change applies when you save `config.json`. Nothing listens on the
network: only a private socket in the session folder, which only your own
user can open. termocode checks the user of every connection. See
[Settings](../reference/settings.md).

## What the agent can do

- **Read at once:** open a file in your editor, read the editor and IDE
  state, read files, list folders and search, diagnostics, hover, symbols
  and definitions, tasks and test results, debug state, the visible
  terminal (read only), git status, diff, log, blame and branches,
  services and HTTP request lists, the database connections, schemas,
  table DDL and Redis key names, and `GET` requests to your own machine
  (`localhost`).
- **Ask first (card):** run a task or tests, start and steer the debugger,
  evaluate an expression in the debugger, run some palette commands
  (`execute_action`) such as stop, clear or close things, read data from a
  database (`db_query`, `redis_get`, `redis_command`), and send any other
  HTTP request (`http_send`).

### Databases and HTTP

| Tool | Gate | What is allowed |
|---|---|---|
| `db_connections`, `db_schema`, `db_ddl`, `redis_keys` | runs at once | Lists only. |
| `db_query` | asks | One read statement (`SELECT`, `SHOW`, `EXPLAIN` without `ANALYZE` …). It runs in a read-only session. |
| `redis_get`, `redis_command` | asks | Read commands only, in a read-only session. |
| `http_send` | as in the chat | `GET`, `HEAD`, `OPTIONS` to `localhost` run at once. Other hosts and methods ask. Private hosts (`10.x`, `192.168.x`, …) always ask. |

- Writes are refused, with no card: `INSERT`, `UPDATE`, `DELETE`, DDL,
  more than one statement, `BEGIN` and other transaction statements,
  `COPY`, `LOAD`, `ATTACH`, a `PRAGMA` that writes, and Redis write or
  dangerous commands. The agent gets an error and nothing is sent. Run
  writes yourself in the Databases view.
- The card shows the connection and the SQL. Secrets are hidden.
- Passwords never leave termocode. If the password is in a locked secret
  store, the agent gets an error that says to unlock it. Unlock it with
  **AI: Unlock Secrets**, then let the agent try again. The agent is never
  asked for your passphrase.
- Results are cut and cleaned like in the chat (`db_ai_max_kb`, long cells
  cut, passwords and known secrets hidden).
- `{{$secret NAME}}` and values from `http-client.private.env.json` are
  filled in by termocode. The card says that a secret is sent, but does not
  show it. The answer hides it too.
- The database tools are in the tool list only when you have a connection
  of that kind. When you add or remove one, termocode tells the agent that
  the list changed (`notifications/tools/list_changed`).

## Protocol and resource

termocode answers the MCP revisions `2025-03-26`, `2025-06-18` and
`2025-11-25`, and the stateless revision `2026-07-28` (no `initialize`;
`server/discover` tells what is supported). Besides tools it offers one
read-only resource, `termocode://editor/state`: the active file, cursor,
open buffers, git branch and the number of diagnostics. It needs no
confirmation and is empty when termocode is not open.

## What the agent can not do

- Write, create, delete or rename files. The agent has its own editor for
  that.
- Run a shell command, or type into your terminal.
- Stage, commit, push, pull or change branches.
- Change settings, read secrets or credentials.
- Start or stop services, containers or databases.
- Write to a database, or save requests to `.http` files.
- Use the tools of your own MCP servers (`mcp__…`). They are never passed on,
  unless you mark a server with `"gateway": true` (see
  [Share your servers with outside agents](mcp.md#share-your-servers-with-outside-agents)).

Some palette commands are closed too: AI, database and container commands,
extension commands and your own commands.

## The confirm card

When the agent calls a tool that runs or changes something, a card opens:

```text
✦ claude-code wants to run task "build"
  Arguments: {"label": "build"}
  Asked by an outside agent over MCP (tool tasks_run). The name is the agent's own claim.
  [ Deny ] [ Allow ] [ Always allow ]
```

- **Deny** (or `Esc`) refuses it. It comes first and has the focus when
  the card opens, so a stray `Enter` never allows a call. The agent gets an
  error with code `-32000` with `data: {"reason":"declined"}`, and nothing
  runs. (A tool that ran and failed uses `-32000` too, with
  `{"reason":"tool_error"}`.)
- **Allow** runs this one call. Move to it with `→` (or `Tab`), or click it.
- Several calls wait in line: one card at a time, the oldest first. A call
  that waits longer than two minutes, or whose agent went away, is dropped.
- If another dialog opens over the card, the call keeps waiting and the
  card comes back when that dialog closes. The two minutes keep running.
- Secrets in the arguments are hidden.

The name on the card is what the agent says about itself. Any program that
runs as you can send any name, so treat the name as a label, not as a
login.

### Always allow

**Always allow** is for that agent name and that tool only. Another agent,
or another tool of the same agent, still asks. For `execute_action` it is
for that one command. It is not offered for `debug_evaluate`, because that
can run any code in your program.

For the database reads it is for one connection, for example
`db_query:local`. A connection with `"environment": "production"` always
asks. For `http_send` it is for one host, for example
`http_send:api.example.com` (`GET`, `HEAD`, `OPTIONS`) or
`http_send:api.example.com/write` (any method). Private hosts always ask.

It is saved as `mcp_server_auto_approve` in `config.json`, for example
`{"claude-code": ["tasks_run"]}`. It is separate from the chat's
auto-approve: neither one applies to the other. The AI chat can not change
it.

### Revoke it

Run **AI: Agent Auto-Approve…**. The section "External agents (MCP server)"
lists each entry. Press `Enter` on one to make that tool ask again. You can
also edit `config.json`.

## See what is connected

- The status bar shows a dim **⇄ ready** while termocode listens and no
  agent is connected, and **⇄ 1 agent** while one is. Click it.
- **MCP: Server Status…** shows if termocode listens (and why not), each
  agent with its calls, and the last calls in plain words, for example
  `09:41 claude-code · tasks_run · asked, you allowed`. From there you can
  copy the config, open the auto-approve list, stop listening for this
  session (your settings stay as they are), or press `Enter` on an agent to
  disconnect it. Both ask first. Press `?` on the screen for its keys.
- Every call, allowed or not, is in the AI audit log (**Output: AI**) as
  `mcp:<agent name>`.

The chat AI can not run the status and config commands. They are for you.

## If something does not work

| What you see | Try this |
|---|---|
| The agent lists the tools but calls fail with "termocode is not open" | Open termocode in the same folder, then restart the agent. |
| Status says **Not listening** and "The MCP server is turned off." | Press `Enter` on **Turn on**: the screen opens on it. It sets `mcp_server` to `on` in `config.json`. |
| A message says the agent is not installed | Install the agent so its program is on your `PATH`, then start it again. |
| The agent shows no termocode tools | Start it again from the palette, or add the config by hand. |
| A card never appears | Another dialog may be open. The card waits until it closes. |
