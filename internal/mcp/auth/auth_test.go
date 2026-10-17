package auth

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// fixture is a fake MCP server that needs a token from a fake AS.
type fixture struct {
	as  *mcptest.AuthServer
	mcp *mcptest.HTTPServer
}

func newFixture(t *testing.T, ao mcptest.AuthOptions, scopes ...string) fixture {
	t.Helper()
	as := mcptest.NewAuthServer(t, ao)
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer(), Scopes: scopes})
	return fixture{as: as, mcp: srv}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

// browse plays the browser: it opens the authorize URL and follows the
// redirect to the loopback listener, returning the page it shows.
func browse(t *testing.T, authURL string) string {
	t.Helper()
	resp, err := http.Get(authURL)
	if err != nil {
		t.Fatalf("browser: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// noFollow fetches the authorize URL without following the redirect and
// returns the callback URL the browser would open.
func noFollow(t *testing.T, authURL string) string {
	t.Helper()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.Header.Get("Location")
}

// Spec: discovery follows the resource_metadata URL of the 401 to the
// authorization server metadata.
func TestDiscoverFromHeader(t *testing.T) {
	f := newFixture(t, mcptest.AuthOptions{}, "mcp", "read")
	md, err := Discover(ctx(t), nil, f.mcp.MCPURL(), f.mcp.URL+"/.well-known/oauth-protected-resource/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if md.Issuer != f.as.Issuer() || md.TokenEndpoint != f.as.Issuer()+"/token" ||
		md.RegistrationEndpoint == "" || md.Resource != f.mcp.MCPURL() || strings.Join(md.Scopes, " ") != "mcp read" {
		t.Errorf("metadata %+v", md)
	}
}

// Spec: without the header, the well-known URLs are probed (path form
// first); an issuer with a path finds OpenID metadata too.
func TestDiscoverProbes(t *testing.T) {
	f := newFixture(t, mcptest.AuthOptions{Path: "/tenant", OIDCOnly: true})
	md, err := Discover(ctx(t), nil, f.mcp.MCPURL(), "")
	if err != nil {
		t.Fatal(err)
	}
	if md.Issuer != f.as.Issuer() || md.AuthorizationEndpoint != f.as.Issuer()+"/authorize" {
		t.Errorf("metadata %+v", md)
	}
}

// Spec: metadata whose issuer is not the one asked for, or an AS without
// S256 PKCE, is refused.
func TestDiscoverRefusesBadMetadata(t *testing.T) {
	for name, o := range map[string]mcptest.AuthOptions{"issuer": {WrongIssuer: true}, "pkce": {PlainOnly: true}} {
		f := newFixture(t, o)
		if _, err := Discover(ctx(t), nil, f.mcp.MCPURL(), ""); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// Spec (MC-6 main flow): DCR as a native public client with the
// loopback redirect, authorize with PKCE S256 + state + resource + scope,
// the browser lands on the 127.0.0.1 listener, the code is exchanged
// form-encoded with the verifier, and the listener is closed after its
// one use.
func TestSignInLoopbackDCR(t *testing.T) {
	f := newFixture(t, mcptest.AuthOptions{ExpiresIn: 3600}, "mcp")
	fl, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL(), ResourceMetadata: f.mcp.URL + "/.well-known/oauth-protected-resource/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	defer fl.Close()
	ru, _ := url.Parse(fl.RedirectURI())
	if ru.Hostname() != "127.0.0.1" || ru.Path != "/callback" || ru.Port() == "" {
		t.Errorf("redirect %s", fl.RedirectURI())
	}
	reg := f.as.Regs()
	if len(reg) != 1 || reg[0]["application_type"] != "native" || reg[0]["token_endpoint_auth_method"] != "none" {
		t.Fatalf("registration %v", reg)
	}
	if uris, _ := reg[0]["redirect_uris"].([]any); len(uris) != 1 || uris[0] != fl.RedirectURI() {
		t.Errorf("registered redirect %v", reg[0]["redirect_uris"])
	}
	au, _ := url.Parse(fl.AuthURL())
	q := au.Query()
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
		q.Get("state") == "" || q.Get("resource") != f.mcp.MCPURL() || q.Get("scope") != "mcp" || q.Get("redirect_uri") != fl.RedirectURI() {
		t.Errorf("authorize URL %s", fl.AuthURL())
	}
	if page := browse(t, fl.AuthURL()); !strings.Contains(page, "close this tab") {
		t.Errorf("callback page %q", page)
	}
	tok, err := fl.Wait(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if !f.as.Valid(tok.AccessToken) || tok.RefreshToken == "" || tok.Issuer != f.as.Issuer() ||
		tok.Resource != f.mcp.MCPURL() || tok.TokenEndpoint != f.as.Issuer()+"/token" || tok.ExpiresAtMs == 0 {
		t.Errorf("tokens %+v", tok)
	}
	form := f.as.Forms()[0]
	if form.Get("grant_type") != "authorization_code" || form.Get("code_verifier") == "" || form.Get("resource") != f.mcp.MCPURL() {
		t.Errorf("token form %v", form)
	}
	if c, err := net.DialTimeout("tcp", ru.Host, 300*time.Millisecond); err == nil {
		c.Close()
		t.Error("the loopback listener is still open after the sign-in")
	}
}

// Spec: CIMD is used when the AS advertises it and has no registration
// endpoint: the client id is our metadata document URL.
func TestSignInCIMD(t *testing.T) {
	f := newFixture(t, mcptest.AuthOptions{NoDCR: true, CIMD: true})
	fl, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL(), CIMDURL: "https://docs.example/mcp/client.json"})
	if err != nil {
		t.Fatal(err)
	}
	defer fl.Close()
	if q, _ := url.Parse(fl.AuthURL()); q.Query().Get("client_id") != "https://docs.example/mcp/client.json" {
		t.Errorf("client id in %s", fl.AuthURL())
	}
	browse(t, fl.AuthURL())
	if _, err := fl.Wait(ctx(t)); err != nil {
		t.Fatal(err)
	}
}

// Spec (Slack): a pre-registered confidential client from the config:
// no registration, the secret goes to the token endpoint, the config
// scopes are asked for.
func TestSignInPreregistered(t *testing.T) {
	f := newFixture(t, mcptest.AuthOptions{NoDCR: true, PreClientID: "A0SLACK", PreSecret: "slack-secret-xyz"})
	fl, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL(), ClientID: "A0SLACK", ClientSecret: "slack-secret-xyz",
		Scopes: []string{"search:read.public", "chat:write"}})
	if err != nil {
		t.Fatal(err)
	}
	defer fl.Close()
	if q, _ := url.Parse(fl.AuthURL()); q.Query().Get("scope") != "search:read.public chat:write" {
		t.Errorf("scope in %s", fl.AuthURL())
	}
	browse(t, fl.AuthURL())
	tok, err := fl.Wait(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if tok.ClientSecret != "slack-secret-xyz" || len(f.as.Regs()) != 0 {
		t.Errorf("tokens %+v, registrations %d", tok, len(f.as.Regs()))
	}
}

// Spec: no DCR, no CIMD and no client id in the config: the server needs
// an app (Slack-like), said clearly.
func TestSignInNeedsApp(t *testing.T) {
	f := newFixture(t, mcptest.AuthOptions{NoDCR: true})
	_, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL()})
	if !errors.Is(err, ErrNeedsApp) {
		t.Fatalf("err %v", err)
	}
}

// Spec (SSH): pasting the callback URL from the browser's address bar
// completes the sign-in; a URL with another state is refused and ends
// the flow.
func TestSignInPaste(t *testing.T) {
	f := newFixture(t, mcptest.AuthOptions{})
	fl, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL()})
	if err != nil {
		t.Fatal(err)
	}
	defer fl.Close()
	cb := noFollow(t, fl.AuthURL())
	if err := fl.Paste("not a url at all"); err == nil {
		t.Error("garbage accepted")
	}
	if err := fl.Paste("  " + cb + "\n"); err != nil {
		t.Fatal(err)
	}
	tok, err := fl.Wait(ctx(t))
	if err != nil || !f.as.Valid(tok.AccessToken) {
		t.Fatalf("paste: %+v %v", tok, err)
	}

	fl2, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL()})
	if err != nil {
		t.Fatal(err)
	}
	defer fl2.Close()
	cb2 := noFollow(t, fl2.AuthURL())
	bad := strings.Replace(cb2, "state=", "state=x", 1)
	if err := fl2.Paste(bad); err == nil || !strings.Contains(err.Error(), "state") {
		t.Errorf("wrong state: %v", err)
	}
	if _, err := fl2.Wait(ctx(t)); err == nil {
		t.Error("a flow with a wrong state went on")
	}
}

