// Package mcp is termocode's MCP (Model Context Protocol) client: the AI
// chat agent can call the tools of external MCP servers (filesystem,
// Postgres, Playwright, GitHub …). Phase A speaks stdio only: a server is
// a child process that talks newline-delimited JSON-RPC 2.0.
//
// File map:
//
//	config.go    — mcp.json (Claude Desktop / Claude Code / Cursor shape),
//	               user + project merge, ${…} expansion, validation
//	proto.go     — MCP message types and result text
//	client.go    — one connection: initialize, tools/list, tools/call,
//	               server→client requests (ping, roots/list)
//	stdio.go     — the child process (process group, minimal env)
//	manager.go   — every configured server: lazy start, idle stop, trust,
//	               tool cache, status
//	adapter.go   — MCP tool → ai.Tool (wire names, gates, result caps)
//	settings.go  — the mcp_* keys of config.json (timeouts, trust)
//	mcptest/     — a fake stdio server for tests (re-exec of the test binary)
//
// The app glue is internal/app/ai_mcp.go; the user docs are
// docs/guides/mcp.md.
package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/secrets"
)

// Transport types (mcp.json "type").
const (
	TypeStdio = "stdio"
	TypeHTTP  = "http" // Phase B (MC-5): parsed, listed, not connected yet
	TypeSSE   = "sse"
)

// Where an entry comes from.
const (
	SourceUser    = "user"    // ~/.config/termocode/mcp.json
	SourceProject = "project" // <root>/.termocode/mcp.json — never auto-trusted
)

// Server is one mcpServers entry. String fields keep their ${…}
// references; Expand resolves them right before a start.
type Server struct {
	Name    string
	Type    string
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string
	URL     string
	Headers map[string]string
	// OAuth is a pre-registered OAuth app (Slack): client id, secret (a
	// ${secret:NAME}), scopes. nil: discovery + dynamic registration.
	OAuth *OAuthConfig
	// Auth is termocode's own sign-in source ("auth": {"gh": "<login>"}):
	// the token comes from the GitHub CLI at connect time. Only allowed in
	// the user file.
	Auth *AuthConfig

	Disabled    bool
	ReadOnly    bool     // expose only read-only tools
	AutoApprove []string // tool names that run without asking (Cursor / Cline key)
	Deny        []string // tool names (globs) never exposed
	Timeout     int      // seconds per tools/call; 0: mcp_call_timeout

	Source     string // SourceUser or SourceProject
	SourcePath string
	// Problem is why the entry can not run ("" when it is valid). The
	// servers list shows it; it never blocks startup.
	Problem string

	// TrustReason is set on the copy given to AskTrust: why the user is
	// asked (not part of the file).
	TrustReason TrustReason

	raw json.RawMessage
}

// TrustReason says why AskTrust asks.
type TrustReason int

const (
	// ReasonStart: an untrusted (project) server is about to start.
	ReasonStart TrustReason = iota
	// ReasonToolsChanged: the server's tools differ from the list the
	// user trusted before (a "rug pull" check).
	ReasonToolsChanged
)

