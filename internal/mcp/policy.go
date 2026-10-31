package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Policy files (admin allow / deny list): a machine-wide file an admin
// owns and the user's own mcp-policy.json next to mcp.json. Both apply.
// A denied entry stays in the list with Problem "blocked by policy
// (<file>)" and is never started.
//
//	{"deny":  [{"command": "curl"}, {"url": "http://*"}],
//	 "allow": [{"name": "fs"}, {"command": "npx"}, {"url": "https://mcp.example.com/*"}]}
//
// A rule's set fields are globs (path.Match) that must all match: name
// against the entry name, command against the command alone or the
// command line, url against the url. Deny wins; when a file has an
// allow list, an entry must match one of its rules. A file that can not
// be read blocks every server (fail closed).

// SystemPolicyPath is the machine-wide policy file (a var for tests).
var SystemPolicyPath = defaultSystemPolicyPath()

func defaultSystemPolicyPath() string {
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("ProgramData"); d != "" {
			return filepath.Join(d, "termocode", "mcp-policy.json")
		}
		return ""
	case "darwin":
		return "/Library/Application Support/termocode/mcp-policy.json"
	}
	return "/etc/termocode/mcp-policy.json"
}

// UserPolicyPath is the user's policy file next to mcp.json.
func UserPolicyPath(userPath string) string {
	if userPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(userPath), "mcp-policy.json")
}

// PolicyRule matches entries; empty fields match anything.
type PolicyRule struct {
	Name    string `json:"name,omitempty"`
	Command string `json:"command,omitempty"`
	URL     string `json:"url,omitempty"`
}

// Policy is one policy file.
type Policy struct {
	Allow []PolicyRule `json:"allow,omitempty"`
	Deny  []PolicyRule `json:"deny,omitempty"`

	path   string
	broken bool
}

func (r PolicyRule) matches(s Server) bool {
	if r.Name == "" && r.Command == "" && r.URL == "" {
		return false // an empty rule matches nothing, never everything
	}
	if r.Name != "" && !globMatch(r.Name, s.Name) {
		return false
	}
	if r.Command != "" {
		line := strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
		if s.Command == "" || !(globMatch(r.Command, s.Command) || globMatch(r.Command, filepath.Base(s.Command)) || globMatch(r.Command, line)) {
			return false
		}
	}
	if r.URL != "" && (s.URL == "" || !globMatch(r.URL, s.URL)) {
		return false
	}
	return true
}

// globMatch is path.Match where "*" also crosses "/" (a url or a
// command line is not a path).
func globMatch(pattern, s string) bool {
	if ok, err := path.Match(pattern, s); err == nil && ok {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return false
	}
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for i, p := range parts[1:] {
		if i == len(parts)-2 {
			return strings.HasSuffix(s, p)
		}
		j := strings.Index(s, p)
		if j < 0 {
			return false
		}
		s = s[j+len(p):]
	}
	return true
}

// LoadPolicies reads the machine and the user policy file. Missing
// files are fine; a broken one is a warning and blocks every server.
func LoadPolicies(userPath string) ([]Policy, []string) {
	var out []Policy
	var warns []string
	for _, p := range []string{SystemPolicyPath, UserPolicyPath(userPath)} {
		if p == "" {
			continue
		}
		b, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		pol := Policy{path: p}
		if err == nil {
			err = json.Unmarshal(b, &pol)
		}
		if err != nil {
			pol = Policy{path: p, broken: true}
			warns = append(warns, fmt.Sprintf("%s: can not read the MCP policy, so every server is blocked: %v", p, err))
		}
		out = append(out, pol)
	}
	return out, warns
}

// Blocked is the policy problem of an entry ("" when it may run).
func Blocked(policies []Policy, s Server) string {
	for _, p := range policies {
		blocked := p.broken
		for _, r := range p.Deny {
			blocked = blocked || r.matches(s)
		}
		if !blocked && len(p.Allow) > 0 {
			blocked = true
			for _, r := range p.Allow {
				if r.matches(s) {
					blocked = false
					break
				}
			}
		}
		if blocked {
			return "blocked by policy (" + p.path + ")"
		}
	}
	return ""
}
