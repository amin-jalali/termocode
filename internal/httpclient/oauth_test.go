package httpclient

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseOAuth2(t *testing.T) {
	c, err := ParseOAuth2(`grant=client_credentials token_url=https://a/token client_id=app client_secret=s3 scope="read write"`)
	if err != nil {
		t.Fatal(err)
	}
	if c.Grant != GrantClientCredentials || c.TokenURL != "https://a/token" || c.ClientID != "app" || c.ClientSecret != "s3" || c.Scope != "read write" {
		t.Fatalf("%+v", c)
	}
	if _, err := ParseOAuth2("grant=magic token_url=x"); err == nil {
		t.Error("unknown grant accepted")
	}
	if _, err := ParseOAuth2("grant=authorization_code token_url=https://a/t client_id=x"); err == nil {
		t.Error("authorization_code without auth_url accepted")
	}
	k1 := c.CacheKey()
	c.Scope = "read"
	if k1 == c.CacheKey() || !strings.HasPrefix(k1, "http.oauth2.") {
		t.Errorf("cache key %q", k1)
	}
}

// tokenServer answers the token endpoint and records the forms.
func tokenServer(t *testing.T, handle func(f url.Values, r *http.Request) (int, string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		code, body := handle(r.PostForm, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOAuth2ClientCredentialsAndRefresh(t *testing.T) {
	srv := tokenServer(t, func(f url.Values, r *http.Request) (int, string) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "app" || p != "s3" {
			return 401, `{"error":"invalid_client"}`
		}
		switch f.Get("grant_type") {
		case "client_credentials":
			if f.Get("scope") != "read" {
				return 400, `{"error":"invalid_scope"}`
			}
			return 200, `{"access_token":"AT1","token_type":"Bearer","expires_in":3600,"refresh_token":"RT1"}`
		case "refresh_token":
			if f.Get("refresh_token") != "RT1" {
				return 400, `{"error":"invalid_grant"}`
			}
			return 200, `{"access_token":"AT2","expires_in":60}`
		}
		return 400, `{"error":"unsupported_grant_type"}`
	})
	c := OAuth2Config{Grant: GrantClientCredentials, TokenURL: srv.URL, ClientID: "app", ClientSecret: "s3", Scope: "read"}
	now := time.Unix(1000, 0)
	tok, err := OAuth2Fetch(context.Background(), srv.Client(), c, now)
	if err != nil || tok.AccessToken != "AT1" || tok.RefreshToken != "RT1" || !tok.Expiry.Equal(now.Add(time.Hour)) {
		t.Fatalf("%+v %v", tok, err)
	}
	if !tok.Valid(now) || tok.Valid(now.Add(time.Hour)) {
		t.Error("Valid wrong")
	}
	tok2, err := OAuth2Refresh(context.Background(), srv.Client(), c, tok.RefreshToken, now)
	if err != nil || tok2.AccessToken != "AT2" || tok2.RefreshToken != "RT1" {
		t.Fatalf("refresh %+v %v", tok2, err)
	}
	b, _ := json.Marshal(tok2)
	if strings.Contains(string(b), "s3") {
		t.Error("client secret in the cached token")
	}
	c.ClientSecret = "bad"
	if _, err := OAuth2Fetch(context.Background(), srv.Client(), c, now); err == nil || !strings.Contains(err.Error(), "invalid_client") || strings.Contains(err.Error(), "bad") {
		t.Errorf("err = %v", err)
	}
}

func TestOAuth2PasswordGrant(t *testing.T) {
	srv := tokenServer(t, func(f url.Values, r *http.Request) (int, string) {
		if f.Get("grant_type") != "password" || f.Get("username") != "ann" || f.Get("password") != "pw" || f.Get("client_id") != "pub" {
			return 400, `{"error":"invalid_grant"}`
		}
		return 200, `{"access_token":"PW"}`
	})
	c := OAuth2Config{Grant: GrantPassword, TokenURL: srv.URL, ClientID: "pub", Username: "ann", Password: "pw"}
	tok, err := OAuth2Fetch(context.Background(), srv.Client(), c, time.Now())
	if err != nil || tok.AccessToken != "PW" || !tok.Expiry.IsZero() {
		t.Fatalf("%+v %v", tok, err)
	}
}

// Auth code + PKCE: the loopback catches the code (state checked) and
// the exchange sends the verifier.
func TestOAuth2AuthCodePKCELoopback(t *testing.T) {
	v, ch := NewPKCE()
	sum := sha256.Sum256([]byte(v))
	if ch != base64.RawURLEncoding.EncodeToString(sum[:]) || len(v) < 43 {
		t.Fatalf("pkce %q %q", v, ch)
	}
	srv := tokenServer(t, func(f url.Values, r *http.Request) (int, string) {
		if f.Get("grant_type") != "authorization_code" || f.Get("code") != "C0DE" || f.Get("code_verifier") != v || f.Get("redirect_uri") == "" {
			return 400, `{"error":"invalid_grant"}`
		}
		return 200, `{"access_token":"AC"}`
	})
	c := OAuth2Config{Grant: GrantAuthCode, TokenURL: srv.URL, AuthURL: "https://idp/authorize", ClientID: "app", Scope: "openid"}
	lb, err := NewOAuth2Loopback()
	if err != nil {
		t.Fatal(err)
	}
	defer lb.Close()
	u, _ := url.Parse(OAuth2AuthCodeURL(c, lb.RedirectURI, lb.State, ch))
	q := u.Query()
	if q.Get("response_type") != "code" || q.Get("code_challenge") != ch || q.Get("code_challenge_method") != "S256" || q.Get("state") != lb.State || q.Get("redirect_uri") != lb.RedirectURI {
		t.Fatalf("auth url %s", u)
	}
	go func() {
		resp, err := http.Get(lb.RedirectURI + "?state=wrong&code=X")
		if err == nil {
			resp.Body.Close()
		}
		resp, err = http.Get(lb.RedirectURI + "?state=" + lb.State + "&code=C0DE")
		if err == nil {
			resp.Body.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := lb.Wait(ctx)
	if err != nil || code != "C0DE" {
		t.Fatalf("code %q %v", code, err)
	}
	tok, err := OAuth2ExchangeCode(ctx, srv.Client(), c, code, lb.RedirectURI, v, time.Now())
	if err != nil || tok.AccessToken != "AC" {
		t.Fatalf("%+v %v", tok, err)
	}
}

// Device code: start, then poll through authorization_pending and
// slow_down.
func TestOAuth2DeviceFlow(t *testing.T) {
	var polls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"device_code":"DC","user_code":"ABCD-EFGH","verification_uri":"https://idp/device","interval":1,"expires_in":600}`))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("device_code") != "DC" || r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		switch polls.Add(1) {
		case 1:
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
		case 2:
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"slow_down"}`))
		default:
			_, _ = w.Write([]byte(`{"access_token":"DEV"}`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := OAuth2Config{Grant: GrantDeviceCode, TokenURL: srv.URL + "/token", DeviceURL: srv.URL + "/device", ClientID: "app"}
	d, err := OAuth2DeviceStart(context.Background(), srv.Client(), c)
	if err != nil || d.UserCode != "ABCD-EFGH" || d.VerificationURI != "https://idp/device" {
		t.Fatalf("%+v %v", d, err)
	}
	var waits []time.Duration
	sleep := func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	tok, err := OAuth2DevicePoll(context.Background(), srv.Client(), c, d, sleep, time.Now())
	if err != nil || tok.AccessToken != "DEV" {
		t.Fatalf("%+v %v", tok, err)
	}
	if len(waits) != 3 || waits[2] <= waits[0] {
		t.Errorf("waits = %v (slow_down must wait longer)", waits)
	}
}

// OAuth 1.0 HMAC-SHA1: the RFC 5849 section 1.2 example.
func TestOAuth1SignatureRFC5849(t *testing.T) {
	a := OAuth1{ConsumerKey: "dpf43f3p2l4k3l03", ConsumerSecret: "kd94hf93k423kf44", Token: "nnch734d00sl2jdk", TokenSecret: "pfkkdhi9sl3r4s00"}
	h := a.Header("GET", "http://photos.example.net/photos?file=vacation.jpg&size=original", nil, "kllo9940pd9333jh", time.Unix(1191242096, 0))
	if !strings.Contains(h, `oauth_signature="tR3%2BTy81lMeYAr%2FFid0kMTYa%2FWM%3D"`) || !strings.HasPrefix(h, "OAuth ") {
		t.Fatalf("header = %s", h)
	}
	rv := &Resolver{}
	rr, un := rv.Resolve(Request{Method: "GET", URL: "http://x/a", Directives: map[string]string{"auth": "oauth1 ck cs"}})
	if len(un) != 0 || !strings.HasPrefix(rr.Header("Authorization"), "OAuth ") {
		t.Fatalf("resolved %+v %v", rr.Headers, un)
	}
}
