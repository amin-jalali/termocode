//go:build unix

package webgw

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// postCookie posts body from origin with the bookmark cookie set to key
// (none when key is ""), over TLS when tlsOn; it returns the response.
func postCookie(t *testing.T, h http.Handler, origin, path, body, key string, tlsOn bool) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	if key != "" {
		req.AddCookie(&http.Cookie{Name: DeviceCookie, Value: key})
	}
	if tlsOn {
		req.TLS = &tls.ConnectionState{}
	} else {
		req.TLS = nil
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Result()
}

func replyOf(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	if _, ok := m["device_key"]; ok {
		t.Errorf("the bookmark key is in the JSON reply: %v", m)
	}
	return m
}

// checkKeyCookie fails unless c is the bookmark cookie with the safe flags:
// httpOnly (page code never reads it), SameSite=Strict, only sent to the
// /login paths, Secure exactly when the page came over https, and kept
// for BookmarkTTL.
func checkKeyCookie(t *testing.T, c *http.Cookie, secure bool) {
	t.Helper()
	if c == nil {
		t.Fatal("no bookmark cookie set")
	}
	if !c.HttpOnly {
		t.Error("the bookmark cookie is not HttpOnly")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want Strict", c.SameSite)
	}
	if c.Path != "/login" {
		t.Errorf("Path = %q, want /login", c.Path)
	}
	if c.Secure != secure {
		t.Errorf("Secure = %v, want %v", c.Secure, secure)
	}
	if c.MaxAge != int(BookmarkTTL/time.Second) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(BookmarkTTL/time.Second))
	}
	if c.Domain != "" {
		t.Errorf("Domain = %q, want host-only", c.Domain)
	}
	if _, _, err := ParseDeviceKey(c.Value); err != nil {
		t.Errorf("the cookie holds no bookmark key: %v", err)
	}
}

// Spec (WEBP-12, security): the bookmark key is set by the server as an
// httpOnly, SameSite=Strict cookie (Secure over TLS), never in a reply
// body, so the page's JavaScript cannot read it. The cookie alone signs
// the browser in, every use sets the next key, and a refused key clears
// the cookie.
func TestBookmarkKeyIsAnHttpOnlyCookie(t *testing.T) {
	r, l, k := passkeyPairRig(t)
	pairWithKey(t, r, k, webdevices.RoleControl)

	_, m := postFrom(t, l, bmOrigin, "/login/start", `{}`)
	opts, _ := json.Marshal(m["options"])
	res := postCookie(t, l, bmOrigin, "/login/finish", `{"login_id":"`+m["login_id"].(string)+`","credential":`+string(k.get(opts))+`,"remember":true}`, "", true)
	f := replyOf(t, res)
	if res.StatusCode != http.StatusOK || f["token"] == nil || f["remembered"] != true {
		t.Fatalf("remember sign-in: %d %v", res.StatusCode, f)
	}
	c1 := deviceCookieOf(res)
	checkKeyCookie(t, c1, true)

	// The cookie alone (an empty body) signs in and rotates.
	res = postCookie(t, l, bmOrigin, "/login/device", `{}`, c1.Value, true)
	g := replyOf(t, res)
	if res.StatusCode != http.StatusOK || g["token"] == nil || g["remembered"] != true {
		t.Fatalf("cookie sign-in: %d %v", res.StatusCode, g)
	}
	c2 := deviceCookieOf(res)
	checkKeyCookie(t, c2, true)
	if c2.Value == c1.Value {
		t.Fatal("the key was not rotated")
	}

	// Over plain http (loopback) the cookie is not Secure, or the browser
	// would never send it back.
	res = postCookie(t, l, "http://localhost:41237", "/login/device", `{}`, c2.Value, false)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a key from another origin: %d", res.StatusCode)
	}
	if c := deviceCookieOf(res); c == nil || c.MaxAge >= 0 || c.Value != "" || !c.HttpOnly || c.Path != "/login" {
		t.Fatalf("a refused key does not clear the cookie: %+v", c)
	}

	// Forget clears the cookie too, and the key works no more.
	res = postCookie(t, l, bmOrigin, "/login/forget", `{}`, c2.Value, true)
	if c := deviceCookieOf(res); res.StatusCode != http.StatusOK || c == nil || c.MaxAge >= 0 {
		t.Fatalf("forget: %d %+v", res.StatusCode, c)
	}
	if res := postCookie(t, l, bmOrigin, "/login/device", `{}`, c2.Value, true); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the key works after forget: %d", res.StatusCode)
	}
}

// Over plain http (loopback, no TLS) the cookie has no Secure flag, or
// the browser would never send it back; behind TLS, or for an https
// page (a TLS proxy in front), it is Secure. The other flags stay.
func TestBookmarkCookieSecureOnlyWithTLS(t *testing.T) {
	key, _, err := NewDeviceKey("dev-0000abcd")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		origin string
		tlsOn  bool
		secure bool
	}{
		{"http://localhost:41237", false, false},
		{"http://127.0.0.1:41237", false, false},
		{"https://" + pkHost, false, true},
		{"http://localhost:41237", true, true},
	} {
		req := httptest.NewRequest(http.MethodPost, "/login/device", nil)
		req.Header.Set("Origin", tc.origin)
		req.TLS = nil
		if tc.tlsOn {
			req.TLS = &tls.ConnectionState{}
		}
		w := httptest.NewRecorder()
		setDeviceCookie(w, req, key)
		checkKeyCookie(t, deviceCookieOf(w.Result()), tc.secure)
	}
}

// Pairing with remember sets the cookie the same way.
func TestBookmarkCookieFromPairing(t *testing.T) {
	r, _, k := passkeyPairRig(t)
	id, _ := r.start("Phone")
	r.approve(webdevices.RoleViewer)
	m := r.waitState(id, "approved")
	opts, _ := json.Marshal(m["options"])
	res := postCookie(t, r.p, bmOrigin, "/pair/finish", `{"pair_id":"`+id+`","credential":`+string(k.create(opts))+`,"remember":true}`, "", true)
	f := replyOf(t, res)
	if res.StatusCode != http.StatusOK || f["remembered"] != true {
		t.Fatalf("pair finish: %d %v", res.StatusCode, f)
	}
	checkKeyCookie(t, deviceCookieOf(res), true)
}

// Migration: a browser that still keeps its key in localStorage (before
// the cookie) sends it once in the body; the answer moves it into the
// cookie (a new key), and the page then deletes its copy.
func TestBookmarkOldKeyMovesIntoTheCookie(t *testing.T) {
	r, l, k := passkeyPairRig(t)
	pairWithKey(t, r, k, webdevices.RoleControl)
	_, f := postFrom(t, l, bmOrigin, "/login/start", `{}`)
	opts, _ := json.Marshal(f["options"])
	_, f = postFrom(t, l, bmOrigin, "/login/finish", `{"login_id":"`+f["login_id"].(string)+`","credential":`+string(k.get(opts))+`,"remember":true}`)
	old, _ := f["cookie_key"].(string)
	res := postCookie(t, l, bmOrigin, "/login/device", keyBody(old), "", true)
	g := replyOf(t, res)
	if res.StatusCode != http.StatusOK || g["token"] == nil {
		t.Fatalf("old key sign-in: %d %v", res.StatusCode, g)
	}
	c := deviceCookieOf(res)
	checkKeyCookie(t, c, true)
	if c.Value == old {
		t.Fatal("the old key was not replaced")
	}
}
