//go:build unix

package webgw

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Pairing (WEB-32, web design 3.3): a phone on the tailnet scans a QR
// code that the laptop shows: https://<host>/pair#p=<secret>. The secret
// works once, for PairTTL, and only its digest reaches the gateway (an
// event on the control connection). Then:
//
//	POST /pair/start  {p, name}   → {pair_id, code}: the phone shows the
//	                                 6-digit code; the laptop shows a card
//	                                 with the device name and the same code
//	POST /pair/wait   {pair_id}   → waiting | approved {role} | denied
//	POST /pair/finish {pair_id}   → the device is stored, the browser gets
//	                                 a session token with the owner's role
//
// The code (SAS) lets the owner see that the request on the card is the
// phone in their hand: someone who saw the QR and was faster shows
// another code, and the real phone gets "already used". Nothing pairs
// without the owner's pick on the laptop, whose safe answer is first.

// PairTTL is how long a pairing link works, and how long a started
// pairing may take until it is finished.
const PairTTL = 120 * time.Second

// ShareTTL is how long a share link for another person works (WEB-50):
// it travels by message, so it gets more time; it is still single use.
const ShareTTL = 10 * time.Minute

// PairWait is how long one /pair/wait holds the request open.
const PairWait = 25 * time.Second

// pairRatePerSecond caps /pair/* requests for the whole gateway.
const pairRatePerSecond = 10

// maxPending caps the pairings waiting at once.
const maxPending = 4

// PairingConfig sets up the pairing endpoints.
type PairingConfig struct {
	Tokens *Tokens
	Store  *webdevices.Store
	Audit  *Audit
	// Ask shows the laptop's card and returns the role the owner picked
	// (webdevices.RoleViewer or RoleControl), or an error for Not now.
	Ask func(ctx context.Context, a webproto.PairAskArgs) (string, error)
	// Now is the clock of secrets and pairings (nil: time.Now).
	Now func() time.Time
	// WaitPoll is how long /pair/wait holds (0: PairWait).
	WaitPoll time.Duration
	// Passkeys, when set, makes every pairing end with a passkey
	// (WEB-33): /pair/wait hands out its options once the owner said
	// yes, and /pair/finish needs the browser's answer.
	Passkeys *Passkeys
}

// Pairing serves /pair/start, /pair/wait and /pair/finish.
type Pairing struct {
	cfg   PairingConfig
	limit *Bucket
	noisy *Bucket

	mu      sync.Mutex
	secret  *LaunchSecret
	guest   bool
	pending map[string]*pendingPair
}

type pairState int

const (
	pairAsking pairState = iota
	pairApproved
	pairDenied
	pairDone
)

type pendingPair struct {
	id, name, code string
	guest          bool
	until          time.Time
	state          pairState
	role           string
	changed        chan struct{}   // closed on the next state change
	reg            *passkeyReg     // the passkey being made (WEB-33)
	options        json.RawMessage // its creation options
}

// NewPairing makes the endpoints; no pairing is open until SetSecret.
func NewPairing(cfg PairingConfig) *Pairing {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.WaitPoll <= 0 {
		cfg.WaitPoll = PairWait
	}
	return &Pairing{cfg: cfg, limit: NewBucket(pairRatePerSecond, 2*pairRatePerSecond),
		noisy: NewBucket(blockedAuditPerSecond, blockedAuditBurst), pending: map[string]*pendingPair{}}
}

// SetSecret opens a pairing link whose secret only the UI knows; the old
// one closes. guest marks a share with another person (viewer only).
func (p *Pairing) SetSecret(digest [sha256.Size]byte, ttl time.Duration, guest bool) {
	s := LaunchSecretFromDigest(digest, p.cfg.Now())
	s.exp = p.cfg.Now().Add(ttl)
	p.mu.Lock()
	p.secret, p.guest = s, guest
	p.mu.Unlock()
}

// CloseSecret closes the open pairing link, if any.
func (p *Pairing) CloseSecret() {
	p.mu.Lock()
	p.secret = nil
	p.mu.Unlock()
}

