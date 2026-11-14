package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/secrets/secretstest"
)

// covOAuthIdP is a fake identity provider: /token serves every grant
// (refresh token "bad" fails), /device starts a device sign-in, /api
// records the Authorization header it got.
type covOAuthIdP struct {
	srv    *httptest.Server
	mu     sync.Mutex
	grants []string
	auth   []string
}

func newCovOAuthIdP(t *testing.T) *covOAuthIdP {
	t.Helper()
	p := &covOAuthIdP{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		p.mu.Lock()
		defer p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			g := r.Form.Get("grant_type")
			p.grants = append(p.grants, g)
			if g == "refresh_token" && r.Form.Get("refresh_token") == "bad" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"tok-` + strings.ReplaceAll(g, ":", "-") + `","expires_in":3600,"refresh_token":"r2"}`))
		case "/device":
			_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"WXYZ-1234","verification_uri":"https://idp.example/device","verification_uri_complete":"https://idp.example/device?c=WXYZ","interval":1,"expires_in":60}`))
		case "/nodevice":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unsupported"}`))
		default:
			p.auth = append(p.auth, r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *covOAuthIdP) seen() ([]string, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.grants...), append([]string(nil), p.auth...)
}

func covOAuthStore(t *testing.T) secrets.Store {
	t.Helper()
	store := secrets.NewStore(secretstest.NewMemory(), secrets.Options{Dir: t.TempDir()})
	old := httpOAuthStore
	httpOAuthStore = func() secrets.Store { return store }
	t.Cleanup(func() { httpOAuthStore = old })
	return store
}

func covOAuthRun(spec, apiURL string) *httpRun {
	return &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: apiURL,
		Directives: map[string]string{httpclient.OAuth2Directive: spec}}}, label: "api"}
}

// A cached token with a refresh token: the refresh is used; a refresh
// that fails falls back to a new client-credentials token. A bad spec
// is not sent; a broken cache entry is ignored.
func TestCovHTTPOAuthRefreshAndFallback(t *testing.T) {
	m, _, _ := covHTTP(t)
	idp := newCovOAuthIdP(t)
	store := covOAuthStore(t)
	spec := "grant=client_credentials token_url=" + idp.srv.URL + "/token client_id=app client_secret=s"
	cfg, err := httpclient.ParseOAuth2(spec)
	if err != nil {
		t.Fatal(err)
	}
	save := func(tok httpclient.OAuth2Token) {
		if err := httpOAuthSave(cfg.CacheKey(), tok); err != nil {
			t.Fatal(err)
		}
	}
	save(httpclient.OAuth2Token{AccessToken: "old", RefreshToken: "good", Expiry: time.Now().Add(-time.Hour)})
	loop := newCovHTTPLoop(t, m)
	s := m.httpS()
	c := m.startHTTPRun(covOAuthRun(spec, idp.srv.URL+"/api"), httpclient.Hooks{})
	if s.oauthWait == nil {
		t.Fatal("no wait")
	}
	loop.run(c, func() bool { return s.last != nil && s.last.resp != nil })
	save(httpclient.OAuth2Token{AccessToken: "old", RefreshToken: "bad", Expiry: time.Now().Add(-time.Hour)})
	s.last = nil
	loop.run(m.startHTTPRun(covOAuthRun(spec, idp.srv.URL+"/api"), httpclient.Hooks{}), func() bool { return s.last != nil && s.last.resp != nil })
	grants, auth := idp.seen()
	if strings.Join(grants, ",") != "refresh_token,refresh_token,client_credentials" {
		t.Errorf("grants %v", grants)
	}
	if len(auth) != 2 || auth[0] != "Bearer tok-refresh_token" || auth[1] != "Bearer tok-client_credentials" {
		t.Errorf("auth %q", auth)
	}
	// A broken cache entry reads as no token.
	if err := store.Set(cfg.CacheKey(), "not json"); err != nil {
		t.Fatal(err)
	}
	if tok := httpOAuthCached(cfg.CacheKey()); tok.AccessToken != "" {
		t.Errorf("broken entry gave %+v", tok)
	}
	// A bad spec says why and sends nothing.
	covHTTPReset(m)
	if c, wait := m.httpOAuthGate(covOAuthRun("grant=nope", idp.srv.URL+"/api"), httpclient.Hooks{}); !wait || c == nil || !strings.Contains(covHTTPToast(m), "OAuth 2.0: request not sent") {
		t.Errorf("bad spec: %v %q", wait, covHTTPToast(m))
	}
	// No store (locked keyring): nothing is cached and nothing fails.
	httpOAuthStore = func() secrets.Store { return nil }
	if httpOAuthSave("k", httpclient.OAuth2Token{AccessToken: "x"}) != nil || httpOAuthCached("k").AccessToken != "" {
		t.Error("nil store")
	}
	// A failed token fetch is a toast.
	covHTTPReset(m)
	bad := "grant=client_credentials token_url=http://127.0.0.1:1/token client_id=app"
	loop.run(m.startHTTPRun(covOAuthRun(bad, idp.srv.URL+"/api"), httpclient.Hooks{}), func() bool { return s.oauthWait == nil })
	if !strings.Contains(covHTTPToast(m), "OAuth 2.0: request not sent") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}

