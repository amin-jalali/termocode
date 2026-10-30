//go:build unix

package webgw

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	limit  *Bucket // every /auth request, before its body is read
	audit  *Audit  // nil: no log

	noisyFail    *Bucket // log lines for repeatable failures
	noisyLimited *Bucket // log lines for 429s

	mu     sync.Mutex
	secret *LaunchSecret // nil: no link is open
}

// AuthRatePerSecond caps /auth requests per second for the whole gateway.
const AuthRatePerSecond = 20

// NewAuth issues tokens from tokens and logs every outcome to audit (nil:
// no log). now is the clock (nil: time.Now).
func NewAuth(tokens *Tokens, now func() time.Time, audit *Audit) *Auth {
	if now == nil {
		now = time.Now
	}
	return &Auth{tokens: tokens, now: now, audit: audit,
		limit:        NewBucket(AuthRatePerSecond, AuthRatePerSecond),
		noisyFail:    NewBucket(blockedAuditPerSecond, blockedAuditBurst),
		noisyLimited: NewBucket(blockedAuditPerSecond, blockedAuditBurst)}
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

// SetSecretDigest opens a new launch link whose secret only the UI knows
// (NewLinkSecret); the old link closes.
func (a *Auth) SetSecretDigest(digest [sha256.Size]byte) {
	s := LaunchSecretFromDigest(digest, a.now())
	a.mu.Lock()
	a.secret = s
	a.mu.Unlock()
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
		a.fail(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if !a.limit.Allow(a.now()) {
		w.Header().Set("Retry-After", "1")
		if a.noisyLimited.Allow(a.now()) {
			_ = a.audit.Log(Event{Kind: EvRateLimited, Code: http.StatusTooManyRequests, Op: "auth"})
		}
		writeJSON(w, http.StatusTooManyRequests, errorReply("rate_limited"))
		return
	}
	if r.URL.RawQuery != "" {
		// A secret in a URL lands in history and logs (T10).
		a.fail(w, http.StatusBadRequest, "no_query")
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		a.fail(w, http.StatusUnsupportedMediaType, "need_json")
		return
	}
	secret, ok := decodeAuth(r.Body)
	if !ok {
		a.fail(w, http.StatusBadRequest, "bad_request")
		return
	}
	now := a.now()
	if err := a.redeem(secret, now); err != nil {
		a.fail(w, http.StatusUnauthorized, redeemWhy(err))
		if errors.Is(err, ErrSecretBurned) {
			_ = a.audit.Log(Event{Kind: EvSecretBurned})
		}
		return
	}
	device, err := deviceID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorReply("internal"))
		return
	}
	tok, sess, err := a.tokens.Issue(device, RoleControl, now)
	if err != nil {
		a.fail(w, http.StatusServiceUnavailable, "too_many_sessions")
		return
	}
	_ = a.audit.Log(Event{Kind: EvAuthOK, Conn: sess.ID, Device: sess.Device, Code: http.StatusOK})
	writeJSON(w, http.StatusOK, authReply{Token: tok,
		IdleS: int(a.tokens.idle / time.Second), MaxS: int(a.tokens.max / time.Second)})
}

// fail answers a refused /auth and logs it as auth_fail with the rule's
// name. Every secret failure gets the same answer, auth_failed; only the
// log tells them apart. Failures anyone can repeat without end pass a
// rate limit on their way to the log; wrong secrets always get logged
// (at most MaxBadTries per secret).
func (a *Auth) fail(w http.ResponseWriter, code int, why string) {
	if why == "wrong" || a.noisyFail.Allow(a.now()) {
		_ = a.audit.Log(Event{Kind: EvAuthFail, Code: code, Why: why})
	}
	reply := why
	if code == http.StatusUnauthorized {
		reply = "auth_failed"
	}
	writeJSON(w, code, errorReply(reply))
}

// redeemWhy is the log name of a Redeem error.
func redeemWhy(err error) string {
	switch {
	case errors.Is(err, ErrSecretWrong), errors.Is(err, ErrSecretBurned):
		return "wrong"
	case errors.Is(err, ErrSecretUsed):
		return "used"
	case errors.Is(err, ErrSecretExpired):
		return "expired"
	default:
		return "closed"
	}
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