// OAuthConfig is the termocode "oauth" key of an http entry.
type OAuthConfig struct {
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

// AuthConfig is the termocode "auth" key of an http entry.
type AuthConfig struct {
	// GH is the github.com login whose `gh auth token` is the bearer.
	GH string `json:"gh,omitempty"`
}

// entry is the JSON shape of one server. Claude Desktop's "transport" is
// an alias of "type".
type entry struct {
	Type        string            `json:"type"`
	Transport   string            `json:"transport"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	Cwd         string            `json:"cwd"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	OAuth       *OAuthConfig      `json:"oauth"`
	Auth        *AuthConfig       `json:"auth"`
	Disabled    bool              `json:"disabled"`
	ReadOnly    bool              `json:"readOnly"`
	AutoApprove []string          `json:"autoApprove"`
	Deny        []string          `json:"deny"`
	Timeout     int               `json:"timeout"`
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

// ParseFile reads one mcp.json body. Bad entries come back with a
// Problem; warnings name literal secrets. err is only for a file that is
// not JSON at all.
func ParseFile(b []byte, source, path string) ([]Server, []string, error) {
	var f struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	var out []Server
	var warns []string
	for name, raw := range f.MCPServers {
		s := parseEntry(name, raw)
		s.Source, s.SourcePath = source, path
		if s.Auth != nil && s.Auth.GH != "" && source != SourceUser && s.Problem == "" {
			s.Problem = "invalid: auth.gh is only allowed in your own mcp.json"
		}
		if source != SourceUser && secrets.HasCmdRef(string(raw)) {
			// A command from a file that travels with the repository
			// would run on the reader's machine: refuse it, never run it.
			s.Problem = "invalid: " + secrets.ProjectCmdWarning
			warns = append(warns, fmt.Sprintf("%s: server %s uses ${cmd:}; %s", path, name, secrets.ProjectCmdWarning))
		}
		if w := secretLiteral(s); w != "" {
			warns = append(warns, fmt.Sprintf("%s: server %s has a literal secret in %s; use ${secret:NAME} or ${VAR}", path, name, w))
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	sort.Strings(warns)
	return out, warns, nil
}

func parseEntry(name string, raw json.RawMessage) Server {
	s := Server{Name: name, raw: raw}
	var e entry
	if err := json.Unmarshal(raw, &e); err != nil {
		s.Problem = "invalid entry: " + err.Error()
		return s
	}
	s.Command, s.Args, s.Env, s.Cwd = e.Command, e.Args, e.Env, e.Cwd
	s.URL, s.Headers, s.OAuth, s.Auth = e.URL, e.Headers, e.OAuth, e.Auth
	s.Disabled, s.ReadOnly, s.AutoApprove, s.Deny, s.Timeout = e.Disabled, e.ReadOnly, e.AutoApprove, e.Deny, e.Timeout
	t := strings.ToLower(strings.TrimSpace(e.Type))
	if t == "" {
		t = strings.ToLower(strings.TrimSpace(e.Transport))
	}
	switch t {
	case "streamable-http", "streamablehttp", "streamable_http":
		t = TypeHTTP
	case "":
		if e.URL != "" {
			t = TypeHTTP
		} else {
			t = TypeStdio
		}
	}
	s.Type = t
	switch {
	case !nameRe.MatchString(name):
		s.Problem = "name may only use letters, digits, - and _"
	case e.Disabled:
	case e.Command != "" && e.URL != "":
		s.Problem = "invalid: url and command both set"
	case e.Command == "" && e.URL == "":
		s.Problem = "invalid: needs command or url"
	case t == TypeSSE:
		s.Problem = "sse transport is not supported yet; use the server's /mcp URL"
	case t != TypeStdio && t != TypeHTTP:
		s.Problem = "invalid: unknown type " + t
	case t == TypeStdio && e.Command == "":
		s.Problem = "invalid: stdio needs a command"
	case t == TypeHTTP && e.URL == "":
		s.Problem = "invalid: http needs a url"
	case e.Auth != nil && e.Auth.GH != "" && t != TypeHTTP:
		s.Problem = "invalid: auth.gh needs an http server"
	case e.Auth != nil && e.Auth.GH != "" && !ValidGHLogin(e.Auth.GH):
		s.Problem = "invalid: auth.gh must be a GitHub login"
	case t == TypeHTTP && !strings.Contains(e.URL, "${"):
		if err := checkURL(e.URL); err != nil {
			s.Problem = err.Error()
		}
	}
	return s
}

// Load reads the user and the project file and merges them: a project
// entry replaces the user entry of the same name, and a disabled project
// entry hides it. Missing files are fine; a broken one is a warning.
func Load(userPath, projectPath string) ([]Server, []string) {
	by := map[string]Server{}
	var warns []string
	for _, f := range []struct{ path, source string }{{userPath, SourceUser}, {projectPath, SourceProject}} {
		if f.path == "" {
			continue
		}
		b, err := os.ReadFile(f.path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				warns = append(warns, err.Error())
			}
			continue
		}
		list, w, err := ParseFile(b, f.source, f.path)
		if err != nil {
			warns = append(warns, err.Error())
			continue
		}
		warns = append(warns, w...)
		for _, s := range list {
			by[s.Name] = s
		}
	}
	out := make([]Server, 0, len(by))
	for _, s := range by {
		if !s.Disabled {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, warns
}

// UserPath is the user mcp.json (next to config.json).
func UserPath(configDir string) string { return filepath.Join(configDir, "mcp.json") }

// ProjectPath is the project mcp.json of a workspace root.
func ProjectPath(root string) string { return filepath.Join(root, ".termocode", "mcp.json") }

// Hash identifies an entry's exact content (key order does not matter).
// Trust is stored per hash, so any change to the entry asks again.
func (s Server) Hash() string {
	var v any
	_ = json.Unmarshal(s.raw, &v)
	b, _ := json.Marshal(v) // maps marshal with sorted keys
	sum := sha256.Sum256(append([]byte(s.Name+"\x00"), b...))
	return hex.EncodeToString(sum[:])
}

// SecretNames lists the NAMEs of the entry's ${secret:NAME} references,
// sorted, once each.
func (s Server) SecretNames() []string {
	vals := append([]string{s.Command, s.Cwd, s.URL}, s.Args...)
	for _, m := range []map[string]string{s.Env, s.Headers} {
		for _, v := range m {
			vals = append(vals, v)
		}
	}
	if s.OAuth != nil {
		vals = append(vals, s.OAuth.ClientID, s.OAuth.ClientSecret)
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range vals {
		for _, m := range refRe.FindAllStringSubmatch(v, -1) {
			if name, ok := strings.CutPrefix(m[1], "secret:"); ok && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Raw is the entry as written in the file.
func (s Server) Raw() json.RawMessage { return s.raw }

// ── Expansion ──────────────────────────────────────────────────────────

// Expander resolves ${VAR}, ${VAR:-default}, ${secret:NAME},
// ${workspaceFolder} and ${userHome}.
type Expander struct {
	Env          func(string) (string, bool) // os.LookupEnv in production
	Secrets      map[string]string           // compatibility for callers with an in-memory map
	SecretLookup func(string) (string, bool) // store lookup; resolved lazily
	Workspace    string
	Home         string
}

// NeedsError: a reference has no value (the server is not started).
type NeedsError struct{ What string }

func (e *NeedsError) Error() string { return "needs " + e.What }

var refRe = regexp.MustCompile(`\$\{([^}]*)\}`)

func (x Expander) expand(s string) (string, error) {
	var ferr error
	out := refRe.ReplaceAllStringFunc(s, func(m string) string {
		ref := m[2 : len(m)-1]
		switch {
		case ref == "workspaceFolder":
			return x.Workspace
		case ref == "userHome":
			return x.Home
		case strings.HasPrefix(ref, "secret:"):
			name := strings.TrimPrefix(ref, "secret:")
			if x.SecretLookup != nil {
				if v, ok := x.SecretLookup(name); ok && v != "" {
					return v
				}
			}
			if v, ok := x.Secrets[name]; ok && v != "" {
				return v
			}
			if ferr == nil {
				ferr = &NeedsError{"secret " + name}
			}
			return ""
		}
		name, def, hasDef := strings.Cut(ref, ":-")
		if x.Env != nil {
			if v, ok := x.Env(name); ok && v != "" {
				return v
			}
		}
		if hasDef {
			return def
		}
		if ferr == nil {
			ferr = &NeedsError{"${" + name + "}"}
		}
		return ""
	})
	return out, ferr
}

// Expand returns a copy with every reference resolved, or the first
// NeedsError.
func (s Server) Expand(x Expander) (Server, error) {
	var err error
	one := func(v string) string {
		r, e := x.expand(v)
		if e != nil && err == nil {
			err = e
		}
		return r
	}
	out := s
	out.Command, out.Cwd, out.URL = one(s.Command), one(s.Cwd), one(s.URL)
	out.Args = make([]string, len(s.Args))
	for i, a := range s.Args {
		out.Args[i] = one(a)
	}
	out.Env = expandMap(s.Env, one)
	out.Headers = expandMap(s.Headers, one)
	if s.OAuth != nil {
		o := *s.OAuth
		o.ClientID, o.ClientSecret = one(o.ClientID), one(o.ClientSecret)
		out.OAuth = &o
	}
	return out, err
}

func expandMap(m map[string]string, one func(string) string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // the first NeedsError is stable
	for _, k := range keys {
		out[k] = one(m[k])
	}
	return out
}

// Secret values that should never be literal in mcp.json.
var secretLiteralRe = regexp.MustCompile(`(?i)(ghp_|github_pat_|glpat-|xox[pbas]-|sk-|lin_api_|bearer\s+[A-Za-z0-9._~+/=-]{20,})`)

// secretLiteral names the first header / env key whose literal value
// looks like a token ("" when none).
func secretLiteral(s Server) string {
	if s.OAuth != nil && strings.TrimSpace(refRe.ReplaceAllString(s.OAuth.ClientSecret, "")) != "" {
		return "oauth.client_secret"
	}
	for _, m := range []struct {
		kind string
		vals map[string]string
	}{{"headers", s.Headers}, {"env", s.Env}} {
		keys := make([]string, 0, len(m.vals))
		for k := range m.vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := refRe.ReplaceAllString(m.vals[k], "")
			if secretLiteralRe.MatchString(v) {
				return m.kind + "." + k
			}
		}
	}
	return ""
}