func (p *Pairing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.limit.Allow(p.cfg.Now()) {
		w.Header().Set("Retry-After", "1")
		if p.noisy.Allow(p.cfg.Now()) {
			_ = p.cfg.Audit.Log(Event{Kind: EvRateLimited, Code: http.StatusTooManyRequests, Op: "pair"})
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
	case "/pair/start":
		p.start(w, r)
	case "/pair/wait":
		p.wait(w, r)
	case "/pair/finish":
		p.finish(w, r)
	default:
		writeJSON(w, http.StatusNotFound, errorReply("not_found"))
	}
}

// decodeStrict reads exactly one JSON object into v: no unknown field, no
// second value.
func decodeStrict(body io.Reader, v any) bool {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if dec.Decode(v) != nil {
		return false
	}
	return dec.Decode(&struct{}{}) == io.EOF
}

type pairStartReq struct {
	P    *string `json:"p"`
	Name string  `json:"name"`
}

func (p *Pairing) start(w http.ResponseWriter, r *http.Request) {
	var req pairStartReq
	if !decodeStrict(r.Body, &req) || req.P == nil {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	now := p.cfg.Now()
	p.mu.Lock()
	s, guest := p.secret, p.guest
	p.mu.Unlock()
	err := ErrSecretClosed
	if s != nil {
		err = s.Redeem(*req.P, now)
	}
	if err != nil {
		why := redeemWhy(err)
		_ = p.cfg.Audit.Log(Event{Kind: EvPairFail, Code: http.StatusUnauthorized, Why: why})
		reply := "pair_failed"
		if why == "used" || why == "expired" {
			reply = why // the phone says what to do: make a new link
		}
		writeJSON(w, http.StatusUnauthorized, errorReply(reply))
		return
	}
	pp, err := p.newPending(webdevices.CleanName(req.Name), guest, s.exp)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorReply("busy"))
		return
	}
	_ = p.cfg.Audit.Log(Event{Kind: EvPairStart, Device: pp.name, Summary: tailnetUser(r)})
	crash.Go("web gateway: a pairing card", func() { p.ask(pp) })
	writeJSON(w, http.StatusOK, map[string]any{"pair_id": pp.id, "code": pp.code, "guest": pp.guest,
		"ttl_s": int(pp.until.Sub(now) / time.Second)})
}

// tailnetUser is the tailnet login Tailscale Serve names, for the audit.
func tailnetUser(r *http.Request) string {
	if u := r.Header.Get("Tailscale-User-Login"); u != "" {
		return "tailnet user " + u
	}
	return ""
}

// newPending makes a pairing with a fresh id and SAS code.
func (p *Pairing) newPending(name string, guest bool, until time.Time) (*pendingPair, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(b[:16])
	mac := hmac.New(sha256.New, b[16:])
	mac.Write([]byte(id))
	code := fmt.Sprintf("%06d", binary.BigEndian.Uint32(mac.Sum(nil))%1_000_000)
	pp := &pendingPair{id: id, name: name, code: code, guest: guest, until: until, changed: make(chan struct{})}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	if len(p.pending) >= maxPending {
		return nil, errors.New("too many pairings")
	}
	p.pending[id] = pp
	return pp, nil
}

// sweepLocked forgets pairings past their time. p.mu is held.
func (p *Pairing) sweepLocked() {
	now := p.cfg.Now()
	for id, pp := range p.pending {
		if !now.Before(pp.until.Add(PairTTL)) {
			delete(p.pending, id)
		}
	}
}

