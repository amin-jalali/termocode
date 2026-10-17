# MCP sign-in: one-time setup for the maintainer

Users never do these steps. They are done once by whoever ships
termocode, so that **Connect a service** can sign people in without
any setup on their side.

## 1. GitHub OAuth App (the code sign-in)

GitHub's MCP server does not let unknown apps register themselves, and
GitHub's browser sign-in always needs a client secret, which an
open-source program can not keep. The device flow ("type this code on
github.com") needs only a public client id. termocode ships that id.

Until this is done, `GitHubClientID` is empty: the wizard skips the code
step and says so; `gh` and tokens still work.

1. Sign in to GitHub with the account (or organisation) that should own
   the app. For an organisation: **Organization → Settings → Developer
   settings → OAuth Apps → New OAuth App**. For a personal account open
   <https://github.com/settings/applications/new>.
2. Fill in:
   - **Application name:** `termocode`
   - **Homepage URL:** `https://amin-jalali.github.io/termocode/`
   - **Application description:** `Terminal IDE: lets its AI chat use the
     GitHub MCP server.`
   - **Authorization callback URL:** `http://127.0.0.1/callback` (the
     device flow does not use it; GitHub only needs a value).
3. Tick **Enable Device Flow**.
4. **Register application.**
5. Copy the **Client ID** (it starts with `Ov23li…`). Do **not** create a
   client secret: termocode never uses one, and none may be put into the
   repository or a build.
6. Put the id into the release build:

   ```sh
   go build -ldflags "-X github.com/amin-jalali/termocode/internal/mcp.GitHubClientID=Ov23li…" ./cmd/termocode
   ```

   (or set the default of `GitHubClientID` in
   `internal/mcp/presets.go` and commit it: the id is public, like the
   ones gh and VS Code ship). Users can also set it themselves with the
   setting `mcp_github_client_id`.
7. Optional: upload a logo on the app page, so the GitHub consent page
   looks familiar.
8. Check by hand (manual checklist **I4**): without `gh`, connect GitHub,
   type the code on github.com, Authorize, Done.

Notes:

- Organisations with "OAuth app access restrictions" must approve the
  app once (their owners see a request when a member signs in).
- GitHub allows 50 device codes per hour per app.
- The tokens of an OAuth App do not expire; Sign out forgets them in
  termocode (users revoke them at
  <https://github.com/settings/applications>).

## 2. Publish the client metadata document

`docs/mcp/client.json` is termocode's OAuth Client ID Metadata Document.
Services that support it (Linear, Notion, Sentry, Atlassian) take its
URL as the client id, so termocode does not have to register at each
service. It must be live at
`https://amin-jalali.github.io/termocode/mcp/client.json` (published with
the docs site) and list exactly the redirect URIs of the sign-in ports
47391–47395 (a test checks the file).

## 3. Optional: a GitLab app for the code sign-in

gitlab.com lets termocode register itself for the browser sign-in, but
the code sign-in (useful over SSH) needs a registered app:

1. <https://gitlab.com/-/user_settings/applications> → **Add new
   application**.
2. Name `termocode`, redirect URI `http://127.0.0.1:47391/callback`,
   **Confidential: off**, scope `mcp` (GitLab 17.9 and later accept the
   device authorization grant for such a public app).
3. Ship its Application ID for gitlab.com users (self-managed GitLab
   users register their own and put the id into their entry's
   `oauth.client_id`).

## 4. Later: a Slack Marketplace app

Slack's MCP server only accepts registered apps. Until termocode has a
published Slack app, each workspace creates one from the manifest the
wizard copies (about 2 minutes). With a Marketplace app (Slack reviews
it) termocode could ship its client id and fixed port, and Slack would
become one click like the others.
