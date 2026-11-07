// Package agentenv is the per-project agent environment (CMP-63): one
// file, .termocode/agent-env.json, that says how an outside agent (Claude
// Code, Codex) runs in a workflow's worktree:
//
//	{
//	  "setup": ["npm ci"],              // runs once per worktree, before the agent
//	  "setup_timeout": "10m",
//	  "env": {"NODE_ENV": "test"},      // plain values: setup and agent
//	  "secrets": {"NPM_TOKEN": "npm.token"}, // env name -> secret store name: setup only
//	  "network": {"mode": "package-managers"} // open | off | package-managers | allow
//	}
//
// Secrets come only from the secret store (internal/secrets) and only
// the setup commands see them; the agent never does. The network rule
// is kept by a local proxy (proxy.go) that the agent's tools use through
// HTTP(S)_PROXY; blocked hosts are recorded for the run log. The agent's
// own API hosts always pass. The file is code from the repo, so the app
// uses it only after the user trusts its exact content.
package agentenv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/jsonc"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// File is the project file, relative to the workspace root.
const File = ".termocode/agent-env.json"

// Network modes.
const (
	NetOpen     = "open"             // no rule (the default)
	NetOff      = "off"              // only the agent's own API
	NetPackages = "package-managers" // the package registries
	NetAllow    = "allow"            // the allow list (and the registries with package_managers)
)

// Modes are the allowed network modes, the default first.
var Modes = []string{NetOpen, NetOff, NetPackages, NetAllow}

// Setup limits.
const (
	DefaultSetupTimeout = 10 * time.Minute
	MaxSetupTimeout     = time.Hour
	maxSetup            = 50
	maxCommand          = 4096
	maxHosts            = 200
	maxFile             = 256 << 10
)

// Env is a parsed agent environment file.
type Env struct {
	Setup        []string
	SetupTimeout time.Duration
	Vars         map[string]string // env name -> value
	Secrets      map[string]string // env name -> secret store name
	Network      Network
	Hash         string // sha256 of the file bytes (the trust key)
}

// Network is the network rule of the agent.
type Network struct {
	Mode            string
	Allow           []string // host or *.domain, lower case
	PackageManagers bool     // allow mode: add the package registries
}

// Starter is the file AI: Agent Environment… writes.
const Starter = `{
  // Agent environment (termocode). Workflows' agent steps use it after
  // you trust it (AI: Agent Environment…).
  //
  // setup: shell commands, run once per worktree before the agent.
  "setup": [],
  "setup_timeout": "10m",
  // env: plain values for setup and agent.
  "env": {},
  // secrets: ENV_NAME -> name in termocode's secret store. Only the
  // setup commands get them; the agent never does.
  "secrets": {},
  // network: open | off | package-managers | allow
  //   allow: {"mode": "allow", "allow": ["example.com", "*.github.com"], "package_managers": true}
  "network": {"mode": "package-managers"}
}
`

type rawNetwork struct {
	Mode            string   `json:"mode"`
	Allow           []string `json:"allow"`
	PackageManagers bool     `json:"package_managers"`
}

type rawFile struct {
	Setup        []string          `json:"setup"`
	SetupTimeout string            `json:"setup_timeout"`
	Env          map[string]string `json:"env"`
	Secrets      map[string]string `json:"secrets"`
	Network      *rawNetwork       `json:"network"`
}

