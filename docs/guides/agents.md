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

The setting `mcp_server` decides. Set it in **Settings → MCP** (the AI can
not change it), or in `config.json`:

| Value | Meaning |
|---|---|
| `auto` (default) | Listen once you start an agent tab. |
| `on` | Listen from the start of termocode. |
| `off` | Never listen. The flags and the OpenCode card are skipped too. |

A choice in Settings applies at once; a change in the file applies when
you save `config.json`. Nothing listens on the
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
  database (`db_query`, `redis_get`, `redis_command`), send any other
  HTTP request (`http_send`), and save or apply a stash (`git_stash`).
- **Ask on a Cancel-first card, never "Always allow":** drop or pop a stash.
  The stash list (`git_stash` list) runs at once.

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

## Drive agents from a script

Every terminal tab has three environment variables:

| Variable | Value |
|---|---|
| `TERMOCODE_ENV` | `1` inside a termocode terminal |
| `TERMOCODE_TAB_ID` | the tab's id, like `t3` |
| `TERMOCODE_SOCKET` | termocode's control socket for this workspace |

The `termocode agent` command talks to that socket:

```sh
termocode agent list                      # ID  NAME  AGENT  STATE
termocode agent prompt t3 "fix the failing test"
termocode agent wait t3 --until done --timeout 20m
```

- `list` shows each tab with its agent and state (`working`, `waiting`,
  `done`). `--json` prints JSON.
- `prompt` types the text into the tab and presses Enter; `-` reads the
  text from stdin. The first time, termocode asks **Let a script type
  into a terminal tab?** with the text. **Deny** is first; **Allow for
  this session** lets later prompts in until termocode quits.
- `wait` returns when the agent reaches the state (default `done`) and
  exits 1 when the time is up (default 10 minutes).
- Outside a termocode terminal the command uses the socket of the current
  folder's workspace. The socket is in termocode's private session folder;
  only your own user can connect. Not on Windows.

### Exact states with hooks

By default termocode guesses an agent's state from its screen. With
**Agent: State Hooks…** the agent tells termocode itself:

- **Claude Code**: four hooks in `~/.claude/settings.json`
  (`UserPromptSubmit`, `PreToolUse` → working, `Notification` → waiting,
  `Stop` → done).
- **Codex**: one `notify` line at the top of `~/.codex/config.toml`
  (done at the end of each turn). If the file has its own `notify`,
  termocode does not touch it.

The card shows the file and every line that changes; **Not now** is
first. The old file is kept as `<file>.termocode-backup`. The hook runs
`termocode agent report <state>`, which does nothing outside a termocode
terminal. Pick the agent again to **Remove** the hooks. `termocode agent
list` shows `hook` or `screen` as the source of each state (`--json`).

## Your privacy settings first

Some agent features read or touch the agents' own files. Each one is a
setting in `config.json`, **off** by default. The AI chat can not turn
them on.

| Setting | What it allows | Default |
|---|---|---|
| `agent_transcripts` | termocode reads the session files Claude Code and Codex write for themselves (chat view, sessions, search, plan numbers). Read only; secrets are hidden first. | `off` |
| `agent_auto_continue` | termocode types `continue` into an agent tab once its usage limit resets. | `false` |
| `agent_hibernate_minutes` | termocode stops an idle agent and starts it again when you come back. | `0` (off) |
| `agent_accounts` | Named accounts of Claude Code and Codex, each in its own folder. Labels only. | `{}` |

termocode never writes into `~/.claude` or `~/.codex` for these features,
and it never calls a vendor's usage service.

## Start an agent