// Spec (RFC 9207): an iss in the callback that is not the issuer is
// refused.
func TestSignInIssMismatch(t *testing.T) {
	f := newFixture(t, mcptest.AuthOptions{})
	fl, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL()})
	if err != nil {
		t.Fatal(err)
	}
	defer fl.Close()
	cb := noFollow(t, fl.AuthURL())
	u, _ := url.Parse(cb)
	q := u.Query()
	q.Set("iss", "https://evil.example")
	u.RawQuery = q.Encode()
	if err := fl.Paste(u.String()); err == nil || !strings.Contains(err.Error(), "issuer") {
		t.Errorf("iss mismatch: %v", err)
	}
}

// Spec: the listener answers only /callback and only on 127.0.0.1, and
// it ends after its lifetime.
func TestListenerScope(t *testing.T) {
	f := newFixture(t, mcptest.AuthOptions{})
	fl, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL(), Lifetime: 400 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer fl.Close()
	ru, _ := url.Parse(fl.RedirectURI())
	resp, err := http.Get("http://" + ru.Host + "/other")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("/other: %d", resp.StatusCode)
	}
	start := time.Now()
	if _, err := fl.Wait(context.Background()); err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("lifetime: %v after %v", err, time.Since(start))
	}
}

// Spec: a fixed port (mcp_oauth_port) is used as asked.
func TestFixedPort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	f := newFixture(t, mcptest.AuthOptions{})
	fl, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL(), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer fl.Close()
	if fl.Port() != port || !strings.Contains(fl.RedirectURI(), ":"+strconv.Itoa(port)+"/") {
		t.Errorf("port %d, redirect %s", fl.Port(), fl.RedirectURI())
	}
}

// Spec: Refresh posts grant_type=refresh_token with the resource to the
// stored token endpoint and keeps the issuer binding.
func TestRefresh(t *testing.T) {
	f := newFixture(t, mcptest.AuthOptions{})
	fl, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL()})
	if err != nil {
		t.Fatal(err)
	}
	browse(t, fl.AuthURL())
	tok, err := fl.Wait(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	nt, err := Refresh(ctx(t), nil, tok)
	if err != nil {
		t.Fatal(err)
	}
	if nt.AccessToken == tok.AccessToken || !f.as.Valid(nt.AccessToken) || nt.Issuer != tok.Issuer || nt.Resource != tok.Resource {
		t.Errorf("refreshed %+v", nt)
	}
	forms := f.as.Forms()
	last := forms[len(forms)-1]
	if last.Get("grant_type") != "refresh_token" || last.Get("resource") != f.mcp.MCPURL() {
		t.Errorf("refresh form %v", last)
	}
	if _, err := Refresh(ctx(t), nil, ai.MCPOAuth{TokenEndpoint: tok.TokenEndpoint}); err == nil {
		t.Error("refresh without a refresh token: no error")
	}
}
