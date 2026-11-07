package webpush

import (
	"slices"
	"strconv"
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
var Kinds = []string{"tool", "mcp", "plan", "review", "trust", "workflow"}

// Payload is the plaintext of a push: nil for DetailNone, else {"n":N}
// and, for DetailTitle, "k" with kind when it is on the closed list. An
// unknown detail is DetailCount.
func Payload(detail string, n int, kind string) []byte {
	if detail == DetailNone {
		return nil
	}
	n = max(0, min(n, 999))
	out := `{"n":` + strconv.Itoa(n)
	if detail == DetailTitle && slices.Contains(Kinds, kind) {
		out += `,"k":"` + kind + `"`
	}
	return []byte(out + "}")
}
