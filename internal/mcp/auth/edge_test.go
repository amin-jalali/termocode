package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// canary is a fake secret built from parts.
var canary = "sk-" + "canary-" + "auth-9915"

// stubAS is a hand-made authorization server: each path answers with a
// handler that sees the server's own URL.
type stubAS struct {
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]func(base string, w http.ResponseWriter, r *http.Request)
	forms  []url.Values
	basic  []string
}

func newStub(t *testing.T) *stubAS {
	t.Helper()
	s := &stubAS{routes: map[string]func(string, http.ResponseWriter, *http.Request){}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		h := s.routes[r.URL.Path]
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			s.forms = append(s.forms, r.PostForm)
			u, p, _ := r.BasicAuth()
			if u != "" {
				s.basic = append(s.basic, u+":"+p)
			}
		}
		s.mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(s.srv.URL, w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stubAS) on(path string, h func(base string, w http.ResponseWriter, r *http.Request)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[path] = h
}

func (s *stubAS) json(path string, status int, body func(base string) any) {
	s.on(path, func(base string, w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body(base))
	})
}

// asMeta serves good AS metadata at the root well-known URL; edit
// changes it before it is sent.
func (s *stubAS) asMeta(edit func(base string, m map[string]any)) {
	s.json("/.well-known/oauth-authorization-server", 200, func(base string) any {
		m := map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token",
			"code_challenge_methods_supported": []string{"S256"}}
		if edit != nil {
			edit(base, m)
		}
		return m
	})
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

func noCanary(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), canary) {
		t.Fatalf("the error shows a secret: %v", err)
	}
}

// Spec (MC-6): discovery refuses metadata URLs that are not https (or
// on this machine), and stops when the context ends.
func TestDiscoverRefusesBadURLs(t *testing.T) {
	if _, err := Discover(ctx(t), nil, "https://mcp.example/mcp", "http://evil.example/prm"); err == nil || !strings.Contains(err.Error(), "insecure") {
		t.Fatalf("http metadata: err = %v", err)
	}
	if _, err := Discover(ctx(t), nil, "https://mcp.example/mcp", "/no-host"); err == nil || !strings.Contains(err.Error(), "invalid OAuth URL") {
		t.Fatalf("no host: err = %v", err)
	}
	s := newStub(t)
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Discover(c, nil, s.srv.URL+"/mcp", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: err = %v", err)
	}
}

// Spec (MC-6): a 2025-03-26 server has no resource metadata: its origin
// is the authorization server.
func TestDiscoverOriginFallback(t *testing.T) {
	s := newStub(t)
	s.asMeta(func(base string, m map[string]any) {
		m["registration_endpoint"] = "http://evil.example/register" // insecure: dropped
		m["device_authorization_endpoint"] = base + "/device"
		m["revocation_endpoint"] = "http://evil.example/revoke" // insecure: dropped
	})
	md, err := Discover(ctx(t), nil, s.srv.URL+"/mcp", "")
	if err != nil {
		t.Fatal(err)
	}
	if md.Issuer != s.srv.URL || md.Resource != s.srv.URL+"/mcp" || md.RegistrationEndpoint != "" ||
		md.DeviceEndpoint != s.srv.URL+"/device" || md.RevocationEndpoint != "" {
		t.Fatalf("metadata %+v", md)
	}
}

