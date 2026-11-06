//go:build unix

package webgw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// bmOrigin is the page's origin in these tests (the tailnet site).
const bmOrigin = "https://" + pkHost

// postFrom posts like the page does: JSON with an Origin header.
func postFrom(t *testing.T, h http.Handler, origin, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w.Code, m
}

// rememberLogin signs in with the passkey and "Remember this browser",
// and returns the reply.
func rememberLogin(t *testing.T, l *Login, k *softKey) map[string]any {
	t.Helper()
	_, m := postFrom(t, l, bmOrigin, "/login/start", `{}`)
	opts, _ := json.Marshal(m["options"])
	code, f := postFrom(t, l, bmOrigin, "/login/finish", `{"login_id":"`+m["login_id"].(string)+`","credential":`+string(k.get(opts))+`,"remember":true}`)
	if code != http.StatusOK {
		t.Fatalf("login finish: %d %v", code, f)
	}
	return f
}

func keyBody(key string) string { return `{"key":"` + key + `"}` }

// Spec (WEBP-12): a passkey sign-in with remember gives a bookmark key
// next to the session token; the laptop keeps only its hash. The key
// alone signs the device in again with its role, and every use gives a
// new key: the old one then works no more, and a second use of it (a
// stolen copy) forgets the bookmark, so the new key stops too.
func TestBookmarkSignInRotatesAndCatchesReuse(t *testing.T) {
	r, l, k := passkeyPairRig(t)
	pairWithKey(t, r, k, webdevices.RoleControl)
	f := rememberLogin(t, l, k)
	key1, _ := f["device_key"].(string)
	if _, _, err := ParseDeviceKey(key1); err != nil || f["token"] == nil {
		t.Fatalf("remember reply %v", f)
	}
	raw, _ := os.ReadFile(r.store.Path())
	if strings.Contains(string(raw), key1) || strings.Contains(string(raw), key1[len(key1)-43:]) {
		t.Fatal("devices.json holds the key")
	}
	ds, _ := r.store.List()
	if ds[0].Bookmark == nil || len(ds[0].Bookmark.Hash) != 32 || ds[0].Bookmark.Origin != bmOrigin {
		t.Fatalf("bookmark kept %+v", ds[0].Bookmark)
	}

	// Without remember: no key.
	_, m := postFrom(t, l, bmOrigin, "/login/start", `{}`)
	opts, _ := json.Marshal(m["options"])
	if _, f := postFrom(t, l, bmOrigin, "/login/finish", `{"login_id":"`+m["login_id"].(string)+`","credential":`+string(k.get(opts))+`}`); f["device_key"] != nil {
		t.Fatalf("a key without remember: %v", f)
	}

	code, g := postFrom(t, l, bmOrigin, "/login/device", keyBody(key1))
	key2, _ := g["device_key"].(string)
	if code != http.StatusOK || g["token"] == nil || key2 == "" || key2 == key1 {
		t.Fatalf("bookmark sign-in: %d %v", code, g)
	}
	sess, err := r.tokens.Check(g["token"].(string), r.clock.Now())
	if err != nil || sess.Device != ds[0].ID || sess.Role != RoleControl {
		t.Fatalf("session %+v %v", sess, err)
	}

	if code, g := postFrom(t, l, bmOrigin, "/login/device", keyBody(key1)); code != http.StatusUnauthorized || g["error"] != "revoked" || g["token"] != nil {
		t.Fatalf("reused old key: %d %v", code, g)
	}
	if code, _ := postFrom(t, l, bmOrigin, "/login/device", keyBody(key2)); code != http.StatusUnauthorized {
		t.Fatalf("the new key still works after a reuse: %d", code)
	}
	if d, _, _ := r.store.Get(ds[0].ID); d.Bookmark != nil {
		t.Fatal("the bookmark is still kept after a reuse")
	}
}

// Pairing with remember gives the first key at once.
func TestBookmarkFromPairing(t *testing.T) {
	r, l, k := passkeyPairRig(t)
	id, _ := r.start("Phone")
	r.approve(webdevices.RoleViewer)
	m := r.waitState(id, "approved")
	opts, _ := json.Marshal(m["options"])
	code, f := postFrom(t, r.p, bmOrigin, "/pair/finish", `{"pair_id":"`+id+`","credential":`+string(k.create(opts))+`,"remember":true}`)
	key, _ := f["device_key"].(string)
	if code != http.StatusOK || key == "" {
		t.Fatalf("pair finish with remember: %d %v", code, f)
	}
	code, g := postFrom(t, l, bmOrigin, "/login/device", keyBody(key))
	if code != http.StatusOK {
		t.Fatalf("bookmark after pairing: %d %v", code, g)
	}
	if sess, _ := r.tokens.Check(g["token"].(string), r.clock.Now()); sess.Role != RoleViewer {
		t.Fatalf("role %v", sess.Role)
	}
}

