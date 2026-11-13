package webpush

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// What a push says (web_push_detail).
const (
	DetailNone  = "none"  // nothing: the phone shows "Something needs you"
	DetailCount = "count" // how many things wait (the default)
	DetailTitle = "title" // the count and the kind of the newest one
)

// Details are the web_push_detail choices.
var Details = []string{DetailNone, DetailCount, DetailTitle}

// Kinds is the closed list of kinds a push may name; the service worker
// has one fixed text for each. They are the card kinds of the web.
// done and failed are not cards: an AI run or a task ended (CMP-47).
var Kinds = []string{"tool", "mcp", "plan", "review", "trust", "workflow", "done", "failed"}

// Payload is the plaintext of a push: nil for DetailNone, else {"n":N}
// and, for DetailTitle, "k" with kind when it is on the closed list. An
// unknown detail is DetailCount.
func Payload(detail string, n int, kind string) []byte {
	return PayloadFor(detail, n, kind, "")
}

// MaxSession is the longest session name a push carries, in characters.
const MaxSession = 40

// cleanSession keeps letters, digits, spaces and . _ - of a session
// name, at most MaxSession characters: no path, quote or command.
func cleanSession(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= MaxSession {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			n++
		}
	}
	return strings.TrimSpace(b.String())
}

// PayloadFor is Payload that also names the session (the folder the
// panel runs in) for DetailTitle, as "s": the phone shows it in the
// title of the notification. Count and none never carry it.
func PayloadFor(detail string, n int, kind, session string) []byte {
	if detail == DetailNone {
		return nil
	}
	n = max(0, min(n, 999))
	out := `{"n":` + strconv.Itoa(n)
	ended := kind == "done" || kind == "failed" // nothing waits: say so at count too
	if (detail == DetailTitle || ended) && slices.Contains(Kinds, kind) {
		out += `,"k":"` + kind + `"`
	}
	if detail == DetailTitle {
		if c := cleanSession(session); c != "" {
			out += `,"s":"` + c + `"`
		}
	}
	return []byte(out + "}")
}
