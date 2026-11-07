package httpclient

// OAuth 2.0 (`# @oauth2 grant=… token_url=… client_id=…`): client
// credentials, password, refresh token, authorization code + PKCE with
// a loopback redirect, and device code. The app asks for a token, caches
// it in the secret store and sends the request with
// "Authorization: Bearer <token>". OAuth 1.0 (`# @auth oauth1 …`) signs
// with HMAC-SHA1 (oauth1.go). Errors never echo secrets.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OAuth2Directive is the `# @oauth2 …` directive.
const OAuth2Directive = "oauth2"

// The grants `grant=` takes.
const (
	GrantClientCredentials = "client_credentials"
	GrantPassword          = "password"
	GrantRefreshToken      = "refresh_token"
	GrantAuthCode          = "authorization_code"
	GrantDeviceCode        = "device_code"
)

// OAuth2Config is a parsed `# @oauth2` directive.
type OAuth2Config struct {
	Grant        string
	TokenURL     string
	AuthURL      string // authorization_code
	DeviceURL    string // device_code
	ClientID     string
	ClientSecret string
	Scope        string
	Audience     string
	Username     string // password
	Password     string // password
	RefreshToken string // refresh_token
}

var errOAuth2Spec = errors.New("@oauth2: use grant=client_credentials|password|refresh_token|authorization_code|device_code token_url=URL client_id=ID [client_secret=S] [scope=\"a b\"] (auth_url= for authorization_code, device_url= for device_code, username= password= for password, refresh_token= for refresh_token)")

// ParseOAuth2 reads key=value pairs (values may be "quoted").
func ParseOAuth2(spec string) (OAuth2Config, error) {
	var c OAuth2Config
	for _, kv := range splitKV(spec) {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return OAuth2Config{}, errOAuth2Spec
		}
		v = strings.Trim(v, `"`)
		switch strings.ToLower(k) {
		case "grant", "grant_type":
			c.Grant = strings.ToLower(v)
		case "token_url":
			c.TokenURL = v
		case "auth_url":
			c.AuthURL = v
		case "device_url":
			c.DeviceURL = v
		case "client_id":
			c.ClientID = v
		case "client_secret":
			c.ClientSecret = v
		case "scope":
			c.Scope = v
		case "audience":
			c.Audience = v
		case "username":
			c.Username = v
		case "password":
			c.Password = v
		case "refresh_token":
			c.RefreshToken = v
		default:
			return OAuth2Config{}, fmt.Errorf("@oauth2: unknown key %q", k)
		}
	}
	if c.TokenURL == "" || c.ClientID == "" {
		return OAuth2Config{}, errOAuth2Spec
	}
	switch c.Grant {
	case GrantClientCredentials:
	case GrantPassword:
		if c.Username == "" {
			return OAuth2Config{}, errOAuth2Spec
		}
	case GrantRefreshToken:
		if c.RefreshToken == "" {
			return OAuth2Config{}, errOAuth2Spec
		}
	case GrantAuthCode:
		if c.AuthURL == "" {
			return OAuth2Config{}, errOAuth2Spec
		}
	case GrantDeviceCode:
		if c.DeviceURL == "" {
			return OAuth2Config{}, errOAuth2Spec
		}
	default:
		return OAuth2Config{}, errOAuth2Spec
	}
	return c, nil
}

// splitKV splits on spaces, keeping "quoted values" whole.
func splitKV(s string) []string {
	var out []string
	var cur strings.Builder
	q := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			q = !q
			cur.WriteByte(c)
		case (c == ' ' || c == '\t') && !q:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// CacheKey names the cached token in the secret store (no secret in it).
func (c OAuth2Config) CacheKey() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{c.Grant, c.TokenURL, c.AuthURL, c.DeviceURL, c.ClientID, c.Scope, c.Audience, c.Username}, "\x00")))
	return "http.oauth2." + hex.EncodeToString(sum[:8])
}

// OAuth2Token is an access token as cached (never the client secret).
type OAuth2Token struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitzero"`
}

// Valid reports a token that can be used at now (30 s of slack; no
// expiry = valid).
func (t OAuth2Token) Valid(now time.Time) bool {
	if t.AccessToken == "" {
		return false
	}
	return t.Expiry.IsZero() || now.Add(30*time.Second).Before(t.Expiry)
}