// A key is refused: from another origin or none, for a removed device, a
// forgotten bookmark (the laptop's Forget, the page's Forget), after
// BookmarkTTL unused, for a device that must pair again, in any other
// shape. None of these gives a token or a new key.
func TestBookmarkRefusals(t *testing.T) {
	r, l, k := passkeyPairRig(t)
	pairWithKey(t, r, k, webdevices.RoleControl)
	ds, _ := r.store.List()
	dev := ds[0].ID
	fresh := func() string { return rememberLogin(t, l, k)["device_key"].(string) }
	refused := func(what, origin, body, want string) {
		t.Helper()
		code, g := postFrom(t, l, origin, "/login/device", body)
		if code == http.StatusOK || g["token"] != nil || g["device_key"] != nil {
			t.Fatalf("%s: %d %v", what, code, g)
		}
		if want != "" && g["error"] != want {
			t.Fatalf("%s: error %v, want %s", what, g["error"], want)
		}
	}

	key := fresh()
	refused("wrong origin", "http://localhost:41237", keyBody(key), "login_failed")
	refused("no origin", "", keyBody(key), "login_failed")
	if code, _ := postFrom(t, l, bmOrigin, "/login/device", keyBody(key)); code != http.StatusOK {
		t.Fatal("a refused origin used the key up")
	}

	key = fresh()
	wrong := key[:len(key)-2] + "AA"
	if wrong == key {
		wrong = key[:len(key)-2] + "EA"
	}
	refused("wrong secret", bmOrigin, keyBody(wrong), "login_failed")
	refused("bad shape", bmOrigin, keyBody("tcb1.x"), "")
	refused("extra field", bmOrigin, `{"key":"`+key+`","x":1}`, "")
	if code, _ := postFrom(t, l, bmOrigin, "/login/device?key="+key, keyBody(key)); code == http.StatusOK {
		t.Fatal("a key in the URL was taken")
	}

	key = fresh()
	if _, err := r.store.Change(dev, func(d *webdevices.Device) { d.Bookmark = nil }); err != nil {
		t.Fatal(err)
	}
	refused("laptop forgot it", bmOrigin, keyBody(key), "login_failed")

	key = fresh()
	if code, _ := postFrom(t, l, bmOrigin, "/login/forget", keyBody(key)); code != http.StatusOK {
		t.Fatalf("page forget: %d", code)
	}
	refused("page forgot it", bmOrigin, keyBody(key), "login_failed")

	key = fresh()
	r.clock.Add(BookmarkTTL)
	refused("unused too long", bmOrigin, keyBody(key), "expired")

	key = fresh()
	_, _ = r.store.Change(dev, func(d *webdevices.Device) { d.NeedsRepair = true })
	refused("must pair again", bmOrigin, keyBody(key), "")
	_, _ = r.store.Change(dev, func(d *webdevices.Device) { d.NeedsRepair = false })

	key = fresh()
	if _, _, err := r.store.Remove(dev); err != nil {
		t.Fatal(err)
	}
	refused("device removed", bmOrigin, keyBody(key), "removed")
}

// With TOTP on, the key alone is not enough: a ticket first, and the
// token and the next key only with the right code.
func TestBookmarkAsksTOTP(t *testing.T) {
	r, l, k := passkeyPairRig(t)
	pairWithKey(t, r, k, webdevices.RoleControl)
	key := rememberLogin(t, l, k)["device_key"].(string)
	l.SetRequireTOTP(true)
	l.cfg.CheckTOTP = func(_ context.Context, _, code string) error {
		if code == "123456" {
			return nil
		}
		return errors.New("wrong")
	}
	code, f := postFrom(t, l, bmOrigin, "/login/device", keyBody(key))
	if code != http.StatusOK || f["totp"] != true || f["token"] != nil || f["device_key"] != nil {
		t.Fatalf("bookmark with TOTP on: %d %v", code, f)
	}
	tk := f["ticket"].(string)
	if _, g := postFrom(t, l, bmOrigin, "/login/totp", `{"ticket":"`+tk+`","code":"000000"}`); g["token"] != nil || g["device_key"] != nil {
		t.Fatalf("wrong code: %v", g)
	}
	_, g := postFrom(t, l, bmOrigin, "/login/totp", `{"ticket":"`+tk+`","code":"123456"}`)
	next, _ := g["device_key"].(string)
	if g["token"] == nil || next == "" || next == key {
		t.Fatalf("right code: %v", g)
	}
	l.SetRequireTOTP(false)
	if code, _ := postFrom(t, l, bmOrigin, "/login/device", keyBody(next)); code != http.StatusOK {
		t.Fatalf("the key after TOTP: %d", code)
	}
}

// The key never reaches the audit log, in any outcome.
func TestBookmarkKeyNeverLogged(t *testing.T) {
	r, l, k := passkeyPairRig(t)
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a, err := OpenAudit(path, r.clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	l.cfg.Audit = a
	pairWithKey(t, r, k, webdevices.RoleControl)
	key := rememberLogin(t, l, k)["device_key"].(string)
	_, g := postFrom(t, l, bmOrigin, "/login/device", keyBody(key))
	next := g["device_key"].(string)
	postFrom(t, l, "http://localhost:1", "/login/device", keyBody(next))
	postFrom(t, l, bmOrigin, "/login/device", keyBody(key)) // reuse
	postFrom(t, l, bmOrigin, "/login/forget", keyBody(next))
	b, _ := os.ReadFile(path)
	for _, s := range []string{key, next, key[len(key)-43:], next[len(next)-43:]} {
		if strings.Contains(string(b), s) {
			t.Fatalf("audit holds a key:\n%s", b)
		}
	}
	if !strings.Contains(string(b), "key_reused") {
		t.Fatalf("the reuse is not in the audit log:\n%s", b)
	}
}
