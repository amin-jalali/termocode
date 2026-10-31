// Package auth is the OAuth 2.1 client of termocode's MCP client (MC-6):
// the MCP authorization flow for remote servers (Linear, GitLab, Notion,
// Sentry, Atlassian, ClickUp; Slack with a pre-registered app).
//
//  1. Discovery: protected-resource metadata (RFC 9728) from the 401's
//     resource_metadata URL or the well-known URLs, then the
//     authorization server metadata (RFC 8414, OpenID discovery as a
//     fallback). The issuer must match and S256 PKCE must be offered.
//  2. Client identity: a pre-registered client from mcp.json "oauth",
//     else Dynamic Client Registration (RFC 7591) as a native public
//     client, else a Client ID Metadata Document URL when the server
//     supports only that, else ErrNeedsApp.
//  3. Authorization code + PKCE S256 + state + resource (RFC 8707) +
//     scope. The browser comes back to a one-shot listener on
//     127.0.0.1 (RFC 8252), or the user pastes the callback URL from the
//     address bar (SSH: the browser is on another machine). state and,
//     when present, iss (RFC 9207) are checked.
//  4. Token exchange and refresh: form-encoded, at the discovered token
//     endpoint only.
//
// It stores nothing itself: the caller saves the ai.MCPOAuth it returns
// (credentials.json, 0600). Tokens are registered with ai.RegisterSecret
// so ai.Redact hides them.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
)

// ErrNeedsApp: the authorization server lets no unknown client in (no
// registration, no client metadata documents): the user creates an app
// and puts its client id (and secret) into mcp.json "oauth".
var ErrNeedsApp = errors.New("this server needs an app: create one at the service and put its client_id (and the secret as ${secret:NAME}) into the \"oauth\" key of the server in mcp.json")

// Metadata is what discovery found.
type Metadata struct {
	Resource              string // the resource indicator to ask tokens for
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	Scopes                []string // scopes_supported of the protected resource
	CIMD                  bool     // client_id_metadata_document_supported
	AuthMethods           []string // token_endpoint_auth_methods_supported
	DeviceEndpoint        string   // device_authorization_endpoint (RFC 8628), "" none
	RevocationEndpoint    string   // revocation_endpoint (RFC 7009), "" none
}

// DefaultLifetime is how long the loopback listener waits.
const DefaultLifetime = 5 * time.Minute

func client(hc *http.Client) *http.Client {
	if hc != nil {
		return hc
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// checkEndpoint allows https, and http only on this machine.
func checkEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid OAuth URL %q", raw)
	}
	h := u.Hostname()
	if u.Scheme == "https" || (u.Scheme == "http" && (h == "localhost" || h == "::1" || strings.HasPrefix(h, "127."))) {
		return nil
	}
	return fmt.Errorf("insecure OAuth URL %q (https only)", raw)
}

func getJSON(ctx context.Context, hc *http.Client, u string, v any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client(hc).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("GET %s: HTTP %d", u, resp.StatusCode)
	}
	return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

// wellKnown builds origin + "/.well-known/<name>" + path (RFC 8414 /
// 9728 insertion form).
func wellKnown(raw, name string) (pathForm, rootForm string) {
	u, _ := url.Parse(raw)
	origin := u.Scheme + "://" + u.Host
	p := strings.TrimSuffix(u.EscapedPath(), "/")
	rootForm = origin + "/.well-known/" + name
	if p == "" {
		return rootForm, ""
	}
	return rootForm + p, rootForm
}

