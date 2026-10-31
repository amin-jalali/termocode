package ai

import (
	"errors"
	"fmt"
	"strings"
)

// Model fallback (OMX-4). A slot may list backup models
// (ai_models.<slot>.fallback; ai_fallback for main). When a request
// fails before any text and the cause is on the provider's side — a busy
// or broken server after the retries, a usage or credit limit, a key or
// sign-in that is missing or expired — the turn moves to the next model
// and the chat says so. It stays there for the rest of the turn.

// Fallback is one backup model of an Agent.
type Fallback struct {
	Name     string // the slot name shown in the chat ("gpt", "openai/gpt-5")
	Provider Provider
	Model    string // "" = the provider's own model
}

// signInError is a missing or failed sign-in (a token that could not be
// read or refreshed).
type signInError struct{ err error }

func (e signInError) Error() string { return e.err.Error() }
func (e signInError) Unwrap() error { return e.err }

// FallbackWorthy reports whether err should move a turn to the next
// model: busy or broken servers (Retryable, after the retries), 401 /
// 402 / 403 (key, credit, access), no provider or no sign-in. A bad
// request or a stopped turn is not: the next model would fail the same
// way, or the user asked to stop.
func FallbackWorthy(err error) bool {
	if err == nil {
		return false
	}
	if Retryable(err) || errors.Is(err, ErrNotConfigured) {
		return true
	}
	var si signInError
	if errors.As(err, &si) {
		return true
	}
	var se statusError
	if errors.As(err, &se) {
		switch se.code {
		case 401, 402, 403:
			return true
		}
	}
	return false
}

// fallbackReason is the short cause shown in the chat.
func fallbackReason(err error) string {
	var se statusError
	switch {
	case errors.As(err, &se):
		switch se.code {
		case 401:
			return "the key or sign-in was refused (HTTP 401)"
		case 402:
			return "no credit left (HTTP 402)"
		case 403:
			return "access denied (HTTP 403)"
		case 429:
			return "rate or usage limit (HTTP 429)"
		}
		return "the server is busy or down (HTTP " + itoa(se.code) + ")"
	case errors.Is(err, ErrNotConfigured):
		return "it is not set up"
	}
	var si signInError
	if errors.As(err, &si) {
		return "not signed in"
	}
	return strings.TrimSpace(Redact(err.Error()))
}

// MaxFallbacks caps a slot's backup list.
const MaxFallbacks = 4

// CheckFallbacks reports why slot name's backup list is not usable: each
// entry is a slot name or "<provider>/<model>", not the slot itself, at
// most MaxFallbacks.
func CheckFallbacks(name string, list []string) error {
	if len(list) > MaxFallbacks {
		return fmt.Errorf("fallback lists at most %d models", MaxFallbacks)
	}
	for _, f := range list {
		f = strings.TrimSpace(f)
		if _, _, ok := SplitSyntheticSlot(f); !ok && !profileIDRe.MatchString(f) {
			return fmt.Errorf("fallback %q is not a slot name or <provider>/<model>", f)
		}
		if f == name || (name == "" && f == SlotMain) {
			return fmt.Errorf("fallback %q is the slot itself", f)
		}
	}
	return nil
}

// FallbackNames is the backup list of slot name: ai_fallback for main
// ("" too), else ai_models.<name>.fallback; nil when the list is bad
// (ResolveSlots reports it).
func (s Settings) FallbackNames(name string) []string {
	list := s.Models[name].Fallback
	if name == "" || name == SlotMain {
		name, list = SlotMain, s.Fallback
	}
	if CheckFallbacks(name, list) != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, f := range list {
		out = append(out, strings.TrimSpace(f))
	}
	return out
}
