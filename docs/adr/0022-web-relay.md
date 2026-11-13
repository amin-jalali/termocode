# ADR 0022 — Web relay: a blind byte pipe, end-to-end encrypted from QR pairing

Status: **Accepted, pending hosting** (proposed 2026-11-07; built
2026-11-13). The relay, the end-to-end layer and the gateway's side are
built and tested, and anyone can run their own relay. Who runs a public
relay, where and at what cost is still the owner's call: nothing is
hosted, and `web_relay` is empty by default.

## Context
The web panel (ADR 0020) reaches a phone in two ways today: loopback or
an SSH tunnel (v1), and the user's own Tailscale network with passkeys
and QR pairing (v2). Both need setup on the phone. Some users cannot
install a VPN app on a work phone, or are on a network that blocks it.
v3 of ADR 0020 is an end-to-end encrypted relay.

Constraints (from ADR 0007, 0017 and 0020):
- The UI process never listens on the network. The gateway process
  dials out; it never opens a public port.
- The relay must not be able to read or change a session. It sees only
  ciphertext, sizes and timing.
- The relay is never called "end-to-end encrypted" before a test proves
  it sees only ciphertext.
- The web never widens the AI: no pairing, no role change, no secrets
  from the web. Pairing stays laptop-only.
- No new secret is stored outside `internal/secrets`.
- Pure Go, no cgo.

Alternatives on the table:
- **Tailscale only.** Already shipped. No new server, but it needs an
  app and an account on the phone.
- **A public port on the laptop** (port forward, UPnP). Breaks the "no
  listener on the network" rule. Refused.
- **A relay that terminates TLS and proxies HTTP.** Simple, but the
  relay could read and change every byte. Refused.
- **A blind relay with an end-to-end layer from QR pairing** (chosen).

## Decision
- **Shape.** The relay is a small Go server: `internal/relay` and its
  own binary `cmd/termocode-relay`. It pairs two WebSocket connections
  by a random room id and copies binary messages between them. It never
  parses what it copies. Its log (JSON lines on stderr) has connects,
  ends, byte counts and the first 8 bytes of SHA-256(room id); never the
  room id, never a payload; the operator keeps it at most 7 days.
- **Where it lives.** In this repository, as its own small binary, until
  the hosting is decided (the first draft said a separate repository).
  It shares the room-id and URL rules (`relay.CheckURL`, `relay.WSURL`)
  and the page's built files with the panel, so one change and one test
  run cover both ends. It can move to its own repository later without
  a protocol change.
- **Rooms.** The gateway keeps one connection waiting in its room. A
  page that comes pairs with it, the relay sends that gateway the text
  `paired` (the only text it ever sends) and the gateway opens the next
  waiting one. A room holds at most 2 waiting gateway connections and 4
  pairs. A waiting gateway that goes away leaves the room at once.
- **Who dials.** The gateway dials out to the relay (`https` only; plain
  `http` only on loopback, for tests) when `web_relay` is set. The phone
  page dials the same room. Nothing on the laptop listens on the
  network.
- **End-to-end layer (WEB-53).** `Noise_IK_25519_AESGCM_SHA256`
  (`internal/relaytun` on `github.com/flynn/noise`; the page's own
  TypeScript in `src/relay/` on the browser's WebCrypto). The first
  draft named ChaCha20-Poly1305 and BLAKE2s; neither is in WebCrypto,
  so the page would have needed a crypto library of its own. X25519,
  AES-256-GCM and SHA-256 are all in WebCrypto, so the page carries no
  crypto code to review and the private key can stay non-extractable.
  A Go test runs the page's code in node against the Go side.
- **Keys.** The UI makes the gateway's static key and the room id once
  and keeps them in `internal/secrets` (`web.relay.key`). The gateway
  gets them only in a `relay_key` event on its control connection (the
  0600 hub socket): never argv, env, a file of the gateway or a log. The
  QR code (laptop-only) carries the relay address, the room id, the
  gateway's public key and the one-time pairing secret, all in the URL
  fragment, which the browser never sends to the relay. The phone makes
  its own static key on first use and keeps it in IndexedDB as a
  non-extractable CryptoKey. The relay never sees a key.
- **Room id.** 128 random bits, one per laptop identity (not one per
  pairing, as first drafted): a paired phone keeps the room with the
  key and reaches the laptop again later. Knowing the room only lets
  someone knock; the handshake decides. Deleting `web.relay.key` makes
  a new key and room, and every relay phone pairs again.
- **Trust.** After the handshake the gateway knows the page's proven
  key. It lets in a paired, non-guest device bound to that key, or a
  page that brought the open pairing link's secret (checked, not used
  up); every other page is refused before any HTTP. Inside, the
  panel's own rules apply: the pairing card on the laptop with Not now
  first and the 6-digit code, roles, tokens, idle and max time.
