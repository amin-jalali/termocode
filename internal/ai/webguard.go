package ai

import (
	"errors"
	"regexp"
)

// The AI never opens the web panel (ADR 0020, web-easy): the palette ids
// and web_* settings are aiNever, and a shell command it sends that runs
// `termocode web` (start is the default) is refused before it runs.
// Stop, status and help stay allowed.

// webStartRe finds `termocode web` with the word after it, if any.
var webStartRe = regexp.MustCompile("(?:^|[\\s;&|(`/])termocode\\s+web(?:$|[\\s;&|)`])\\s*(\\S*)")

// ErrNoWebPanel is the refusal of a command that would open the web panel.
var ErrNoWebPanel = errors.New("the web panel gives shell access from a browser; only the user opens it (Web: Open in Browser)")

// StartsWebPanel reports whether cmd would run `termocode web` to open
// the web panel.
func StartsWebPanel(cmd string) bool {
	for _, m := range webStartRe.FindAllStringSubmatch(cmd, -1) {
		switch m[1] {
		case "stop", "status", "help", "-h", "--help":
			continue
		}
		return true
	}
	return false
}
