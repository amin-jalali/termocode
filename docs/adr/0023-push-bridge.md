# ADR 0023 — Push bridge: a self-hostable APNs / FCM forwarder for opaque wakes

Status: **Proposed** (2026-11-13). Nothing is built, hosted or signed
up for. Building it needs the owner's OK, and an iOS app (MOB-40) needs
an Apple developer account that only the owner can make.

## Context
A paired phone learns that something waits for it (a card, a failed
run) in three ways today (docs/guides/web.md, "Phone pushes"):

- **Watch socket** in the Android app (MOB-24): no push service at all.
- **Web Push** (VAPID, RFC 8291, MOB-30): the laptop posts an encrypted
  count to the browser's own push service. Works for the page on the
  iPhone Home Screen and in Android browsers.
- **UnifiedPush** (MOB-32): the laptop posts the word `wake` to the
  user's own distributor.

A native app shell (the iOS Capacitor app, MOB-40; a Play Store build of
the Android app) cannot use any of these. Apple delivers to an app only
through APNs, and Google Play apps through FCM. Both need a provider key
that belongs to the app's publisher (an APNs `.p8` key for the bundle id,
an FCM service account for the project). That key cannot ship inside
termocode: whoever holds it can push to every install of the app.

Constraints (from ADR 0007, 0013, 0020, 0022 and the push docs):
- No cloud of ours sees content. A push carries no content: a wake, or
  at most a count (`web_push_detail`).
- No accounts for the user: no sign-up, no email, no user id at the
  bridge.
- The laptop never listens on the network; it only dials out.
- A secret lives only in `internal/secrets`, never in argv, env, a log
  or a file of the gateway.
- Pure Go, no cgo. No vendor SDK.
- Pairing, answers and every change stay on the laptop or in the
  panel behind its sign-in. A push has no buttons that allow anything.

Alternatives on the table:
- **No push for the native app.** The iOS app shows waiting work only
  while it is open. Simple; the iPhone keeps Web Push from the Home
  Screen page. Weak: the app is the better client and goes silent.
- **The laptop holds the APNs / FCM key.** Every user would need the
  publisher's key. Refused: one leaked laptop pushes to every phone.
- **A hosted bridge that keeps a user list.** Accounts, a database of
  phones, and a server that knows who uses termocode. Refused.
- **A blind forwarder that holds only the publisher's key** (chosen).

## Decision
- **Shape.** A small Go server, `termocode-pushd` (its own binary, like
  `termocode-relay` in ADR 0022). It has one job: take an opaque wake
  from a laptop and hand it to APNs or FCM for one device token. It
  parses nothing else and keeps nothing on disk.
- **Who runs it.** Whoever publishes the app. The bridge is open
  source, so a user who builds the app with their own bundle id and
  Apple key runs their own bridge with it. A public bridge for the
  store build waits for the owner (hosting, cost, operator), as the
  public relay does.
- **No accounts.** The bridge knows no users. A laptop proves nothing
  about itself; a send is allowed because it carries a valid **send
  ticket** for one device (below). Without a ticket the bridge answers
  `403` and sends nothing.
- **Tickets, not tokens.** On pairing (or when the app gets a new token)
  the app asks the bridge for a ticket: it sends its APNs / FCM token
  and gets back `ticket = AEAD_seal(bridge_key, token ‖ platform ‖
  expiry)` (AES-256-GCM, a key only the bridge holds, in its own
  secret file). The app hands the ticket to the laptop over the
  panel's own link (a `push.subscribe` with a new `kind: "bridge"`, next to `webpush` and `unifiedpush`). The
  laptop keeps the ticket in `internal/secrets` and never sees the raw
  device token. The bridge keeps no list: it opens the ticket on each
  send. A ticket lives 90 days; the app renews it while it runs.
- **The wake.** The laptop posts `POST /v1/wake` with `{ticket, n}`
  where `n` is the count or absent (`web_push_detail: none` sends no
  `n`). The bridge sends:
  - APNs: a background push (`content-available: 1`, no `alert`) or,
    with `n`, an alert whose text is the app's own fixed string and
    `badge: n`. Never a title, path or name from the laptop.
  - FCM: a data message `{"w":"1"}` or `{"w":"1","n":"3"}`, high
    priority, no notification block. The app draws its own text.
  The app wakes, opens its link to the laptop and reads the inbox
  itself, over the tailnet or the relay. The content never travels
  through the bridge or through Apple or Google.
- **What the bridge sees.** The sender's IP address, the time, the
  platform and, while it sends, the device token. Not the laptop's
  name, not the user, not the project, not the content. Its log (JSON
  lines on stderr) holds the time, the platform, the result code and
  the first 8 bytes of SHA-256(ticket); never a token or a ticket. The
  operator keeps it at most 7 days.
- **Limits.** Per ticket: one wake per 30 s (as UnifiedPush today), 200
  a day. Per IP: 20 requests a second. Request body at most 1 KiB. All
  are flags.
- **Gone tokens.** When APNs answers `410` or FCM `UNREGISTERED`, the
  bridge answers the laptop `410`; the laptop drops the ticket, as it
  drops a gone Web Push address today.
- **Laptop side.** A third push kind next to Web Push and UnifiedPush:
  the bridge address comes from the app's `push.subscribe` and must be
  `https` and pass the same dial guard as UnifiedPush (no private
  addresses unless the user set `web_push_hosts`). The wake rule, the
  30 s gap, "live phones are skipped" and `web_push_detail` are the same
  for every kind.
- **Keys.** The publisher's APNs `.p8` key (or FCM service account) and
  the ticket key live in the bridge's own secret file (0600) or its
  OS keyring. APNs uses token auth (ES256 JWT, pure Go); FCM uses the
  HTTP v1 API with an OAuth2 JWT grant (pure Go). No vendor SDK.
- **Kill switch.** Removing the device on the laptop drops its ticket.
  Rotating the ticket key on the bridge voids every ticket at once; apps
  ask for a new one on next start.

## Consequences
- **+** The iOS app and a store Android build can wake without the
  content ever leaving the user's laptop and phone.
- **+** The bridge holds no user list and no content; a broken bridge
  leaks that some phone got a wake, and when.
- **+** Anyone can run it; a user with their own app build needs no
  server of ours.
- **−** Someone has to run a bridge for the store build of the app, and
  hold the publisher's APNs / FCM key. That is the owner's call.
- **−** Apple and Google see that a wake reached a device, and when
  (the same as Web Push today).
- **−** A stolen ticket lets its holder wake that one phone (rate
  limited, no content) until it expires or the device is removed.
- **−** Another small binary to keep updated.
- Open for the owner: OK to build it; who runs the public bridge (domain,
  certificate, cost, the 7-day log); the Apple developer account and
  bundle id (MOB-40); whether a store Android build ships at all (the
  side-loaded app keeps the watch socket and UnifiedPush).
