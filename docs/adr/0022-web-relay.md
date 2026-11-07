# ADR 0022 — Web relay: a blind byte pipe, end-to-end encrypted from QR pairing

Status: **Proposed** (2026-11-07) — waits for the owner's OK on hosting,
cost and who runs it. No relay code is written before that OK.

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
- **Shape.** The relay is a small Go server in its own repository
  (WEB-52). It pairs two WebSocket connections by a random room id and
  copies bytes between them. It never parses what it copies. It keeps
  no log of payloads; it logs only connect, disconnect, room id hash and
  byte counts, kept 7 days.
- **Who dials.** The gateway dials out to the relay over TLS when the
  user turns on **Web: Use Relay**. The phone page dials the same room.
  Nothing on the laptop listens on the network.
- **End-to-end layer (WEB-53).** Noise `IK` with X25519, ChaCha20-Poly1305
  and BLAKE2s. The gateway's static key lives in `internal/secrets`. The
  QR code (laptop-only, as in v2 pairing) carries the relay address, the
  room id and the gateway's public key. The phone makes its own static
  key on first pairing and keeps it in IndexedDB as a non-extractable
  key where the browser allows it. The relay never sees a key.
- **Trust.** A phone that knows the gateway key but is not paired is
  refused by the gateway after the Noise handshake, before any panel
  data. Passkeys and roles from v2 still apply inside the tunnel.
- **Limits.** Per room: 2 peers, 4 MiB/s, idle 15 minutes, at most 12
  hours (the same as ADR 0020). Per IP: 20 rooms. A room id is 128
  random bits and is used once per pairing.
- **Proof.** A test runs the gateway, the relay and a scripted phone,
  records every byte the relay copies, and checks that no plaintext
  marker (a file name, a chat line, a terminal line) appears in it.
  Until that test is green, the UI calls it "relay (preview)", never
  "end-to-end encrypted".
- **Kill switch.** **Web: Stop Relay** drops the room at once. Turning
  the web panel off closes it too.

## Open questions for the owner
1. **Hosting.** (a) We run one public relay for everyone; (b) only a
   self-hosted relay: users run the binary themselves; (c) both, with
   (b) always possible. Recommended: (c), but start with (b) so no
   public service is needed for the first release.
2. **Cost.** A blind relay uses little CPU; the cost is traffic. A
   terminal session is mostly small text, so a small server goes far.
   Recommended: a hard monthly traffic cap per room and per server, and
   the relay refuses new rooms when the cap is near.
3. **Who runs it.** Who owns the domain, the TLS certificate, updates,
   abuse reports and the 7-day connection logs. Recommended: the owner
   for the public relay; nobody for self-hosted.
4. **Default.** Recommended: off. The user turns it on per workspace.

## Consequences
- **+** A phone works without a VPN app, on any network that allows
  HTTPS.
- **+** The relay holds no keys and no payload, so a broken relay
  leaks sizes and timing, not code.
- **−** A second repository and, for (a), a public service to run.
- **−** A new crypto dependency (a pure-Go Noise library) and a matching
  one in the page bundle; both need review.
- **−** Timing and size metadata is visible to the relay.
- Follow-ups: WEB-52 (relay server) and WEB-53 (end-to-end layer) wait
  for this ADR to be accepted.
