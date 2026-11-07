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
| `# @aws-sigv4 {{key}} {{$secret AWS_SECRET}} us-east-1 execute-api` | sign the request with AWS Signature V4 (access key, secret key, region, service, optional session token) |
| `# @auth basic USER PASS`, `bearer TOKEN`, `apikey header\|query NAME VALUE`, `digest USER PASS`, `none` | auth helper; values may use `{{vars}}` and `{{$secret name}}`. `"$auth"` in `http-client.env.json` (an env or `$shared`; the nearest folder wins) sets the default; a header you write yourself wins |
| `# @insecure` | skip TLS certificate checks for this request |
| `# @ca-cert ca.pem` | also trust the CAs in this PEM file |
| `# @client-cert me.crt`, `# @client-key me.key` | send a client certificate (mTLS); one PEM file with both works too |

TLS file paths are read from the `.http` file's folder (`~/` is your home folder).
Only your own sends read the TLS lines; the AI's sends never skip checks or use your keys.
An encrypted private key is not supported.

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

### Streamed answers

A streamed answer (Server-Sent Events `text/event-stream`, NDJSON, or
a chunked body without a length) shows live: row 2 says `streaming…`
and the body shows the newest text as it comes (the last 64 KB). The
cursor follows the end while it is on the last row. **Esc** stops the
stream and keeps what came. An event stream waits for its headers up
to the timeout, then runs until the server ends it or the body limit.
When it ends, **Body** lists the events (`name · id`, data below) and
row 2 adds `sse · N events`; **Raw** shows the text as it came.

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
| `{{$datetime "YYYY-MM-DD HH:mm" 1 d}}` | a custom date format (also `$localDatetime`; text in `[ ]` stays as written) |
| `{{$random.email}}`, `{{$random.fullName}}`, `{{$random.firstName}}`, `{{$random.lastName}}`, `{{$random.words 5}}` | fake test data |

Unknown names stay as written and get a wavy underline; sending asks
"Send anyway?". **HTTP: Select Environment…** picks the environment (per
workspace). **HTTP: Open Environment File** and **HTTP: Open Private
Environment File** create the files; the private one is for tokens and
termocode offers to add it to `.gitignore` (it also warns once when git
would commit it).

## Request bodies

| Body | How to write it |
|---|---|
| a file's bytes | the body is one line `< ./photo.png` (the path is next to the `.http` file and must stay in its folder) |
| multipart/form-data | `Content-Type: multipart/form-data; boundary=B`, then the parts; a `< ./file` line inside a part is replaced by the file. Lines are sent with CRLF |
| form fields | `Content-Type: application/x-www-form-urlencoded` and one `key = value` per line (a leading `&` is fine); termocode encodes and joins them. One line is sent as written |
| GraphQL | `GRAPHQL https://…` (or the header `X-Request-Type: GraphQL`), the query, a blank line, then the variables as a JSON object; it is sent as POST JSON |

## MQTT

The send key on an `MQTT` request connects to the broker, subscribes,
publishes and listens, then shows the session as a read-only page.

```http
### kitchen
# @subscribe sensors/#
# @listen 5s
MQTT mqtts://ann:{{mqttPass}}@broker.local/sensors/kitchen

{"temp": 21}
===
second message
```

| Part | Meaning |
|---|---|
| `mqtt://host[:port]/topic` | plain TCP, port 1883; `mqtts://` is TLS on 8883 (`http_tls_insecure` skips the certificate check) |
| user info in the URL | the user name and password (hidden on the page) |
| body | messages published to the URL's topic, split by `===` lines |
| `# @subscribe a/#, b` | topic filters to listen to; with no body and no `@subscribe`, the URL's topic |
| `# @listen 5s` | how long to listen (default 3 s, at most 60 s) |
| `# @retain`, `# @client-id x` | publish as retained; a fixed client ID |

Messages use QoS 0 and MQTT 3.1.1.

## More commands

| Palette | What it does |
|---|---|
| HTTP: Quick Request… | `https://host/path` or `POST https://host/path {"a":1}` with `-H 'Name: v'` |
| HTTP: History… | newest first; Enter sends again with today's environment; rows grouped by day; type to filter; Ctrl+O opens the stored answer; Ctrl+T pins a row (pinned rows stay on top and are never trimmed); Ctrl+S saves it to Saved Requests; Ctrl+D deletes |
| HTTP: Cookies… | the cookies of the active environment (each environment has its own jar); Enter edits the value; Ctrl+D deletes; Clear all cookies… asks first |
| HTTP: Saved Requests… | requests saved with `S` (`~/.config/termocode/http/saved.http`) |
| HTTP: Copy as curl | one line; secrets stay as `{{templates}}` unless you choose "Copy with secrets" |
| HTTP: Open API Docs | the open .http file as a read-only Markdown page: a table of requests, then headers, bodies and variables; secrets are hidden |
| HTTP: Import from curl… | `-X`, `-H`, `-d`, `--json`, `-u`, `-G` … become a new `###` block |
| HTTP: Import OpenAPI… | an OpenAPI 3.x or Swagger 2.0 file (YAML or JSON) becomes `<title>.http` next to it: one `###` block per operation with example bodies; `{{baseUrl}}` goes into a new `http-client.env.json`; put `token` / `apiKey` / `basicAuth` in the private env file |
| HTTP: New .http File… | a file with two example requests |
| HTTP: Fetch GraphQL Schema | sends the introspection query to the GraphQL request at the cursor (same URL and headers) and keeps the schema for this session |
| HTTP: Complete GraphQL Field… | the fields that fit at the cursor, with their types; Enter types the rest of the name. Fetches the schema first when there is none |
| HTTP: Validate GraphQL Query | checks every field of the query against the schema and that the braces match |
| HTTP: GraphQL Schema Docs… | the schema's types (root types first); Enter opens one as read-only SDL |

Settings: `http_timeout`, `http_max_redirects`, `http_max_body_mb`,
`http_cookie_jar`, `http_history_max`, `http_tls_insecure`,
`http_ai_max_kb` (see the settings reference). `HTTP_PROXY`, `HTTPS_PROXY`
and `NO_PROXY` work like in curl. History keeps the request as written
(never the resolved token) and lives in `http/history.jsonl` (mode 0600).

## Run files for CI

`termocode http run` sends every request of `.http` / `.rest` files (or
of every such file below a folder) in order, without the UI, and prints a
report. It exits 0 when all passed, 1 when a request failed.

```sh
termocode http run --env dev --data users.csv --junit report.xml api/
```

| Option | What it does |
|---|---|
| `--env NAME` | the environment from `http-client.env.json` |
| `--iterations N` | run the whole set N times (default once, or once per data row) |
| `--data FILE` | a `.csv` (first line = names) or `.json` (array of objects) file; each iteration uses one row, its values win over `@vars` |
| `--delay 200ms` | wait between two requests |
| `--stop-on-fail` | stop at the first failed request |
| `--json FILE`, `--junit FILE` | also write the report as JSON or JUnit XML |
| `--timeout 10s` | the timeout per request |

A request passes when it was sent and answered with a status below 400.
A placeholder that stays unknown fails the request without sending it.
Reports show the URL as written (`{{base}}/users`), never the resolved
tokens.

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
