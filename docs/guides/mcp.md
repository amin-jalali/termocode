# Connect MCP servers

MCP (Model Context Protocol) servers give the AI chat more tools: your
issues in Linear, GitHub pull requests, Slack messages, a database, a
browser, your notes folder and many more. termocode reads the same
`mcpServers` block as Claude Desktop, Claude Code and Cursor, so you can
paste the config you already have.

There are two kinds of servers:

- **Local** servers are programs that termocode starts on your computer
  ("stdio"), for example `npx @modelcontextprotocol/server-filesystem`.
- **Remote** servers have a URL (`"type": "http"`, Streamable HTTP), for
  example Linear, GitHub, GitLab, Notion or Slack. You sign in once in
  the browser, or give a token.

## Add a server

The fast way: press `F1`, run **AI: MCP Servers…** and pick
**+ Add a server…**. Choose a service from the list (Linear, GitHub,
GitLab, Atlassian, Slack, Notion, Sentry, ClickUp). termocode writes it
into your `mcp.json` and shows what is left to do, for example
**Sign in** or **Set secret github_pat…**.

The other way: run **Open Config: MCP Servers**. It opens
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

Then ask the chat something that needs the server ("list my notes",
"show my 3 newest Linear issues"). The next chat message uses the new
file; no restart is needed.

Run **AI: MCP Servers…** to see every server and its state, for example
`○ notes   local · stopped · 11 tools` or
`● linear   remote · connected · signed in · 14 tools`. Press `Enter` on
a server for its actions: **Connect / Refresh tools**, **Show tools**,
**Stop**, **Sign in**, **Sign out**, **Set secret NAME…**, **Trust**,
**Edit in mcp.json** and **Show log**.

When servers are configured, the chat panel header shows a chip like
`⌁ 2 MCP` (the number of connected servers). Click it to open
**AI: MCP Servers…**.

## The file

| Key | What it does |
|---|---|
| `command`, `args` | Local server: the program to start and its arguments. |
| `env` | Local server: extra environment variables. |
| `cwd` | Local server: folder to start it in. |
| `type`, `url` | Remote server: `"http"` (or `"streamable-http"`) and its URL. A `url` without `type` is remote too. |
| `headers` | Remote server: HTTP headers sent with every request, for example a token. |
| `oauth` | Remote server: a pre-registered app: `client_id`, `client_secret`, `scopes`. Only needed for Slack-like servers. |
| `disabled` | `true` hides the server. |
| `readOnly` | `true` offers only the tools the server marks read-only. |
| `autoApprove` | Tool names that run without asking (`["list_issues"]`). |
| `deny` | Tool names (or patterns like `"delete_*"`) never offered to the AI. |
| `timeout` | Seconds one tool call may wait (default `mcp_call_timeout`, 60 s). |

`type` and Claude Desktop's `transport` are read too. `readOnly`,
`autoApprove`, `deny`, `timeout` and `oauth` are termocode keys: other
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
{ "mcp_secrets": { "github_pat": "ghp_…", "pg_uri": "postgres://me:pass@localhost/db" } }
```

termocode warns in the MCP log when a value in `mcp.json` looks like a
token. When a variable or secret is missing, the server shows
`needs ${VAR}` or `needs secret NAME` and does not start.

### Project servers

A workspace can have its own `.termocode/mcp.json`. Its servers replace
yours with the same name, and `"disabled": true` hides one of yours in
that project.

A cloned repository could put any command or URL in that file, so
termocode asks before it starts (or signs in to) a project server:
**Allow once**, **Always allow** or **Deny**. The dialog shows the command
or URL and the names (not the values) of its environment variables.
**Always allow** is stored per entry; when the entry changes, termocode
asks again. Servers in your own `mcp.json` do not ask.

## Remote servers

### Sign in (OAuth)

Most remote servers (Linear, GitLab, Atlassian, Notion, Sentry, ClickUp)
use OAuth. When such a server answers "not authorized", its row says
`! linear   remote · sign-in needed`. Then:

1. **AI: MCP Servers…** → the server → **Sign in**.
2. Your browser opens the sign-in page (the address is also copied).
   Allow the access.
3. The browser comes back to termocode and says "You can close this
   tab". The server connects: `remote · connected · signed in · 14 tools`.

termocode finds the server's sign-in service by itself and registers as
an app there when the service allows it, so there is nothing to create.
It uses PKCE and checks the `state` of the answer, so a sign-in from
another session cannot be used. The browser comes back to a small
listener on `127.0.0.1` only; it stops after one answer or 5 minutes.

The tokens are saved in `credentials.json` (`mcp_oauth`, mode `0600`)
and refreshed by themselves. They are only sent to the URL you signed in
for: when you change the server's URL, termocode forgets them and asks
you to sign in again. **Sign out** forgets them at once.

### Sign in over SSH

When termocode runs on a server and your browser is on your laptop, the
browser can not reach the server's `127.0.0.1`. The sign-in prompt then
explains two ways:

- **Paste the address.** Open the copied sign-in address in your
  laptop's browser and allow the access. The browser then shows "can't
  connect" (or "This site can't be reached"). That is expected: copy the
  whole address from its address bar (it starts with
  `http://127.0.0.1:…/callback?code=…`) and paste it into the prompt.
