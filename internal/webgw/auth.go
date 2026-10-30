//go:build unix

package webgw

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"
)

// Auth serves POST /auth: the browser trades the one-time launch secret
// (from the URL #fragment) for a session token, which it keeps in
// sessionStorage and sends as the first WebSocket message. No cookie is
// ever set (T4), so no other site can ride on the browser's credentials.
type Auth struct {
	tokens *Tokens
	now    func() time.Time

	mu     sync.Mutex
	secret *LaunchSecret // nil: no link is open
}

// NewAuth issues tokens from tokens. now is the clock (nil: time.Now).
func NewAuth(tokens *Tokens, now func() time.Time) *Auth {
	if now == nil {
		now = time.Now
	}
	return &Auth{tokens: tokens, now: now}
}

// NewSecret opens a new launch link: it makes a fresh one-time secret and
// closes the old one. The plain secret goes to the opener once and is
// not kept.
func (a *Auth) NewSecret() (string, error) {
	plain, s, err := NewLaunchSecret(a.now())
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.secret = s
	a.mu.Unlock()
	return plain, nil
}

// authRequest is the only body /auth reads.
type authRequest struct {
	Secret *string `json:"secret"`
}

// decodeAuth reads exactly one JSON object with a string "secret" and
// nothing else: no other field, no second value, no trailing bytes.
func decodeAuth(body io.Reader) (string, bool) {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req authRequest
	if err := dec.Decode(&req); err != nil || req.Secret == nil {
		return "", false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return "", false
	}
	return *req.Secret, true
}

// authReply is the answer to a good secret.
type authReply struct {
	Token string `json:"token"`
	IdleS int    `json:"idle_s"`
	MaxS  int    `json:"max_s"`
}

func (a *Auth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, errorReply("method_not_allowed"))
		return
	}
	if r.URL.RawQuery != "" {
		// A secret in a URL lands in history and logs (T10).
		writeJSON(w, http.StatusBadRequest, errorReply("no_query"))
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, errorReply("need_json"))
		return
	}
	secret, ok := decodeAuth(r.Body)
	if !ok {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	now := a.now()
	if err := a.redeem(secret, now); err != nil {
		writeJSON(w, http.StatusUnauthorized, errorReply("auth_failed"))
		return
	}
	device, err := deviceID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorReply("internal"))
		return
	}
	tok, _, err := a.tokens.Issue(device, RoleControl, now)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorReply("too_many_sessions"))
		return
	}
	writeJSON(w, http.StatusOK, authReply{Token: tok,
		IdleS: int(a.tokens.idle / time.Second), MaxS: int(a.tokens.max / time.Second)})
}

// redeem uses up the current launch secret with plain.
func (a *Auth) redeem(plain string, now time.Time) error {
	a.mu.Lock()
	s := a.secret
	a.mu.Unlock()
	if s == nil {
		return ErrSecretClosed
	}
	return s.Redeem(plain, now)
}

// deviceID names a local browser in the audit log and the TUI.
func deviceID() (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "loc-" + hex.EncodeToString(b), nil
}

func errorReply(code string) map[string]string { return map[string]string{"error": code} }

// writeJSON answers with v as JSON. v never holds a secret from the
// request.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
