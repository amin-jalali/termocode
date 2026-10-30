//go:build unix

package webgw

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// Login serves the passkey sign-in of a paired device (WEB-33):
//
//	POST /login/start  {}                        → {login_id, options}
//	POST /login/finish {login_id, credential}    → {token, role, device}
//
// The token works like one from the launch link: in sessionStorage, sent
// only in the WebSocket hello, gone with the gateway. The role is the
// device's stored role.
type Login struct {
	cfg   LoginConfig
	limit *Bucket
	noisy *Bucket
}

// LoginConfig sets up the login endpoints.
type LoginConfig struct {
	Tokens   *Tokens
	Passkeys *Passkeys
	Audit    *Audit
	Now      func() time.Time
}

// loginRatePerSecond caps /login/* requests for the whole gateway.
const loginRatePerSecond = 10

// NewLogin makes the login endpoints.
func NewLogin(cfg LoginConfig) *Login {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Login{cfg: cfg, limit: NewBucket(loginRatePerSecond, 2*loginRatePerSecond),
		noisy: NewBucket(blockedAuditPerSecond, blockedAuditBurst)}
}

func (l *Login) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := l.cfg.Now()
	if !l.limit.Allow(now) {
		w.Header().Set("Retry-After", "1")
		if l.noisy.Allow(now) {
			_ = l.cfg.Audit.Log(Event{Kind: EvRateLimited, Code: http.StatusTooManyRequests, Op: "login"})
		}
		writeJSON(w, http.StatusTooManyRequests, errorReply("rate_limited"))
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, errorReply("method_not_allowed"))
		return
	}
	if r.URL.RawQuery != "" {
		writeJSON(w, http.StatusBadRequest, errorReply("no_query"))
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, errorReply("need_json"))
		return
	}
	switch r.URL.Path {
	case "/login/start":
		l.start(w, r)
	case "/login/finish":
		l.finish(w, r)
	default:
		writeJSON(w, http.StatusNotFound, errorReply("not_found"))
	}
}

func (l *Login) start(w http.ResponseWriter, r *http.Request) {
	if !decodeStrict(r.Body, &struct{}{}) {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	id, opts, err := l.cfg.Passkeys.BeginLogin()
	if err != nil {
		code := "internal"
		if errors.Is(err, ErrTooManyLogins) {
			code = "busy"
		}
		writeJSON(w, http.StatusServiceUnavailable, errorReply(code))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"login_id": id, "options": opts})
}

type loginFinishReq struct {
	LoginID    string          `json:"login_id"`
	Credential json.RawMessage `json:"credential"`
}

func (l *Login) finish(w http.ResponseWriter, r *http.Request) {
	var req loginFinishReq
	if !decodeStrict(r.Body, &req) || req.LoginID == "" || len(req.Credential) == 0 {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	d, err := l.cfg.Passkeys.FinishLogin(req.LoginID, req.Credential)
	if err != nil {
		why := passkeyWhy(err)
		_ = l.cfg.Audit.Log(Event{Kind: EvLoginFail, Code: http.StatusUnauthorized, Why: why})
		reply := "login_failed"
		switch why {
		case "removed", "clone_suspected", "expired":
			reply = why // the page says what to do
		}
		writeJSON(w, http.StatusUnauthorized, errorReply(reply))
		return
	}
	l.issue(w, d)
}

// issue logs a device in: a token with the device's role.
func (l *Login) issue(w http.ResponseWriter, d webdevices.Device) {
	tok, sess, err := l.cfg.Tokens.Issue(d.ID, sessionRole(d.Role), l.cfg.Now())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorReply("too_many_sessions"))
		return
	}
	_ = l.cfg.Audit.Log(Event{Kind: EvLoginOK, Conn: sess.ID, Device: d.ID, Summary: d.Name + " as " + d.Role})
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "role": d.Role, "device": d.ID,
		"idle_s": int(l.cfg.Tokens.idle / time.Second), "max_s": int(l.cfg.Tokens.max / time.Second)})
}
