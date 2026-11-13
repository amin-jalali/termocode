package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseOAuth2EveryKeyAndGrantRule(t *testing.T) {
	c, err := ParseOAuth2(`grant=device_code token_url=https://a/t device_url=https://a/d client_id=app audience=api scope="a b"`)
	if err != nil || c.DeviceURL != "https://a/d" || c.Audience != "api" || c.Scope != "a b" {
		t.Errorf("device = %+v, %v", c, err)
	}
	c, err = ParseOAuth2(`grant=password token_url=https://a/t client_id=app username=me password=pw`)
	if err != nil || c.Username != "me" || c.Password != "pw" {
		t.Errorf("password = %+v, %v", c, err)
	}
	c, err = ParseOAuth2(`grant=refresh_token token_url=https://a/t client_id=app refresh_token=RT auth_url=https://a/a`)
	if err != nil || c.RefreshToken != "RT" || c.AuthURL != "https://a/a" {
		t.Errorf("refresh = %+v, %v", c, err)
	}
	for name, spec := range map[string]string{
		"a bare word":          `grant=client_credentials token_url=https://a/t client_id=app oops`,
		"an unknown key":       `grant=client_credentials token_url=https://a/t client_id=app flavour=x`,
		"password no user":     `grant=password token_url=https://a/t client_id=app`,
		"refresh no token":     `grant=refresh_token token_url=https://a/t client_id=app`,
		"device no device url": `grant=device_code token_url=https://a/t client_id=app`,
		"auth code no url":     `grant=authorization_code token_url=https://a/t client_id=app`,
		"unknown grant":        `grant=magic token_url=https://a/t client_id=app`,
		"no client":            `grant=client_credentials token_url=https://a/t`,
	} {
		if _, err := ParseOAuth2(spec); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestOAuth2TokenValidity(t *testing.T) {
	now := time.Now()
	if (OAuth2Token{}).Valid(now) {
		t.Error("no access token is never valid")
	}
	if !(OAuth2Token{AccessToken: "a"}).Valid(now) {
		t.Error("a token without an expiry stays valid")
	}
	if (OAuth2Token{AccessToken: "a", Expiry: now.Add(10 * time.Second)}).Valid(now) {
		t.Error("a token that ends within 30 seconds is renewed first")
	}
	if !(OAuth2Token{AccessToken: "a", Expiry: now.Add(time.Hour)}).Valid(now) {
		t.Error("a token with an hour left is valid")
	}
}

func oauthServer(t *testing.T, status int, ctype, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOAuth2TokenEndpointAnswers(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	cfg := func(u string) OAuth2Config {
		return OAuth2Config{Grant: GrantClientCredentials, TokenURL: u, ClientID: "app", Audience: "api"}
	}
	// A form-encoded answer is understood.
	srv := oauthServer(t, 200, "application/x-www-form-urlencoded", "access_token=FORM&token_type=bearer&expires_in=60")
	if tok, err := OAuth2Fetch(ctx, srv.Client(), cfg(srv.URL), now); err != nil || tok.AccessToken != "FORM" {
		t.Errorf("form answer = %+v, %v", tok, err)
	}
	// Neither JSON nor a form.
	srv = oauthServer(t, 200, "text/plain", "hello")
	if _, err := OAuth2Fetch(ctx, srv.Client(), cfg(srv.URL), now); err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Errorf("text answer = %v", err)
	}
	// JSON without a token.
	srv = oauthServer(t, 200, "application/json", `{"token_type":"bearer"}`)
	if _, err := OAuth2Fetch(ctx, srv.Client(), cfg(srv.URL), now); err == nil || !strings.Contains(err.Error(), "no access_token") {
		t.Errorf("no token = %v", err)
	}
	// An error status without an oauth error body.
	srv = oauthServer(t, 500, "text/plain", "boom")
	if _, err := OAuth2Fetch(ctx, srv.Client(), cfg(srv.URL), now); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("500 = %v", err)
	}
	srv = oauthServer(t, 400, "application/json", `{"error":"invalid_client"}`)
	if _, err := OAuth2Fetch(ctx, srv.Client(), cfg(srv.URL), now); err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("400 = %v", err)
	}
	// A server that is not there, and a bad URL.
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	if _, err := OAuth2Fetch(ctx, http.DefaultClient, cfg(gone.URL), now); err == nil {
		t.Error("an endpoint that is down: want error")
	}
	if _, err := OAuth2Fetch(ctx, http.DefaultClient, cfg("http://bad host/\x7f"), now); err == nil {
		t.Error("a bad endpoint: want error")
	}
	// A grant that needs the user cannot be fetched silently.
	c := cfg(srv.URL)
	c.Grant = GrantAuthCode
	if _, err := OAuth2Fetch(ctx, srv.Client(), c, now); err == nil || !strings.Contains(err.Error(), "needs the user") {
		t.Errorf("auth code fetch = %v", err)
	}
	// The refresh grant goes through the refresh call.
	srv = oauthServer(t, 200, "application/json", `{"access_token":"NEW"}`)
	c = cfg(srv.URL)
	c.Grant, c.RefreshToken = GrantRefreshToken, "RT"
	if tok, err := OAuth2Fetch(ctx, srv.Client(), c, now); err != nil || tok.AccessToken != "NEW" {
		t.Errorf("refresh fetch = %+v, %v", tok, err)
	}
}

