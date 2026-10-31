package ai

import (
	"errors"
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
