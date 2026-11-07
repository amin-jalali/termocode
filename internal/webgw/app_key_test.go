//go:build unix

package webgw

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// testAppKey is what the Android app keeps in its keystore (MOB-27): a
// P-256 key that never leaves the phone.
type testAppKey struct {
	priv *ecdsa.PrivateKey
	pub  string // SPKI DER, base64url without padding
}

func newTestAppKey(t *testing.T) testAppKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return testAppKey{priv: priv, pub: base64.RawURLEncoding.EncodeToString(der)}
}

// sign signs like the app does: SHA256withECDSA over AppKeyMessage.
func (k testAppKey) sign(t *testing.T, purpose, origin, nonce string) string {
	t.Helper()
	msg := AppKeyMessage(purpose, origin, nonce)
	if msg == nil {
		t.Fatalf("no message for %q %q %q", purpose, origin, nonce)
	}
	sum := sha256.Sum256(msg)
	sig, err := ecdsa.SignASN1(rand.Reader, k.priv, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(sig)
}

// appKeyRig is a passkey rig with app key sign-in on or off.
func appKeyRig(t *testing.T, on bool) (*pairRig, *Login) {
	t.Helper()
	r, l, _ := passkeyPairRig(t)
	r.p.cfg.AppKeys = on
	l.cfg.AppKeys = on
	return r, l
}

func appPairBody(id, pub, sig string) string {
	return `{"pair_id":"` + id + `","app_key":"` + pub + `","app_sig":"` + sig + `"}`
}

// pairWithAppKey pairs a phone with its app key (no passkey) and returns
// the token.
func pairWithAppKey(t *testing.T, r *pairRig, k testAppKey, role string) string {
	t.Helper()
	id, _ := r.start("Android · termocode")
	r.approve(role)
	m := r.waitState(id, "approved")
	if m["app_keys"] != true {
		t.Fatalf("approved without app_keys: %v", m)
	}
	code, f := postFrom(t, r.p, bmOrigin, "/pair/finish", appPairBody(id, k.pub, k.sign(t, "pair", bmOrigin, id)))
	if code != http.StatusOK {
		t.Fatalf("finish: %d %v", code, f)
	}
	return f["token"].(string)
}

// appLogin runs /login/app/start and /login/app/finish with k.
func appLogin(t *testing.T, l *Login, k testAppKey, origin string) (int, map[string]any) {
	t.Helper()
	code, m := postFrom(t, l, origin, "/login/app/start", `{}`)
	if code != http.StatusOK {
		t.Fatalf("app start: %d %v", code, m)
	}
	id, ch := m["login_id"].(string), m["challenge"].(string)
	return postFrom(t, l, origin, "/login/app/finish",
		`{"login_id":"`+id+`","public_key":"`+k.pub+`","signature":"`+k.sign(t, "login", origin, ch)+`"}`)
}

// Spec (MOB-27): app key sign-in is off by default. Pairing still needs
// a passkey, /pair/wait does not offer app keys, /login/app/* is 404.
func TestAppKeyOffByDefault(t *testing.T) {
	r, l := appKeyRig(t, false)
	k := newTestAppKey(t)
	id, _ := r.start("Phone")
	r.approve(webdevices.RoleControl)
	m := r.waitState(id, "approved")
	if _, ok := m["app_keys"]; ok {
		t.Fatalf("app_keys offered while off: %v", m)
	}
	if code, f := postFrom(t, r.p, bmOrigin, "/pair/finish", appPairBody(id, k.pub, k.sign(t, "pair", bmOrigin, id))); code != http.StatusBadRequest || f["error"] != "need_passkey" {
		t.Fatalf("app key pairing while off: %d %v", code, f)
	}
	if ds, _ := r.store.List(); len(ds) != 0 {
		t.Fatalf("stored %v", ds)
	}
	for _, p := range []string{"/login/app/start", "/login/app/finish"} {
		if code, f := postFrom(t, l, bmOrigin, p, `{}`); code != http.StatusNotFound || f["error"] != "not_found" {
			t.Fatalf("%s while off: %d %v", p, code, f)
		}
	}
}

// Spec (MOB-27): with the flag on, a phone pairs with its app key alone
// (the owner still says yes on the laptop), the laptop keeps only the
// public key and the origin, and the phone signs in again with a fresh
// one-use challenge.
func TestAppKeyPairAndSignIn(t *testing.T) {
	r, l := appKeyRig(t, true)
	k := newTestAppKey(t)
	pairWithAppKey(t, r, k, webdevices.RoleControl)
	ds, _ := r.store.List()
	if len(ds) != 1 || ds[0].AppKey == nil || ds[0].AppKey.Origin != bmOrigin || len(ds[0].AppKey.PublicKey) == 0 {
		t.Fatalf("devices %+v", ds)
	}
	if len(ds[0].PublicKey) != 0 || len(ds[0].CredID) != 0 {
		t.Fatalf("a passkey was stored for an app key pairing: %+v", ds[0])
	}
	code, f := appLogin(t, l, k, bmOrigin)
	if code != http.StatusOK || f["token"] == nil {
		t.Fatalf("app login: %d %v", code, f)
	}
	sess, err := r.tokens.Check(f["token"].(string), r.clock.Now())
	if err != nil || sess.Device != ds[0].ID || sess.Role != RoleControl {
		t.Fatalf("session %+v %v", sess, err)
	}
	if f["remembered"] != nil {
		t.Fatalf("an app key sign-in set a bookmark: %v", f)
	}
}

// A challenge works once and for AppChallengeTTL only.
func TestAppKeyChallengeOneUse(t *testing.T) {
	r, l := appKeyRig(t, true)
	k := newTestAppKey(t)
	pairWithAppKey(t, r, k, webdevices.RoleViewer)
	_, m := postFrom(t, l, bmOrigin, "/login/app/start", `{}`)
	id, ch := m["login_id"].(string), m["challenge"].(string)
	if len(ch) != 43 {
		t.Fatalf("challenge %q", ch)
	}
	body := `{"login_id":"` + id + `","public_key":"` + k.pub + `","signature":"` + k.sign(t, "login", bmOrigin, ch) + `"}`
	if code, f := postFrom(t, l, bmOrigin, "/login/app/finish", body); code != http.StatusOK {
		t.Fatalf("first use: %d %v", code, f)
	}
	if code, f := postFrom(t, l, bmOrigin, "/login/app/finish", body); code != http.StatusGone || f["error"] != "expired" {
		t.Fatalf("second use: %d %v", code, f)
	}
	_, m = postFrom(t, l, bmOrigin, "/login/app/start", `{}`)
	id, ch = m["login_id"].(string), m["challenge"].(string)
	r.clock.Add(AppChallengeTTL)
	body = `{"login_id":"` + id + `","public_key":"` + k.pub + `","signature":"` + k.sign(t, "login", bmOrigin, ch) + `"}`
	if code, f := postFrom(t, l, bmOrigin, "/login/app/finish", body); code != http.StatusGone || f["error"] != "expired" {
		t.Fatalf("late: %d %v", code, f)
	}
}

// A wrong key, a signature for another origin or purpose, an unknown
// key and a cloned-looking device are all "login_failed"; none says why.
func TestAppKeyLoginRefusals(t *testing.T) {
	r, l := appKeyRig(t, true)
	k := newTestAppKey(t)
	pairWithAppKey(t, r, k, webdevices.RoleControl)
	other := newTestAppKey(t)
	try := func(name string, pub string, sign func(ch string) string, origin string) {
		t.Helper()
		_, m := postFrom(t, l, origin, "/login/app/start", `{}`)
		id, ch := m["login_id"].(string), m["challenge"].(string)
		code, f := postFrom(t, l, origin, "/login/app/finish", `{"login_id":"`+id+`","public_key":"`+pub+`","signature":"`+sign(ch)+`"}`)
		if code != http.StatusUnauthorized || f["error"] != "login_failed" || f["token"] != nil {
			t.Fatalf("%s: %d %v", name, code, f)
		}
	}
	try("signed by another key", k.pub, func(ch string) string { return other.sign(t, "login", bmOrigin, ch) }, bmOrigin)
	try("unknown key", other.pub, func(ch string) string { return other.sign(t, "login", bmOrigin, ch) }, bmOrigin)
	try("pair purpose", k.pub, func(ch string) string { return k.sign(t, "pair", bmOrigin, ch) }, bmOrigin)
	try("signed for another origin", k.pub, func(ch string) string { return k.sign(t, "login", "https://other.tail1234.ts.net", ch) }, bmOrigin)
	evil := "https://other.tail1234.ts.net"
	try("from another origin", k.pub, func(ch string) string { return k.sign(t, "login", evil, ch) }, evil)
	try("no signature", k.pub, func(string) string { return "" }, bmOrigin)
	try("junk signature", k.pub, func(string) string { return "AAAA" }, bmOrigin)

	ds, _ := r.store.List()
	_, _ = r.store.Change(ds[0].ID, func(d *webdevices.Device) { d.NeedsRepair = true })
	try("needs repair", k.pub, func(ch string) string { return k.sign(t, "login", bmOrigin, ch) }, bmOrigin)
}

// A bad pairing signature stores nothing and leaves the pairing open, so
// the real app can still finish; a guest share never takes an app key.
func TestAppKeyPairRefusals(t *testing.T) {
	r, _ := appKeyRig(t, true)
	k := newTestAppKey(t)
	id, _ := r.start("Phone")
	r.approve(webdevices.RoleControl)
	r.waitState(id, "approved")
	other := newTestAppKey(t)
	bad := []string{
		appPairBody(id, k.pub, other.sign(t, "pair", bmOrigin, id)),
		appPairBody(id, k.pub, k.sign(t, "login", bmOrigin, id)),
		appPairBody(id, k.pub, k.sign(t, "pair", "https://other.tail1234.ts.net", id)),
		appPairBody(id, "not-a-key", k.sign(t, "pair", bmOrigin, id)),
		appPairBody(id, k.pub, ""),
	}
	for _, b := range bad {
		if code, f := postFrom(t, r.p, bmOrigin, "/pair/finish", b); code != http.StatusUnauthorized || f["error"] != "app_key" {
			t.Fatalf("bad pairing %s: %d %v", b, code, f)
		}
	}
	if ds, _ := r.store.List(); len(ds) != 0 {
		t.Fatalf("stored %v", ds)
	}
	if code, f := postFrom(t, r.p, bmOrigin, "/pair/finish", appPairBody(id, k.pub, k.sign(t, "pair", bmOrigin, id))); code != http.StatusOK {
		t.Fatalf("the good try after bad ones: %d %v", code, f)
	}

	r.open(true) // a share with another person
	gid, _ := r.start("Guest")
	r.approve(webdevices.RoleViewer)
	r.waitState(gid, "approved")
	g := newTestAppKey(t)
	if code, f := postFrom(t, r.p, bmOrigin, "/pair/finish", appPairBody(gid, g.pub, g.sign(t, "pair", bmOrigin, gid))); code != http.StatusBadRequest || f["error"] != "need_passkey" {
		t.Fatalf("guest with an app key: %d %v", code, f)
	}
}

// With TOTP on, an app key sign-in gives a ticket, like a passkey.
func TestAppKeyAsksTOTP(t *testing.T) {
	r, l := appKeyRig(t, true)
	k := newTestAppKey(t)
	pairWithAppKey(t, r, k, webdevices.RoleControl)
	l.SetRequireTOTP(true)
	code, f := appLogin(t, l, k, bmOrigin)
	if code != http.StatusOK || f["totp"] != true || f["ticket"] == nil || f["token"] != nil {
		t.Fatalf("app login with TOTP on: %d %v", code, f)
	}
}

func TestParseAppPublicKey(t *testing.T) {
	k := newTestAppKey(t)
	if _, der, err := ParseAppPublicKey(k.pub); err != nil || len(der) == 0 {
		t.Fatalf("P-256: %v", err)
	}
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	d384, _ := x509.MarshalPKIXPublicKey(&p384.PublicKey)
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	drsa, _ := x509.MarshalPKIXPublicKey(&rk.PublicKey)
	bad := []string{
		"", "not base64!", base64.RawURLEncoding.EncodeToString([]byte("junk")),
		base64.RawURLEncoding.EncodeToString(d384),
		base64.RawURLEncoding.EncodeToString(drsa),
		base64.URLEncoding.EncodeToString(mustDER(t, k)), // padding
		strings.Repeat("A", MaxAppKeyB64+1),
	}
	for _, b := range bad {
		if _, _, err := ParseAppPublicKey(b); err == nil {
			t.Fatalf("accepted %q", b)
		}
	}
}

func mustDER(t *testing.T, k testAppKey) []byte {
	t.Helper()
	der, err := base64.RawURLEncoding.DecodeString(k.pub)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// The message binds the purpose, the page's origin and the nonce; the
// app builds the very same bytes (AppKeyMessage.kt).
func TestAppKeyMessage(t *testing.T) {
	got := string(AppKeyMessage("login", "https://box.tail1234.ts.net", "abcDEF0123456789_-xyz"))
	if got != "termocode-app-login-v1\nhttps://box.tail1234.ts.net\nabcDEF0123456789_-xyz" {
		t.Fatalf("message %q", got)
	}
	bad := [][3]string{
		{"other", bmOrigin, "abcdef0123456789"},
		{"login", "", "abcdef0123456789"},
		{"login", "http://box.tail1234.ts.net", "abcdef0123456789"},
		{"login", bmOrigin + "\nx", "abcdef0123456789"},
		{"login", bmOrigin, "short"},
		{"login", bmOrigin, "bad nonce with spaces"},
		{"login", bmOrigin, strings.Repeat("a", 129)},
	}
	for _, b := range bad {
		if AppKeyMessage(b[0], b[1], b[2]) != nil {
			t.Fatalf("message for %q", b)
		}
	}
}
