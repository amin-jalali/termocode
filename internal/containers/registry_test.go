package containers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// CP-7: a host in `auths` is signed in; a host only in `credHelpers`
// names a helper but may have no login.
func TestRegistryConfigSignedInAndHelpers(t *testing.T) {
	cfg, err := ReadRegistryConfig(filepath.Join("testdata", "registry-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Registry{}
	for _, r := range cfg.Registries {
		got[r.Host] = r
	}
	if r := got["https://index.docker.io/v1/"]; !r.SignedIn || r.CredHelper != "" || r.CredsStore != "desktop" {
		t.Errorf("hub = %+v, want signed in through the store", r)
	}
	if r := got["ghcr.io"]; !r.SignedIn || r.CredHelper != "secretservice" {
		t.Errorf("ghcr = %+v, want signed in with its helper", r)
	}
	if r := got["quay.io"]; r.SignedIn || r.CredHelper != "pass" {
		t.Errorf("quay = %+v, want a helper only, not signed in", r)
	}
}

// A broken file is an error that names no stored value; it never panics.
func TestRegistryConfigMalformedNamesNoValue(t *testing.T) {
	const blob = "c2VjcmV0LWJsb2ItbmV2ZXItc2hvd24="
	for name, body := range map[string]string{
		"auths not an object": `{"auths":"` + blob + `"}`,
		"cut off":             `{"auths":{"ghcr.io":{"auth":"` + blob + `"`,
		"not json":            `auth=` + blob,
		"array":               `["` + blob + `"]`,
		"helpers wrong type":  `{"credHelpers":{"ghcr.io":{"auth":"` + blob + `"}}}`,
	} {
		_, err := DecodeRegistryConfig(strings.NewReader(body))
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if strings.Contains(err.Error(), blob) || strings.Contains(err.Error(), "c2VjcmV0") {
			t.Errorf("%s: error shows the stored value: %v", name, err)
		}
	}
}

func TestRegistryDisplayHost(t *testing.T) {
	for in, want := range map[string]string{
		"https://index.docker.io/v1/": "docker.io",
		"index.docker.io":             "docker.io",
		"registry-1.docker.io":        "docker.io",
		"docker.io":                   "docker.io",
		"ghcr.io":                     "ghcr.io",
		"https://ghcr.io":             "ghcr.io",
		"localhost:5000":              "localhost:5000",
		"http://registry.lan:5000/":   "registry.lan:5000",
	} {
		if got := RegistryDisplayHost(in); got != want {
			t.Errorf("RegistryDisplayHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestImageRegistryHost(t *testing.T) {
	for in, want := range map[string]string{
		"postgres:16":              "docker.io",
		"library/postgres":         "docker.io",
		"acme/app:1":               "docker.io",
		"ghcr.io/acme/shop:latest": "ghcr.io",
		"localhost/app":            "localhost",
		"localhost:5000/app:dev":   "localhost:5000",
		"registry.gitlab.com/g/p/img@sha256:" + strings.Repeat("a", 64): "registry.gitlab.com",
		"docker.io/library/redis:7":                                     "docker.io",
	} {
		if got := ImageRegistryHost(in); got != want {
			t.Errorf("ImageRegistryHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidRegistryHostAndUser(t *testing.T) {
	for _, h := range []string{"ghcr.io", "localhost:5000", "registry.example.com/v2", "https://index.docker.io/v1/", "[::1]:5000"} {
		if err := ValidRegistryHost(h); err != nil {
			t.Errorf("host %q refused: %v", h, err)
		}
	}
	for _, h := range []string{"", "-ghcr.io", "--password=x", "gh cr.io", "ghcr.io\n", "a\x00b", "ghcr.io;rm"} {
		if ValidRegistryHost(h) == nil {
			t.Errorf("host %q accepted", h)
		}
	}
	for _, u := range []string{"amin", "amin-jalali", "user@example.com", "robot$ci+bot"} {
		if err := ValidRegistryUser(u); err != nil {
			t.Errorf("user %q refused: %v", u, err)
		}
	}
	for _, u := range []string{"", "-p", "--password-stdin", "a b", "a\nb"} {
		if ValidRegistryUser(u) == nil {
			t.Errorf("user %q accepted", u)
		}
	}
}

func TestLoginRefusesFlagLikeHostOrUser(t *testing.T) {
	f := servicestest.New(t)
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}}
	for _, hu := range [][2]string{{"--help", "u"}, {"ghcr.io", "--password=x"}} {
		if err := e.Login(context.Background(), hu[0], hu[1], "topsecret-1"); err == nil {
			t.Errorf("login %q / %q ran", hu[0], hu[1])
		}
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("refused logins still called the engine: %q", f.Calls())
	}
}

// The engine's own message is shown, with the secret scrubbed.
func TestLoginErrorShowsEngineMessageWithoutSecret(t *testing.T) {
	f := servicestest.New(t)
	const secret = "hunter2-very-secret"
	f.Set(t, "login.err", "Error response from daemon: unauthorized: bad token "+secret+"\n")
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}}
	err := e.Login(context.Background(), "ghcr.io", "amin", secret)
	if err == nil {
		t.Fatal("a failed login returned no error")
	}
	if strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("error = %q, want the engine's words without the secret", err)
	}
	if strings.Contains(fmt.Sprintf("%v %#v", err, err), secret) {
		t.Fatal("the secret is inside the error value")
	}
}

// Every login has a timeout; a hanging engine is killed and named.
func TestLoginTimesOut(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "sleep-login", "5")
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}, LoginTimeout: 300 * time.Millisecond}
	start := time.Now()
	err := e.Login(context.Background(), "ghcr.io", "amin", "topsecret-1")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("login waited %v", d)
	}
	if _, statErr := os.Stat(filepath.Join(f.Data, "login.stdin")); statErr == nil {
		t.Log("stdin was read before the timeout (allowed)")
	}
}

// The session's DOCKER_CONTEXT reaches login too.
func TestLoginUsesSessionContext(t *testing.T) {
	f := servicestest.New(t)
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}, Context: "prod"}
	if err := e.Login(context.Background(), "ghcr.io", "amin", "topsecret-1"); err != nil {
		t.Fatal(err)
	}
	calls := f.CtxCalls()
	if len(calls) != 1 || calls[0] != "prod|login ghcr.io --username amin --password-stdin" {
		t.Fatalf("ctx calls = %q", calls)
	}
}
