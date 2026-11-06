package ledger

import (
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/services"
)

// Redacted stands in for a value that held a secret.
const Redacted = "[REDACTED]"

// redact hides registered secrets, API keys, bearer tokens, URL
// passwords and secret-looking KEY=value pairs in s.
func redact(s string) string {
	if s == "" {
		return s
	}
	s, _ = services.Redact(ai.Redact(s))
	return s
}

// clean is e with every stored text redacted. An undo arg that held a
// secret (its key looks secret, or part of its value is one) is dropped
// whole: an undo must never run with a half-hidden value.
func clean(e Entry) Entry {
	e.Target, e.Error, e.ExternalID = redact(e.Target), redact(e.Error), redact(e.ExternalID)
	if e.Undo != nil {
		u := Undo{Title: redact(e.Undo.Title), Tool: redact(e.Undo.Tool)}
		if len(e.Undo.Args) > 0 {
			u.Args = make(map[string]string, len(e.Undo.Args))
			for k, v := range e.Undo.Args {
				if redact(k+"="+v) != k+"="+v || redact(v) != v {
					v = Redacted
				}
				u.Args[k] = v
			}
		}
		e.Undo = &u
	}
	return e
}
