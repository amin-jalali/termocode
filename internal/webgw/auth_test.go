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
	a := NewAuth(ts, clk.Now, nil)
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
	a := NewAuth(NewTokens(DefaultIdle, DefaultMax), nil, nil)
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

// TestAuthNeedsJSONAndOrigin: a simple cross-site form cannot send
// application/json, and the guard wants the exact Origin (T3). Neither
// refusal uses up a try of the secret.
func TestAuthNeedsJSONAndOrigin(t *testing.T) {
	a, _, _, plain := newAuth(t)
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded",
		"multipart/form-data; boundary=x", "application/json-patch+json", "application/jsonx", "json"} {
		r := authReq(secretBody(plain))
		if ct == "" {
			r.Header.Del("Content-Type")
		} else {
			r.Header.Set("Content-Type", ct)
		}
		if w := do(a, r); w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q: code %d, want 415", ct, w.Code)
		}
	}
	noOrigin := authReq(secretBody(plain))
	noOrigin.Header.Del("Origin")
	if w := do(a, noOrigin); w.Code != http.StatusForbidden {
		t.Errorf("no Origin: code %d, want 403", w.Code)
	}
	ok := authReq(secretBody(plain))
	ok.Header.Set("Content-Type", "application/json; charset=utf-8")
	if w := do(a, ok); w.Code != http.StatusOK {
		t.Fatalf("the refused requests used up the secret: code %d", w.Code)
	}
}

// TestFormPostRejected: the secret sent as a form is refused and stays
// unused.
func TestFormPostRejected(t *testing.T) {
	a, _, _, plain := newAuth(t)
	r := authReq("secret=" + plain)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if w := do(a, r); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form post: code %d", w.Code)
	}
	if w := do(a, authReq(secretBody(plain))); w.Code != http.StatusOK {
		t.Fatalf("secret used by the form: code %d", w.Code)
	}
}

// TestAuthStrictJSON: exactly one object with exactly one string field.
func TestAuthStrictJSON(t *testing.T) {
	a, _, _, plain := newAuth(t)
	for _, body := range []string{
		``, `{}`, `[]`, `null`, `"x"`, `{"secret":1}`, `{"secret":null}`,
		`{"secret":"x","extra":1}`, `{"secret":"` + plain + `"} {}`,
		`{"secret":"` + plain + `"}x`, `{"secret":"` + plain + `"`,
		`{"token":"x"}`,
	} {
		if w := do(a, authReq(body)); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: code %d, want 400", body, w.Code)
		}
	}
	if w := do(a, authReq(" "+secretBody(plain)+"\n")); w.Code != http.StatusOK {
		t.Fatalf("good body after bad ones: code %d", w.Code)
	}
}

// TestSecretInQueryRefused: a secret or token in a URL ends up in history
// and logs, so /auth refuses any query string, even with a good body (T10).
func TestSecretInQueryRefused(t *testing.T) {
	a, _, _, plain := newAuth(t)
	for _, q := range []string{"?s=" + plain, "?token=x", "?secret=" + plain, "?x"} {
		r := authReq(secretBody(plain))
		r.URL.RawQuery = q[1:]
		r.RequestURI = "/auth" + q
		if w := do(a, r); w.Code != http.StatusBadRequest {
			t.Errorf("query %q: code %d, want 400", q, w.Code)
		}
	}
	if w := do(a, authReq(secretBody(plain))); w.Code != http.StatusOK {
		t.Fatalf("the refused requests used up the secret: code %d", w.Code)
	}
}

// TestGlobalRate429: /auth takes AuthRatePerSecond requests per second in
// all; more get 429 before the body is read, so they cannot try secrets
// (T13).
func TestGlobalRate429(t *testing.T) {
	if AuthRatePerSecond != 20 {
		t.Fatalf("AuthRatePerSecond = %d, want 20", AuthRatePerSecond)
	}
	a, _, clk, plain := newAuth(t)
	for i := 0; i < AuthRatePerSecond; i++ {
		if w := do(a, authReq(`{}`)); w.Code != http.StatusBadRequest {
			t.Fatalf("request %d: code %d", i, w.Code)
		}
	}
	w := do(a, authReq(secretBody(plain)))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("over the rate: code %d Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	clk.Add(time.Second)
	if w := do(a, authReq(secretBody(plain))); w.Code != http.StatusOK {
		t.Fatalf("after a second: code %d (was the secret used while limited?)", w.Code)
	}
}

// TestAuthBurnsSecretAfter5: five wrong secrets over HTTP close the link;
// the right one is refused after that (T13).
func TestAuthBurnsSecretAfter5(t *testing.T) {
	a, _, _, plain := newAuth(t)
	for i := 0; i < MaxBadTries; i++ {
		if w := do(a, authReq(secretBody("wrong"))); w.Code != http.StatusUnauthorized {
			t.Fatalf("wrong try %d: code %d", i, w.Code)
		}
	}
	if w := do(a, authReq(secretBody(plain))); w.Code != http.StatusUnauthorized {
		t.Fatalf("right secret after the burn: code %d", w.Code)
	}
}

// TestBodyLimit4KiB: a body over 4 KiB never reaches the decoder.
func TestBodyLimit4KiB(t *testing.T) {
	a, _, _, plain := newAuth(t)
	big := `{"secret":"` + plain + `","pad":"` + strings.Repeat("a", MaxBodyBytes) + `"}`
	if w := do(a, authReq(big)); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big body: code %d", w.Code)
	}
	r := authReq(big)
	r.ContentLength = -1 // undeclared length: cut at the cap while reading
	if w := do(a, r); w.Code != http.StatusBadRequest {
		t.Fatalf("big chunked body: code %d", w.Code)
	}
	if w := do(a, authReq(secretBody(plain))); w.Code != http.StatusOK {
		t.Fatalf("the big bodies used up the secret: code %d", w.Code)
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