// ResourceIssuer reads a protected-resource metadata document and
// returns its first authorization server ("" when it names none).
func ResourceIssuer(ctx context.Context, hc *http.Client, resourceMetadata string) (string, error) {
	if err := checkEndpoint(resourceMetadata); err != nil {
		return "", err
	}
	var prm struct {
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if _, err := getJSON(ctx, hc, resourceMetadata, &prm); err != nil {
		return "", err
	}
	if len(prm.AuthorizationServers) == 0 {
		return "", nil
	}
	return prm.AuthorizationServers[0], nil
}

// SameIssuer compares issuer URLs (a trailing slash does not count).
func SameIssuer(a, b string) bool { return sameURL(a, b) }

func sameURL(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

// Discover finds the authorization server of an MCP server.
func Discover(ctx context.Context, hc *http.Client, serverURL, resourceMetadata string) (Metadata, error) {
	return DiscoverWith(ctx, hc, serverURL, resourceMetadata, "")
}

// DiscoverWith is Discover with the authorization server's metadata URL
// given ("oauth": {"metadata_url"}; "" finds it): the issuer is the
// one that document names.
func DiscoverWith(ctx context.Context, hc *http.Client, serverURL, resourceMetadata, asMetadataURL string) (Metadata, error) {
	if asMetadataURL != "" {
		if err := checkEndpoint(asMetadataURL); err != nil {
			return Metadata{}, err
		}
	}
	var prm struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		Scopes               []string `json:"scopes_supported"`
	}
	var tried []string
	if resourceMetadata != "" {
		if err := checkEndpoint(resourceMetadata); err != nil {
			return Metadata{}, err
		}
		tried = append(tried, resourceMetadata)
	} else {
		a, b := wellKnown(serverURL, "oauth-protected-resource")
		tried = append(tried, a)
		if b != "" {
			tried = append(tried, b)
		}
	}
	found := false
	for _, u := range tried {
		if _, err := getJSON(ctx, hc, u, &prm); err == nil && len(prm.AuthorizationServers) > 0 {
			found = true
			break
		}
		if ctx.Err() != nil {
			return Metadata{}, ctx.Err()
		}
	}
	md := Metadata{Resource: serverURL}
	issuer := ""
	if asMetadataURL != "" {
		md.Resource = serverURL
		if found && prm.Resource != "" && strings.HasPrefix(strings.TrimSuffix(serverURL, "/"), strings.TrimSuffix(prm.Resource, "/")) {
			md.Resource = prm.Resource
		}
		if found {
			md.Scopes = prm.Scopes
		}
		return asMetadata(ctx, hc, md, "", []string{asMetadataURL})
	}
	if found {
		issuer = prm.AuthorizationServers[0]
		md.Scopes = prm.Scopes
		if prm.Resource != "" {
			if !strings.HasPrefix(strings.TrimSuffix(serverURL, "/"), strings.TrimSuffix(prm.Resource, "/")) {
				return Metadata{}, fmt.Errorf("the server's metadata names another resource (%s)", prm.Resource)
			}
			md.Resource = prm.Resource
		}
	} else {
		// 2025-03-26 servers: the authorization server is the server's origin.
		u, _ := url.Parse(serverURL)
		issuer = u.Scheme + "://" + u.Host
	}
	if err := checkEndpoint(issuer); err != nil {
		return Metadata{}, err
	}
	oa, oaRoot := wellKnown(issuer, "oauth-authorization-server")
	oi, _ := wellKnown(issuer, "openid-configuration")
	cands := []string{oa, oi}
	if oaRoot != "" { // an issuer with a path: also issuer + /.well-known/openid-configuration
		cands = append(cands, strings.TrimSuffix(issuer, "/")+"/.well-known/openid-configuration")
	}
	return asMetadata(ctx, hc, md, issuer, cands)
}

// asMetadata reads the first authorization server metadata of cands.
// issuer "" takes the one the document names (a metadata_url override).
func asMetadata(ctx context.Context, hc *http.Client, md Metadata, issuer string, cands []string) (Metadata, error) {
	var as struct {
		Issuer       string   `json:"issuer"`
		Authorize    string   `json:"authorization_endpoint"`
		Token        string   `json:"token_endpoint"`
		Registration string   `json:"registration_endpoint"`
		PKCE         []string `json:"code_challenge_methods_supported"`
		CIMD         bool     `json:"client_id_metadata_document_supported"`
		AuthMethods  []string `json:"token_endpoint_auth_methods_supported"`
		Device       string   `json:"device_authorization_endpoint"`
		Revocation   string   `json:"revocation_endpoint"`
	}
	var lastErr error
	ok := false
	for _, u := range cands {
		if _, err := getJSON(ctx, hc, u, &as); err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return Metadata{}, ctx.Err()
			}
			continue
		}
		ok = true
		break
	}
	if !ok {
		return Metadata{}, fmt.Errorf("no OAuth metadata for %s: %v", issuer, lastErr)
	}
	if issuer == "" {
		if err := checkEndpoint(as.Issuer); err != nil {
			return Metadata{}, err
		}
		issuer = as.Issuer
	}
	if !sameURL(as.Issuer, issuer) {
		return Metadata{}, fmt.Errorf("OAuth metadata names issuer %q, not %q", as.Issuer, issuer)
	}
	if as.Authorize == "" || as.Token == "" {
		return Metadata{}, errors.New("OAuth metadata has no authorization or token endpoint")
	}
	for _, e := range []string{as.Authorize, as.Token} {
		if err := checkEndpoint(e); err != nil {
			return Metadata{}, err
		}
	}
	if len(as.PKCE) > 0 && !contains(as.PKCE, "S256") {
		return Metadata{}, errors.New("the authorization server does not offer PKCE S256")
	}
	if as.Registration != "" && checkEndpoint(as.Registration) != nil {
		as.Registration = ""
	}
	md.Issuer, md.AuthorizationEndpoint, md.TokenEndpoint = issuer, as.Authorize, as.Token
	md.RegistrationEndpoint, md.CIMD, md.AuthMethods = as.Registration, as.CIMD, as.AuthMethods
	if as.Device != "" && checkEndpoint(as.Device) == nil {
		md.DeviceEndpoint = as.Device
	}
	if as.Revocation != "" && checkEndpoint(as.Revocation) == nil {
		md.RevocationEndpoint = as.Revocation
	}
	return md, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Config starts one sign-in.