// oauth2Post posts form to the token (or device) endpoint and decodes
// the JSON answer into out.
func oauth2Post(ctx context.Context, hc *http.Client, c OAuth2Config, endpoint string, form url.Values, out any) (status int, oerr string, err error) {
	if c.ClientSecret == "" {
		form.Set("client_id", c.ClientID)
	}
	if c.Scope != "" && form.Get("scope") == "" && form.Get("grant_type") != GrantAuthCode && form.Get("grant_type") != GrantRefreshToken {
		form.Set("scope", c.Scope)
	}
	if c.Audience != "" {
		form.Set("audience", c.Audience)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, "", fmt.Errorf("oauth2: bad endpoint %s", RedactURL(endpoint))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if c.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(c.ClientID), url.QueryEscape(c.ClientSecret))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("oauth2: %s: %v", RedactURL(endpoint), errors.Unwrap(err))
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("oauth2: reading the answer: %v", err)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
			Desc  string `json:"error_description"`
		}
		_ = json.Unmarshal(b, &e)
		return resp.StatusCode, e.Error, nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		if v, perr := url.ParseQuery(string(b)); perr == nil && v.Get("access_token") != "" {
			// Some servers answer form-encoded.
			b, _ = json.Marshal(map[string]any{"access_token": v.Get("access_token"), "token_type": v.Get("token_type"), "refresh_token": v.Get("refresh_token"), "expires_in": v.Get("expires_in")})
			if json.Unmarshal(b, out) == nil {
				return resp.StatusCode, "", nil
			}
		}
		return resp.StatusCode, "", errors.New("oauth2: the answer is not JSON")
	}
	return resp.StatusCode, "", nil
}

type tokenJSON struct {
	AccessToken  string          `json:"access_token"`
	TokenType    string          `json:"token_type"`
	RefreshToken string          `json:"refresh_token"`
	ExpiresIn    json.RawMessage `json:"expires_in"`
}

func (t tokenJSON) token(now time.Time) OAuth2Token {
	out := OAuth2Token{AccessToken: t.AccessToken, TokenType: t.TokenType, RefreshToken: t.RefreshToken}
	if n, err := strconv.Atoi(strings.Trim(string(t.ExpiresIn), `"`)); err == nil && n > 0 {
		out.Expiry = now.Add(time.Duration(n) * time.Second)
	}
	return out
}

func oauth2Token(ctx context.Context, hc *http.Client, c OAuth2Config, form url.Values, now time.Time) (OAuth2Token, error) {
	var t tokenJSON
	status, oerr, err := oauth2Post(ctx, hc, c, c.TokenURL, form, &t)
	if err != nil {
		return OAuth2Token{}, err
	}
	if oerr != "" || status >= 300 {
		if oerr == "" {
			oerr = "error"
		}
		return OAuth2Token{}, fmt.Errorf("oauth2: token endpoint said %s (HTTP %d)", oerr, status)
	}
	if t.AccessToken == "" {
		return OAuth2Token{}, errors.New("oauth2: the answer has no access_token")
	}
	return t.token(now), nil
}

// OAuth2Fetch gets a token for the grants that need no user step:
// client_credentials, password and refresh_token.
func OAuth2Fetch(ctx context.Context, hc *http.Client, c OAuth2Config, now time.Time) (OAuth2Token, error) {
	form := url.Values{}
	switch c.Grant {
	case GrantClientCredentials:
		form.Set("grant_type", GrantClientCredentials)
	case GrantPassword:
		form.Set("grant_type", GrantPassword)
		form.Set("username", c.Username)
		form.Set("password", c.Password)
	case GrantRefreshToken:
		return OAuth2Refresh(ctx, hc, c, c.RefreshToken, now)
	default:
		return OAuth2Token{}, fmt.Errorf("oauth2: grant %s needs the user", c.Grant)
	}
	return oauth2Token(ctx, hc, c, form, now)
}

// OAuth2Refresh trades a refresh token for a new access token; the old
// refresh token is kept when the server sends none.
func OAuth2Refresh(ctx context.Context, hc *http.Client, c OAuth2Config, refresh string, now time.Time) (OAuth2Token, error) {
	form := url.Values{"grant_type": {GrantRefreshToken}, "refresh_token": {refresh}}
	t, err := oauth2Token(ctx, hc, c, form, now)
	if err == nil && t.RefreshToken == "" {
		t.RefreshToken = refresh
	}
	return t, err
}

// ── authorization code + PKCE ──────────────────────────────────────────

func randomURL(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewPKCE returns a code verifier and its S256 challenge.
func NewPKCE() (verifier, challenge string) {
	verifier = randomURL(32)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// OAuth2AuthCodeURL is the page the user signs in on.
func OAuth2AuthCodeURL(c OAuth2Config, redirect, state, challenge string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.ClientID},
		"redirect_uri":          {redirect},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if c.Scope != "" {
		q.Set("scope", c.Scope)
	}
	if c.Audience != "" {
		q.Set("audience", c.Audience)
	}
	sep := "?"
	if strings.Contains(c.AuthURL, "?") {
		sep = "&"
	}
	return c.AuthURL + sep + q.Encode()
}

// OAuth2ExchangeCode trades the code for a token.
func OAuth2ExchangeCode(ctx context.Context, hc *http.Client, c OAuth2Config, code, redirect, verifier string, now time.Time) (OAuth2Token, error) {
	form := url.Values{"grant_type": {GrantAuthCode}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier}}
	return oauth2Token(ctx, hc, c, form, now)
}

