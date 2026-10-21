package containers

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// CP-12: remote tags over the Registry HTTP API v2. Every test talks to an
// in-process loopback server; nothing reaches the network.

const testToken = "tok-SECRET-0123456789" // must never show in an error or a result

func certPool(srv *httptest.Server) *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(srv.Certificate())
	return p
}

func repoOn(t *testing.T, srv *httptest.Server, name string) RemoteRepo {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	r, err := ParseRemoteRepo(u.Host + "/" + name)
	if err != nil {
		t.Fatalf("ParseRemoteRepo: %v", err)
	}
	return r
}

func tagsJSON(name string, tags []string) string {
	b, _ := json.Marshal(map[string]any{"name": name, "tags": tags})
	return string(b)
}

// The anonymous token exchange: 401 + Bearer challenge → GET the realm
// with service and scope and no credentials → retry with the token.
func TestRegistryTagsTokenExchange(t *testing.T) {
	var mu sync.Mutex
	var tokenReqs []*http.Request
	auth := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tokenReqs = append(tokenReqs, r.Clone(context.Background()))
		mu.Unlock()
		fmt.Fprintf(w, `{"token":%q,"expires_in":300}`, testToken)
	}))
	defer auth.Close()
	var sawBearer bool
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/acme/api/tags/list" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+auth.URL+`/token",service="registry.test",scope="repository:acme/api:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sawBearer = true
		fmt.Fprint(w, tagsJSON("acme/api", []string{"1.0", "1.1", "latest"}))
	}))
	defer reg.Close()
	pool := certPool(reg)
	pool.AddCert(auth.Certificate())
	api := &RegistryAPI{RootCAs: pool}
	got, err := api.Tags(context.Background(), repoOn(t, reg, "acme/api"))
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if strings.Join(got.Tags, ",") != "1.0,1.1,latest" || got.Truncated || !sawBearer {
		t.Fatalf("tags = %v truncated=%v bearer=%v", got.Tags, got.Truncated, sawBearer)
	}
	if len(tokenReqs) != 1 {
		t.Fatalf("token requests = %d, want 1", len(tokenReqs))
	}
	q := tokenReqs[0].URL.Query()
	if q.Get("service") != "registry.test" || q.Get("scope") != "repository:acme/api:pull" {
		t.Errorf("token query = %v", q)
	}
	if h := tokenReqs[0].Header.Get("Authorization"); h != "" {
		t.Errorf("token request carried credentials %q: only anonymous tokens are asked for", h)
	}
	if strings.Contains(fmt.Sprintf("%+v", got), testToken) {
		t.Error("the token leaked into the result")
	}
}

// Pagination follows the Link header (relative URL) and the tag cap ends
// the walk with Truncated set.
func TestRegistryTagsPaginationAndCap(t *testing.T) {
	page := func(from, n int) []string {
		var out []string
		for i := from; i < from+n; i++ {
			out = append(out, fmt.Sprintf("v%d", i))
		}
		return out
	}
	var pages int
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		last := r.URL.Query().Get("last")
		from := 0
		if last != "" {
			fmt.Sscanf(last, "v%d", &from)
			from++
		}
		if from < 900 {
			w.Header().Set("Link", fmt.Sprintf(`</v2/big/app/tags/list?n=300&last=v%d>; rel="next"`, from+299))
		}
		fmt.Fprint(w, tagsJSON("big/app", page(from, 300)))
	}))
	defer reg.Close()
	api := &RegistryAPI{RootCAs: certPool(reg), MaxTags: 500}
	got, err := api.Tags(context.Background(), repoOn(t, reg, "big/app"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 500 || !got.Truncated || got.Tags[0] != "v0" || got.Tags[499] != "v499" {
		t.Fatalf("got %d tags (truncated=%v) first=%v", len(got.Tags), got.Truncated, got.Tags[:1])
	}
	if pages != 2 {
		t.Errorf("pages fetched = %d, want 2 (stop at the cap)", pages)
	}
	// The page cap also ends the walk.
	pages = 0
	api = &RegistryAPI{RootCAs: certPool(reg), MaxPages: 2, MaxTags: 5000}
	got, err = api.Tags(context.Background(), repoOn(t, reg, "big/app"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 600 || !got.Truncated || pages != 2 {
		t.Fatalf("page cap: %d tags, truncated=%v, pages=%d", len(got.Tags), got.Truncated, pages)
	}
}

// A next page on another host is refused.
func TestRegistryTagsLinkToOtherHostRefused(t *testing.T) {
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://evil.example/v2/a/tags/list?last=x>; rel="next"`)
		fmt.Fprint(w, tagsJSON("a", []string{"1"}))
	}))
	defer reg.Close()
	_, err := (&RegistryAPI{RootCAs: certPool(reg)}).Tags(context.Background(), repoOn(t, reg, "a"))
	if err == nil || !strings.Contains(err.Error(), "another host") {
		t.Fatalf("err = %v, want a refusal", err)
	}
}