var (
	envNameRe = lazyre.New(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	hostRe    = lazyre.New(`^(\*\.)?[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
	ipRe      = lazyre.New(`^[0-9.]+$`)
)

// proxyNames are set by the proxy; the file may not set them.
var proxyNames = map[string]bool{
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
}

// Load reads root's file; no file is (nil, nil).
func Load(root string) (*Env, error) {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(File)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFile {
		return nil, fmt.Errorf("%s is larger than %d KB", File, maxFile>>10)
	}
	return Parse(b)
}

// Parse reads the file bytes (JSON with comments).
func Parse(b []byte) (*Env, error) {
	sum := sha256.Sum256(b)
	dec := json.NewDecoder(bytes.NewReader(jsonc.Strip(b)))
	dec.DisallowUnknownFields()
	var raw rawFile
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%s: %w", File, err)
	}
	e := &Env{SetupTimeout: DefaultSetupTimeout, Vars: map[string]string{}, Secrets: map[string]string{},
		Network: Network{Mode: NetOpen}, Hash: hex.EncodeToString(sum[:])}
	if len(raw.Setup) > maxSetup {
		return nil, fmt.Errorf("setup: at most %d commands", maxSetup)
	}
	for i, c := range raw.Setup {
		c = strings.TrimSpace(c)
		if c == "" || len(c) > maxCommand {
			return nil, fmt.Errorf("setup %d: empty or longer than %d bytes", i+1, maxCommand)
		}
		e.Setup = append(e.Setup, c)
	}
	if raw.SetupTimeout != "" {
		d, err := time.ParseDuration(raw.SetupTimeout)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("setup_timeout %q: a duration like 10m", raw.SetupTimeout)
		}
		if d > MaxSetupTimeout {
			return nil, fmt.Errorf("setup_timeout: at most %s", MaxSetupTimeout)
		}
		e.SetupTimeout = d
	}
	for k, v := range raw.Env {
		if err := checkEnvName(k); err != nil {
			return nil, fmt.Errorf("env: %w", err)
		}
		e.Vars[k] = v
	}
	for k, name := range raw.Secrets {
		if err := checkEnvName(k); err != nil {
			return nil, fmt.Errorf("secrets: %w", err)
		}
		if _, dup := e.Vars[k]; dup {
			return nil, fmt.Errorf("secrets: %s is also in env", k)
		}
		if err := secrets.ValidName(name); err != nil {
			return nil, fmt.Errorf("secrets: %s: %w", k, err)
		}
		e.Secrets[k] = name
	}
	if raw.Network != nil {
		n, err := parseNetwork(*raw.Network)
		if err != nil {
			return nil, fmt.Errorf("network: %w", err)
		}
		e.Network = n
	}
	return e, nil
}

func checkEnvName(k string) error {
	switch {
	case !envNameRe.MatchString(k):
		return fmt.Errorf("%q is not an environment name", k)
	case proxyNames[strings.ToUpper(k)]:
		return fmt.Errorf("%s is set by the network rule", k)
	case strings.HasPrefix(strings.ToUpper(k), "TERMOCODE_"):
		return fmt.Errorf("%s: TERMOCODE_ names are termocode's own", k)
	}
	return nil
}

func parseNetwork(r rawNetwork) (Network, error) {
	n := Network{Mode: r.Mode, PackageManagers: r.PackageManagers}
	ok := false
	for _, m := range Modes {
		ok = ok || m == r.Mode
	}
	if !ok {
		return n, fmt.Errorf("mode %q: one of %s", r.Mode, strings.Join(Modes, ", "))
	}
	if r.Mode != NetAllow && (len(r.Allow) > 0 || r.PackageManagers) {
		return n, errors.New(`"allow" and "package_managers" need "mode": "allow"`)
	}
	if len(r.Allow) > maxHosts {
		return n, fmt.Errorf("allow: at most %d hosts", maxHosts)
	}
	for _, h := range r.Allow {
		h = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
		if !hostRe.MatchString(h) || ipRe.MatchString(strings.TrimPrefix(h, "*.")) {
			return n, fmt.Errorf("allow: %q is not a host name (example.com or *.example.com)", h)
		}
		n.Allow = append(n.Allow, h)
	}
	return n, nil
}

// VarEnv is the plain values as NAME=value, sorted.
func (e *Env) VarEnv() []string {
	out := make([]string, 0, len(e.Vars))
	for k, v := range e.Vars {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// SecretNames is the env names of the secrets, sorted.
func (e *Env) SecretNames() []string {
	out := make([]string, 0, len(e.Secrets))
	for k := range e.Secrets {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Getter returns a named secret (secrets.Store.Get).
type Getter interface {
	Get(name string) (value string, ok bool, err error)
}

// SecretEnv reads the secrets from g: NAME=value lines and the values
// (to redact output). Errors never hold a value.
func (e *Env) SecretEnv(g Getter) (env, values []string, err error) {
	for _, k := range e.SecretNames() {
		name := e.Secrets[k]
		if g == nil {
			return nil, nil, fmt.Errorf("secret %q (for %s): the secret store is not open", name, k)
		}
		v, ok, err := g.Get(name)
		if err != nil {
			return nil, nil, fmt.Errorf("secret %q (for %s): %s", name, k, Redact(err.Error(), []string{v}))
		}
		if !ok {
			return nil, nil, fmt.Errorf("secret %q (for %s) is not in the secret store", name, k)
		}
		env = append(env, k+"="+v)
		values = append(values, v)
	}
	return env, values, nil
}

// Merge is base without the names in add, then add.
func Merge(base []string, add ...string) []string {
	drop := make(map[string]bool, len(add))
	for _, kv := range add {
		k, _, _ := strings.Cut(kv, "=")
		drop[k] = true
	}
	out := make([]string, 0, len(base)+len(add))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			out = append(out, kv)
		}
	}
	return append(out, add...)
}

// Redact replaces every value (3+ bytes) in s with ***.
func Redact(s string, values []string) string {
	for _, v := range values {
		if len(v) >= 3 {
			s = strings.ReplaceAll(s, v, "***")
		}
	}
	return s
}

// Summary is the short text of the network rule ("allow: a.com, b.com").
func (n Network) Summary() string {
	switch n.Mode {
	case NetAllow:
		s := "allow list: " + strings.Join(n.Allow, ", ")
		if n.PackageManagers {
			s += " + package registries"
		}
		return s
	case NetPackages:
		return "package registries only"
	case NetOff:
		return "off (only the agent's own service)"
	}
	return "open (no rule)"
}