// Spec (MC-6): metadata that points elsewhere or lacks what a sign-in
// needs is refused with a clear reason.
func TestDiscoverRefusesBadMetadata2(t *testing.T) {
	cases := map[string]struct {
		prep func(s *stubAS)
		want string
	}{
		"other resource": {func(s *stubAS) {
			s.json("/.well-known/oauth-protected-resource/mcp", 200, func(base string) any {
				return map[string]any{"resource": "https://other.example/mcp", "authorization_servers": []string{base}}
			})
		}, "names another resource"},
		"insecure issuer": {func(s *stubAS) {
			s.json("/.well-known/oauth-protected-resource/mcp", 200, func(string) any {
				return map[string]any{"authorization_servers": []string{"http://evil.example"}}
			})
		}, "insecure"},
		"no metadata": {func(s *stubAS) {}, "no OAuth metadata for"},
		"no token endpoint": {func(s *stubAS) {
			s.asMeta(func(_ string, m map[string]any) { delete(m, "token_endpoint") })
		}, "no authorization or token endpoint"},
		"insecure authorize": {func(s *stubAS) {
			s.asMeta(func(_ string, m map[string]any) { m["authorization_endpoint"] = "http://evil.example/authorize" })
		}, "insecure"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStub(t)
			c.prep(s)
			_, err := Discover(ctx(t), nil, s.srv.URL+"/mcp", "")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// Spec (MC-6): a sign-in port that is taken fails with the port and the
// setting to change.
func TestBeginPortBusy(t *testing.T) {
	s := newStub(t)
	s.asMeta(nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	_, err = Begin(ctx(t), Config{ServerURL: s.srv.URL + "/mcp", ClientID: "app", Port: port})
	if err == nil || !strings.Contains(err.Error(), "mcp_oauth_port") {
		t.Fatalf("err = %v", err)
	}
}

// Spec (MC-6): dynamic registration failures say what to do; the
// listener is closed again.
func TestBeginRegistrationFailures(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"approved clients only": {403, `{}`, "only approved clients"},
		"server error":          {500, `{"error":"server_error","error_description":"try later"}`, "HTTP 500: server_error: try later"},
		"no client id":          {201, `{"client_name":"x"}`, "no client_id"},
		"long text":             {500, strings.Repeat("e", 300), "…"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStub(t)
			s.asMeta(func(base string, m map[string]any) { m["registration_endpoint"] = base + "/register" })
			s.on("/register", func(_ string, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			})
			port := freePort(t)
			_, err := Begin(ctx(t), Config{ServerURL: s.srv.URL + "/mcp", Port: port})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if c.want == "…" && len(err.Error()) > 300 {
				t.Fatalf("the server text was not cut: %d chars", len(err.Error()))
			}
			ln, lerr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if lerr != nil {
				t.Fatalf("the listener stayed open after the failure: %v", lerr)
			}
			ln.Close()
		})
	}
	t.Run("unreachable", func(t *testing.T) {
		s := newStub(t)
		dead := httptest.NewServer(http.NotFoundHandler())
		deadURL := dead.URL
		dead.Close()
		s.asMeta(func(_ string, m map[string]any) { m["registration_endpoint"] = deadURL + "/register" })
		_, err := Begin(ctx(t), Config{ServerURL: s.srv.URL + "/mcp", Port: freePort(t)})
		if err == nil || !strings.Contains(err.Error(), "client registration:") {
			t.Fatalf("err = %v", err)
		}
	})
}

// signIn runs Begin, pastes a callback with the right state and the
// code "c1", and waits for the tokens.
func signIn(t *testing.T, s *stubAS, cfg Config) (ai.MCPOAuth, error) {
	t.Helper()
	cfg.ServerURL = s.srv.URL + "/mcp"
	cfg.Port = freePort(t)
	f, err := Begin(ctx(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Paste(f.RedirectURI() + "?code=c1&state=" + url.QueryEscape(f.state)); err != nil {
		t.Fatal(err)
	}
	return f.Wait(ctx(t))
}

// Spec (MC-6): a registered client with a secret, where the server
// takes only HTTP Basic, sends the secret that way, never in the form.
// A registration that gives a secret with "none" uses Basic too.
func TestSignInClientSecretBasic(t *testing.T) {
	tok := func(_ string, w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"at-1","token_type":"Bearer","expires_in":60}`)
	}
	t.Run("preregistered", func(t *testing.T) {
		s := newStub(t)
		s.asMeta(func(_ string, m map[string]any) {
			m["token_endpoint_auth_methods_supported"] = []string{"client_secret_basic"}
			m["authorization_endpoint"] = s.srv.URL + "/authorize?tenant=a"
		})
		s.on("/token", tok)
		e, err := signIn(t, s, Config{ClientID: "app", ClientSecret: canary})
		if err != nil {
			t.Fatal(err)
		}
		if e.AuthMethod != "client_secret_basic" || e.AccessToken != "at-1" || e.ExpiresAtMs == 0 {
			t.Fatalf("tokens %+v", e)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.basic) != 1 || s.basic[0] != "app:"+canary {
			t.Fatalf("basic auth %v", s.basic)
		}
		if f := s.forms[len(s.forms)-1]; f.Get("client_secret") != "" || f.Get("client_id") != "" {
			t.Fatalf("the form carries the client: %v", f)
		}
	})
	t.Run("registered with none", func(t *testing.T) {
		s := newStub(t)
		s.asMeta(func(base string, m map[string]any) { m["registration_endpoint"] = base + "/register" })
		s.json("/register", 201, func(string) any {
			return map[string]any{"client_id": "dyn", "client_secret": canary, "token_endpoint_auth_method": "none"}
		})
		s.on("/token", tok)
		e, err := signIn(t, s, Config{})
		if err != nil {
			t.Fatal(err)
		}
		if e.AuthMethod != "client_secret_basic" || e.ClientID != "dyn" {
			t.Fatalf("tokens %+v", e)
		}
	})
}

// Spec (MC-6): the authorize URL keeps a query the endpoint already has.
func TestAuthURLKeepsEndpointQuery(t *testing.T) {
	s := newStub(t)
	s.asMeta(func(base string, m map[string]any) { m["authorization_endpoint"] = base + "/authorize?tenant=a" })
	f, err := Begin(ctx(t), Config{ServerURL: s.srv.URL + "/mcp", ClientID: "app", Port: freePort(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	u, _ := url.Parse(f.AuthURL())
	if u.Query().Get("tenant") != "a" || u.Query().Get("state") == "" || strings.Count(f.AuthURL(), "?") != 1 {
		t.Fatalf("authorize URL %s", f.AuthURL())
	}
}

// Spec (MC-6): the browser callback: a refusal, a missing code and a
// second callback each fail; the page escapes the server's text.
func TestCallbackRefusals(t *testing.T) {
	s := newStub(t)
	s.asMeta(nil)
	begin := func() *Flow {
		f, err := Begin(ctx(t), Config{ServerURL: s.srv.URL + "/mcp", ClientID: "app", Port: freePort(t)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(f.Close)
		return f
	}
	f := begin()
	resp, err := http.Get(f.RedirectURI() + "?state=" + url.QueryEscape(f.state) + "&error=access_denied&error_description=" + url.QueryEscape("<b>no</b>"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || strings.Contains(string(b), "<b>") || !strings.Contains(string(b), "&lt;b&gt;no&lt;/b&gt;") {
		t.Fatalf("page %d %s", resp.StatusCode, b)
	}
	if _, err := f.Wait(ctx(t)); err == nil || !strings.Contains(err.Error(), "the server refused: access_denied") {
		t.Fatalf("Wait err = %v", err)
	}

	f = begin()
	if err := f.deliver(url.Values{"state": {f.state}}); err == nil || !strings.Contains(err.Error(), "no code") {
		t.Fatalf("no code: err = %v", err)
	}
	if err := f.deliver(url.Values{"state": {f.state}, "code": {"c"}}); err == nil || !strings.Contains(err.Error(), "already got its answer") {
		t.Fatalf("second callback: err = %v", err)
	}
}

// Spec (MC-6): Wait ends with its context.
func TestWaitCancelled(t *testing.T) {
	s := newStub(t)
	s.asMeta(nil)
	f, err := Begin(ctx(t), Config{ServerURL: s.srv.URL + "/mcp", ClientID: "app", Port: freePort(t)})
	if err != nil {
		t.Fatal(err)
	}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Wait(c); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

// Spec (MC-7): refresh failures say why and never show a token.
func TestRefreshFailures(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"expired grant": {400, `{"error":"invalid_grant","error_description":"refresh token expired"}`, "HTTP 400: invalid_grant: refresh token expired"},
		"no token":      {200, `{"token_type":"Bearer"}`, "no access_token"},
		"not json":      {200, `<html>`, "no access_token"},
		"wrong type":    {200, `{"access_token":"x","token_type":"mac"}`, `unsupported token type "mac"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStub(t)
			s.on("/token", func(_ string, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			})
			e := ai.MCPOAuth{TokenEndpoint: s.srv.URL + "/token", ClientID: "app", RefreshToken: canary, AccessToken: "old"}
			got, err := Refresh(ctx(t), nil, e)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			noCanary(t, err)
			if got.AccessToken != "old" || got.RefreshToken != canary {
				t.Fatalf("a failed refresh changed the tokens: %+v", got)
			}
		})
	}
	if _, err := Refresh(ctx(t), nil, ai.MCPOAuth{TokenEndpoint: "http://evil.example/token", RefreshToken: canary}); err == nil || !strings.Contains(err.Error(), "insecure") {
		t.Fatalf("insecure endpoint: err = %v", err)
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	_, err := Refresh(ctx(t), nil, ai.MCPOAuth{TokenEndpoint: deadURL + "/token", RefreshToken: canary})
	if err == nil || !strings.Contains(err.Error(), "token request:") {
		t.Fatalf("unreachable: err = %v", err)
	}
	noCanary(t, err)
}

// Spec (MC-9): revoking with an insecure endpoint or a refusing server
// reports the first failure, never a token.
func TestRevokeFailures(t *testing.T) {
	if err := Revoke(ctx(t), nil, ai.MCPOAuth{RevocationEndpoint: "http://evil.example/revoke", AccessToken: canary}); err == nil || !strings.Contains(err.Error(), "insecure") {
		t.Fatalf("insecure: err = %v", err)
	}
	s := newStub(t)
	s.on("/revoke", func(_ string, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
		_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
	})
	err := Revoke(ctx(t), nil, ai.MCPOAuth{RevocationEndpoint: s.srv.URL + "/revoke", AccessToken: canary, RefreshToken: canary + "r"})
	if err == nil || !strings.Contains(err.Error(), "revoke: HTTP 503: temporarily_unavailable") {
		t.Fatalf("err = %v", err)
	}
	noCanary(t, err)
	s.mu.Lock()
	n := len(s.forms)
	s.mu.Unlock()
	if n != 2 {
		t.Fatalf("%d revoke calls, want 2 (refresh, then access)", n)
	}
}

// Spec (CX-1): the device sign-in refuses an answer it can not use.
func TestBeginDeviceBadAnswers(t *testing.T) {
	cases := map[string]struct {
		status int
		body   func(base string) any
		want   string
	}{
		"http error": {400, func(string) any { return map[string]any{"error": "invalid_client"} }, "HTTP 400: invalid_client"},
		"no code":    {200, func(base string) any { return map[string]any{"user_code": "U", "verification_uri": base} }, "no code or page"},
		"insecure page": {200, func(string) any {
			return map[string]any{"device_code": "d", "user_code": "U", "verification_uri": "http://evil.example/device"}
		}, "insecure"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStub(t)
			s.json("/device", c.status, c.body)
			_, err := BeginDevice(ctx(t), DeviceConfig{DeviceEndpoint: s.srv.URL + "/device", TokenEndpoint: s.srv.URL + "/token", ClientID: "app"})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
	if _, err := BeginDevice(ctx(t), DeviceConfig{DeviceEndpoint: "https://a.example/d", TokenEndpoint: "https://a.example/t"}); err == nil || !strings.Contains(err.Error(), "client id") {
		t.Fatalf("no client id: err = %v", err)
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	if _, err := BeginDevice(ctx(t), DeviceConfig{DeviceEndpoint: deadURL + "/d", TokenEndpoint: deadURL + "/t", ClientID: "app"}); err == nil || !strings.Contains(err.Error(), "device sign-in:") {
		t.Fatalf("unreachable: err = %v", err)
	}
}

// Spec (CX-1): an older server's verification_url works; with no
// interval or lifetime the RFC defaults apply; an insecure "complete"
// page is dropped.
func TestBeginDeviceDefaults(t *testing.T) {
	s := newStub(t)
	s.json("/device", 200, func(base string) any {
		return map[string]any{"device_code": "d", "user_code": "U", "verification_url": base + "/activate",
			"verification_uri_complete": "http://evil.example/activate?u=U"}
	})
	d, err := BeginDevice(ctx(t), DeviceConfig{DeviceEndpoint: s.srv.URL + "/device", TokenEndpoint: s.srv.URL + "/token", ClientID: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if d.VerificationURI() != s.srv.URL+"/activate" || d.VerificationURIComplete() != "" || d.Interval() != 5*time.Second {
		t.Fatalf("flow %q %q %v", d.VerificationURI(), d.VerificationURIComplete(), d.Interval())
	}
	if left := time.Until(d.ExpiresAt()); left < 14*time.Minute || left > 15*time.Minute {
		t.Fatalf("expires in %v", left)
	}
}

// Spec (CX-1): a poll that the server answers with an HTTP error or an
// unknown error ends the sign-in; a dead server ends it too.
func TestDevicePollServerErrors(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"http error":    {502, `bad gateway`, "HTTP 502: bad gateway"},
		"unknown error": {400, `{"error":"invalid_client"}`, "device sign-in: invalid_client"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStub(t)
			s.on("/token", func(_ string, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			})
			d := &DeviceFlow{cfg: DeviceConfig{TokenEndpoint: s.srv.URL + "/token", ClientID: "app"}, deviceCode: canary,
				interval: time.Millisecond, expires: time.Now().Add(time.Minute), sleep: (&fakeSleep{}).sleep, now: time.Now}
			_, err := d.Poll(ctx(t))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			noCanary(t, err)
		})
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	d := &DeviceFlow{cfg: DeviceConfig{TokenEndpoint: deadURL + "/token", ClientID: "app"}, deviceCode: canary,
		interval: time.Millisecond, expires: time.Now().Add(time.Minute), sleep: (&fakeSleep{}).sleep, now: time.Now}
	if _, err := d.Poll(ctx(t)); err == nil || !strings.Contains(err.Error(), "token request:") {
		t.Fatalf("dead server: err = %v", err)
	}
}

// Spec (CX-1): the real wait between polls ends with the context.
func TestSleepCtx(t *testing.T) {
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("short sleep: %v", err)
	}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(c, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled sleep: %v", err)
	}
}