**Agent: Start…** lists every agent: **INSTALLED** (with the sign-in state
of Claude Code and Codex), **BUILT-IN** (termocode's own AI profiles) and
**NOT INSTALLED**. `Enter` on a missing agent installs it with npm or pip
into termocode's tools folder, after a card (**Not now** first). Then a
second card runs the CLI's own sign-in command in a terminal tab.

From the **WORKTREES** view, **Agent: Start…** starts the agent in the
selected worktree, and the agent shows on that worktree's card.

## Race agents on one task

**Agent: Race…** gives one task to 2 to 4 agents at once. Tick the lanes,
type the task and, if you like, a verify command such as `go test ./...`.
Each lane gets its own worktree from `HEAD` (hidden in the git folder) and
its own terminal tab. termocode types the task once, only when the agent's
input box is on screen, never into a question.

**Agent: Race Compare** shows one row per lane: state, files and `+/-`.

| Key | What it does |
|---|---|
| `Enter` | Review the lane's diff (read only) |
| `m` | Merge this lane into your folder; a conflict stops first and asks (**Cancel** first) |
| `d` | Discard one lane (**Cancel** first) |
| `v` | Run the verify command in every finished lane |
| `s` | Ask your **Quick** model for a 3-line summary of the differences (capped, secrets hidden; the cost is shown) |
| `x` | Stop every lane |

After a merge, a card asks about the other lanes: **Keep** is first. A
lane whose agent still works is never merged or discarded. The AI chat can
start a race with `agent_race_start`, always behind a card (**Deny**
first).

## Chat view and sessions

With `agent_transcripts` on:

- **Agent: Chat View** (or `v` on an agent tab) shows the session as a
  chat. Tool calls are one line each. What you type goes to the tab. Press
  `v` again for the terminal.
- **Agent: Sessions…** lists this project's past sessions. `Enter` resumes
  one in a new tab with the CLI's own resume command; `→` reads it.
- `/` in the list, or **Agent: Search Sessions…**, searches the text of
  this project's sessions, on this computer only.

While the setting is off, termocode asks first and opens no file there.

## Usage limits and auto-continue

When Claude Code or Codex shows its usage-limit message, the tab is named
`limit ↻14:30` until the reset, and a toast says when it resets.

With `agent_auto_continue` set to `true`, or **Agent: Toggle
Auto-Continue** on the tab, termocode types `continue` (then `Enter`) one
minute after the reset, **once** per limit, and a toast says so. It never
types while the agent works, while a y/n or permission question is on the
screen, or into a closed tab.

## Auto-accept prompts

**Agent: Toggle Auto-Accept** lets an agent tab run without you. It is
off by default, works per tab, and has no setting.

1. Open the agent's tab and run the command.
2. A card explains what it does. **Cancel** is first; pick **Turn on**.
3. The tab is named `Codex · auto-yes`. termocode now answers each
   permission prompt with "yes, this once" and a toast counts the answers.

It never:

- picks an "always" or "don't ask again" choice;
- answers a question with its own choices;
- answers a prompt that shows a risky command (`rm -rf`, `sudo`, a force
  push, `reset --hard`, a download piped into a shell, SSH keys, `.env`,
  `kubectl delete`, …). That prompt waits for you, and a toast says why.

It answers each prompt once and turns itself off after 50 answers. Run the
command again to turn it off. It works for Claude Code, Codex, Gemini CLI,
OpenCode and Aider. The AI cannot turn it on. Use it on a worktree you can
throw away: the agent still edits files and runs commands you did not read.

## Usage and plan limits

The status bar shows a pill like `✦ Claude 62% ↻14:30`: the part of the
plan window used and the next reset. `✦ Claude limit ↻14:30` means a
limit is on. On a narrow bar it shrinks to `✦62%`, then goes. When
termocode knows nothing, there is no pill.

The numbers come only from:

1. the limit message on an agent tab's screen, and
2. the agents' session files, when `agent_transcripts` is on.

termocode never asks the vendor (no usage endpoint, no use of the CLI's
sign-in). Click the pill, or run **Agent: Usage Plans**, to open **AI:
Usage**. Its **PLANS** block has one row per account: used share, reset,
last limit, today's tokens and auto-continue. The AI's `usage_summary`
tool gets the same numbers, never the message text.

## Accounts

**Agent: Accounts…** gives Claude Code and Codex named accounts, like
`work` and `personal`:

- Each account has its own folder in termocode's config folder
  (`agents/<agent>/<label>`). Only your user can open it.
- A tab started as that account runs the CLI with `CLAUDE_CONFIG_DIR` or
  `CODEX_HOME` set to that folder, so the CLI keeps that sign-in there.
- **Add a … account…** asks for a label, then a card (**Not now** first)
  runs the CLI's own sign-in with that folder.
- **Agent: Start…** shows `as work` rows under the agent.

termocode keeps only the labels in `config.json`. It never reads the
sign-in files and never copies or swaps them. Your default
`~/.claude` and `~/.codex` stay as they are. Agents without such a
variable (Gemini CLI, OpenCode, …) have no accounts. The account folders
are hidden in bug reports.

## Hibernate idle agents

Set `agent_hibernate_minutes` (for example `30`). When a Claude Code or
Codex tab is done or waiting with no output for that long, a card asks
**Hibernate Claude Code?** (**Keep running** first; **Always for this
session** skips the card later). Then termocode:

- keeps the session id (from the session file when `agent_transcripts` is
  on),
- stops the agent's program (the shell and the tab stay), and
- marks the tab `☾ Claude Code`.

Go back to the tab and termocode types the CLI's resume command:
`claude --resume <id>` or `codex resume <id>`. Without a known id it uses
`claude --continue` or `codex resume --last` and says so. A script prompt
to a hibernated tab resumes it first and asks to send the prompt again.
termocode never hibernates a tab with a question on the screen, or the tab
you are looking at. **Agent: Hibernate or Wake** does it at once. Linux and
macOS only.

## Suggested worktree names

**Worktree: Suggest a Name…** asks for the task or issue title. Only that
text (cut to 300 characters, secrets hidden) goes to your **Quick** model
slot. You get up to 3 names like `fix-toast-queue`, with the cost. `Enter`
copies one. Without a model nothing is sent, and the list says to set a
quick model.

## Agent packs

**Agent: Agent Packs…** installs a pack of ready agents, skills, rules
and commands for Claude Code or Codex.

1. Pick **Install a pack…** and give a folder, or a git address with a
   version: `https://example.com/pack.git@v2.1` or `…@<commit>`. A git
   address without a version is refused, so the install is pinned.
2. Pick the agent. Claude Code gets `~/.claude/agents`, `skills`,
   `rules` and `commands`. Codex gets `~/.codex/skills` and
   `~/.codex/prompts`; it has no agent or rule files, so those are skipped.
3. Pick **Everything** or one part. Each row shows its files and the
   tokens every chat loads (rules in full, the name and description of
   agents, skills and commands).
4. The card lists every file it adds (`+`) or replaces (`~`), and the
   token cost. **Cancel** is first. Nothing is written before **Install**.

The old files are copied to termocode's config folder first. The
installed pack shows in **Agent Packs…** with its version; Enter on it
opens the **Undo** card, which puts your old files back and removes the
added ones. A file you changed after the install is kept. To install
another version of the same pack, undo the first one.

## If something does not work

| What you see | Try this |
|---|---|
| The agent lists the tools but calls fail with "termocode is not open" | Open termocode in the same folder, then restart the agent. |
| Status says **Not listening** and "The MCP server is turned off." | Press `Enter` on **Turn on**: the screen opens on it. It sets `mcp_server` to `on` in `config.json`. |
| A message says the agent is not installed | Install the agent so its program is on your `PATH`, then start it again. |
| The agent shows no termocode tools | Start it again from the palette, or add the config by hand. |
| A card never appears | Another dialog may be open. The card waits until it closes. |
| No plan pill in the status bar | termocode has seen no limit message yet. Turn on `agent_transcripts` to read the numbers from the session files. |
| Auto-accept did not answer | The prompt showed a risky command, or it was a question with its own choices. Answer it yourself. |
| Auto-continue did not type | A question was on the screen, or the agent was working again. The toast says why. |
| A resumed agent starts a new session | No session id was known. Turn on `agent_transcripts` so termocode can keep it. |
