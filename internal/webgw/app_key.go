//go:build unix

package webgw

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// App key sign-in (MOB-27), only with web_app_key_login on: the
// termocode Android app pairs and signs in with a P-256 key it made in
// its keystore, for when passkeys do not work inside its WebView (the
// MOB-1 result). The page asks the app's bridge to sign; the app builds
// the message itself, with the WebView's real origin:
//
//	termocode-app-<purpose>-v1 \n <origin> \n <nonce>
//
// purpose "pair" signs the pair_id on /pair/finish {app_key, app_sig};
// purpose "login" signs a one-use challenge:
//
//	POST /login/app/start  {}                                   → {login_id, challenge}
//	POST /login/app/finish {login_id, public_key, signature}   → {token, role, device} (or a TOTP ticket)
//
// The laptop keeps the public key and its one origin; the owner still
// says yes to every pairing on the laptop. A guest never gets an app key.

// AppChallengeTTL is how long a sign-in challenge works.
const AppChallengeTTL = time.Minute

// maxAppChallenges caps the challenges waiting at once.
const maxAppChallenges = 16

// MaxAppKeyB64 caps the public key text (a P-256 SPKI is 91 bytes).
const MaxAppKeyB64 = 200

// maxAppSigB64 caps the signature text (a DER P-256 signature is at most 72 bytes).
const maxAppSigB64 = 128

// ErrBadAppKey: not a P-256 public key in SPKI DER, base64url.
var ErrBadAppKey = errors.New("not an app key")

// ParseAppPublicKey reads the app's public key: strict base64url without
// padding, SPKI DER, ECDSA on P-256 only.
func ParseAppPublicKey(s string) (*ecdsa.PublicKey, []byte, error) {
	if s == "" || len(s) > MaxAppKeyB64 {
		return nil, nil, ErrBadAppKey
	}
	der, err := keyB64.DecodeString(s)
	if err != nil {
		return nil, nil, ErrBadAppKey
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, nil, ErrBadAppKey
	}
	ec, ok := pub.(*ecdsa.PublicKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, nil, ErrBadAppKey
	}
	return ec, der, nil
}

// AppKeyMessage is what the app signs, or nil when a part is not allowed:
// purpose "pair" or "login", an https origin on one line, and a nonce of
// 16..128 base64url (or hex) characters.
func AppKeyMessage(purpose, origin, nonce string) []byte {
	if purpose != "pair" && purpose != "login" {
		return nil
	}
	host, ok := strings.CutPrefix(origin, "https://")
	if !ok || host == "" || strings.ContainsAny(host, "\n\r/ ") {
		return nil
	}
	if len(nonce) < 16 || len(nonce) > 128 {
		return nil
	}
	for i := 0; i < len(nonce); i++ {
		c := nonce[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return nil
		}
	}
	return []byte("termocode-app-" + purpose + "-v1\n" + origin + "\n" + nonce)
}

// verifyAppSig checks a base64url DER signature over the message.
func verifyAppSig(pub *ecdsa.PublicKey, msg []byte, sigB64 string) bool {
	if pub == nil || msg == nil || sigB64 == "" || len(sigB64) > maxAppSigB64 {
		return false
	}
	sig, err := keyB64.DecodeString(sigB64)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(msg)
	return ecdsa.VerifyASN1(pub, sum[:], sig)
}

// checkAppPairing verifies an app key pairing for origin and pair id:
// the key and its proof that the phone holds the private half.
func checkAppPairing(pubB64, sigB64, origin, pairID string, now time.Time) (*webdevices.AppKey, bool) {
	pub, der, err := ParseAppPublicKey(pubB64)
	if err != nil || !verifyAppSig(pub, AppKeyMessage("pair", origin, pairID), sigB64) {
		return nil, false
	}
	return &webdevices.AppKey{PublicKey: der, Origin: origin, Created: now.UTC()}, true
}

