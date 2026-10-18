# Test an HTTP API (.http files)

termocode has a built-in HTTP client. It reads the common `.http` file
format, so existing request files work. The request is a `.http` (or `.rest`) file
in the editor; the answer opens as an **HTTP Response** page next to it.
It needs no curl and no browser, so it works over SSH and in Termux.

## Send a request

```http
@host = http://localhost:8080

### Get one user
# @name getUser
GET {{host}}/users/42
Accept: application/json

###
# @name createUser
POST {{host}}/users
Content-Type: application/json

{
  "name": "Ada",
  "email": "ada@example.com"
}
```

- Each request line gets a lens at the end: `▸ send Ctrl+Alt+R`, and after
  a run `· 200 · 118 ms`. Put the cursor in a block and press
  **Ctrl+Alt+R**, or click the lens, or use the ⋮ menu → **Send Request**.
- The text in the editor is sent, also when it is not saved yet.
- The **HTTP Response** page opens in a split on the right when the editor
  is at least 120 columns wide, else in the same window (`q` goes back to
  the `.http` file).
- While a request runs the lens says `sending… Esc cancel`; **Esc**
  cancels it.

The format (same as mobocode, so one file works in both apps):

| Line | Meaning |
|---|---|
| `###` or `### Title` | starts a request; the title names it |
| `# @name x` / `// @name x` | names the request |
| `# comment`, `// comment` | comments |
| `@name = value` | a file variable for `{{name}}` |
| `METHOD URL [HTTP/1.1]` | the request line; a bare URL means GET |
| `Name: value` | headers, until the first blank line |
| the rest of the block | the body (JSON gets `Content-Type: application/json` when you set none) |
| `# @no-redirect`, `# @no-cookie-jar`, `# @timeout 10s` | per-request options |

## The response page

Row 1 is the request, row 2 the status (`200 OK · 118 ms · 1.4 KB · json`),
then the body. **Tab** (or `1` `2` `3`, or a click on the chips) switches
**Body** (pretty JSON, folds with `zc` / `zo`), **Headers** (request and
response headers, redirect hops, timing) and **Raw**. All Vim motions,
`/` and yank work.

| Key | Action |
|---|---|
| `r` / Ctrl+Alt+R | send again |
| `c` / `C` | copy the body / copy as curl |
| `s` / `S` | save the body to a file / save the request to `saved.http` |
| `h` / `e` | history / environment |
| `x` | show secret header values (this response only) |
| `k` | after a TLS error: retry once without TLS checks (this host, this session) |
| `o` | open an image body in the image preview |
| `q` / `Esc` | close (Esc cancels while sending) |

Secret header values (`Authorization`, cookies, `X-Api-Key`, names with
token / secret / password) show as `[REDACTED]`.

## Environments and variables

`{{name}}` is looked up in this order, first hit wins:

| Source | Example |
|---|---|
| file variables | `@host = http://localhost:8080` |
| the selected environment in `http-client.private.env.json`, then `http-client.env.json` (next to the file or in a parent folder up to the workspace root), then their `$shared` | `{"dev": {"host": "…"}}` |
| `{{$dotenv NAME}}` | from `.env` |
| `{{$processEnv NAME}}` | from the environment |
| `{{$secret NAME}}` | from `ai/credentials.json` (the store MCP uses) |
| `{{$guid}}`, `{{$timestamp -1 d}}`, `{{$datetime iso8601}}`, `{{$randomInt 1 10}}` | dynamic values |

Unknown names stay as written and get a wavy underline; sending asks
"Send anyway?". **HTTP: Select Environment…** picks the environment (per
workspace). **HTTP: Open Environment File** and **HTTP: Open Private
Environment File** create the files; the private one is for tokens and
termocode offers to add it to `.gitignore` (it also warns once when git
would commit it).

## More commands

| Palette | What it does |
|---|---|
| HTTP: Quick Request… | `https://host/path` or `POST https://host/path {"a":1}` with `-H 'Name: v'` |
| HTTP: History… | newest first; Enter sends again with today's environment; Ctrl+O opens the stored answer; Ctrl+D deletes |
| HTTP: Saved Requests… | requests saved with `S` (`~/.config/termocode/http/saved.http`) |
| HTTP: Copy as curl | one line; secrets stay as `{{templates}}` unless you choose "Copy with secrets" |
| HTTP: Import from curl… | `-X`, `-H`, `-d`, `--json`, `-u`, `-G` … become a new `###` block |
| HTTP: New .http File… | a file with two example requests |

Settings: `http_timeout`, `http_max_redirects`, `http_max_body_mb`,
`http_cookie_jar`, `http_history_max`, `http_tls_insecure`,
`http_ai_max_kb` (see the settings reference). `HTTP_PROXY`, `HTTPS_PROXY`
and `NO_PROXY` work like in curl. History keeps the request as written
(never the resolved token) and lives in `http/history.jsonl` (mode 0600).

## With the AI

Ask "test createUser and tell me why it returns 500". The agent lists
your requests (`http_list_requests`), sends one (`http_send`), reads the
answer and your code, and may fix the request (`http_save_request`, shown
as a diff you accept). Every AI send also opens on the response page with
a "by AI" tag and lands in history.

| Request | What happens |
|---|---|
| GET / HEAD / OPTIONS to localhost | runs at once |
| other GET / HEAD / OPTIONS | asks; **Always allow http:host** |
| POST / PUT / PATCH / DELETE (localhost too) | asks; **Always allow http:host/write** |
| private, link-local or metadata addresses, `*.internal`, `*.local`, names without a dot | always asks, no "Always allow" |

The host includes the port, so `http:localhost:8080` does not allow
another service on the same machine. AI requests never skip TLS checks,
cannot reach a private address through DNS tricks or redirects, and see
tokens only as `[REDACTED]`. Remove an "Always allow" with
**AI: Agent Auto-Approve…**.

## mobocode

mobocode reads the same files. Until it supports them, keep values in
`http-client.env.json` instead of `@name = value` lines (mobocode reads
those as a request), and note that it leaves `{{$guid}}`-style values as
written.
