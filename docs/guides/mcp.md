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
while you paste it and saved in your secret store (see
[Which store is used](ai-setup.md#which-store-is-used)), never in
`mcp.json`. The entry gets a header like
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

### Docs, code search and browser servers

**AI: Web Tools…** adds a few servers with one pick: library docs, code
search on GitHub, web search and browser automation (with a `visual-qa`
skill). The card shows the exact entry it writes to your `mcp.json`;
**Cancel** is first. See [Web tools](ai-use.md#web-tools).

## Add a local server

**Local server…** in the Connect wizard adds a program that runs on your
computer. Fill in one short form:

- **Runs with**: a command, an npm package (`npx -y …`), a pip package
  (`uvx …`) or a container image (`docker run -i --rm …`).
- **Command or package**, then its arguments, for example
  `@modelcontextprotocol/server-filesystem ~/notes`.
- **Name**: leave it empty to take one from the package (`filesystem`).
- **Secret env names**, for example `API_KEY`. You type each value next,
  in a hidden field. Values go to the secret store; `mcp.json` gets
  `${secret:<name>_api_key}`.
- **Save in**: your `mcp.json`, or this project's `.termocode/mcp.json`.
  termocode asks you to trust a project server before it first starts.

The next screen shows the entry and its file. Nothing is written before
**Add**.

## Manage servers in the terminal

The same works without the UI, for scripts and dotfiles:

```sh
termocode mcp add notes -- npx -y @modelcontextprotocol/server-filesystem ~/notes
termocode mcp add --npm @modelcontextprotocol/server-filesystem -- ~/notes
termocode mcp add --secret API_KEY --scope project tracker -- tracker-mcp
termocode mcp add https://mcp.example.com/mcp      # a remote server
termocode mcp add - < servers.json                 # an mcpServers block
termocode mcp list            # or: list --json
termocode mcp get notes
termocode mcp remove notes
```

- `--secret NAME` reads the value from a hidden prompt, or one line of
  stdin per secret. A value is never taken from the command line, and
  the commands never print one.
- `--env NAME=VALUE` is for plain values; a value that looks like a token
  is refused.
- `--scope project` writes `<dir>/.termocode/mcp.json` (`--dir`, default:
  the current folder).
- `remove` also deletes the entry's secrets when no other entry uses them.
- `termocode mcp add <url|json>` is the terminal install link: give it a
  server URL or a pasted block. Tokens in a block move to the secret
  store. Use `-` to read the block from stdin, so tokens stay out of your
  shell history.

`termocode mcp` alone is still the server for an agent CLI (see below).

## Add a server by hand

**Paste a config…** in the Connect wizard takes an `mcpServers` block from
another tool as it is: tokens in it move to your
secret store and the entries get `${secret:…}` instead. Names that
already exist are skipped.

**AI: MCP: Import from Other Apps…** (`F1`) looks in the MCP config files
that other apps keep in your home folder. A card lists each new server:
its name, what it runs (command or URL) and the file it comes from.
**Cancel** comes first and has the focus; **Import N** copies them to your
`mcp.json`, with tokens moved to your secret store as above. The other
apps' files are only read, never changed. Names you already have are left
out.

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

Above the actions, the server's screen shows its details in one place:
the file it comes from, local or remote, the command or URL, the names
of its env variables and headers (never their values), the server's own
name, version and MCP version, its tools, its state and the last error.

**Copy for a teammate** puts the server's entry on the clipboard as an
`mcpServers` block. Secrets are never copied: a `${secret:NAME}` stays
as it is, and a token written in the file becomes a new
`${secret:NAME}`. Your own sign-in (`auth`) is left out. The message
names the secrets your teammate sets.

### Let the AI add a server

Paste a server's setup block (or its URL) in the chat and ask the AI to
add it. The AI never adds a server on its own:

- A card shows the whole entry and what it does ("It runs on this
  computer: npx …" or "It connects to https://…"). **Deny** has the
  focus; **Add server** writes it to your `mcp.json`. There is no
  **Always allow**.
- Nothing starts at once. The tools reach the AI from the next message.
- The AI can not add an entry with `${cmd:}`, a literal token,
  `autoApprove`, `gateway` or `auth`. Add a server with a token through
  **Paste a config…** in the Connect list (it stores the token safely).
- For a known service (GitHub, Linear …) the AI opens the Connect
  wizard instead, so you sign in yourself.

## Find a server in the catalog

**AI: MCP: Browse Catalog…** lists the servers of the official MCP
Registry. termocode goes to the network only when you pick **Load the
catalog**; the list is kept, so the next open is instant and works
offline. Type to search.

- `✓` means the MCP Registry checked who published the name (a GitHub
  account or a web domain). It does not mean the code is safe.
- `⚠` means the publisher marked the server deprecated or removed.
- Enter shows a card with the publisher, the version, the exact command
  or URL and the secrets it needs. **Cancel** is first. **Add server**
  writes it to your `mcp.json`, pinned to that version. A server with
  secrets opens its actions, so you can set them; they go to the secret
  store, never into the file.
- Your team can keep its own approved list: set `mcp_catalog_url` to a
  server with the same API (https only).

### Versions and updates

A server added from the catalog is pinned to its version (the
`registry` key in its entry). It does not change by itself.

- **AI: MCP: Check for Updates** asks the catalog what changed since
  the last look. The servers list then says `update 1.3.0` or
  `⚠ deprecated` in front of the server.
- In the server's actions, **Update to 1.3.0…** shows the new command on
  a card. **Cancel** is first. **Update** moves the pin; your `env`
  values and other settings stay.
- `mcp_auto_update`: `off` (default) checks only when you ask. `notify`
  also checks once a day when you open the MCP servers list. `on` does
  the same and updates your own pinned servers; a project file is never
  changed.

## Install a bundle (.mcpb)

A `.mcpb` file is one server in one file: its code and a manifest.
**AI: MCP: Install Bundle (.mcpb)…** asks for the file, then:

1. A card shows the name, version, author, tools and the settings it
   asks for. Nothing is installed yet. **Cancel** is first.
2. termocode asks each setting. A secret is typed hidden.
3. A last card shows the exact command. **Install** unpacks the bundle
   into `~/.local/share/termocode/mcp-bundles/`, saves the secrets in the
   secret store and adds the server to your `mcp.json`.

The author is not checked: install bundles only from people you trust.

## The file

| Key | What it does |
|---|---|
| `command`, `args` | Local server: the program to start and its arguments. |
| `env` | Local server: extra environment variables. |
| `envFile` | Local server: a file of `KEY=value` lines added to `env` (a key in `env` wins). A relative path is in the workspace. A project file may only name a file inside the workspace. |
| `cwd` | Local server: folder to start it in. |
| `type`, `url` | Remote server: `"http"` (or `"streamable-http"`) and its URL. A `url` without `type` is remote too. |
| `headers` | Remote server: HTTP headers sent with every request, for example a token. |
| `headersCommand` | Remote server: a command line run at each connect that prints one JSON object of headers, for example `{"Authorization": "Bearer …"}` for a short-lived token. They replace `headers` of the same name. Its output never shows in a log or error. Only allowed in your own `mcp.json`, never in a project's. |
| `oauth` | Remote server: a pre-registered app: `client_id`, `client_secret`, `scopes`. The wizard writes it for Slack (your app's client id) and for the read / write scopes. |
| `auth` | Remote server: `{"gh": "<login>"}` takes the token from the GitHub CLI (`gh auth token`) at each connect. Only allowed in your own `mcp.json`, never in a project's. |
| `disabled` | `true` turns the server off: it stays in the list as `off`, does not start and gives the AI no tools. **Disable** / **Enable** in the list set it. |
| `readOnly` | `true` offers only the tools the server marks read-only. |
| `autoApprove` | Tool names that run without asking (`["list_issues"]`). |
| `deny` | Tool names (or patterns like `"delete_*"`) never offered to the AI. |
| `timeout` | Seconds one tool call may wait (default `mcp_call_timeout`, 60 s). |
| `startTimeout` | Seconds the start may take (1–600, default 15). Raise it for a package that downloads first. |
| `share` | `"user"`: one process serves every project (local servers, your own file only). |
| `gateway` | `true` lets outside agents use this server through termocode, each call after a card. Only in your own `mcp.json`. |
| `autostart` | `false`: never start the server just to list its tools. Its tools come after **Connect** in the list. |
| `sandbox` | Local server: run it in a container or a sandbox tool. See [Run a local server in a sandbox](#run-a-local-server-in-a-sandbox). |

`type` and `transport` are read too, and the top key `servers` works
like `mcpServers` (when both have a name, `mcpServers` wins). `readOnly`,
`autoApprove`, `deny`, `timeout`, `startTimeout`, `share`, `sandbox`, `gateway`, `oauth` and `auth` are termocode keys: other
apps ignore them, so the same file still works there.

A remote URL must use `https://`. Plain `http://` works only for
servers on your own computer (`localhost`, `127.0.0.1`).

### Values with `${…}`

| Write | Gets |
|---|---|
| `${VAR}` or `${env:VAR}` | the environment variable `VAR` |
| `${VAR:-default}` | `VAR`, or `default` when it is empty |
| `${secret:NAME}` | the secret `NAME` in your secret store |
| `${workspaceFolder}` | the open workspace folder |
| `${workspaceFolderBasename}` | the folder's name only, for example `my-app` |
| `${/}` | the path separator (`/`, or `\` on Windows) |
| `${userHome}` | your home folder |

Do not put tokens in `mcp.json`. Use `${secret:NAME}` and set the value
with **AI: MCP Servers…** → the server → **Set secret NAME…**. It is saved
in your secret store: the OS keyring, the encrypted file `secrets.age`, or
the plain file `ai/credentials.json` only if you chose `secret_store:
plain`. See [Which store is used](ai-setup.md#which-store-is-used).
**AI: Secret Store…** lists the names (never the values).

termocode warns in the MCP log when a value in `mcp.json` looks like a
token. A `${cmd:…}` in a project file (`.mcp.json` or `.termocode/mcp.json`) is refused: the
server shows `invalid: ${cmd:} is not allowed in project files…` and
nothing is run. A project file may not put an environment variable
(`${VAR}`, `${env:VAR}`) into a remote server's `url` or `headers`: keys
live in such variables, and a cloned repository must not send them to its
own server. The server shows `invalid: a project file may not send env
vars…`; use `${secret:NAME}` or move the entry to your own `mcp.json`.
When a variable or secret is missing, the server shows
`needs ${VAR}` or `needs secret NAME` and does not start.

### Project servers

A workspace can have its own servers in two files: `.mcp.json` at the
root (the common file other apps read too) and `.termocode/mcp.json`.
termocode reads your `mcp.json`, then `.mcp.json`, then
`.termocode/mcp.json`; a later entry replaces an earlier one with the
same name, so `.termocode/mcp.json` wins. `"disabled": true` hides one of
yours in that project. The server page and the trust dialog say which
file an entry comes from.

A cloned repository could put any command or URL in that file, so
termocode asks before it starts (or signs in to) a project server:
**Deny**, **Allow once** or **Always allow**. **Deny** comes first and has the
focus, so a stray `Enter` starts nothing. The dialog shows the command
or URL and the names (not the values) of its environment variables.
**Always allow** is stored per entry; when the entry changes, termocode
asks again. Servers in your own `mcp.json` do not ask.

### Allow and deny lists (policy)

On a company machine an admin can say which servers may run. termocode
reads two policy files, and both apply:

- the machine file `/etc/termocode/mcp-policy.json`
  (`/Library/Application Support/termocode/mcp-policy.json` on macOS,
  `%ProgramData%\termocode\mcp-policy.json` on Windows), and
- your own `~/.config/termocode/mcp-policy.json`.

```json
{
  "deny":  [{ "command": "curl" }, { "url": "http://*" }],
  "allow": [{ "name": "notes" }, { "command": "npx" }, { "url": "https://mcp.linear.app/*" }]
}
```

A rule has `name`, `command` and / or `url`; `*` matches any text, and
every key you set must match. `command` is compared with the program, its
file name and the whole command line. A `deny` rule always wins. When a
file has an `allow` list, a server must match one of its rules. A blocked
server stays in **AI: MCP Servers…** as `blocked by policy (<file>)` and
never starts. If a policy file is not valid JSON, every server is blocked
until it is fixed, and the MCP log says why.

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

The tokens are saved in your secret store with the account name, and refreshed by themselves. They
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
- To undo a wrong **Always allow**, open the server in
  **AI: MCP Servers…** and choose **Reset trust…**: the server asks again
  before it starts (project servers) and its tools ask again before they
  change something. A tool you listed in `autoApprove` has its own
  **Untrust tool NAME…** row. Both cards put **Cancel** first.
- A tool whose name says delete, remove, archive, destroy, purge or drop
  always asks.
- A call whose arguments hold a secret always asks, even a read-only
  tool: a key or password termocode knows, or a value that looks like a
  token (`ghp_…`, `sk-…`, `Bearer …`). The card says which argument
  (never the value), and **Deny** comes first. The log and the chat
  show such values as `[REDACTED]`.
- Set `mcp_trust_annotations` to `false` (Settings → MCP) to make every
  MCP tool ask (except the ones in a server's `autoApprove`).

- A tool the server marks "needs a person" (`requiresUserInteraction`)
  always asks, with no **Always allow**, even in `autoApprove`.

The server's marks are hints, so review a new server with **Show tools**.
A tool with a broken description (a bad input schema, a name with
spaces, a name listed twice) is left out and the **MCP** log says why;
the server's other tools still work. When a tool says what its result
looks like (an output schema) and the result does not match, the AI is
told to treat it with care and the log says so. When the connection
drops during a call, a read-only or "safe to repeat" tool runs once
more on a new connection; other tools are never run twice.
Tool results are cleaned and marked as outside data for the model.

**Big results.** A result over `mcp_max_result_kb` (32 KB) is not cut
off. The whole result is saved in a file in your temp folder (only you
can read it). The AI gets the first 32 KB and the size, and reads the
rest in parts when it needs it (the `mcp_read_result` tool, read only).
The **MCP** log says when this happens. A server can set a smaller or
bigger limit for one tool (its `maxResultSizeChars`, at most 256 KB).

**Turn a tool off.** **Show tools** lists each tool with its title, how
it runs (`runs at once`, `asks`, `always asks`) and `reaches outside`
when the server says the tool talks to the outside world. Each tool is
marked `[x]` (on) or `[ ]` (off). Press `Space` on a tool to turn it off or on. An off
tool is not sent to the AI at all. termocode keeps your choice in
config.json (`mcp_disabled_tools`), so it stays after a restart. The
server's **Show tools** row says how many are off, for example
`12 tools · 2 off`.

**What the tools cost.** Every chat message carries the description of
every MCP tool. **AI: MCP Servers…** shows an estimate next to each
server (for example `~1.2k tokens`), and the server's screen shows it
too. When all servers together pass about 10k tokens, the list warns:
many tools make each message slower and more costly. Turn off the tools
you do not need.

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

**Many tools.** Every tool's full description costs room in the AI's
context. When your MCP tools are big (about 10k tokens), termocode sends
the AI one search tool instead: `mcp_tool_search`. The AI searches by
keywords, gets the full input of the tools it found, and calls them from
its next step. A tool still asks before it runs, as before. Set
`mcp_tool_search` to `on` to always do this, or `off` to always send
every tool.

**Profiles.** A profile is a named set of servers and tools, so a chat
uses only what a task needs. **AI: MCP: Switch Profile…** picks one for
this chat; **All servers and tools** goes back. Servers a profile leaves
out never start. Write profiles in `mcp-profiles.json` in the config
folder (the picker's **Edit profiles file…** makes one), or share them
with your team in `.termocode/mcp-profiles.json`:

```json
{
  "profiles": {
    "review": {
      "description": "read-only GitHub",
      "servers": ["github"],
      "tools": ["github.get_*", "github.list_*", "!github.delete_*"]
    }
  }
}
```

No `servers` means every server; no `tools` means every tool. Tool
rules are globs on `server.tool`; `!` removes. A profile can only make
the list smaller: it never adds a server, a command or a secret. Your
own profile wins over a project one with the same name.

**Odd schemas.** Some servers describe a tool's input as "one of these
forms" at the top level. The AI services refuse that, so termocode sends
one object with the fields of all forms.

## Prompts, resources and questions from a server

**Pictures.** When a tool sends an image (a screenshot, a chart), a
model that takes images gets the image itself (PNG, JPEG, GIF or WebP,
up to 5 MB, at most 4 per result). Other models, and audio, get a short
note like `[image image/png, 30 KB — not shown: this model does not take
images]`.

**Resources.** Some servers share files, docs or records. The AI can
list and read them with `mcp_list_resources` and `mcp_read_resource`
(read only, no question). You can attach one yourself: type
`@server:uri` in a message, for example `@docs:file:///guide.md`. The
chat shows the read as a tool line.

**Prompts.** Some servers have ready-made prompts. Type
`/server.prompt` and the values, in order or as `name=value`:
`/myserver.review main.go style=short`. **Tab** completes the prompt name
and the values (when the server can suggest them). The prompt's text is
sent as your message, so you see what goes to the model.

**AI: MCP Prompts and Resources…** lists both. A pick puts the text in
the chat input; nothing is sent until you press **Enter**.

**When a server asks you.** During a tool call a server may ask for
input. A card shows what it asks; **Deny** comes first and has the focus.

- A form: **Fill in…** asks for each field, then a last card shows what
  will be sent (**Cancel** first). A field that looks like a password or
  key is hidden while you type, and the card warns you: servers should
  not ask for these. termocode never fills in an answer for you.
- A page to open: the card shows the whole address and its site.
  **Open in browser** opens it.

Servers that speak the newest MCP version (2026-07-28) ask the same
way: the call stops, the same card shows, and termocode sends the call
again with your answer. A server never gets to use the AI model
(sampling): termocode refuses that on purpose.

**Long calls.** While a tool runs, the line under the chat shows what it
does (`mcp.ci.build · 40 s · step 3 · 3 of 10 (30%)`) when the server
reports progress. Press **Ctrl+B** in the chat (or use **AI: Move MCP
Call to Background**) to let the AI go on: the call keeps running, and
its result shows in the chat when it ends and goes with your next
message.

## Let an outside agent use termocode

This is the other direction: an agent CLI such as Claude Code or Codex,
running in a terminal, calls termocode's IDE tools (open a file, read
diagnostics, run a task or a test, steer the debugger, read git state).
termocode asks you on a card before a tool runs or changes something.

How it starts, how to add it by hand, what the agent can and can not do,
**Always allow** and the status screen are in
[Give your agent the IDE's tools](agents.md).

### Share your servers with outside agents

Set up a server once and let outside agents use it too. Add
`"gateway": true` to its entry in **your own** `mcp.json`:

```json
"files": { "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "."], "gateway": true }
```

- The agent then sees the server's tools next to the IDE tools, named
  `mcp__files__read_file` and so on.
- **Every** call asks on a card first, even a read. **Deny** has the
  focus. **Always allow** remembers that tool for that agent.
- Only servers you trusted before are shared. termocode never shows a
  trust card for an outside agent.
- A project's `.termocode/mcp.json` can not set `gateway`: a repository
  can not share your servers.
- Without `gateway`, a server stays private to the chat.

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

### Run a local server in a sandbox

A local server is a program from the net. Add `"sandbox"` to its entry
to keep it away from your files and the network:

```json
"files": {
  "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "."],
  "sandbox": true
}
```

- `true` picks the best tool found: a container when `image` is set
  (docker, then podman), else `bwrap` (bubblewrap) on Linux or
  `sandbox-exec` on macOS.
- `"sandbox": "container"`, `"bwrap"` or `"sandbox-exec"` picks one.
- The long form sets more:
  `{"image": "node:22", "cpus": "1", "memory": "512m", "network": false, "workspace": "ro"}`.
- By default the network is **off** and the workspace folder is mounted
  **read-only** (`"workspace": "rw"` lets it write, `"none"` hides it).
  Under `bwrap` your home folder is hidden.
- `cpus` and `memory` work only in a container.
- The row of the server shows its sandbox, for example
  `sandbox: bwrap · network off · workspace read-only`.
- If no sandbox tool is installed, the server does **not** start and the
  row says what to install. It never runs without the sandbox.
- Secrets in `env` go into the container by name only; their values
  never show on a command line.

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

By default, each project folder gets its own server. To let one process
serve every project, add `"share": "user"` to the entry in your own
`mcp.json` (or `termocode mcp add --share user …`):

```json
"notes": { "command": "notes-mcp", "args": ["${userHome}/notes"], "share": "user" }
```

- The server runs in your home folder and gets no project folder, so give
  it its paths in `args`. Do not use `${workspaceFolder}` in such an entry.
- A project file (`.termocode/mcp.json`) can not set `share`.
- Only local servers can be shared, and only while `mcp_share` is on.

## Settings

The MCP section of the Settings modal (**Preferences: Open Settings (UI)**)
has `mcp_enabled`, `mcp_idle_timeout`, `mcp_call_timeout`,
`mcp_max_result_kb`, `mcp_trust_annotations`, `mcp_oauth_port`,
`mcp_share`, `mcp_log_level`, `mcp_tool_search`, `mcp_catalog_url`, `mcp_auto_update` and `mcp_github_client_id` (a GitHub OAuth App for the code sign-in, when
your build has none). The timeouts apply at once. The AI can not change these settings. They are
listed in the settings reference.

## Troubleshooting

Run **AI: MCP: Show Log** (the **MCP** channel of the Output panel). It
shows each start or connect, the server's version and tool count, its
error output, sign-ins, token refreshes, idle stops and failures.
Secrets are hidden.

A server's **Show log** shows only that server's lines; a note row at
the top says so. **AI: MCP: Show Log** shows every server again. Set
`mcp_log_level` (Settings → MCP) to `error`, `warning`, `info` (the
default) or `debug` to hide the lines below that level, such as a
server's debug messages. termocode keeps every line, so a lower level
also shows the older lines.

| You see | Do this |
|---|---|
| `npx not found. Install Node.js …` | Install what the message says (Node.js for `npx`, uv for `uvx`, Docker for `docker`), or set `command` to a full path. termocode checks this before the start; the Local server… preview and `termocode mcp add` warn too. |
| `failed: …` | Fix the cause, then the server → **Retry** (the first action of a failed server). |
| a slow first start fails | Raise `startTimeout` for that server. |
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
| `speaks MCP 2026-07-28 (stateless)` | The server speaks the newest MCP version. Nothing to do: termocode asks each server first and uses the older start for older servers. |
| `the change stream broke` | A new-version server's change list stream ended. termocode opens it again by itself. |
| `tools changed` | Review with **Show tools**, then **Trust the new tools**. |
| `server not trusted` | **AI: MCP Servers…** → the server → **Trust**. |
| A server shows old tools | Run **AI: MCP: Reset Tool Lists**. Each server lists its tools again on next use. A server can say how long its list stays fresh; a list it marks private (only for your sign-in) is never saved on disk. |
| `sandbox: no sandbox tool found` | Install what the message says (bubblewrap, docker or podman), or remove `"sandbox"`. |

### Debug your own server

To step through a server you write, start it with its debugger on and
attach a debugger to it. For a Node.js server, add
`"--inspect=9229"` before the script in `args` (or set
`"env": {"NODE_OPTIONS": "--inspect=9229"}`). Then attach to port 9229.
A Python server can wait for a debugger with `debugpy --listen 5678
--wait-for-client`. **Stop** in the servers list and the next tool call
start it again with your changes.
