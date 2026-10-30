//go:build unix

package webgw

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// passkeyPairRig is a pairRig whose pairings make a passkey, plus the
// login endpoints on the same store.
func passkeyPairRig(t *testing.T) (*pairRig, *Login, *softKey) {
	t.Helper()
	r := newPairRig(t)
	site, _ := TailnetSite(pkHost)
	pk, err := NewPasskeys(site, r.store, r.clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	r.p.cfg.Passkeys = pk
	l := NewLogin(LoginConfig{Tokens: r.tokens, Passkeys: pk, Now: r.clock.Now})
	return r, l, newSoftKey(t, pkHost, "https://"+pkHost)
}

func postTo(t *testing.T, h http.Handler, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w.Code, m
}

// pairWithKey pairs a phone whose passkey is k and returns its token.
func pairWithKey(t *testing.T, r *pairRig, k *softKey, role string) string {
	t.Helper()
	id, _ := r.start("Phone")
	r.approve(role)
	m := r.waitState(id, "approved")
	opts, _ := json.Marshal(m["options"])
	if m["options"] == nil {
		t.Fatalf("approved without passkey options: %v", m)
	}
	cred := k.create(opts)
	code, f := r.post("/pair/finish", `{"pair_id":"`+id+`","credential":`+string(cred)+`}`)
	if code != http.StatusOK {
		t.Fatalf("finish: %d %v", code, f)
	}
	return f["token"].(string)
}

// Spec (WEB-33): with passkeys on, pairing ends by making a passkey: no
// credential, no device; then the phone signs in again with it alone.
func TestPasskeyPairFlow(t *testing.T) {
	r, l, k := passkeyPairRig(t)
	id, _ := r.start("Phone")
	r.approve(webdevices.RoleControl)
	r.waitState(id, "approved")
	if code, _ := r.post("/pair/finish", `{"pair_id":"`+id+`"}`); code == http.StatusOK {
		t.Fatal("paired without a passkey")
	}
	r.open(false)
	pairWithKey(t, r, k, webdevices.RoleControl)
	ds, _ := r.store.List()
	if len(ds) != 1 || len(ds[0].PublicKey) == 0 {
		t.Fatalf("devices %+v", ds)
	}
	code, m := postTo(t, l, "/login/start", `{}`)
	if code != http.StatusOK || m["login_id"] == nil {
		t.Fatalf("login start: %d %v", code, m)
	}
	opts, _ := json.Marshal(m["options"])
	code, f := postTo(t, l, "/login/finish", `{"login_id":"`+m["login_id"].(string)+`","credential":`+string(k.get(opts))+`}`)
	if code != http.StatusOK {
		t.Fatalf("login finish: %d %v", code, f)
	}
	sess, err := r.tokens.Check(f["token"].(string), r.clock.Now())
	if err != nil || sess.Device != ds[0].ID || sess.Role != RoleControl {
		t.Fatalf("session %+v %v", sess, err)
	}
}

// A failed login says only "login_failed" (or what to do for a removed
// or cloned device), never why the signature was wrong.
func TestLoginFailures(t *testing.T) {
	r, l, k := passkeyPairRig(t)
	pairWithKey(t, r, k, webdevices.RoleViewer)
	_, m := postTo(t, l, "/login/start", `{}`)
	opts, _ := json.Marshal(m["options"])
	k.origin = "https://evil.example"
	code, f := postTo(t, l, "/login/finish", `{"login_id":"`+m["login_id"].(string)+`","credential":`+string(k.get(opts))+`}`)
	if code != http.StatusUnauthorized || f["error"] != "login_failed" {
		t.Fatalf("phishing origin: %d %v", code, f)
	}
	for _, tc := range []struct{ path, body string }{
		{"/login/finish", `{"login_id":"nope","credential":{}}`},
		{"/login/finish", `{"login_id":"x"}`},
		{"/login/start", `{"x":1}`},
	} {
		if code, _ := postTo(t, l, tc.path, tc.body); code == http.StatusOK {
			t.Errorf("%s %s passed", tc.path, tc.body)
		}
	}
	limited := false
	for i := 0; i < 100 && !limited; i++ {
		code, _ := postTo(t, l, "/login/start", `{}`)
		limited = code == http.StatusTooManyRequests
	}
	if !limited {
		t.Fatal("no rate limit on /login")
	}
	_ = time.Second
}

// The finish requests carry a passkey answer, which can pass 4 KiB (an
// RSA key, a long credential id): they get 16 KiB, everything else stays
// at 4 KiB.
func TestBodyCapForPasskeyAnswers(t *testing.T) {
	for _, tc := range []struct {
		path string
		size int
		code int
	}{
		{"/pair/finish", 12 << 10, http.StatusOK},
		{"/login/finish", 12 << 10, http.StatusOK},
		{"/login/finish", MaxPasskeyBodyBytes + 1, http.StatusRequestEntityTooLarge},
		{"/auth", 12 << 10, http.StatusRequestEntityTooLarge},
		{"/pair/start", MaxBodyBytes + 1, http.StatusRequestEntityTooLarge},
	} {
		r := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(strings.Repeat("a", tc.size)))
		r.Host = "127.0.0.1:41237"
		r.Header.Set("Origin", "http://127.0.0.1:41237")
		w := httptest.NewRecorder()
		Guard(testSite, &okHandler{}).ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Errorf("%s %d bytes: %d, want %d", tc.path, tc.size, w.Code, tc.code)
		}
	}
}
