package mcptest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// AuthOptions configure the fake OAuth 2.1 authorization server.
type AuthOptions struct {
	NoDCR       bool   // no registration_endpoint
	CIMD        bool   // client_id_metadata_document_supported
	ExpiresIn   int    // access token lifetime in seconds (default 3600)
	Path        string // issuer path ("" = the server root)
	OIDCOnly    bool   // metadata only at /.well-known/openid-configuration
	PreClientID string // a pre-registered confidential client …
	PreSecret   string // … and its secret
	PlainOnly   bool   // advertise code_challenge_methods_supported ["plain"]
	WrongIssuer bool   // metadata "issuer" is not this server (must be refused)
}

type authCode struct {
	client, redirect, challenge, resource, scope string
}

// AuthServer is a fake authorization server: metadata, /register (DCR),
// /authorize (redirects at once with a code), /token (code + PKCE,
// refresh_token).
type AuthServer struct {
	*httptest.Server
	O AuthOptions

	mu            sync.Mutex
	clients       map[string]string // client_id → secret ("" public)
	codes         map[string]authCode
	access        map[string]string // token → resource
	refresh       map[string]string // refresh token → client_id
	n             int
	Registrations []map[string]any
	Authorizes    []url.Values
	TokenForms    []url.Values
	TokenAuth     []string // Authorization header of each /token request
}

// NewAuthServer starts the fake; it closes with the test.
func NewAuthServer(t testing.TB, o AuthOptions) *AuthServer {
	a := &AuthServer{O: o, clients: map[string]string{}, codes: map[string]authCode{},
		access: map[string]string{}, refresh: map[string]string{}}
	if o.PreClientID != "" {
		a.clients[o.PreClientID] = o.PreSecret
	}
	mux := http.NewServeMux()
	if o.OIDCOnly {
		mux.HandleFunc("/.well-known/openid-configuration"+o.Path, a.metadata)
	} else {
		mux.HandleFunc("/.well-known/oauth-authorization-server"+o.Path, a.metadata)
	}
	mux.HandleFunc(o.Path+"/register", a.register)
	mux.HandleFunc(o.Path+"/authorize", a.authorize)
	mux.HandleFunc(o.Path+"/token", a.token)
	a.Server = httptest.NewServer(mux)
	t.Cleanup(a.Close)
	return a
}

// Issuer is the issuer URL.
func (a *AuthServer) Issuer() string { return a.URL + a.O.Path }

// Valid reports an access token this server issued (and did not revoke).
func (a *AuthServer) Valid(token string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.access[token]
	return ok
}

// ResourceOf is the resource an access token was issued for.
func (a *AuthServer) ResourceOf(token string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.access[token]
}

// RevokeAccess forgets every access token (refresh tokens stay valid).
func (a *AuthServer) RevokeAccess() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.access = map[string]string{}
}

// Regs returns the DCR request bodies so far.
func (a *AuthServer) Regs() []map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]map[string]any(nil), a.Registrations...)
}

// Forms returns the /token request forms so far.
func (a *AuthServer) Forms() []url.Values {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]url.Values(nil), a.TokenForms...)
}

// Counts returns how many registrations, authorizations and token
// requests the server got.
func (a *AuthServer) Counts() (reg, auth, token int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.Registrations), len(a.Authorizes), len(a.TokenForms)
}

func (a *AuthServer) next(prefix string) string {
	a.n++
	return fmt.Sprintf("%s-%d-%s", prefix, a.n, randomHex(6))
}

func (a *AuthServer) metadata(w http.ResponseWriter, r *http.Request) {
	iss := a.Issuer()
	if a.O.WrongIssuer {
		iss = "https://evil.example"
	}
	md := map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                a.Issuer() + "/authorize",
		"token_endpoint":                        a.Issuer() + "/token",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
	}
	if a.O.PlainOnly {
		md["code_challenge_methods_supported"] = []string{"plain"}
	}
	if !a.O.NoDCR {
		md["registration_endpoint"] = a.Issuer() + "/register"
	}
	if a.O.CIMD {
		md["client_id_metadata_document_supported"] = true
	}
	writeJSON(w, md)
}