- **Forward the port.** Run the command the prompt shows, for example
  `ssh -L 8765:127.0.0.1:8765 myserver`, in a terminal on your laptop
  before you allow the access. Then the browser comes back by itself.
  Set `mcp_oauth_port` (Settings → MCP) to a fixed port to put the
  forward in your `~/.ssh/config` once.

The copy works over SSH too (OSC 52) when your terminal allows it.

### Tokens instead of a sign-in

Some servers take a token in a header, for example a GitHub personal
access token:

```json
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "https://api.githubcopilot.com/mcp/",
      "headers": { "Authorization": "Bearer ${secret:github_pat}" }
    }
  }
}
```

A server with its own `Authorization` header never signs in. If the
server refuses the token, its row says `not authorized`: check the
token's scopes.

### The services

| Service | URL | How to connect |
|---|---|---|
| Linear | `https://mcp.linear.app/mcp` | Sign in. |
| GitHub | `https://api.githubcopilot.com/mcp/` | A personal access token (scopes `repo`, `read:org`) as `${secret:github_pat}`. Not for GitHub Enterprise Server: use the local `github-mcp-server` there. |
| GitLab | `https://gitlab.com/api/v4/mcp` | Sign in (the preset asks for the scope `mcp`). Needs GitLab Premium or Ultimate with Duo. For a self-managed GitLab, change the host. A token header works too. |
| Atlassian (Jira, Confluence) | `https://mcp.atlassian.com/v1/mcp` | Sign in. If Atlassian refuses termocode as a client, use the API-token mode your admin enables, as a header. |
| Slack | `https://mcp.slack.com/mcp` | Needs your own Slack app, see below. |
| Notion | `https://mcp.notion.com/mcp` | Sign in. |
| Sentry | `https://mcp.sentry.dev/mcp` | Sign in. |
| ClickUp | `https://mcp.clickup.com/mcp` | Sign in. Only third-party pages describe this endpoint; if it fails, check ClickUp's own MCP page. |

**Slack.** Slack lets only apps you create in your workspace use its MCP
server, and it needs a fixed redirect address:

1. Pick a port, for example `8765`, and set `mcp_oauth_port` to it
   (Settings → MCP).
2. Create an app at [api.slack.com/apps](https://api.slack.com/apps)
   (an internal app of your workspace). Under **OAuth & Permissions** add
   the redirect URL `http://127.0.0.1:8765/callback` and the user token
   scopes you want, for example `search:read.public` and `chat:write`.
3. Add the Slack preset. Then **Set secret slack_client_id…** and
   **Set secret slack_client_secret…** with the values of your app.
4. **Sign in**.

The preset writes this entry (you may put the client id there as plain
text instead; it is not a secret):

```json
"slack": {
  "type": "http",
  "url": "https://mcp.slack.com/mcp",
  "oauth": {
    "client_id": "${secret:slack_client_id}",
    "client_secret": "${secret:slack_client_secret}",
    "scopes": ["search:read.public", "chat:write"]
  }
}
```

## What the AI may do

Each MCP tool shows up as `mcp__<server>__<tool>`; the dialogs, the
transcript and the AI audit log (Output: AI) show `mcp.<server>.<tool>`.

- A tool the server marks read-only runs at once.
- Any other tool asks first. **Always allow mcp:<server>** stops the
  questions for that server (change it in **AI: Agent Auto-Approve…**).
- A tool whose name says delete, remove, archive, destroy, purge or drop
  always asks.
- Set `mcp_trust_annotations` to `false` (Settings → MCP) to make every
  MCP tool ask (except the ones in a server's `autoApprove`).

The server's marks are hints, so review a new server with **Show tools**.
Tool results are cleaned, cut at `mcp_max_result_kb` (32 KB) and marked
as outside data for the model.

**When a server's tools change.** termocode remembers the tool list of
each server the first time it sees it. If the list changes later (a
server can change what its tools do), the next chat message asks again:
**Trust once**, **Trust the new tools** or **Not now**. With **Not now**
every tool of that server asks before it runs, even a read-only one,
and the servers list says `tools changed`. Review them with **Show
tools**, then use **Trust the new tools** in the server's actions.

**A broken server.** After two failed calls to the same server in one
chat message, its tools answer "unavailable" for the rest of that
message, so the AI does not keep trying.

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

## Settings

The MCP section of the Settings modal (**Preferences: Open Settings (UI)**)
has `mcp_enabled`, `mcp_idle_timeout`, `mcp_call_timeout`,
`mcp_max_result_kb`, `mcp_trust_annotations` and `mcp_oauth_port`. The
timeouts apply at once. The AI can not change these settings. They are
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
| `needs secret github_pat` | The server → **Set secret github_pat…**. |
| `sign-in needed` | The server → **Sign in**. |
| `needs an app` | The server has no open registration (Slack): create an app and fill `oauth`. |
| `this server allows only approved clients` | Use the service's token mode as a header. |
| `not authorized` | The token in `headers` is wrong or misses scopes. |
| `insecure url` | Use the server's `https://` URL. |
| `no answer in 60s` | Raise `timeout` for that server or `mcp_call_timeout`. |
| `tools changed` | Review with **Show tools**, then **Trust the new tools**. |
| `server not trusted` | **AI: MCP Servers…** → the server → **Trust**. |