// A private repository (401 / 403 / denied token) reads as "needs
// sign-in"; the token never shows in the error.
func TestRegistryTagsPrivateNeedsSignIn(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"basic challenge": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Registry"`)
			w.WriteHeader(http.StatusUnauthorized)
		},
		"no challenge": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"forbidden":    func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) },
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			reg := httptest.NewTLSServer(h)
			defer reg.Close()
			_, err := (&RegistryAPI{RootCAs: certPool(reg)}).Tags(context.Background(), repoOn(t, reg, "acme/private"))
			if !errors.Is(err, ErrNeedsSignIn) {
				t.Fatalf("err = %v, want ErrNeedsSignIn", err)
			}
		})
	}
	// The anonymous token works, the repo still says no (Docker Hub for a
	// private or unknown repo).
	var reg *httptest.Server
	reg = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			fmt.Fprintf(w, `{"access_token":%q}`, testToken)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+reg.URL+`/token",service="s",scope="repository:acme/private:pull",error="insufficient_scope"`)
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":"`+testToken+`"}]}`)
	}))
	defer reg.Close()
	_, err := (&RegistryAPI{RootCAs: certPool(reg)}).Tags(context.Background(), repoOn(t, reg, "acme/private"))
	if !errors.Is(err, ErrNeedsSignIn) {
		t.Fatalf("err = %v, want ErrNeedsSignIn", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatal("the token leaked into the error")
	}
	if err.Error() != "needs sign-in; pull by name still works" {
		t.Errorf("note = %q", err.Error())
	}
	// A token endpoint that refuses anonymous tokens: needs sign-in too.
	var reg2 *httptest.Server
	reg2 = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+reg2.URL+`/token",service="s"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer reg2.Close()
	if _, err := (&RegistryAPI{RootCAs: certPool(reg2)}).Tags(context.Background(), repoOn(t, reg2, "x/y")); !errors.Is(err, ErrNeedsSignIn) {
		t.Fatalf("token refused: err = %v", err)
	}
}

func TestRegistryTagsNotFoundAndBadAnswers(t *testing.T) {
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/missing/"):
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"errors":[{"code":"NAME_UNKNOWN"}]}`)
		case strings.Contains(r.URL.Path, "/huge/"):
			fmt.Fprint(w, `{"name":"huge","tags":["`+strings.Repeat("a", 5000)+`"]}`)
		case strings.Contains(r.URL.Path, "/junk/"):
			fmt.Fprint(w, `<html>not json</html>`)
		case strings.Contains(r.URL.Path, "/evil/"):
			fmt.Fprint(w, tagsJSON("evil", []string{"ok-1", "\x1b[31mred", "a b", "-flag", strings.Repeat("x", 200), "1.0"}))
		case strings.Contains(r.URL.Path, "/busy/"):
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer reg.Close()
	api := &RegistryAPI{RootCAs: certPool(reg), MaxBody: 1024}
	if _, err := api.Tags(context.Background(), repoOn(t, reg, "missing")); !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("missing: %v", err)
	}
	if _, err := api.Tags(context.Background(), repoOn(t, reg, "huge")); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("huge: %v", err)
	}
	if _, err := api.Tags(context.Background(), repoOn(t, reg, "junk")); err == nil {
		t.Error("junk: no error")
	}
	if _, err := api.Tags(context.Background(), repoOn(t, reg, "busy")); err == nil || !strings.Contains(err.Error(), "too many requests") {
		t.Errorf("busy: %v", err)
	}
	got, err := api.Tags(context.Background(), repoOn(t, reg, "evil"))
	if err != nil || strings.Join(got.Tags, ",") != "ok-1,1.0" {
		t.Errorf("evil: tags %q err %v (unsafe tags must be dropped)", got.Tags, err)
	}
}

