# ADR 0020 — Web UI: a second UI over one structured WebSocket

Status: **Accepted** (2026-10-30)

## Context
Users want to reach their running termocode from a phone or another
browser: answer an AI card, see what the AI changed, watch a build, fix
one line, commit. The laptop must keep working at the same time.

Constraints:
- The UI process never listens on the network (ADR 0007, ADR 0017).
  A bug in HTTP code must not take the editor down.
- The state lives in the UI process, on one goroutine (Bubble Tea).
  Buffers and shells live in the one embedded nvim.
- A browser that can type into a shell is as strong as the user's
  account. Every new door needs a clear trust line.
- A phone screen is small and the link is often weak.
- No node in the build and in CI.

Alternatives on the table:
- **Mirror the terminal in the browser** (send the PTY bytes to a
  terminal widget). Cheap, but the phone gets a laptop-sized screen it
  cannot use, and every view would be a picture of text.
- **An API in the session server** (ADR 0007). It sees PTY bytes only:
  no buffers, no chats, no cards.
- **A daemon with its own copy of the state.** It could not answer a
  card and would drift from the editor.
- **HTTP and WebSocket inside the UI process.** It knows everything,
  but it breaks the "UI process never listens on the network" rule.
- **A hub in the UI process on a private unix socket, plus a separate
  gateway process for the network** (chosen).

## Decision
- **A second, real UI.** The browser draws its own HTML layout at its
  own size. It shows the same live termocode: the same nvim buffers,
  shells, chats and cards. There are never two copies of anything.
- **The hub** lives in the UI process on `<key>.web.sock` in the session
  folder (mode `0600`, folder `0700`, same-user peer check, at most 16
  connections). This is the trust line of ADR 0017. Every browser
  request becomes a function that runs on Update, like an MCP call.
  State changes the UI already handles are published as small deltas.
  Publishing never blocks; a slow browser gets a `resync`, never a
  stalled editor.
- **The gateway** is a separate process (`termocode __web-gateway`).
  It does HTTP, auth, tokens, rate limits, the audit log and the
  WebSocket. It opens one hub connection per browser. If it dies, the
  editor does not notice.
- **One WebSocket, one typed protocol** (`internal/webproto`).
  Subprotocol `termocode.v2`, text frames, one JSON object each.
  Requests carry ids; topics carry sequence numbers; the server sends
  snapshots and deltas. The decoder for browser messages is strict
  (unknown fields are refused, size is capped); browser-side readers
  ignore unknown fields. Golden files and a generated TypeScript type
  file keep Go and the browser in step.
- **One policy table** (`internal/webproto/ops.go`): one row per request
  (op, args type, role, rate, size, laptop card, audit, phase). The
  gateway, the hub, the docs and the tests all read it. An op without a
  row fails a test.
- **No dynamic code.** No message can carry Lua, an RPC name, a command
  line or a key sequence for nvim. Ops are fixed functions with typed
  arguments. Paths are workspace-relative and checked by the workspace
  resolver.
- **The web never widens the AI.** No "Always allow", no trust, no
  pairing, no role change, no settings change, no secrets from the web.
  The AI cannot start, stop or talk to the web side. The hub never
  imports `internal/secrets`.
- **Both at the same time.** The browser never resizes the TUI, nvim
  windows or PTYs. Buffers are shared; a web edit carries nvim's
  `changedtick` and a conflict changes nothing. Cards have one id; the
  first answer wins. Terminal keys from both sides arrive in order.
  Opening a file on the phone does not move the laptop.
- **Access, in phases.** v1: loopback and SSH, a one-time secret in the
  URL fragment, then a token in `sessionStorage`; no cookies. v2: the
  user's own Tailscale network with passkeys and QR pairing. v3: an
  end-to-end encrypted relay. The relay is never called end-to-end
  encrypted before a test proves it sees only ciphertext.
- **Limits.** Idle 15 minutes, at most 12 hours, at most 4 viewers,
  paste and clipboard at most 1 MiB. There is no default key for the
  kill switch; the user can bind one.
- **Frontend.** Preact with `@preact/signals`, TypeScript, built by the
  esbuild Go API (no node). No `innerHTML`; DOM from text only; chat
  markdown arrives as a syntax tree, never as HTML. The server side
  uses a small, proven Go WebSocket library with compression off.

Answers to the open design questions (the recommended default each):
1. **v1 ships in two halves.** First chat, cards, file reading and
   terminals; then editing, git, search, tasks and SSH.
2. **Editing in v1 is an edit sheet** (a few lines in a text box). A
   real in-page editor comes in v2, built on CodeMirror 6, if the page
   stays inside its size budget.
3. **Only destructive or trust actions wait for a tap on the laptop**
   (delete, discard, close a shell, remove a container). Pairing, roles
   and "Always allow" are laptop-only. Commits, task runs and staging
   get an in-page confirm.
4. **Terminals are plain text in v1.** Full-screen programs show as a
   fixed text grid with an "Open on laptop" button. Colours are best
   effort in v2.
5. **The safe answer comes first** on every card and has focus. Risky
   buttons arm after 600 ms.
6. **On a wide screen the chat is pinned** on the right and can be
   collapsed.
7. **Swipe to stage stays**, always next to a button that does the same.
8. **A light theme ships with the first half of v1.**

## Consequences
- **+** The phone gets views made for a phone, not a shrunken laptop.
- **+** The network code runs in its own process; the editor keeps the
  "no listener on the network" rule.
- **+** One table drives the gateway checks, the hub checks, the
  generated TypeScript types and the docs, so they cannot drift.
- **−** Much more work than a mirror: every view has a second data
  contract, a frontend and its own security tests.
- **−** Terminals have one width, the laptop's. The phone wraps the rows
  again; there is no true reflow.
- **−** A process of the same user can talk to the hub socket. This is
  accepted: it can already act as the user (same line as ADR 0017).
- Follow-ups: the relay needs its own design and a hosting decision
  before any code.