// ask shows the laptop card and records the owner's answer.
func (p *Pairing) ask(pp *pendingPair) {
	ctx, cancel := context.WithTimeout(context.Background(), pp.until.Sub(p.cfg.Now())+time.Second)
	defer cancel()
	role, err := p.cfg.Ask(ctx, webproto.PairAskArgs{PairID: pp.id, Name: pp.name, Code: pp.code, Guest: pp.guest})
	if err == nil && role != webdevices.RoleViewer && role != webdevices.RoleControl {
		err = errors.New("no role")
	}
	if pp.guest {
		role = webdevices.RoleViewer // a share is never more than a viewer
	}
	var reg *passkeyReg
	var opts json.RawMessage
	if err == nil && p.cfg.Passkeys != nil {
		reg, opts, err = p.cfg.Passkeys.BeginRegistration(pp.name)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if pp.state != pairAsking {
		return
	}
	if err != nil {
		pp.state = pairDenied
		_ = p.cfg.Audit.Log(Event{Kind: EvPairFail, Device: pp.name, Why: "denied"})
	} else {
		pp.state, pp.role, pp.reg, pp.options = pairApproved, role, reg, opts
	}
	close(pp.changed)
	pp.changed = make(chan struct{})
}

type pairIDReq struct {
	PairID string `json:"pair_id"`
}

// lookup finds a live pairing; an expired one is gone (410).
func (p *Pairing) lookup(w http.ResponseWriter, r *http.Request) (*pendingPair, bool) {
	var req pairIDReq
	if !decodeStrict(r.Body, &req) {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return nil, false
	}
	p.mu.Lock()
	pp := p.pending[req.PairID]
	p.mu.Unlock()
	if pp == nil || !p.cfg.Now().Before(pp.until) {
		writeJSON(w, http.StatusGone, errorReply("expired"))
		return nil, false
	}
	return pp, true
}

func (p *Pairing) wait(w http.ResponseWriter, r *http.Request) {
	pp, ok := p.lookup(w, r)
	if !ok {
		return
	}
	t := time.NewTimer(p.cfg.WaitPoll)
	defer t.Stop()
	for {
		p.mu.Lock()
		state, role, ch := pp.state, pp.role, pp.changed
		p.mu.Unlock()
		switch state {
		case pairApproved:
			out := map[string]any{"state": "approved", "role": role}
			if pp.options != nil {
				out["options"] = pp.options
			}
			writeJSON(w, http.StatusOK, out)
			return
		case pairDenied:
			writeJSON(w, http.StatusForbidden, errorReply("denied"))
			return
		case pairDone:
			writeJSON(w, http.StatusGone, errorReply("expired"))
			return
		}
		select {
		case <-ch:
		case <-t.C:
			writeJSON(w, http.StatusOK, map[string]any{"state": "waiting"})
			return
		case <-r.Context().Done():
			return
		}
	}
}

// pairFinishReq is /pair/finish's body: the pairing and, with passkeys,
// the browser's answer to the creation options.
type pairFinishReq struct {
	PairID     string          `json:"pair_id"`
	Credential json.RawMessage `json:"credential,omitempty"`
}

func (p *Pairing) finish(w http.ResponseWriter, r *http.Request) {
	var req pairFinishReq
	if !decodeStrict(r.Body, &req) {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	p.mu.Lock()
	pp := p.pending[req.PairID]
	p.mu.Unlock()
	if pp == nil || !p.cfg.Now().Before(pp.until) {
		writeJSON(w, http.StatusGone, errorReply("expired"))
		return
	}
	if p.cfg.Passkeys != nil && len(req.Credential) == 0 {
		writeJSON(w, http.StatusBadRequest, errorReply("need_passkey"))
		return
	}
	p.mu.Lock()
	if pp.state != pairApproved {
		p.mu.Unlock()
		writeJSON(w, http.StatusForbidden, errorReply("not_approved"))
		return
	}
	pp.state = pairDone // single use, whatever happens next
	close(pp.changed)
	pp.changed = make(chan struct{})
	delete(p.pending, pp.id)
	p.mu.Unlock()

	now := p.cfg.Now()
	id, err := newDeviceID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorReply("internal"))
		return
	}
	d := webdevices.Device{ID: id, Name: pp.name, Role: pp.role, Guest: pp.guest, Created: now.UTC(), LastSeen: now.UTC()}
	if p.cfg.Passkeys != nil {
		key, err := p.cfg.Passkeys.FinishRegistration(pp.reg, req.Credential)
		if err != nil {
			_ = p.cfg.Audit.Log(Event{Kind: EvPairFail, Device: pp.name, Why: passkeyWhy(err)})
			writeJSON(w, http.StatusUnauthorized, errorReply("passkey"))
			return
		}
		d.CredID, d.PublicKey, d.SignCount, d.AAGUID, d.UserID, d.RPID = key.CredID, key.PublicKey, key.SignCount, key.AAGUID, key.UserID, key.RPID
	}
	if err := p.cfg.Store.Add(d); err != nil {
		code := "internal"
		if errors.Is(err, webdevices.ErrFull) {
			code = "full"
		}
		writeJSON(w, http.StatusServiceUnavailable, errorReply(code))
		return
	}
	tok, sess, err := p.cfg.Tokens.Issue(id, sessionRole(pp.role), now)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorReply("too_many_sessions"))
		return
	}
	_ = p.cfg.Audit.Log(Event{Kind: EvPairOK, Conn: sess.ID, Device: id, Summary: pp.name + " as " + pp.role})
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "role": pp.role, "device": id,
		"idle_s": int(p.cfg.Tokens.idle / time.Second), "max_s": int(p.cfg.Tokens.max / time.Second)})
}

// sessionRole is the session role of a device role.
func sessionRole(role string) Role {
	if role == webdevices.RoleControl {
		return RoleControl
	}
	return RoleViewer
}

// newDeviceID names a paired device: random, never derived from a secret.
func newDeviceID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "dev-" + hex.EncodeToString(b), nil
}
