package main

import (
	"errors"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/session"
)

// serverArg is the hidden first argument of the background session server
// (`termocode __session-server <folder>`), started by the client.
const serverArg = "__session-server"

// envNoSession keeps --no-session across Reload Window / Open Recent
// Workspace, which re-exec termocode without its flags.
const envNoSession = "TERMOCODE_NO_SESSION"

// wantSession decides whether `termocode` runs the UI in a persistent
// session (see docs/adr/0007-persistent-sessions.md). setting is
// persistent_session from config.json ("auto", "on", "off"; "" = auto).
// auto means on over SSH, where a dropped connection is common, and off
// locally and inside tmux / screen, which already keep programs alive.
func wantSession(setting string, noSession bool, getenv func(string) string, tty bool) bool {
	// Inside a session (e.g. in its terminal panel): run directly, never
	// nest or attach to ourselves.
	if noSession || !tty || getenv(session.EnvSession) != "" || getenv(envNoSession) != "" {
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

// attachRefusal returns why this terminal must not attach to the session
// with key, or "" when it may. Inside a session (a shell in its terminal
// panel) attaching to the same session would show the session inside
// itself, and every key would loop back to it. Attaching to another
// folder's session would nest one session in another, so it is refused
// too: detach first, then attach from the outer terminal.
func attachRefusal(getenv func(string) string, key string) string {
	inside := getenv(session.EnvSession)
	switch {
	case inside == "":
		return ""
	case inside == key:
		return "termocode: already inside this session (this is its terminal panel)"
	default:
		return "termocode: cannot attach from inside another session; detach it first (Session: Detach)"
	}
}

// clientOpts is how runClient attaches.
type clientOpts struct {
	// create starts the session when none runs.
	create bool
	// link: the client runs at the far end of `termocode ssh` (always in
	// a session; it answers the heartbeat).
	link bool
	// note is shown by the UI after the attach ("Reconnected after 12 s").
	note string
}

// linkArgs removes the flags `termocode ssh` gives the termocode it starts
// on the far side: --link, and on a reconnect --reconnected=<time away>
// with ",dropped" when keys typed offline were dropped. It returns the
// note the UI shows.
func linkArgs(args []string) (rest []string, link bool, note string) {
	rest, link = extractFlag(args, "--link")
	out := rest[:0:0]
	for _, a := range rest {
		v, ok := strings.CutPrefix(a, "--reconnected=")
		if !ok {
			out = append(out, a)
			continue
		}
		d, dropped := v, false
		if s, ok := strings.CutSuffix(v, ",dropped"); ok {
			d, dropped = s, true
		}
		away, err := time.ParseDuration(d)
		if err != nil {
			away = 0
		}
		note = session.ReconnectNote(away, dropped)
	}
	return out, link, note
}

// sshCommandArgs splits `termocode ssh [--dir DIR] [ssh args] destination`
// into the far folder and the arguments for ssh.
func sshCommandArgs(args []string) (dir string, sshArgs []string, err error) {
	for len(args) > 0 {
		if v, ok := strings.CutPrefix(args[0], "--dir="); ok {
			dir, args = v, args[1:]
			continue
		}
		if args[0] == "--dir" && len(args) > 1 {
			dir, args = args[1], args[2:]
			continue
		}
		break
	}
	if len(args) == 0 {
		return "", nil, errors.New("usage: termocode ssh [--dir DIR] [ssh options] destination")
	}
	return dir, args, nil
}