// appChallenge is one sign-in waiting for its signature.
type appChallenge struct {
	nonce string
	until time.Time
}

type appStartReq struct{}

type appFinishReq struct {
	LoginID   string `json:"login_id"`
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

// appStart hands out a one-use challenge.
func (l *Login) appStart(w http.ResponseWriter, r *http.Request) {
	if !decodeStrict(r.Body, &appStartReq{}) {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	var b [48]byte
	if _, err := rand.Read(b[:]); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorReply("internal"))
		return
	}
	id := hex.EncodeToString(b[:16])
	nonce := keyB64.EncodeToString(b[16:])
	now := l.cfg.Now()
	l.mu.Lock()
	for k, c := range l.appChallenges {
		if !now.Before(c.until) {
			delete(l.appChallenges, k)
		}
	}
	if len(l.appChallenges) >= maxAppChallenges {
		l.mu.Unlock()
		writeJSON(w, http.StatusServiceUnavailable, errorReply("busy"))
		return
	}
	l.appChallenges[id] = &appChallenge{nonce: nonce, until: now.Add(AppChallengeTTL)}
	l.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"login_id": id, "challenge": nonce,
		"ttl_s": int(AppChallengeTTL / time.Second)})
}

// appFinish signs a device in with its app key.
func (l *Login) appFinish(w http.ResponseWriter, r *http.Request) {
	var req appFinishReq
	if !decodeStrict(r.Body, &req) || req.LoginID == "" {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	now := l.cfg.Now()
	l.mu.Lock()
	c := l.appChallenges[req.LoginID]
	delete(l.appChallenges, req.LoginID) // one use, whatever happens next
	l.mu.Unlock()
	if c == nil || !now.Before(c.until) {
		writeJSON(w, http.StatusGone, errorReply("expired"))
		return
	}
	origin := r.Header.Get("Origin")
	pub, der, err := ParseAppPublicKey(req.PublicKey)
	if err != nil {
		l.appFail(w, "", "bad_key")
		return
	}
	if !verifyAppSig(pub, AppKeyMessage("login", origin, c.nonce), req.Signature) {
		l.appFail(w, "", "bad_signature")
		return
	}
	st := l.store()
	if st == nil {
		writeJSON(w, http.StatusNotFound, errorReply("not_found"))
		return
	}
	ds, err := st.List()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorReply("internal"))
		return
	}
	var dev *webdevices.Device
	for i := range ds {
		if k := ds[i].AppKey; k != nil && bytes.Equal(k.PublicKey, der) {
			dev = &ds[i]
			break
		}
	}
	switch {
	case dev == nil:
		l.appFail(w, "", "no_device")
		return
	case dev.AppKey.Origin != origin:
		l.appFail(w, dev.ID, "origin")
		return
	case dev.NeedsRepair:
		l.appFail(w, dev.ID, "clone_suspected")
		return
	case dev.Guest:
		l.appFail(w, dev.ID, "guest")
		return
	}
	var got webdevices.Device
	found, err := st.Change(dev.ID, func(d *webdevices.Device) {
		d.LastSeen = now.UTC()
		got = *d
	})
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorReply("internal"))
		return
	}
	if !found || got.AppKey == nil || !bytes.Equal(got.AppKey.PublicKey, der) {
		l.appFail(w, dev.ID, "removed")
		return
	}
	if l.totpOn.Load() {
		tk, err := l.newTicket(got, false, "")
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, errorReply("busy"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"totp": true, "ticket": tk})
		return
	}
	l.issueWith(w, r, got, "", "app key")
}

// appFail refuses an app key sign-in: an audit line with the rule, and
// the one short error for the page.
func (l *Login) appFail(w http.ResponseWriter, dev, why string) {
	_ = l.cfg.Audit.Log(Event{Kind: EvLoginFail, Code: http.StatusUnauthorized, Device: dev, Why: why, Summary: "app key"})
	writeJSON(w, http.StatusUnauthorized, errorReply("login_failed"))
}