func TestOAuth2AuthCodeURLKeepsItsQuery(t *testing.T) {
	c := OAuth2Config{AuthURL: "https://idp/authorize?tenant=x", ClientID: "app", Audience: "api"}
	u := OAuth2AuthCodeURL(c, "http://127.0.0.1:1/callback", "st", "ch")
	if !strings.HasPrefix(u, "https://idp/authorize?tenant=x&") {
		t.Errorf("a query already there is extended with &: %s", u)
	}
	if !strings.Contains(u, "audience=api") {
		t.Errorf("audience missing: %s", u)
	}
}

func TestOAuth2LoopbackAnswers(t *testing.T) {
	lb, err := NewOAuth2Loopback()
	if err != nil {
		t.Fatal(err)
	}
	defer lb.Close()
	base := strings.TrimSuffix(lb.RedirectURI, "/callback")
	if resp, err := http.Get(base + "/other"); err != nil || resp.StatusCode != 404 {
		t.Errorf("another path = %v, %v", resp, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := http.Get(lb.RedirectURI + "?state=" + url.QueryEscape(lb.State) + "&error=access_denied")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := lb.Wait(ctx); err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("a denied sign-in = %v", err)
	}
	// No code at all.
	lb2, _ := NewOAuth2Loopback()
	defer lb2.Close()
	resp, err = http.Get(lb2.RedirectURI + "?state=" + url.QueryEscape(lb2.State))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := lb2.Wait(ctx); err == nil || !strings.Contains(err.Error(), "no code") {
		t.Errorf("no code = %v", err)
	}
	// A wait that is cancelled.
	lb3, _ := NewOAuth2Loopback()
	defer lb3.Close()
	cctx, ccancel := context.WithCancel(ctx)
	ccancel()
	if _, err := lb3.Wait(cctx); err == nil {
		t.Error("a cancelled wait: want error")
	}
}

func TestOAuth2DeviceStartAndPollEdges(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	// The older verification_url spelling is accepted.
	srv := oauthServer(t, 200, "application/json", `{"device_code":"D","user_code":"U","verification_url":"https://v","expires_in":60}`)
	c := OAuth2Config{Grant: GrantDeviceCode, DeviceURL: srv.URL, TokenURL: srv.URL, ClientID: "app", Scope: "openid"}
	d, err := OAuth2DeviceStart(ctx, srv.Client(), c)
	if err != nil || d.VerificationURI != "https://v" {
		t.Errorf("start = %+v, %v", d, err)
	}
	srv = oauthServer(t, 200, "application/json", `{"device_code":"D"}`)
	c.DeviceURL = srv.URL
	if _, err := OAuth2DeviceStart(ctx, srv.Client(), c); err == nil || !strings.Contains(err.Error(), "no device_code") {
		t.Errorf("an incomplete answer = %v", err)
	}
	srv = oauthServer(t, 400, "application/json", `{"error":"invalid_client"}`)
	c.DeviceURL = srv.URL
	if _, err := OAuth2DeviceStart(ctx, srv.Client(), c); err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("a refused start = %v", err)
	}
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	c.DeviceURL = gone.URL
	if _, err := OAuth2DeviceStart(ctx, http.DefaultClient, c); err == nil {
		t.Error("a device endpoint that is down: want error")
	}
	noSleep := func(context.Context, time.Duration) error { return nil }
	dev := OAuth2Device{DeviceCode: "D", UserCode: "U", VerificationURI: "https://v"} // interval and expiry default
	// A token endpoint that says something else.
	srv = oauthServer(t, 400, "application/json", `{"error":"access_denied"}`)
	c.TokenURL = srv.URL
	if _, err := OAuth2DevicePoll(ctx, srv.Client(), c, dev, noSleep, now); err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("denied = %v", err)
	}
	// Success status without a token.
	srv = oauthServer(t, 200, "application/json", `{}`)
	c.TokenURL = srv.URL
	if _, err := OAuth2DevicePoll(ctx, srv.Client(), c, dev, noSleep, now); err == nil || !strings.Contains(err.Error(), "HTTP 200") {
		t.Errorf("no token = %v", err)
	}
	// A transport failure.
	c.TokenURL = gone.URL
	if _, err := OAuth2DevicePoll(ctx, http.DefaultClient, c, dev, noSleep, now); err == nil {
		t.Error("a token endpoint that is down: want error")
	}
	// A sleep that is cancelled ends the wait.
	boom := func(context.Context, time.Duration) error { return context.Canceled }
	if _, err := OAuth2DevicePoll(ctx, srv.Client(), c, dev, boom, now); err != context.Canceled {
		t.Errorf("a cancelled sleep = %v", err)
	}
	// slow_down raises the interval; the expired code ends the poll.
	srv = oauthServer(t, 400, "application/json", `{"error":"slow_down"}`)
	c.TokenURL = srv.URL
	var waits []time.Duration
	rec := func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	short := OAuth2Device{DeviceCode: "D", Interval: 1, ExpiresIn: 1}
	_, _ = OAuth2DevicePoll(ctx, srv.Client(), c, short, func(ctx context.Context, d time.Duration) error {
		_ = rec(ctx, d)
		time.Sleep(400 * time.Millisecond)
		return nil
	}, now)
	if len(waits) < 2 || waits[1] != waits[0]+5*time.Second {
		t.Errorf("slow_down should add 5s: %v", waits)
	}
}

func TestSleepCtx(t *testing.T) {
	if err := SleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Error(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SleepCtx(ctx, time.Hour); err != context.Canceled {
		t.Errorf("a cancelled sleep = %v", err)
	}
}
