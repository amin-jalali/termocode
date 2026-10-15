# Connect MCP servers

MCP (Model Context Protocol) servers give the AI chat more tools: a
database, a browser, your notes folder, GitHub and many more. termocode
reads the same `mcpServers` block as Claude Desktop, Claude Code and
Cursor, so you can paste the config you already have.

!!! note "Local servers only, for now"
    This version runs **local** servers (a program that termocode starts,
    "stdio"). Remote servers with a URL (`"type": "http"`, Linear, Slack,
    the GitHub remote server …) are listed but do not connect yet.

## Add a server

1. Press `F1` and run **Open Config: MCP Servers**. It opens
   `~/.config/termocode/mcp.json` (created when missing).
2. Paste a server and save:

    ```json
    {
      "mcpServers": {
        "notes": {
          "command": "npx",
          "args": ["-y", "@modelcontextprotocol/server-filesystem", "${userHome}/notes"]
        }
      }
    }
    ```

3. Ask the chat something that needs the server ("list my notes").
   The next chat message uses the new file; no restart is needed.

Run **AI: MCP Servers…** to see every server and its state, for example
`○ notes   local · stopped · 11 tools`. Press `Enter` on a server for
**Connect / Refresh tools**, **Show tools**, **Stop**, **Trust**,
**Edit in mcp.json** and **Show log**.

## The file

| Key | What it does |
|---|---|
| `command`, `args` | The program to start and its arguments. |
| `env` | Extra environment variables for the server. |
| `cwd` | Folder to start it in. |
| `disabled` | `true` hides the server. |
| `readOnly` | `true` offers only the tools the server marks read-only. |
| `autoApprove` | Tool names that run without asking (`["list_issues"]`). |
| `deny` | Tool names (or patterns like `"delete_*"`) never offered to the AI. |
| `timeout` | Seconds one tool call may take (default `mcp_call_timeout`, 60 s). |

`type` and Claude Desktop's `transport` are read too. `readOnly`,
`autoApprove`, `deny` and `timeout` are termocode keys: other apps ignore
them, so the same file still works there.

### Values with `${…}`

| Write | Gets |
|---|---|
| `${VAR}` | the environment variable `VAR` |
| `${VAR:-default}` | `VAR`, or `default` when it is empty |
| `${secret:NAME}` | `mcp_secrets.NAME` in `~/.config/termocode/ai/credentials.json` |
| `${workspaceFolder}` | the open workspace folder |
| `${userHome}` | your home folder |

Do not put tokens in `mcp.json`. Put them in `credentials.json` (mode
`0600`) and use `${secret:NAME}`:

```json
{ "mcp_secrets": { "pg_uri": "postgres://me:pass@localhost/db" } }
```

termocode warns in the MCP log when a value in `mcp.json` looks like a
token. When a variable or secret is missing, the server shows
`needs ${VAR}` and does not start.

### Project servers

A workspace can have its own `.termocode/mcp.json`. Its servers replace
yours with the same name, and `"disabled": true` hides one of yours in
that project.

A cloned repository could put any command in that file, so termocode asks
before it starts a project server: **Allow once**, **Always allow** or
**Deny**. The dialog shows the command and the names (not the values) of
its environment variables. **Always allow** is stored per entry; when the
entry changes, termocode asks again. Servers in your own `mcp.json` do
not ask.

## What the AI may do

Each MCP tool shows up as `mcp__<server>__<tool>`; the dialogs and the
transcript show `mcp.<server>.<tool>`.

- A tool the server marks read-only runs at once.
- Any other tool asks first. **Always allow mcp:<server>** stops the
  questions for that server (change it in **AI: Agent Auto-Approve…**).
- A tool whose name says delete, remove, archive, destroy, purge or drop
  always asks.
- Set `"mcp_trust_annotations": false` in `config.json` to make every MCP
  tool ask (except the ones in a server's `autoApprove`).

The server's marks are hints, so review a new server with **Show tools**.
Tool results are cleaned, cut at `mcp_max_result_kb` (32 KB) and marked
as outside data for the model.

## Resources and privacy

- Nothing starts with termocode. A server starts on the first tool call,
  or once to list its tools when there is no saved list yet
  (`~/.cache/termocode/mcp/`).
- It stops after `mcp_idle_timeout` (10 minutes) without a call, and when
  termocode quits. `"0"` keeps it running.
- A server gets only `PATH`, `HOME`, `USER`, `LANG`, `TERM`, `TMPDIR`,
  `XDG_*` and the `env` you set: never your AI API keys.
- Servers can not use your AI model (sampling is refused).

## Troubleshooting

Run **AI: MCP: Show Log** (the **MCP** channel of the Output panel). It
shows each start, the server's version and tool count, its error output,
idle stops and failures. Secrets are hidden.

| You see | Do this |
|---|---|
| `failed: npx not found` | Install Node.js, or set `command` to a full path. |
| `needs ${GITHUB_PAT}` | Export the variable, or use `${secret:…}`. |
| `no answer in 60s` | Raise `timeout` for that server. |
| `remote (http) servers are not supported yet` | Use the server's local (stdio) version for now. |
| `server not trusted` | **AI: MCP Servers…** → the server → **Trust**. |

The `mcp_*` settings are listed in the settings reference.