// The device grant shows the code card (the complete link), polls and
// sends; Open opens the page, Cancel stops the wait; a stale answer is
// dropped; a device start that fails is a toast.
func TestCovHTTPOAuthDeviceGrant(t *testing.T) {
	m, _, _ := covHTTP(t)
	idp := newCovOAuthIdP(t)
	covOAuthStore(t)
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { osOpen = old })
	spec := "grant=device_code token_url=" + idp.srv.URL + "/token device_url=" + idp.srv.URL + "/device client_id=app"
	loop := newCovHTTPLoop(t, m)
	s := m.httpS()
	loop.run(m.startHTTPRun(covOAuthRun(spec, idp.srv.URL+"/api"), httpclient.Hooks{}), func() bool { return m.confirmOpen })
	if m.confirmKind != confirmKindHTTPOAuthDevice || !strings.Contains(cardText(m), "WXYZ-1234") {
		t.Fatalf("card: %s", cardText(m))
	}
	m.handleHTTPConfirm("open")
	if len(opened) != 1 || opened[0] != "https://idp.example/device?c=WXYZ" {
		t.Errorf("opened %q", opened)
	}
	loop.run(nil, func() bool { return s.last != nil && s.last.resp != nil })
	if _, auth := idp.seen(); len(auth) != 1 || !strings.HasPrefix(auth[0], "Bearer tok-urn") {
		t.Errorf("auth %q", auth)
	}
	if m.confirmOpen {
		t.Error("the card stayed open")
	}
	// Cancel: the wait stops; its late answers are dropped.
	httpOAuthStore = func() secrets.Store { return nil } // no cache: ask again
	loop.run(m.startHTTPRun(covOAuthRun(spec, idp.srv.URL+"/api"), httpclient.Hooks{}), func() bool { return m.confirmOpen })
	w := s.oauthWait
	m.handleHTTPConfirm("cancel")
	if s.oauthWait != nil {
		t.Fatal("cancel kept the wait")
	}
	if c, ok := m.handleHTTPOAuthMsg(httpOAuthDoneMsg{w: w}); !ok || c != nil {
		t.Error("a stale done was used")
	}
	if c, ok := m.handleHTTPOAuthMsg(httpOAuthDeviceMsg{w: w}); !ok || c != nil {
		t.Error("a stale device answer was used")
	}
	if c, ok := m.handleHTTPConfirm("open"); !ok || c != nil {
		t.Error("open without a wait")
	}
	// The device start fails.
	covHTTPReset(m)
	spec = "grant=device_code token_url=" + idp.srv.URL + "/token device_url=" + idp.srv.URL + "/nodevice client_id=app"
	loop.run(m.startHTTPRun(covOAuthRun(spec, idp.srv.URL+"/api"), httpclient.Hooks{}), func() bool { return s.oauthWait == nil })
	if !strings.Contains(covHTTPToast(m), "OAuth 2.0: request not sent") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}

