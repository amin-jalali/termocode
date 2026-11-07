//go:build unix

package webgw

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"sync"
	"sync/atomic"
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

	totpOn atomic.Bool // RequireTOTP, changeable while serving

	mu      sync.Mutex
	tickets map[string]*totpTicket
	// appChallenges: app key sign-ins waiting for their signature (MOB-27).
	appChallenges map[string]*appChallenge
}

// SetRequireTOTP turns the sign-in code step on or off for the next
// sign-ins.
func (l *Login) SetRequireTOTP(on bool) { l.totpOn.Store(on) }

// With TOTP on (WEB-48), a good passkey gives a ticket instead of a
// token, and the token comes with the code:
//
//	POST /login/totp {ticket, code} → {token, role, device}
//
// The laptop checks the code against the seed in its secret store.
type totpTicket struct {
	dev   webdevices.Device
	until time.Time
	tries int
	// remember: the token comes with a (new) bookmark key for origin.
	remember bool
	origin   string
}

// TOTP ticket limits.
const (
	TOTPTicketTTL = 2 * time.Minute
	MaxTOTPTries  = 5
	maxTickets    = 16
)

// LoginConfig sets up the login endpoints.
type LoginConfig struct {
	Tokens   *Tokens
	Passkeys *Passkeys
	Audit    *Audit
	Now      func() time.Time
	// Store is the device list (nil: the passkeys' one); bookmark keys
	// live there as hashes (WEBP-12).
	Store *webdevices.Store
	// RequireTOTP asks for a one-time code after the passkey; CheckTOTP
	// asks the laptop to check it for a device (nil error: right).
	RequireTOTP bool
	CheckTOTP   func(ctx context.Context, device, code string) error
	// AppKeys turns on /login/app/* (web_app_key_login, MOB-27): the
	// Android app signs in with its keystore key.
	AppKeys bool
}

// loginRatePerSecond caps /login/* requests for the whole gateway.
const loginRatePerSecond = 10

// NewLogin makes the login endpoints.
func NewLogin(cfg LoginConfig) *Login {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	l := &Login{cfg: cfg, limit: NewBucket(loginRatePerSecond, 2*loginRatePerSecond),
		noisy: NewBucket(blockedAuditPerSecond, blockedAuditBurst), tickets: map[string]*totpTicket{},
		appChallenges: map[string]*appChallenge{}}
	l.totpOn.Store(cfg.RequireTOTP)
	return l
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
	case "/login/start", "/login/finish":
		if l.cfg.Passkeys == nil {
			writeJSON(w, http.StatusNotFound, errorReply("no_passkeys"))
			return
		}
		if r.URL.Path == "/login/start" {
			l.start(w, r)
		} else {
			l.finish(w, r)
		}
	case "/login/totp":
		l.totp(w, r)
	case "/login/device":
		l.device(w, r)
	case "/login/forget":
		l.forget(w, r)
	case "/login/app/start", "/login/app/finish":
		if !l.cfg.AppKeys {
			writeJSON(w, http.StatusNotFound, errorReply("not_found"))
			return
		}
		if r.URL.Path == "/login/app/start" {
			l.appStart(w, r)
		} else {
			l.appFinish(w, r)
		}
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
	// Remember: "Remember this browser" was ticked (WEBP-12).
	Remember bool `json:"remember,omitempty"`
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
	origin := ""
	if req.Remember {
		origin = r.Header.Get("Origin")
	}
	if l.totpOn.Load() {
		tk, err := l.newTicket(d, req.Remember, origin)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, errorReply("busy"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"totp": true, "ticket": tk})
		return
	}
	l.issueRemember(w, r, d, req.Remember, origin)
}

// issueRemember issues a token and, when asked, a new bookmark key. A key
// that cannot be made leaves the sign-in good, without a key.
func (l *Login) issueRemember(w http.ResponseWriter, r *http.Request, d webdevices.Device, remember bool, origin string) {
	key := ""
	if remember {
		key, _ = rememberDevice(l.store(), d.ID, origin, l.cfg.Now())
	}
	l.issueWith(w, r, d, key, "passkey")
}

// newTicket keeps a device that passed its passkey until its code comes.
func (l *Login) newTicket(d webdevices.Device, remember bool, origin string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	now := l.cfg.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, t := range l.tickets {
		if !now.Before(t.until) {
			delete(l.tickets, k)
		}
	}
	if len(l.tickets) >= maxTickets {
		return "", ErrTooManyLogins
	}
	l.tickets[id] = &totpTicket{dev: d, until: now.Add(TOTPTicketTTL), remember: remember, origin: origin}
	return id, nil
}

type totpReq struct {
	Ticket string `json:"ticket"`
	Code   string `json:"code"`
}

func (l *Login) totp(w http.ResponseWriter, r *http.Request) {
	var req totpReq
	if !decodeStrict(r.Body, &req) || !sixDigits(req.Code) {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	l.mu.Lock()
	tk := l.tickets[req.Ticket]
	live := tk != nil && l.cfg.Now().Before(tk.until) && tk.tries < MaxTOTPTries
	if live {
		tk.tries++
	} else {
		delete(l.tickets, req.Ticket)
	}
	l.mu.Unlock()
	if !live || l.cfg.CheckTOTP == nil {
		writeJSON(w, http.StatusGone, errorReply("expired"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := l.cfg.CheckTOTP(ctx, tk.dev.ID, req.Code); err != nil {
		_ = l.cfg.Audit.Log(Event{Kind: EvLoginFail, Device: tk.dev.ID, Why: "totp"})
		writeJSON(w, http.StatusUnauthorized, errorReply("wrong_code"))
		return
	}
	l.mu.Lock()
	_, still := l.tickets[req.Ticket]
	delete(l.tickets, req.Ticket) // one use
	l.mu.Unlock()
	if !still {
		writeJSON(w, http.StatusGone, errorReply("expired"))
		return
	}
	l.issueRemember(w, r, tk.dev, tk.remember, tk.origin)
}

// sixDigits: exactly six ASCII digits.
func sixDigits(s string) bool {
	if len(s) != 6 {
		return false
	}
	for i := 0; i < 6; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// issueWith logs a device in: a token with the device's role, and the
// bookmark key (when not "") in its httpOnly cookie; how names the way in
// for the audit log. False: no token (too many sessions).
func (l *Login) issueWith(w http.ResponseWriter, r *http.Request, d webdevices.Device, key, how string) bool {
	tok, sess, err := l.cfg.Tokens.Issue(d.ID, sessionRole(d.Role), l.cfg.Now())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorReply("too_many_sessions"))
		return false
	}
	_ = l.cfg.Audit.Log(Event{Kind: EvLoginOK, Conn: sess.ID, Device: d.ID, Summary: d.Name + " as " + d.Role + " by " + how})
	reply := map[string]any{"token": tok, "role": d.Role, "device": d.ID,
		"idle_s": int(l.cfg.Tokens.idle / time.Second), "max_s": int(l.cfg.Tokens.max / time.Second)}
	if key != "" {
		setDeviceCookie(w, r, key)
		reply["remembered"] = true
	}
	writeJSON(w, http.StatusOK, reply)
	return true
}
