// Package session keeps termocode alive when the terminal goes away.
//
// `termocode` (the client) runs the real UI inside a PTY owned by a small
// background process (the server), one per workspace folder. The client
// only relays keys, output and the window size over a unix socket, so a
// dropped SSH connection kills the client and nothing else: the UI, nvim,
// terminals and jobs keep running, and the next `termocode` in the same
// folder attaches to them again. See docs/adr/0007-persistent-sessions.md.
package session