// OAuth2Loopback is a one-shot redirect server on 127.0.0.1.
type OAuth2Loopback struct {
	RedirectURI string
	State       string
	srv         *http.Server
	got         chan loopbackResult
}

type loopbackResult struct {
	code string
	err  error
}

// NewOAuth2Loopback listens on a free 127.0.0.1 port.
func NewOAuth2Loopback() (*OAuth2Loopback, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("oauth2: loopback: %v", err)
	}
	lb := &OAuth2Loopback{
		RedirectURI: "http://" + ln.Addr().String() + "/callback",
		State:       randomURL(16),
		got:         make(chan loopbackResult, 1),
	}
	lb.srv = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("state") != lb.State {
			http.Error(w, "Sign-in state does not match. Start again from termocode.", http.StatusBadRequest)
			return
		}
		res := loopbackResult{code: q.Get("code")}
		if e := q.Get("error"); e != "" || res.code == "" {
			if e == "" {
				e = "no code"
			}
			res = loopbackResult{err: fmt.Errorf("oauth2: sign-in failed: %s", e)}
		}
		select {
		case lb.got <- res:
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>Signed in</title><p>Signed in. You can close this tab and go back to termocode.</p>")
	})}
	go func() { _ = lb.srv.Serve(ln) }()
	return lb, nil
}

// Wait returns the code once the browser comes back (or ctx ends).
func (lb *OAuth2Loopback) Wait(ctx context.Context) (string, error) {
	select {
	case r := <-lb.got:
		return r.code, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Close stops the server.
func (lb *OAuth2Loopback) Close() { _ = lb.srv.Close() }

// ── device code ────────────────────────────────────────────────────────

// OAuth2Device is the answer of the device endpoint.
type OAuth2Device struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURL         string `json:"verification_url"` // older servers
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpiresIn               int    `json:"expires_in"`
}

// OAuth2DeviceStart asks the device endpoint for a user code.
func OAuth2DeviceStart(ctx context.Context, hc *http.Client, c OAuth2Config) (OAuth2Device, error) {
	var d OAuth2Device
	form := url.Values{}
	if c.Scope != "" {
		form.Set("scope", c.Scope)
	}
	status, oerr, err := oauth2Post(ctx, hc, c, c.DeviceURL, form, &d)
	if err != nil {
		return d, err
	}
	if oerr != "" || status >= 300 {
		return d, fmt.Errorf("oauth2: device endpoint said %s (HTTP %d)", oerr, status)
	}
	if d.VerificationURI == "" {
		d.VerificationURI = d.VerificationURL
	}
	if d.DeviceCode == "" || d.UserCode == "" || d.VerificationURI == "" {
		return d, errors.New("oauth2: the device answer has no device_code, user_code or verification_uri")
	}
	return d, nil
}

// OAuth2DevicePoll polls the token endpoint until the user approved,
// waiting with sleep between polls (slow_down adds 5 s).
func OAuth2DevicePoll(ctx context.Context, hc *http.Client, c OAuth2Config, d OAuth2Device, sleep func(context.Context, time.Duration) error, now time.Time) (OAuth2Token, error) {
	interval := time.Duration(max(d.Interval, 1)) * time.Second
	if d.Interval == 0 {
		interval = 5 * time.Second
	}
	expires := d.ExpiresIn
	if expires <= 0 {
		expires = 900
	}
	deadline := time.Now().Add(time.Duration(expires) * time.Second)
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {d.DeviceCode}}
	for time.Now().Before(deadline) {
		if err := sleep(ctx, interval); err != nil {
			return OAuth2Token{}, err
		}
		var t tokenJSON
		f := url.Values{}
		for k, v := range form {
			f[k] = v
		}
		status, oerr, err := oauth2Post(ctx, hc, c, c.TokenURL, f, &t)
		if err != nil {
			return OAuth2Token{}, err
		}
		switch oerr {
		case "":
			if status < 300 && t.AccessToken != "" {
				return t.token(now), nil
			}
			return OAuth2Token{}, fmt.Errorf("oauth2: token endpoint said HTTP %d", status)
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		default:
			return OAuth2Token{}, fmt.Errorf("oauth2: token endpoint said %s (HTTP %d)", oerr, status)
		}
	}
	return OAuth2Token{}, errors.New("oauth2: the device code expired; send again")
}

// SleepCtx waits d or until ctx ends (the default OAuth2DevicePoll sleep).
func SleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