func (a *AuthServer) register(w http.ResponseWriter, r *http.Request) {
	if a.O.NoDCR || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	a.Registrations = append(a.Registrations, body)
	id := a.next("client")
	a.clients[id] = ""
	a.mu.Unlock()
	w.WriteHeader(http.StatusCreated)
	out := map[string]any{"client_id": id}
	for k, v := range body {
		out[k] = v
	}
	writeJSON(w, out)
}

func oauthError(w http.ResponseWriter, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": desc})
}

func (a *AuthServer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a.mu.Lock()
	a.Authorizes = append(a.Authorizes, q)
	_, known := a.clients[q.Get("client_id")]
	a.mu.Unlock()
	if a.O.CIMD && strings.HasPrefix(q.Get("client_id"), "https://") {
		known = true // a client id metadata document URL
	}
	switch {
	case !known:
		http.Error(w, "unknown client", http.StatusBadRequest)
		return
	case q.Get("response_type") != "code":
		http.Error(w, "response_type must be code", http.StatusBadRequest)
		return
	case q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256":
		http.Error(w, "PKCE S256 required", http.StatusBadRequest)
		return
	case q.Get("redirect_uri") == "" || q.Get("state") == "":
		http.Error(w, "redirect_uri and state required", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	code := a.next("code")
	a.codes[code] = authCode{client: q.Get("client_id"), redirect: q.Get("redirect_uri"),
		challenge: q.Get("code_challenge"), resource: q.Get("resource"), scope: q.Get("scope")}
	a.mu.Unlock()
	u, _ := url.Parse(q.Get("redirect_uri"))
	v := u.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	v.Set("iss", a.Issuer())
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (a *AuthServer) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		oauthError(w, "invalid_request", "form POST required")
		return
	}
	b, _ := io.ReadAll(r.Body)
	f, _ := url.ParseQuery(string(b))
	a.mu.Lock()
	defer a.mu.Unlock()
	a.TokenForms = append(a.TokenForms, f)
	a.TokenAuth = append(a.TokenAuth, r.Header.Get("Authorization"))
	client := f.Get("client_id")
	secret := f.Get("client_secret")
	if u, p, ok := r.BasicAuth(); ok {
		client, secret = u, p
	}
	want, known := a.clients[client]
	if a.O.CIMD && strings.HasPrefix(client, "https://") {
		known = true
	}
	if !known || want != secret {
		oauthError(w, "invalid_client", "bad client credentials")
		return
	}
	var resource, scope string
	switch f.Get("grant_type") {
	case "authorization_code":
		c, ok := a.codes[f.Get("code")]
		delete(a.codes, f.Get("code"))
		sum := sha256.Sum256([]byte(f.Get("code_verifier")))
		switch {
		case !ok || c.client != client:
			oauthError(w, "invalid_grant", "bad code")
			return
		case c.redirect != f.Get("redirect_uri"):
			oauthError(w, "invalid_grant", "redirect_uri differs")
			return
		case base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge:
			oauthError(w, "invalid_grant", "PKCE verifier does not match")
			return
		case c.resource != f.Get("resource"):
			oauthError(w, "invalid_target", "resource differs")
			return
		}
		resource, scope = c.resource, c.scope
	case "refresh_token":
		owner, ok := a.refresh[f.Get("refresh_token")]
		if !ok || owner != client {
			oauthError(w, "invalid_grant", "bad refresh token")
			return
		}
		delete(a.refresh, f.Get("refresh_token"))
		resource = f.Get("resource")
	default:
		oauthError(w, "unsupported_grant_type", f.Get("grant_type"))
		return
	}
	at, rt := a.next("at"), a.next("rt")
	a.access[at] = resource
	a.refresh[rt] = client
	exp := a.O.ExpiresIn
	if exp == 0 {
		exp = 3600
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": at, "token_type": "Bearer", "expires_in": exp,
		"refresh_token": rt, "scope": scope})
}