type Config struct {
	ServerURL        string
	ResourceMetadata string   // from the 401 (WWW-Authenticate), "" to probe
	MetadataURL      string   // the authorization server's metadata ("oauth": {"metadata_url"}), "" to find it
	Scopes           []string // from mcp.json "oauth" (wins over the server's list)
	ClientID         string   // pre-registered client (mcp.json "oauth")
	ClientSecret     string
	Port             int    // loopback port; 0 takes the first free one of LoopbackPorts
	RedirectHost     string // "localhost" for services that want it (Slack); default 127.0.0.1
	CIMDURL          string // our client id metadata document ("" none)
	ClientName       string // for DCR ("termocode")
	HTTP             *http.Client
	Lifetime         time.Duration // listener lifetime (DefaultLifetime)
}

type callback struct {
	code string
	err  error
}

// Flow is one pending sign-in: the browser URL, the loopback listener and
// the PKCE secrets. Wait gets the tokens; Paste feeds a callback URL the
// user copied.
type Flow struct {
	md                     Metadata
	hc                     *http.Client
	clientID, clientSecret string
	authMethod             string
	registration           json.RawMessage
	verifier, state        string
	redirect, authURL      string
	scope                  string
	port                   int
	deadline               time.Time

	ln        net.Listener
	srv       *http.Server
	results   chan callback
	once      sync.Once // one callback (loopback or paste)
	closeOnce sync.Once
}

// AuthURL is the URL to open in the browser.
func (f *Flow) AuthURL() string { return f.authURL }

// RedirectURI is the loopback callback URL.
func (f *Flow) RedirectURI() string { return f.redirect }

// Port is the loopback port (for "ssh -L port:127.0.0.1:port host").
func (f *Flow) Port() int { return f.port }

// LoopbackPorts are the sign-in listener's ports: the first free one is
// used. They are fixed (not random) so the redirect URIs in termocode's
// client metadata document (docs/mcp/client.json) match exactly, and so
// "ssh -L 47391:127.0.0.1:47391" can live in ~/.ssh/config.
var LoopbackPorts = []int{47391, 47392, 47393, 47394, 47395}

// LoopbackRedirects are the redirect URIs of LoopbackPorts, 127.0.0.1
// first, then localhost (docs/mcp/client.json lists exactly these).
func LoopbackRedirects() []string {
	var out []string
	for _, h := range []string{"127.0.0.1", "localhost"} {
		for _, p := range LoopbackPorts {
			out = append(out, fmt.Sprintf("http://%s:%d/callback", h, p))
		}
	}
	return out
}