// The authorization code grant opens the browser on the IdP's page; the
// loopback redirect brings the code back, it is exchanged and the
// request goes out. A second send replaces the first wait.
func TestCovHTTPOAuthAuthCodeGrant(t *testing.T) {
	m, _, _ := covHTTP(t)
	idp := newCovOAuthIdP(t)
	covOAuthStore(t)
	var mu sync.Mutex
	var opened []string
	old := osOpen
	osOpen = func(u string) error {
		mu.Lock()
		opened = append(opened, u)
		mu.Unlock()
		pu, err := url.Parse(u)
		if err != nil {
			return err
		}
		q := pu.Query()
		if q.Get("client_id") == "late" {
			return nil // the user never finishes this one
		}
		cb := q.Get("redirect_uri") + "?code=c0de&state=" + url.QueryEscape(q.Get("state"))
		go func() {
			if resp, err := http.Get(cb); err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
	t.Cleanup(func() { osOpen = old })
	loop := newCovHTTPLoop(t, m)
	s := m.httpS()
	// One the user never finishes, then a second send replaces it.
	late := "grant=authorization_code auth_url=" + idp.srv.URL + "/auth token_url=" + idp.srv.URL + "/token client_id=late"
	c1 := m.startHTTPRun(covOAuthRun(late, idp.srv.URL+"/api"), httpclient.Hooks{})
	if !m.confirmOpen || m.confirmKind != confirmKindHTTPOAuthBrowser {
		t.Fatalf("no browser card: %s", cardText(m))
	}
	first := s.oauthWait
	spec := "grant=authorization_code auth_url=" + idp.srv.URL + "/auth token_url=" + idp.srv.URL + "/token client_id=app"
	c2 := m.startHTTPRun(covOAuthRun(spec, idp.srv.URL+"/api"), httpclient.Hooks{})
	if s.oauthWait == first {
		t.Fatal("the second send did not replace the wait")
	}
	loop.run(c1, func() bool { return true })
	loop.run(c2, func() bool { return s.last != nil && s.last.resp != nil })
	grants, auth := idp.seen()
	if strings.Join(grants, ",") != "authorization_code" || len(auth) != 1 || auth[0] != "Bearer tok-authorization_code" {
		t.Errorf("grants %v auth %q", grants, auth)
	}
	mu.Lock()
	n := len(opened)
	mu.Unlock()
	if n != 2 {
		t.Errorf("opened %d pages", n)
	}
	// A cached token that is still good is used without asking.
	cfg, _ := httpclient.ParseOAuth2(spec)
	if tok := httpOAuthCached(cfg.CacheKey()); tok.AccessToken != "tok-authorization_code" {
		t.Errorf("cached %+v", tok)
	}
	run := covOAuthRun(spec, idp.srv.URL+"/api")
	if c, wait := m.httpOAuthGate(run, httpclient.Hooks{}); wait || c != nil || run.rr.Header("Authorization") != "Bearer tok-authorization_code" {
		t.Errorf("cached gate: %v %+v", wait, run.rr.Headers)
	}
	// The browser card: Open opens the page again.
	m.httpOAuthBrowserCard(&httpOAuthWait{})
	s.oauthWait = &httpOAuthWait{url: "https://idp.example/x", cancel: func() {}}
	m.handleHTTPConfirm("open")
	mu.Lock()
	last := opened[len(opened)-1]
	mu.Unlock()
	if last != "https://idp.example/x" {
		t.Errorf("open again: %q", last)
	}
}