// A redirect to another scheme (https → http) is never followed.
func TestRegistryTagsRedirectToOtherSchemeRefused(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the plain-http target was reached")
		fmt.Fprint(w, tagsJSON("a", []string{"1"}))
	}))
	defer plain.Close()
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	defer reg.Close()
	_, err := (&RegistryAPI{RootCAs: certPool(reg)}).Tags(context.Background(), repoOn(t, reg, "a"))
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("err = %v, want a refused redirect", err)
	}
}

// A token realm on plain http (not loopback) is refused before any
// request; a loopback registry may answer on plain http (a local
// `registry:2` on localhost:5000).
func TestRegistrySchemeRules(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost:5000": true, "127.0.0.1:5000": true, "[::1]:5000": true, "localhost": true, "127.0.0.2:5000": true,
		"registry.example.com": false, "example.com:5000": false, "10.0.0.1:5000": false, "localhost.evil.com:5000": false,
	} {
		if got := plainHTTPAllowed(host); got != want {
			t.Errorf("plainHTTPAllowed(%q) = %v, want %v", host, got, want)
		}
	}
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, tagsJSON("my/app", []string{"dev"}))
	}))
	defer plain.Close()
	got, err := (&RegistryAPI{}).Tags(context.Background(), repoOn(t, plain, "my/app"))
	if err != nil || strings.Join(got.Tags, ",") != "dev" {
		t.Fatalf("loopback plain http: %v %v", got.Tags, err)
	}
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="http://auth.example.com/token",service="s"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer reg.Close()
	_, err = (&RegistryAPI{RootCAs: certPool(reg)}).Tags(context.Background(), repoOn(t, reg, "a"))
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("plain-http realm: err = %v", err)
	}
}

func TestRegistryTagsTimeout(t *testing.T) {
	release := make(chan struct{})
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	}))
	defer reg.Close()
	defer close(release)
	start := time.Now()
	_, err := (&RegistryAPI{RootCAs: certPool(reg), Timeout: 300 * time.Millisecond}).Tags(context.Background(), repoOn(t, reg, "slow/app"))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("timeout took %v", d)
	}
}

func TestParseRemoteRepo(t *testing.T) {
	good := map[string][3]string{ // ref → host, api host, name
		"nginx":                           {"docker.io", "registry-1.docker.io", "library/nginx"},
		"nginx:1.27":                      {"docker.io", "registry-1.docker.io", "library/nginx"},
		"bitnami/redis":                   {"docker.io", "registry-1.docker.io", "bitnami/redis"},
		"docker.io/library/nginx":         {"docker.io", "registry-1.docker.io", "library/nginx"},
		"index.docker.io/nginx":           {"docker.io", "registry-1.docker.io", "library/nginx"},
		"ghcr.io/acme/api:1.4":            {"ghcr.io", "ghcr.io", "acme/api"},
		"localhost:5000/my/app":           {"localhost:5000", "localhost:5000", "my/app"},
		"Registry.Example.com:8443/t/a_b": {"registry.example.com:8443", "registry.example.com:8443", "t/a_b"},
		"quay.io/x/y@sha256:" + strings.Repeat("a", 64): {"quay.io", "quay.io", "x/y"},
	}
	for ref, want := range good {
		r, err := ParseRemoteRepo(ref)
		if err != nil || r.Host != want[0] || r.APIHost != want[1] || r.Name != want[2] {
			t.Errorf("ParseRemoteRepo(%q) = %+v, %v; want %v", ref, r, err, want)
		}
	}
	if r, _ := ParseRemoteRepo("nginx"); r.Ref() != "nginx" {
		t.Errorf("Ref of library image = %q", r.Ref())
	}
	if r, _ := ParseRemoteRepo("ghcr.io/acme/api"); r.Ref() != "ghcr.io/acme/api" {
		t.Errorf("Ref = %q", r.Ref())
	}
	for _, bad := range []string{"", "-x", "../etc", "a/../b", "a/./b", "UPPER/case", "a b", "a//b", "ghcr.io/", "/x",
		"host:99999/x", "%2e%2e/x", "a\nb", "ghcr.io/acme/api?x=1", "ghcr.io/acme/api#f", "user@ghcr.io/x", "h_st.io/x/y/" + strings.Repeat("a", 260),
		"http://ghcr.io/x", "ghcr.io/x\x1b[31m"} {
		if r, err := ParseRemoteRepo(bad); err == nil {
			t.Errorf("ParseRemoteRepo(%q) = %+v, want an error", bad, r)
		}
	}
}
