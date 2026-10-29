package containers

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// Registry tags and registry config: what the user sees when a registry
// or its token server misbehaves, and when config.json is odd. Every
// server is an in-process loopback server.

// tokenRegistry is a registry that always asks for a bearer token from
// its own /token path, which the token handler answers.
func tokenRegistry(t *testing.T, challenge func(base string) string, token http.HandlerFunc) *httptest.Server {
	t.Helper()
	var reg *httptest.Server
	reg = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			token(w, r)
			return
		}
		if r.Header.Get("Authorization") == "Bearer "+testToken {
			fmt.Fprint(w, tagsJSON("a/b", []string{"1.0"}))
			return
		}
		w.Header().Set("WWW-Authenticate", challenge(reg.URL))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(reg.Close)
	return reg
}

func realm(base string) string { return `Bearer realm="` + base + `/token",service="reg"` }

func TestRegistryTokenServerFailures(t *testing.T) {
	cases := []struct {
		name      string
		challenge func(string) string
		token     http.HandlerFunc
		want      string
		signIn    bool
	}{
		{"no realm", func(string) string { return `Bearer service="reg"` }, nil, "", true},
		{"relative realm", func(string) string { return `Bearer realm="/token"` }, nil, "not a plain https URL", false},
		{"realm with user", func(b string) string {
			return `Bearer realm="` + strings.Replace(b, "https://", "https://u:p@", 1) + `/token"`
		}, nil, "not a plain https URL", false},
		{"token server error", realm, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }, "the token server answered 500", false},
		{"token not json", realm, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>") }, "no usable token", false},
		{"empty token", realm, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"token":""}`) }, "no usable token", false},
		{"token with a newline", realm, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"token":"ab\ncd"}`) }, "no usable token", false},
		{"huge token", realm, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"token":%q}`, strings.Repeat("x", 17<<10))
		}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tok := c.token
			if tok == nil {
				tok = func(w http.ResponseWriter, r *http.Request) { t.Error("the token server was asked") }
			}
			reg := tokenRegistry(t, c.challenge, tok)
			_, err := (&RegistryAPI{RootCAs: certPool(reg)}).Tags(context.Background(), repoOn(t, reg, "a/b"))
			if err == nil {
				t.Fatal("no error")
			}
			if c.signIn != errors.Is(err, ErrNeedsSignIn) {
				t.Fatalf("err = %v, needs sign-in %v", err, c.signIn)
			}
			if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// access_token (the OAuth2 name) works as well as token, and an unquoted
// challenge parameter is read.
func TestRegistryAccessTokenAndUnquotedChallenge(t *testing.T) {
	var scope string
	reg := tokenRegistry(t, func(b string) string {
		return `Bearer realm="` + b + `/token",service=reg,scope=repository:a/b:pull`
	}, func(w http.ResponseWriter, r *http.Request) {
		scope = r.URL.Query().Get("scope")
		fmt.Fprintf(w, `{"access_token":%q}`, testToken)
	})
	got, err := (&RegistryAPI{RootCAs: certPool(reg)}).Tags(context.Background(), repoOn(t, reg, "a/b"))
	if err != nil || strings.Join(got.Tags, ",") != "1.0" {
		t.Fatalf("tags = %v, %v", got.Tags, err)
	}
	if scope != "repository:a/b:pull" {
		t.Fatalf("scope = %q", scope)
	}
}

func TestParseChallenge(t *testing.T) {
	cases := []struct {
		in     string
		scheme string
		params map[string]string
	}{
		{`Bearer realm="https://a/t",service="s"`, "bearer", map[string]string{"realm": "https://a/t", "service": "s"}},
		{`BEARER Realm=https://a/t, service=s`, "bearer", map[string]string{"realm": "https://a/t", "service": "s"}},
		{`Bearer realm="https://a/t`, "bearer", map[string]string{}},
		{`Bearer junk`, "bearer", map[string]string{}},
		{``, "", map[string]string{}},
	}
	for _, c := range cases {
		scheme, params := parseChallenge(c.in)
		if scheme != c.scheme || fmt.Sprint(params) != fmt.Sprint(c.params) {
			t.Errorf("parseChallenge(%q) = %q %v, want %q %v", c.in, scheme, params, c.scheme, c.params)
		}
	}
}

func TestNextPageLinks(t *testing.T) {
	cur := "https://reg.example/v2/a/tags/list?n=100"
	cases := []struct {
		link, want, err string
	}{
		{``, "", ""},
		{`</v2/a/tags/list?last=9>; rel=next`, "https://reg.example/v2/a/tags/list?last=9", ""},
		{`</v2/a/tags/list?last=1>; rel="prev"`, "", ""},
		{`/v2/a/tags/list?last=9; rel="next"`, "", "bad next-page link"},
		{`<http://[::1>; rel="next"`, "", "bad next-page link"},
		{`</v1/other>; rel="next"`, "", "another host"},
	}
	for _, c := range cases {
		got, err := nextPage(cur, c.link)
		if got != c.want || (c.err == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), c.err) {
			t.Errorf("nextPage(%q) = %q, %v; want %q, %q", c.link, got, err, c.want, c.err)
		}
	}
	if _, err := nextPage("http://[::1", `</v2/x>; rel="next"`); err == nil {
		t.Error("a broken current URL was accepted")
	}
}

