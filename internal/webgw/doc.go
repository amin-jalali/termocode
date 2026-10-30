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
//
// Never do: listen on 0.0.0.0, :: or a LAN address; set a cookie; send a
// CORS header; put a secret or token in a URL, argv, env, log or error.
package webgw