- **No passkey over the relay.** A passkey is bound to one origin, and
  the relay's origin is not the laptop's. A relay pairing therefore
  ends without a passkey and binds the device to the phone's relay key
  (`devices.json` `relay_key`, public half only); that key signs it in
  at `/login/relay`, which exists only inside the tunnel.
  `web_require_totp` still asks for the code there (the relay page does
  not show the code step yet: such a phone signs in on the tailnet or
  LAN). Removing the device ends its relay sessions at once.
- **Requests in the tunnel.** Each HTTP request and WebSocket of the
  page is one stream (frames OPEN, DATA, FIN, CLOSE, WINDOW; a 256 KiB
  window per stream; at most 128 streams per session and 4 sessions per
  gateway). The gateway serves them with its own handlers, with the
  site's Host and Origin set and any Cookie or proxy header dropped:
  only our page, holding a proven key, can open a stream.
- **The page.** The relay serves `relay.html` and the panel's built
  files (the same bytes the laptop serves) with the panel's strict CSP
  plus one Trusted Types policy, `relay`, which may load only
  `/assets/app.js`, by its SRI hash. `relay.js` opens the tunnel, puts
  the laptop's own paths (`/auth`, `/ws`, `/watch`, `/pair/`, `/login/`)
  through it and then loads the panel unchanged.
- **Limits.** Per pair: 4 MiB/s, idle 15 minutes, at most 12 hours (the
  same as ADR 0020), messages up to 64 KiB. Per client address: 20 open
  connections and 2 new ones per second (burst 20). In all: 10000
  rooms. All are flags of `termocode-relay`.
- **Proof.** `TestRelayEndToEndAndTheRelaySeesOnlyCiphertext` runs the
  gateway, the relay and a scripted phone, records every byte on the
  relay's sockets and checks that no page, chat line, token, path or
  device name appears in it. It is green, so the docs may say the relay
  cannot read the panel.
- **Kill switch.** Stopping the panel (or the kill switch) ends every
  relay session and the waiting connection. Removing `web_relay` turns
  it off for the next start.

## Answers to the open questions
1. **Hosting.** (b) first: the binary is built so anyone runs their own
   (`termocode-relay -listen 127.0.0.1:8787 -trust-proxy` behind a TLS
   proxy, or `-listen :443 -tls-cert … -tls-key …`). It refuses to
   listen beyond loopback without TLS. (a) or (c), a public relay, waits
   for the owner; nothing is deployed and no account is made.
2. **Cost.** Bounded by the flags above (pace per pair, rooms, pairs per
   room, connections per address). A monthly traffic cap per server is
   left for a public relay, together with its hosting.
3. **Who runs it.** Self-hosted: the user, for themselves. A public
   relay: open, for the owner to decide (domain, certificate, updates,
   abuse reports, the 7-day log).
4. **Default.** Off: `web_relay` is empty until the user sets it in
   their own `config.json` (file only; the AI can never set it).

## Consequences
- **+** A phone works without a VPN app, on any network that allows
  HTTPS.
- **+** The relay holds no keys and no payload, so a broken relay
  leaks sizes and timing, not code.
- **+** The page carries no crypto library: WebCrypto only.
- **−** The relay serves the page's code. A relay operator who changes
  that code could read the panel in that browser: the end-to-end layer
  protects against the network and a passive relay, not against the
  page's own server. Run your own relay, or wait for the app (MOB-42),
  which carries its own code.
- **−** No passkey over the relay (the phone's non-extractable key takes
  its place); the sign-in code step is not in the relay page yet.
- **−** A new crypto dependency (`github.com/flynn/noise`, pure Go).
- **−** Timing and size metadata is visible to the relay.
- Follow-ups: owner OK on a public relay (hosting, cost, operator);
  the sign-in code step in the relay page; MOB-42 (the app's own relay
  transport); share as viewer and phone pushes over the relay.