// listen opens the one-shot listener on 127.0.0.1 only.
func listen(port int) (net.Listener, error) {
	if port > 0 {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return nil, fmt.Errorf("sign-in listener on 127.0.0.1:%d (mcp_oauth_port): %w", port, err)
		}
		return ln, nil
	}
	for _, p := range LoopbackPorts {
		if ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p))); err == nil {
			return ln, nil
		}
	}
	return nil, fmt.Errorf("the sign-in ports %d–%d are all busy (another sign-in is open?); close it, or set mcp_oauth_port",
		LoopbackPorts[0], LoopbackPorts[len(LoopbackPorts)-1])
}

// Begin discovers the server's authorization server, gets a client id,
// starts the loopback listener and builds the authorize URL.
func Begin(ctx context.Context, cfg Config) (*Flow, error) {
	md, err := DiscoverWith(ctx, cfg.HTTP, cfg.ServerURL, cfg.ResourceMetadata, cfg.MetadataURL)
	if err != nil {
		return nil, err
	}
	ln, err := listen(cfg.Port)
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	host := "127.0.0.1"
	if cfg.RedirectHost == "localhost" {
		host = "localhost"
	}
	f := &Flow{md: md, hc: cfg.HTTP, port: port, ln: ln, results: make(chan callback, 1),
		redirect: fmt.Sprintf("http://%s:%d/callback", host, port)}
	life := cfg.Lifetime
	if life <= 0 {
		life = DefaultLifetime
	}
	f.deadline = time.Now().Add(life)
	ok := false
	defer func() {
		if !ok {
			f.Close()
		}
	}()

	switch {
	case cfg.ClientID != "":
		f.clientID, f.clientSecret = cfg.ClientID, cfg.ClientSecret
		if f.clientSecret != "" {
			f.authMethod = "client_secret_post"
			if !contains(md.AuthMethods, "client_secret_post") && contains(md.AuthMethods, "client_secret_basic") {
				f.authMethod = "client_secret_basic"
			}
		}
	// The MCP spec's order: pre-registered, then a Client ID Metadata
	// Document, then Dynamic Client Registration.
	case md.CIMD && cfg.CIMDURL != "":
		f.clientID = cfg.CIMDURL
	case md.RegistrationEndpoint != "":
		if err := f.register(ctx, cfg.ClientName); err != nil {
			return nil, err
		}
	default:
		return nil, ErrNeedsApp
	}
	ai.RegisterSecret(f.clientSecret)

	pk, err := ai.NewPKCE()
	if err != nil {
		return nil, err
	}
	f.verifier, f.state = pk.Verifier, pk.State
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = md.Scopes
	}
	f.scope = strings.Join(scopes, " ")
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", f.clientID)
	q.Set("redirect_uri", f.redirect)
	q.Set("code_challenge", pk.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", f.state)
	q.Set("resource", md.Resource)
	if f.scope != "" {
		q.Set("scope", f.scope)
	}
	sep := "?"
	if strings.Contains(md.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	f.authURL = md.AuthorizationEndpoint + sep + q.Encode()

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", f.serveCallback)
	f.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	crash.Go("the sign-in page", func() { _ = f.srv.Serve(ln) })
	ok = true
	return f, nil
}

// register runs Dynamic Client Registration as a native public client.
func (f *Flow) register(ctx context.Context, name string) error {
	if name == "" {
		name = "termocode"
	}
	body, _ := json.Marshal(map[string]any{
		"client_name":                name,
		"redirect_uris":              []string{f.redirect},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"application_type":           "native",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.md.RegistrationEndpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client(f.hc).Do(req)
	if err != nil {
		return fmt.Errorf("client registration: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			return errors.New("this server allows only approved clients; use its API-token mode (a token header) instead")
		}
		return fmt.Errorf("client registration: HTTP %d: %s", resp.StatusCode, oauthErrText(b))
	}
	var r struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		AuthMethod   string `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(b, &r); err != nil || r.ClientID == "" {
		return errors.New("client registration: no client_id in the answer")
	}
	f.clientID, f.clientSecret, f.registration = r.ClientID, r.ClientSecret, b
	if r.ClientSecret != "" {
		f.authMethod = r.AuthMethod
		if f.authMethod == "" || f.authMethod == "none" {
			f.authMethod = "client_secret_basic" // RFC 7591 default
		}
	}
	return nil
}

func oauthErrText(b []byte) string {
	var e struct {
		Error string `json:"error"`
		Desc  string `json:"error_description"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		if e.Desc != "" {
			return e.Error + ": " + e.Desc
		}
		return e.Error
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

const donePage = `<!doctype html><meta charset="utf-8"><title>termocode</title>
<body style="font-family:sans-serif;margin:3em">%s</body>`

// serveCallback takes the one browser callback; the listener closes
// after it, whatever it holds.
func (f *Flow) serveCallback(w http.ResponseWriter, r *http.Request) {
	err := f.deliver(r.URL.Query())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, donePage, "Sign-in failed: "+htmlEscape(err.Error())+". Go back to termocode.")
	} else {
		fmt.Fprintf(w, donePage, "Signed in. You can close this tab and go back to termocode.")
	}
	crash.Go("the sign-in page", func() { f.stopListener() })
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// deliver checks a callback (state, iss, error) and hands the code to
// Wait. Only the first callback counts.
func (f *Flow) deliver(q url.Values) error {
	var cb callback
	switch {
	case q.Get("state") != f.state:
		cb.err = errors.New("the sign-in state does not match (an old or foreign sign-in); start again")
	case q.Get("iss") != "" && !sameURL(q.Get("iss"), f.md.Issuer):
		cb.err = errors.New("the callback names another issuer; start again")
	case q.Get("error") != "":
		cb.err = fmt.Errorf("the server refused: %s %s", q.Get("error"), q.Get("error_description"))
	case q.Get("code") == "":
		cb.err = errors.New("the callback has no code")
	default:
		cb.code = q.Get("code")
	}
	used := true
	f.once.Do(func() {
		used = false
		f.results <- cb
	})
	if used {
		return errors.New("this sign-in already got its answer")
	}
	return cb.err
}

// Paste takes what the user copied from the browser's address bar after
// the redirect (the full callback URL). A wrong state ends the flow.
func (f *Flow) Paste(input string) error {
	s := strings.TrimSpace(input)
	u, err := url.Parse(s)
	if err != nil || u.RawQuery == "" || (u.Query().Get("code") == "" && u.Query().Get("error") == "") {
		return errors.New("paste the whole address from the browser (it has ?code=…&state=…)")
	}
	err = f.deliver(u.Query())
	crash.Go("the sign-in page", func() { f.stopListener() })
	return err
}

// Wait waits for the callback (loopback or Paste), then exchanges the
// code for tokens. It ends with the flow's lifetime or ctx.
func (f *Flow) Wait(ctx context.Context) (ai.MCPOAuth, error) {
	defer f.Close()
	t := time.NewTimer(time.Until(f.deadline))
	defer t.Stop()
	var cb callback
	select {
	case cb = <-f.results:
	case <-t.C:
		return ai.MCPOAuth{}, errors.New("the sign-in timed out (5 minutes); start again")
	case <-ctx.Done():
		return ai.MCPOAuth{}, ctx.Err()
	}
	if cb.err != nil {
		return ai.MCPOAuth{}, cb.err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", cb.code)
	form.Set("redirect_uri", f.redirect)
	form.Set("code_verifier", f.verifier)
	form.Set("resource", f.md.Resource)
	e := ai.MCPOAuth{Issuer: f.md.Issuer, Resource: f.md.Resource, TokenEndpoint: f.md.TokenEndpoint,
		ClientID: f.clientID, ClientSecret: f.clientSecret, AuthMethod: f.authMethod, RedirectURI: f.redirect,
		Registration: f.registration, Scope: f.scope, RevocationEndpoint: f.md.RevocationEndpoint}
	return tokenRequest(ctx, f.hc, e, form)
}

func (f *Flow) stopListener() {
	if f.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = f.srv.Shutdown(ctx)
		cancel()
	}
	_ = f.ln.Close()
}

// Close stops the listener and ends the flow.
func (f *Flow) Close() {
	f.closeOnce.Do(func() {
		f.stopListener()
		f.once.Do(func() { f.results <- callback{err: errors.New("the sign-in was closed")} })
	})
}

// Refresh gets a new access token with the refresh token, at the stored
// token endpoint (the issuer the tokens belong to).
func Refresh(ctx context.Context, hc *http.Client, e ai.MCPOAuth) (ai.MCPOAuth, error) {
	if e.RefreshToken == "" {
		return e, errors.New("no refresh token: sign in again")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", e.RefreshToken)
	if e.Resource != "" {
		form.Set("resource", e.Resource)
	}
	return tokenRequest(ctx, hc, e, form)
}

// Revoke asks the authorization server to forget a sign-in (RFC 7009):
// the refresh token first, then the access token. Without a revocation
// endpoint it does nothing. Callers log a failure; the local sign-in is
// forgotten anyway.
func Revoke(ctx context.Context, hc *http.Client, e ai.MCPOAuth) error {
	if e.RevocationEndpoint == "" {
		return nil
	}
	if err := checkEndpoint(e.RevocationEndpoint); err != nil {
		return err
	}
	var first error
	for _, tok := range []struct{ v, hint string }{{e.RefreshToken, "refresh_token"}, {e.AccessToken, "access_token"}} {
		if tok.v == "" {
			continue
		}
		form := url.Values{"token": {tok.v}, "token_type_hint": {tok.hint}}
		r := e
		r.TokenEndpoint = e.RevocationEndpoint
		status, b, err := postToken(ctx, hc, r, form)
		if err == nil && status != http.StatusOK {
			err = fmt.Errorf("revoke: HTTP %d: %s", status, ai.Redact(oauthErrText(b)))
		}
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

// tokenRequest posts to the token endpoint and fills e with the answer.
func tokenRequest(ctx context.Context, hc *http.Client, e ai.MCPOAuth, form url.Values) (ai.MCPOAuth, error) {
	status, b, err := postToken(ctx, hc, e, form)
	if err != nil {
		return e, err
	}
	if status != http.StatusOK {
		return e, fmt.Errorf("token request: HTTP %d: %s", status, ai.Redact(oauthErrText(b)))
	}
	return tokenAnswer(e, b)
}

// postToken posts form to e's token endpoint with e's client
// authentication and returns the status and body.
func postToken(ctx context.Context, hc *http.Client, e ai.MCPOAuth, form url.Values) (int, []byte, error) {
	if err := checkEndpoint(e.TokenEndpoint); err != nil {
		return 0, nil, err
	}
	if e.AuthMethod != "client_secret_basic" {
		form.Set("client_id", e.ClientID)
		if e.ClientSecret != "" {
			form.Set("client_secret", e.ClientSecret)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if e.AuthMethod == "client_secret_basic" {
		req.SetBasicAuth(url.QueryEscape(e.ClientID), url.QueryEscape(e.ClientSecret))
	}
	resp, err := client(hc).Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, nil
}

// tokenAnswer fills e from a token endpoint's success answer.
func tokenAnswer(e ai.MCPOAuth, b []byte) (ai.MCPOAuth, error) {
	var t struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(b, &t); err != nil || t.AccessToken == "" {
		return e, errors.New("token request: no access_token in the answer")
	}
	if t.TokenType != "" && !strings.EqualFold(t.TokenType, "bearer") {
		return e, fmt.Errorf("token request: unsupported token type %q", t.TokenType)
	}
	e.AccessToken = t.AccessToken
	if t.RefreshToken != "" {
		e.RefreshToken = t.RefreshToken
	}
	e.ExpiresAtMs = 0
	if t.ExpiresIn > 0 {
		e.ExpiresAtMs = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second).UnixMilli()
	}
	if t.Scope != "" {
		e.Scope = t.Scope
	}
	ai.RegisterSecret(e.AccessToken)
	ai.RegisterSecret(e.RefreshToken)
	return e, nil
}
