package main

import "github.com/amin-jalali/termocode/internal/session"

// serverArg is the hidden first argument of the background session server
// (`termocode __session-server <folder>`), started by the client.
const serverArg = "__session-server"

// wantSession decides whether `termocode` runs the UI in a persistent
// session (see docs/adr/0007-persistent-sessions.md). setting is
// persistent_session from config.json ("auto", "on", "off"; "" = auto).
// auto means on over SSH, where a dropped connection is common, and off
// locally and inside tmux / screen, which already keep programs alive.
func wantSession(setting string, noSession bool, getenv func(string) string, tty bool) bool {
	// Inside a session (e.g. in its terminal panel): run directly, never
	// nest or attach to ourselves.
	if noSession || !tty || getenv(session.EnvSession) != "" {
		return false
	}
	switch setting {
	case "on":
		return true
	case "off":
		return false
	}
	if getenv("TMUX") != "" || getenv("STY") != "" {
		return false
	}
	return getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" || getenv("SSH_TTY") != ""
}

// extractFlag removes every copy of flag from args and reports whether
// there was one.
func extractFlag(args []string, flag string) ([]string, bool) {
	out := args[:0:0]
	found := false
	for _, a := range args {
		if a == flag {
			found = true
			continue
		}
		out = append(out, a)
	}
	return out, found
}