func TestRegistryUnreachableAndServerError(t *testing.T) {
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	repo := repoOn(t, reg, "a/b")
	api := &RegistryAPI{RootCAs: certPool(reg), ConnectTimeout: 2 * time.Second}
	if _, err := api.Tags(context.Background(), repo); err == nil || err.Error() != "the registry answered 502" {
		t.Fatalf("502: %v", err)
	}
	reg.Close()
	_, err := api.Tags(context.Background(), repo)
	if err == nil || !strings.HasPrefix(err.Error(), "cannot reach the registry: ") {
		t.Fatalf("closed registry: %v", err)
	}
}

func TestRegistryRedirectLoopStops(t *testing.T) {
	hits := 0
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, fmt.Sprintf("/v2/a/b/tags/list?hop=%d", hits), http.StatusFound)
	}))
	defer reg.Close()
	_, err := (&RegistryAPI{RootCAs: certPool(reg)}).Tags(context.Background(), repoOn(t, reg, "a/b"))
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("err = %v", err)
	}
	if hits > 5 {
		t.Fatalf("followed %d redirects", hits)
	}
}

func TestRegistryTagsRefusesBadRepo(t *testing.T) {
	api := &RegistryAPI{}
	for _, r := range []RemoteRepo{{APIHost: "", Name: "a"}, {APIHost: "bad host", Name: "a"}, {APIHost: "reg.example", Name: "a/../b"}, {APIHost: "reg.example", Name: "Upper"}} {
		if _, err := api.Tags(context.Background(), r); err == nil {
			t.Errorf("Tags(%+v) was sent", r)
		}
	}
}

func TestParseRemoteRepoRefusesLongRefAndBadTag(t *testing.T) {
	if _, err := ParseRemoteRepo(strings.Repeat("a", 513)); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("long ref: %v", err)
	}
	if _, err := ParseRemoteRepo("nginx:-bad"); err == nil || !strings.Contains(err.Error(), "bad tag") {
		t.Fatalf("bad tag: %v", err)
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestReadCappedReadError(t *testing.T) {
	if _, err := readCapped(failReader{}, 10); err == nil || err.Error() != "connection reset" {
		t.Fatalf("err = %v", err)
	}
	if b, err := readCapped(strings.NewReader("abc"), 3); err != nil || string(b) != "abc" {
		t.Fatalf("exact size: %q %v", b, err)
	}
}

// config.json with other tools' settings (proxies, plugins, nested arrays
// and plain values) still lists the registries.
func TestRegistryConfigSkipsOtherSettings(t *testing.T) {
	cfg, err := DecodeRegistryConfig(strings.NewReader(`{
		"proxies": {"default": {"httpProxy": "http://p:3128", "noProxy": ["a", "b"]}},
		"plugins": [1, [2, [3]], {"x": true}, null],
		"detachKeys": "ctrl-q",
		"experimental": false,
		"auths": {"ghcr.io": {"auth": "c2VjcmV0", "nested": [{"k": 1}]}},
		"credHelpers": {"123.dkr.ecr.us-east-1.amazonaws.com": "ecr-login"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registries) != 2 || cfg.Registries[0].Host != "123.dkr.ecr.us-east-1.amazonaws.com" || cfg.Registries[0].CredHelper != "ecr-login" || cfg.Registries[1].Host != "ghcr.io" || !cfg.Registries[1].SignedIn {
		t.Fatalf("registries = %+v", cfg.Registries)
	}
}

func TestRegistryConfigBrokenFiles(t *testing.T) {
	for _, in := range []string{
		``,
		`{"proxies": {"default": {"httpProxy": `,
		`{"plugins": [1, 2`,
		`{"plugins": [[`,
		`{"auths": `,
		`{"auths": {"ghcr.io": {}`,
		`{"auths": {"ghcr.io": {"a": [`,
		`{"credHelpers": {"ghcr.io": 5}}`,
		`{"auths": {}`,
		`{"auths": {"ghcr.io"`,
	} {
		if _, err := DecodeRegistryConfig(strings.NewReader(in)); err == nil {
			t.Errorf("DecodeRegistryConfig(%q) accepted", in)
		}
	}
	if _, err := ReadRegistryConfig(filepath.Join(t.TempDir(), "config.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
}

func TestValidRegistryHostAndUserLength(t *testing.T) {
	if err := ValidRegistryHost(strings.Repeat("a", 513)); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("long host: %v", err)
	}
	if err := ValidRegistryUser(strings.Repeat("u", 257)); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("long user: %v", err)
	}
}

// A login that fails with nothing on stderr names the exit code; an
// engine that cannot start names the start error, never the password.
func TestLoginSilentFailureAndMissingEngine(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "login.err", "")
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}}
	if err := e.Login(context.Background(), "ghcr.io", "amin", "pw-1"); err == nil || err.Error() != "login exited 1" {
		t.Fatalf("silent login: %v", err)
	}
	if err := e.Login(context.Background(), "ghcr.io", "amin", ""); err == nil || err.Error() != "login exited 1" {
		t.Fatalf("empty password login: %v", err)
	}
	gone := Engine{Binary: filepath.Join(t.TempDir(), "pw-1"), Kind: "docker", X: services.SpawnExec{}}
	err := gone.Login(context.Background(), "ghcr.io", "amin", "pw-1")
	if err == nil || strings.Contains(err.Error(), "pw-1") || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("missing engine login: %v", err)
	}
}
