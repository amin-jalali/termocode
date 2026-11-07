//go:build unix

// Package webgw is the web gateway: the separate process that serves the
// browser UI over HTTP and one WebSocket, so the UI process never listens
// on the network (ADR 0007, 0017). It owns everything a browser can reach:
// where it listens, which requests get through, the security headers, the
// launch secret and session tokens, rate limits and the audit log.
//
// Threat model (summary; the full table is in the web design notes):
//   - DNS rebinding (T1): every request needs an exact Host from the allow
//     list, else 421.
//   - Cross-site WebSocket and CSRF (T2, T3): no cookies ever; POST and
//     WebSocket upgrades need an exact same-origin Origin header, else 403.
//   - Clickjacking, sniffing, Referer leaks (T5, T6): fixed security
//     headers and a strict CSP on every response, errors included.
//   - LAN exposure (T15): v1 binds 127.0.0.1 or a 0600 unix socket whose
//     peer must run as our uid; wildcard and LAN addresses are refused.
//   - Slow or many connections (T17): header, body and idle timeouts, an
//     8 KiB header cap, a 4 KiB body cap and at most 32 open connections.
//   - The door to other projects (MOB-56, mobile plan §3.7): the paired
//     gateway joins another workspace's hub only over that workspace's
//     session socket (session.PathsFor(ws).WebSocket, 0600, peer = our
//     uid), never over the network. The phone names a project by its id
//     from the known list, never by a path. A project that is not
//     running starts headless only after a card on the laptop (Not now
//     first); the AI can never open one. At most web_projects_max hubs
//     at once and an idle stop (web_project_idle_minutes) for the ones
//     the phone started; each hub has its own connection, timeouts and
//     close code, so one stuck hub cannot stall the others. One pairing
//     and one role cover every project; a viewer stays a viewer in all.
//
// Never do: listen on 0.0.0.0, :: or a LAN address; set a cookie; send a
// CORS header; put a secret or token in a URL, argv, env, log or error.
package webgw
