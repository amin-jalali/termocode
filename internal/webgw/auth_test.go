//go:build unix

package webgw

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock is a clock a test moves by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// newAuth is an Auth on a fake clock with a fresh launch secret.
func newAuth(t *testing.T) (*Auth, *Tokens, *fakeClock, string) {
	t.Helper()
	clk := &fakeClock{now: t0}
	ts := NewTokens(DefaultIdle, DefaultMax)
	a := NewAuth(ts, clk.Now)
	plain, err := a.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	return a, ts, clk, plain
}

// authReq is a same-origin POST /auth with a JSON body.
func authReq(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(body))
	r.Host = testSite.Hosts[0]
	r.Header.Set("Origin", "http://"+testSite.Hosts[0])
	r.Header.Set("Content-Type", "application/json")
	return r
}

// do runs r through the guard and a, as the gateway does.
func do(a http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	Guard(testSite, a).ServeHTTP(w, r)
	return w
}

func secretBody(s string) string {
	b, _ := json.Marshal(map[string]string{"secret": s})
	return string(b)
}

func TestAuthOK(t *testing.T) {
	a, ts, _, plain := newAuth(t)
	w := do(a, authReq(secretBody(plain)))
	if w.Code != http.StatusOK {
		t.Fatalf("code %d: %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type %q", ct)
	}
	var got struct {
		Token string `json:"token"`
		IdleS int    `json:"idle_s"`
		MaxS  int    `json:"max_s"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.IdleS != 900 || got.MaxS != 43200 {
		t.Fatalf("idle_s %d max_s %d", got.IdleS, got.MaxS)
	}
	if strings.Contains(w.Body.String(), plain) {
		t.Fatal("the answer repeats the launch secret")
	}
	sess, err := ts.Check(got.Token, t0)
	if err != nil {
		t.Fatalf("issued token: %v", err)
	}
	if sess.Role != RoleControl || !strings.HasPrefix(sess.Device, "loc-") {
		t.Fatalf("session %+v", sess)
	}
	// The secret is one-time: the same request again fails.
	if w := do(a, authReq(secretBody(plain))); w.Code != http.StatusUnauthorized {
		t.Fatalf("second redeem: code %d", w.Code)
	}
}

// TestAuthFailsTheSameWay: wrong, used and expired secrets get one answer,
// so a caller learns nothing about which rule refused it.
func TestAuthFailsTheSameWay(t *testing.T) {
	a, _, _, plain := newAuth(t)
	wrong := do(a, authReq(secretBody("nope")))
	_ = do(a, authReq(secretBody(plain)))
	used := do(a, authReq(secretBody(plain)))
	a2, _, clk2, plain2 := newAuth(t)
	clk2.Add(LaunchSecretTTL)
	expired := do(a2, authReq(secretBody(plain2)))
	for name, w := range map[string]*httptest.ResponseRecorder{"wrong": wrong, "used": used, "expired": expired} {
		if w.Code != http.StatusUnauthorized || w.Body.String() != `{"error":"auth_failed"}`+"\n" {
			t.Errorf("%s: %d %q", name, w.Code, w.Body)
		}
	}
}

// TestAuthWithoutSecretRefused: no secret was made yet (or it was
// replaced): nothing redeems.
func TestAuthWithoutSecretRefused(t *testing.T) {
	a := NewAuth(NewTokens(DefaultIdle, DefaultMax), nil)
	if w := do(a, authReq(secretBody(""))); w.Code != http.StatusUnauthorized {
		t.Fatalf("code %d", w.Code)
	}
}

// TestNewSecretReplacesOld: a new launch link closes the old one.
func TestNewSecretReplacesOld(t *testing.T) {
	a, _, _, old := newAuth(t)
	fresh, err := a.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if w := do(a, authReq(secretBody(old))); w.Code != http.StatusUnauthorized {
		t.Fatalf("old secret: code %d", w.Code)
	}
	if w := do(a, authReq(secretBody(fresh))); w.Code != http.StatusOK {
		t.Fatalf("new secret: code %d", w.Code)
	}
}

// TestNoCookiesEver: no answer of the auth flow sets a cookie (T4).
func TestNoCookiesEver(t *testing.T) {
	a, _, _, plain := newAuth(t)
	reqs := []*http.Request{authReq(secretBody("bad")), authReq(secretBody(plain)), authReq(secretBody(plain))}
	get := httptest.NewRequest(http.MethodGet, "/auth", nil)
	get.Host = testSite.Hosts[0]
	reqs = append(reqs, get)
	for i, r := range reqs {
		w := do(a, r)
		if v := w.Header().Values("Set-Cookie"); len(v) > 0 {
			t.Fatalf("request %d set a cookie: %q", i, v)
		}
	}
}

func TestAuthOnlyPost(t *testing.T) {
	a, _, _, _ := newAuth(t)
	r := httptest.NewRequest(http.MethodGet, "/auth", nil)
	r.Host = testSite.Hosts[0]
	if w := do(a, r); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /auth: code %d", w.Code)
	}
}
