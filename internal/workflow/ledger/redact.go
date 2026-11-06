package ledger

import (
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/services"
)

// redact hides registered secrets, API keys, bearer tokens, URL
// passwords and secret-looking KEY=value pairs in s.
func redact(s string) string {
	if s == "" {
		return s
	}
	s, _ = services.Redact(ai.Redact(s))
	return s
}

// clean is e with every stored text redacted. A value whose key looks
// secret (token, key, auth, password…) is dropped from the undo args.
func clean(e Entry) Entry {
	e.Target, e.Error, e.ExternalID = redact(e.Target), redact(e.Error), redact(e.ExternalID)
	if e.Undo != nil {
		u := Undo{Title: redact(e.Undo.Title), Tool: redact(e.Undo.Tool)}
		if len(e.Undo.Args) > 0 {
			u.Args = make(map[string]string, len(e.Undo.Args))
			for k, v := range e.Undo.Args {
				if r := redact(k + "=" + v); r != k+"="+v {
					v = "[REDACTED]"
				}
				u.Args[k] = redact(v)
			}
		}
		e.Undo = &u
	}
	return e
}
