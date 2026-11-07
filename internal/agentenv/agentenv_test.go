package agentenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFullFile(t *testing.T) {
	e, err := Parse([]byte(`{
		// comments are fine (jsonc)
		"setup": ["npm ci", "go mod download"],
		"setup_timeout": "5m",
		"env": {"NODE_ENV": "test"},
		"secrets": {"NPM_TOKEN": "npm.token"},
		"network": {"mode": "allow", "allow": ["Example.COM", "*.github.com"], "package_managers": true}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Setup) != 2 || e.SetupTimeout != 5*time.Minute {
		t.Errorf("setup %v %v", e.Setup, e.SetupTimeout)
	}
	if e.Vars["NODE_ENV"] != "test" || e.Secrets["NPM_TOKEN"] != "npm.token" {
		t.Errorf("env %v secrets %v", e.Vars, e.Secrets)
	}
	if e.Network.Mode != NetAllow || !e.Network.PackageManagers || e.Network.Allow[0] != "example.com" {
		t.Errorf("network %+v", e.Network)
	}
	if len(e.Hash) != 64 {
		t.Errorf("hash %q", e.Hash)
	}
}

func TestParseDefaults(t *testing.T) {
	e, err := Parse([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if e.Network.Mode != NetOpen || e.SetupTimeout != DefaultSetupTimeout {
		t.Errorf("defaults %+v", e)
	}
}

func TestParseRejects(t *testing.T) {
	for name, src := range map[string]string{
		"unknown key":         `{"setpu": ["x"]}`,
		"bad mode":            `{"network": {"mode": "some"}}`,
		"allow without mode":  `{"network": {"mode": "off", "allow": ["a.com"]}}`,
		"bad host":            `{"network": {"mode": "allow", "allow": ["http://a.com/x"]}}`,
		"ip host":             `{"network": {"mode": "allow", "allow": ["10.0.0.1"]}}`,
		"bad env name":        `{"env": {"1A": "x"}}`,
		"proxy env name":      `{"env": {"HTTPS_PROXY": "x"}}`,
		"termocode env name":  `{"env": {"TERMOCODE_X": "x"}}`,
		"both env and secret": `{"env": {"A": "x"}, "secrets": {"A": "a"}}`,
		"bad secret name":     `{"secrets": {"A": "../x"}}`,
		"empty setup":         `{"setup": ["  "]}`,
		"long timeout":        `{"setup_timeout": "3h"}`,
		"bad timeout":         `{"setup_timeout": "soon"}`,
	} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestLoadMissingIsNil(t *testing.T) {
	e, err := Load(t.TempDir())
	if e != nil || err != nil {
		t.Errorf("got %v %v", e, err)
	}
}

func TestLoadReadsTheProjectFile(t *testing.T) {
	ws := t.TempDir()
	p := filepath.Join(ws, filepath.FromSlash(File))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"setup": ["true"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := Load(ws)
	if err != nil || e == nil || e.Setup[0] != "true" {
		t.Fatalf("got %v %v", e, err)
	}
}

func TestStarterParses(t *testing.T) {
	e, err := Parse([]byte(Starter))
	if err != nil {
		t.Fatal(err)
	}
	if e.Network.Mode != NetPackages {
		t.Errorf("starter mode %q", e.Network.Mode)
	}
}

func TestPolicyModes(t *testing.T) {
	open := Network{Mode: NetOpen}.Policy()
	if !open.Open() || !open.Allows("anything.example") {
		t.Error("open blocks")
	}
	off := Network{Mode: NetOff}.Policy("api.agent.example")
	if off.Open() || off.Allows("registry.npmjs.org") || !off.Allows("api.agent.example:443") {
		t.Error("off: only the agent's own host")
	}
	pm := Network{Mode: NetPackages}.Policy()
	if !pm.Allows("registry.npmjs.org") || !pm.Allows("proxy.golang.org:443") || pm.Allows("evil.example") {
		t.Error("package managers")
	}
	al := Network{Mode: NetAllow, Allow: []string{"example.com", "*.github.com"}}.Policy()
	for host, want := range map[string]bool{
		"example.com": true, "EXAMPLE.com.": true, "a.example.com": false,
		"api.github.com:443": true, "github.com": false, "evilgithub.com": false,
		"registry.npmjs.org": false,
	} {
		if got := al.Allows(host); got != want {
			t.Errorf("allow %q = %v, want %v", host, got, want)
		}
	}
	both := Network{Mode: NetAllow, Allow: []string{"example.com"}, PackageManagers: true}.Policy()
	if !both.Allows("pypi.org") {
		t.Error("package_managers adds the registries")
	}
}

func TestAgentHosts(t *testing.T) {
	if len(AgentHosts("codex")) == 0 || len(AgentHosts("claude-code")) == 0 || AgentHosts("other") != nil {
		t.Error("agent hosts")
	}
}

type fakeSecrets map[string]string

func (f fakeSecrets) Get(name string) (string, bool, error) {
	v, ok := f[name]
	return v, ok, nil
}

func TestSecretEnvOnlyFromTheStore(t *testing.T) {
	e := &Env{Secrets: map[string]string{"NPM_TOKEN": "npm.token"}}
	env, vals, err := e.SecretEnv(fakeSecrets{"npm.token": "s3cret"})
	if err != nil || len(env) != 1 || env[0] != "NPM_TOKEN=s3cret" || vals[0] != "s3cret" {
		t.Errorf("got %v %v %v", env, vals, err)
	}
	if _, _, err := e.SecretEnv(fakeSecrets{}); err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("missing secret: %v", err)
	}
	if _, _, err := e.SecretEnv(nil); err == nil {
		t.Error("no store: no error")
	}
}

func TestMergeReplaces(t *testing.T) {
	got := Merge([]string{"A=1", "B=2", "a=3"}, "B=9", "C=1")
	want := "A=1 a=3 B=9 C=1"
	if strings.Join(got, " ") != want {
		t.Errorf("got %v", got)
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("token s3cret here", []string{"s3cret", ""}); got != "token *** here" {
		t.Errorf("got %q", got)
	}
}
