# Connect MCP servers

MCP (Model Context Protocol) servers give the AI chat more tools: your
GitHub pull requests, Linear and Jira issues, Notion pages, Sentry
errors, Slack messages, a database, your notes folder and many more.

The easy way is **Connect a service**: pick the service, press one
button, allow termocode in the browser (or type a short code), done.
You never have to look for a token. termocode also reads the same
`mcpServers` block as Claude Desktop and Claude Code, so you can
paste the config you already have.

[Itermoc](itermoc.md) lists your services with their state (ready, off,
needs a sign-in) in one sidebar view; its rows open the actions below.

## Connect a service

Press `F1` and run **AI: Connect a Service…** (or click `Connect…` in the
AI chat header, or **Apps → Connect…** on the start page's setup card).
Pick a service:

| Service | What the AI gets | How you sign in |
|---|---|---|
| GitHub | issues, pull requests, code | your `gh` login, or a code on github.com |
| GitLab | issues, merge requests, CI | in the browser |
| Linear | issues, projects | in the browser |
| Notion | pages, databases | in the browser |
| Sentry | errors, issues | in the browser |
| ClickUp | tasks, docs, chat | in the browser |
| Atlassian | Jira, Confluence | in the browser |
| Slack | search, read channels | create your Slack app once (2 minutes), then in the browser |
| Custom URL… | any remote MCP server | in the browser, or a token |

The right column of the list tells you the next step on *this* machine:
`gh is signed in`, `sign in in the browser`, `paste the address (SSH)`,
or `connected as amin-jalali` for a service you already connected.

Then termocode shows one screen at a time:

1. **Sign in.** Your browser opens. Allow termocode there; the screen
   closes by itself (a countdown shows how long it waits). If the
   browser did not open, press `c` to copy the link or `o` to try again.
2. **Test call.** termocode connects, lists the tools and asks the
   service who you are.
3. **Done.** `✓ GitHub — connected as amin-jalali · 28 tools · read only`
   and a first thing to ask. **Ask the AI** puts it into the chat input;
   **Show tools** lists what the service gives the AI; **Allow writes**
   (read-only services) turns writes on.

Every service starts **read only** where the service has a read-only
mode (GitHub, Linear) or read scopes (Linear, ClickUp). Turn writes on
later with **Allow writes** on the Done screen or **AI: MCP Servers…** →
the service → **Allow writes**; every
write still asks you first.

### GitHub

- **gh is signed in** (`gh auth status` works): pick the account (gh may
  have several) and the access, then **Use gh**. termocode asks
  `gh auth token` each time it connects and keeps no copy of the token.
  `mcp.json` gets `"auth": {"gh": "<login>"}`.
- **No gh, or over SSH**: **Sign in on github.com** shows a short code
  such as `ABCD-1234` and opens `https://github.com/login/device`. Type
  the code there (on any device, your phone works) and press
  **Authorize**. termocode sees the answer by itself; nothing is pasted.
  This needs termocode's GitHub OAuth App; a build without it says so
  and offers gh and a token.
- **A token** (last way): a fine-grained or classic personal access
  token with `repo` and `read:org`.

GitHub Enterprise Server has no remote MCP server; use the local
`github-mcp-server` there.

### Over SSH

When termocode runs on a server and the browser is on your laptop, the
browser can not reach the server's `127.0.0.1`. The wizard shows:

1. The sign-in link (it is copied; the copy works over SSH through
   OSC 52 when your terminal allows it). Open it on your laptop.
2. Allow termocode. The browser then says "can't connect": that is
   normal. Copy the whole address from its address bar
   (`http://127.0.0.1:47391/callback?code=…`).
3. Paste it into the wizard (it is hidden while you type).

Or skip step 3: run the `ssh -L` command the wizard shows on your laptop
before you allow termocode, for example
`ssh -L 47391:127.0.0.1:47391 myserver`. The sign-in listener always
takes the first free port of **47391–47395**, so you can put
`LocalForward 47391 127.0.0.1:47391` into your `~/.ssh/config` once.

GitHub (and GitLab with your own app id) use the code instead, which
works the same over SSH.

### Use a token instead

**Use a token instead** is on every sign-in screen, as the last way
(some organisations block new apps). The screen names the page where
you make the token and the smallest scopes to tick; the token is hidden
while you paste it and saved in `credentials.json` (mode `0600`), never
in `mcp.json`. The entry gets a header like
`"Authorization": "Bearer ${secret:github_token}"` (Sentry uses
`Sentry-Bearer`, Atlassian an e-mail plus API token). ClickUp has no
token way.

You can paste a reference instead of the token: `${env:NAME}`,
`${dotenv:NAME}` or `${cmd:…}` (not for Atlassian, which joins your e-mail
to the token). It is saved as text and read when the server starts. The
same works for **Set secret NAME…**. See
[References](ai-setup.md#type-a-reference-instead-of-a-secret).

### Slack

Slack lets only registered apps use its MCP server, so you create one
app once (about 2 minutes). The wizard copies an app manifest for you:

1. [api.slack.com/apps](https://api.slack.com/apps) → **Create New App**
   → **From a manifest**, pick your workspace.
2. Paste the manifest (it is copied) → **Create**. It asks only for read
   scopes and has the redirect `http://localhost:47391/callback`.
3. Turn on **PKCE** (OAuth & Permissions) and the **MCP server**
   (Agents & AI Apps).
4. Copy the **Client ID** from Basic Information (not the secret) and
   paste it into the wizard. The browser sign-in follows.

A workspace admin may have to approve the app.

### The services

| Service | URL termocode writes | Notes |
|---|---|---|
| GitHub | `https://api.githubcopilot.com/mcp/readonly` (writes: `…/mcp/`) | gh, a code on github.com, or a token. |
| GitLab | `https://gitlab.com/api/v4/mcp` | Scope `mcp`. Self-managed: change the host. A fine-grained token ("MCP tool: Execute") works too. Group owners can turn MCP access off. |
| Linear | `https://mcp.linear.app/mcp/readonly` (writes: `…/mcp`) | Scope `read` (writes: `read write`). |
| Notion | `https://mcp.notion.com/mcp` | Workspace members only. |
| Sentry | `https://mcp.sentry.dev/mcp` | Token mode: `Sentry-Bearer`, scopes `org:read`, `project:read`, `event:read`. |
| ClickUp | `https://mcp.clickup.com/mcp` | Scope `read`. No refresh: when the sign-in ends the row says `sign-in expired`; one **Sign in** fixes it. |
| Atlassian | `https://mcp.atlassian.com/v2/mcp` | Needs a Jira or Confluence Cloud site; tool calls use Rovo credits. |
| Slack | `https://mcp.slack.com/mcp` | Your own app (above). |

### Accounts, writes and sign out

**AI: MCP Servers…** lists every server, for example
`● github   amin-jalali · read only · connected · 28 tools`. Press
`Enter` on one for its actions:

- **Allow writes** / **Read only**: switch between the read-only and the
  full endpoint (the sign-in stays). Where scopes limit writes (Linear,
  ClickUp) termocode asks you to sign in again with the write scope.
- **Add another account**: a second entry, named after the account
  (`github-perkino-deals`). The AI sees the tools of both.
- **Sign in**: the same wizard (also when a sign-in expired).
- **Sign out**: forgets the tokens; services that offer it (Atlassian,
  Sentry) are also told to revoke them. For a gh entry gh keeps its own
  login.
- **Disable** / **Enable**: Disable keeps the entry (it writes
  `"disabled": true`), stops the server and gives the AI none of its
  tools. The row stays in the list, marked `○ ... off`. **Enable** turns
  it back on.
- **Remove…**: asks first. **Keep** is the first button; **Remove**
  deletes the entry from `mcp.json`, forgets its sign-in and the secrets
  only it uses, and stops the server. Only you can do this; the AI cannot.

The AI's instructions name each connected service and its access
("GitHub is connected as amin-jalali (read only)"), never a token.

### Where else you meet it

- The AI chat header shows `Connect…` when no service is set up,
  `N MCP` (connected servers) or `1 needs sign-in`. Click it.
- When a server answers "sign in first", the row above the chat input
  says `GitHub needs a sign-in  Sign in  Not now`.
- The start page's setup card has an optional **Apps** row once AI is
  set up.

## Add a server by hand

**Paste a config…** in the Connect wizard takes an `mcpServers` block from
another tool as it is: tokens in it move to
`credentials.json` and the entries get `${secret:…}` instead. Names that
already exist are skipped.

Or run **Open Config: MCP Servers**. It opens
`~/.config/termocode/mcp.json` (created when missing). Paste servers and
save:

```json
{
  "mcpServers": {
    "notes": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "${userHome}/notes"]
    },
    "linear": { "type": "http", "url": "https://mcp.linear.app/mcp" }
  }
}
```

There are two kinds of servers:

- **Local** servers are programs that termocode starts on your computer
  ("stdio"), for example `npx @modelcontextprotocol/server-filesystem`.
- **Remote** servers have a URL (`"type": "http"`, Streamable HTTP).

The next chat message uses the new file; no restart is needed. Run
**AI: MCP Servers…** to see every server and its state, for example
`○ notes   local · stopped · 11 tools`. Press `Enter` on a server for its
actions: **Connect / Refresh tools**, **Show tools**, **Stop**,
**Sign in**, **Sign out**, **Set secret NAME…**, **Trust**,
**Edit in mcp.json**, **Show log**, **Disable** and **Remove…**.

## The file

| Key | What it does |
|---|---|
| `command`, `args` | Local server: the program to start and its arguments. |
| `env` | Local server: extra environment variables. |
| `cwd` | Local server: folder to start it in. |
| `type`, `url` | Remote server: `"http"` (or `"streamable-http"`) and its URL. A `url` without `type` is remote too. |
| `headers` | Remote server: HTTP headers sent with every request, for example a token. |
| `oauth` | Remote server: a pre-registered app: `client_id`, `client_secret`, `scopes`. The wizard writes it for Slack (your app's client id) and for the read / write scopes. |
| `auth` | Remote server: `{"gh": "<login>"}` takes the token from the GitHub CLI (`gh auth token`) at each connect. Only allowed in your own `mcp.json`, never in a project's. |
| `disabled` | `true` turns the server off: it stays in the list as `off`, does not start and gives the AI no tools. **Disable** / **Enable** in the list set it. |
| `readOnly` | `true` offers only the tools the server marks read-only. |
| `autoApprove` | Tool names that run without asking (`["list_issues"]`). |
| `deny` | Tool names (or patterns like `"delete_*"`) never offered to the AI. |
| `timeout` | Seconds one tool call may wait (default `mcp_call_timeout`, 60 s). |

`type` and Claude Desktop's `transport` are read too. `readOnly`,
`autoApprove`, `deny`, `timeout`, `oauth` and `auth` are termocode keys: other
apps ignore them, so the same file still works there.

A remote URL must use `https://`. Plain `http://` works only for
servers on your own computer (`localhost`, `127.0.0.1`).

### Values with `${…}`

| Write | Gets |
|---|---|
| `${VAR}` | the environment variable `VAR` |
| `${VAR:-default}` | `VAR`, or `default` when it is empty |
| `${secret:NAME}` | `mcp_secrets.NAME` in `~/.config/termocode/ai/credentials.json` |
| `${workspaceFolder}` | the open workspace folder |
| `${userHome}` | your home folder |

Do not put tokens in `mcp.json`. Use `${secret:NAME}` and set the value
with **AI: MCP Servers…** → the server → **Set secret NAME…**. It is saved
in `credentials.json` (mode `0600`, only you can read it). You can also
edit that file:

```json
{ "mcp_secrets": { "github_token": "ghp_…", "pg_uri": "postgres://me:pass@localhost/db" } }
```

termocode warns in the MCP log when a value in `mcp.json` looks like a
token. A `${cmd:…}` in a project's `.termocode/mcp.json` is refused: the
server shows `invalid: ${cmd:} is not allowed in project files…` and
nothing is run. When a variable or secret is missing, the server shows
`needs ${VAR}` or `needs secret NAME` and does not start.

### Project servers

A workspace can have its own `.termocode/mcp.json`. Its servers replace
yours with the same name, and `"disabled": true` hides one of yours in
that project.

A cloned repository could put any command or URL in that file, so
termocode asks before it starts (or signs in to) a project server:
**Deny**, **Allow once** or **Always allow**. **Deny** comes first and has the
focus, so a stray `Enter` starts nothing. The dialog shows the command
or URL and the names (not the values) of its environment variables.
**Always allow** is stored per entry; when the entry changes, termocode
asks again. Servers in your own `mcp.json` do not ask.

## How the sign-in works

For the browser sign-in termocode finds the service's sign-in server by
itself (OAuth discovery) and identifies itself with its client metadata
document, or registers as an app when the service only allows that, so
there is nothing to create. It uses PKCE and checks the `state` of the
answer, so a sign-in from another session can not be used. The browser
comes back to a small listener on `127.0.0.1` only (the first free port
of 47391–47395); it stops after one answer or 5 minutes.

The code sign-in (device flow, GitHub) polls the service at the pace
the service asks for and stops when the code ends or you press Esc.

The tokens are saved in `credentials.json` (`mcp_oauth`, mode `0600`,
folder `0700`) with the account name, and refreshed by themselves. They
are only sent to the URL you signed in for: when you change the
server's URL to another server, termocode forgets them and asks you to
sign in again (the read-only and full endpoints of one service count as
the same server).
A sign-in also belongs to the service that gave it. When the server
moves to another sign-in service, termocode forgets the old sign-in
(it never sends it there) and asks you to sign in again.

**More access.** When a tool needs more access than your sign-in has,
the server says which. The row says `more access needed: …`. Sign in
again: termocode asks for your old access plus the missing part.

**Sign-in service in another place.** If a server does not say where
its sign-in service is, add its metadata URL to the entry:
`"oauth": {"metadata_url": "https://login.example.com/.well-known/oauth-authorization-server"}`
(`https://` only).

A server with its own `Authorization` header (a token) never signs in.
If the server refuses the token, its row says `not authorized`: check
the token's scopes.

## What the AI may do

Each MCP tool shows up as `mcp__<server>__<tool>`; the dialogs, the
transcript and the AI audit log (Output: AI) show `mcp.<server>.<tool>`.

- A tool the server marks read-only runs at once.
- Any other tool asks first. **Always allow mcp:<server>** stops the
  questions for that server (change it in **AI: Agent Auto-Approve…**).
- A tool whose name says delete, remove, archive, destroy, purge or drop
  always asks.
- A call whose arguments hold a secret always asks, even a read-only
  tool: a key or password termocode knows, or a value that looks like a
  token (`ghp_…`, `sk-…`, `Bearer …`). The card says which argument
  (never the value), and **Deny** comes first. The log and the chat
  show such values as `[REDACTED]`.
- Set `mcp_trust_annotations` to `false` (Settings → MCP) to make every
  MCP tool ask (except the ones in a server's `autoApprove`).

The server's marks are hints, so review a new server with **Show tools**.
Tool results are cleaned and marked as outside data for the model.

**Big results.** A result over `mcp_max_result_kb` (32 KB) is not cut
off. The whole result is saved in a file in your temp folder (only you
can read it). The AI gets the first 32 KB and the size, and reads the
rest in parts when it needs it (the `mcp_read_result` tool, read only).
The **MCP** log says when this happens. A server can set a smaller or
bigger limit for one tool (its `maxResultSizeChars`, at most 256 KB).

**When a server's tools change.** termocode remembers the tool list of
each server the first time it sees it. If the list changes later (a
server can change what its tools do), the next chat message asks again:
**Not now**, **Trust once** or **Trust the new tools**. **Not now** comes first and
has the focus. With **Not now**
every tool of that server asks before it runs, even a read-only one,
and the servers list says `tools changed`. Review them with **Show
tools**, then use **Trust the new tools** in the server's actions.

**A broken server.** After two failed calls to the same server in one
chat message, its tools answer "unavailable" for the rest of that
message, so the AI does not keep trying.

## Let an outside agent use termocode

This is the other direction: an agent CLI such as Claude Code or Codex,
running in a terminal, calls termocode's IDE tools (open a file, read
diagnostics, run a task or a test, steer the debugger, read git state).
termocode asks you on a card before a tool runs or changes something.

How it starts, how to add it by hand, what the agent can and can not do,
**Always allow** and the status screen are in
[Give your agent the IDE's tools](agents.md).

## Resources and privacy

- Nothing starts or connects with termocode. A server starts on the
  first tool call, or once to list its tools when there is no saved list
  yet (`~/.cache/termocode/mcp/`).
- It stops after `mcp_idle_timeout` (10 minutes) without a call, and when
  termocode quits: a local server ends, a remote one ends its session.
  `0` keeps it running.
- A long tool call that reports progress is not cut by
  `mcp_call_timeout`; a silent one is.
- A local server gets only `PATH`, `HOME`, `USER`, `LANG`, `TERM`,
  `TMPDIR`, `XDG_*` and the `env` you set: never your AI API keys.
- Servers can not use your AI model (sampling is refused).
- Tokens and secrets never show in the log, toasts or `errors.log`.

### One server for all windows

With more than one termocode window open, a local server runs only once.
All windows with the same server entry and the same folder use it.
There is nothing to set up.

- The row of the server says `shared · 3 sessions`.
- It stops like before: when the last window stops using it
  (`mcp_idle_timeout`, or quit), it ends.
- If a window crashes, the server waits 30 seconds for it, then ends
  when no window uses it. It never stays forever.
- **Stop** on a shared server asks first: it stops for every window. Each
  window starts it again on its next tool call.
- A different command, args, `env` value or folder gets its own server.
- Secrets stay in memory. They are never written to a file or put on a
  command line.
- Remote servers are not shared: each window has its own session.
- Set `mcp_share` to `false` to give each window its own server. On
  systems without unix sockets, each window always has its own.

## Settings

The MCP section of the Settings modal (**Preferences: Open Settings (UI)**)
has `mcp_enabled`, `mcp_idle_timeout`, `mcp_call_timeout`,
`mcp_max_result_kb`, `mcp_trust_annotations`, `mcp_oauth_port`,
`mcp_share` and `mcp_github_client_id` (a GitHub OAuth App for the code sign-in, when
your build has none). The timeouts apply at once. The AI can not change these settings. They are
listed in the settings reference.

## Troubleshooting

Run **AI: MCP: Show Log** (the **MCP** channel of the Output panel). It
shows each start or connect, the server's version and tool count, its
error output, sign-ins, token refreshes, idle stops and failures.
Secrets are hidden.

| You see | Do this |
|---|---|
| `failed: npx not found` | Install Node.js, or set `command` to a full path. |
| `needs ${GITHUB_PAT}` | Export the variable, or use `${secret:…}`. |
| `needs secret github_token` | The server → **Set secret github_token…**, or connect it again with **AI: Connect a Service…**. |
| `sign-in needed`, `sign-in expired` | The server → **Sign in** (the Connect wizard). |
| `lets only approved apps sign in` | Use a token instead, or ask your admin to allow termocode. |
| `gh is installed but not signed in` | Run `gh auth login`, or sign in on github.com. |
| `The code expired` | **Try again** gets a new code (GitHub: 15 minutes). |
| `the sign-in ports 47391–47395 are all busy` | Close the other sign-in, or set `mcp_oauth_port`. |
| `not authorized` | The token in `headers` (or gh's token) is wrong or misses scopes. |
| `insecure url` | Use the server's `https://` URL. |
| `no answer in 60s` | Raise `timeout` for that server or `mcp_call_timeout`. |
| `connected · old transport` | The server only speaks the old HTTP+SSE transport. It works; termocode tries it only when the server refuses the current one. Ask the service for its `/mcp` URL when it has one. |
| `reconnecting in 8s` | The remote server could not be reached. termocode tries again by itself, waiting longer each time (up to 2 minutes, 8 tries). Nothing to do. |
| `failed: … (gave up after 8 tries …)` | Run **AI: MCP: Reconnect All Servers** when you are back online. It also checks the running servers with a ping. |
| `tools changed` | Review with **Show tools**, then **Trust the new tools**. |
| `server not trusted` | **AI: MCP Servers…** → the server → **Trust**. |
