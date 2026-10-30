package clistep

import (
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// CS-4: the CLIs' usage-limit messages. They are free text and change
// between versions, so the patterns are loose and only used on the
// CLI's error text (never on an answer that went well).

var limitRe = lazyre.New(`(?i)(usage limit|hit your (usage )?limit|limit reached|rate limit exceeded|quota exceeded|out of (credits|usage))`)

// isLimit reports whether text is a usage-limit message.
func isLimit(text string) bool { return limitRe.MatchString(text) }

var loggedOutRe = lazyre.New(`(?i)(please run /login|not logged in|invalid api key|run .?(claude|codex) (auth )?login|authentication[_ ]error|unauthorized|oauth token (has )?expired)`)

// isLoggedOut reports whether text says the CLI has no (valid) login.
func isLoggedOut(text string) bool { return loggedOutRe.MatchString(text) }

var (
	resetPipeRe = lazyre.New(`limit reached\|(\d{9,})`)
	resetsAtRe  = lazyre.New(`(?i)(?:resets|try again at|reset at|resets at)\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?(?:\s*\(([^)]+)\))?`)
	tryInRe     = lazyre.New(`(?i)(?:try again in|resets in)\s+((?:\d+\s*(?:days?|d|hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)\b[\s,and]*)+)`)
	partRe      = lazyre.New(`(?i)(\d+)\s*(days?|d|hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)\b`)
)

// resetTime reads when a limit resets from its message (zero: it does
// not say): "…|<unix seconds>", "resets 4pm (Europe/Berlin)", "try again
// at 3:05 PM", "try again in 2 hours 5 minutes". A clock time that has
// passed today is tomorrow.
func resetTime(text string, now time.Time) time.Time {
	if m := resetPipeRe.FindStringSubmatch(text); m != nil {
		if sec, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return time.Unix(sec, 0)
		}
	}
	if m := tryInRe.FindStringSubmatch(text); m != nil {
		var d time.Duration
		for _, p := range partRe.FindAllStringSubmatch(m[1], -1) {
			n, _ := strconv.Atoi(p[1])
			switch u := strings.ToLower(p[2]); {
			case strings.HasPrefix(u, "d"):
				d += time.Duration(n) * 24 * time.Hour
			case strings.HasPrefix(u, "h"):
				d += time.Duration(n) * time.Hour
			case strings.HasPrefix(u, "m"):
				d += time.Duration(n) * time.Minute
			default:
				d += time.Duration(n) * time.Second
			}
		}
		if d > 0 {
			return now.Add(d)
		}
	}
	if m := resetsAtRe.FindStringSubmatch(text); m != nil {
		h, _ := strconv.Atoi(m[1])
		mins, _ := strconv.Atoi(m[2])
		switch strings.ToLower(m[3]) {
		case "pm":
			if h < 12 {
				h += 12
			}
		case "am":
			if h == 12 {
				h = 0
			}
		}
		if h > 23 || mins > 59 {
			return time.Time{}
		}
		loc := now.Location()
		if m[4] != "" {
			if l, err := time.LoadLocation(strings.TrimSpace(m[4])); err == nil {
				loc = l
			}
		}
		n := now.In(loc)
		t := time.Date(n.Year(), n.Month(), n.Day(), h, mins, 0, 0, loc)
		if !t.After(n) {
			t = t.AddDate(0, 0, 1)
		}
		return t
	}
	return time.Time{}
}
