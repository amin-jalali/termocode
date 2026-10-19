package dbclient

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

const (
	SourceUser    = "user"
	SourceProject = "project"
)

var idPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

func ParseConfig(data []byte, source, file, root string) ([]Conn, error) {
	var f struct {
		Connections []Conn `json:"connections"`
	}
	if e := json.Unmarshal(jsonc.Strip(data), &f); e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	for i := range f.Connections {
		c := &f.Connections[i]
		c.Source = source
		if !idPattern.MatchString(c.ID) {
			c.Problem = "invalid connection id"
		} else if seen[c.ID] {
			c.Problem = "duplicate connection id"
		}
		seen[c.ID] = true
		c.Driver = normDriver(c.Driver)
		if c.Port == 0 {
			c.Port = defaultPort(c.Driver)
		}
		if c.Environment == "" {
			c.Environment = "development"
		}
		if source == SourceProject && literalPassword(c.Password) {
			c.Problem = "literal password in project config"
		}
		if source == SourceProject && urlHasPassword(c.URL) {
			c.Problem = "literal password in project config"
		}
		if source == SourceUser && (literalPassword(c.Password) || urlHasPassword(c.URL)) {
			c.Warning = "literal password in user config; use the secret store"
		}
		if c.Driver != "postgres" && c.Driver != "mysql" && c.Driver != "sqlite" && c.Driver != "redis" {
			c.Problem = "unsupported database driver"
		}
		if c.Driver == "sqlite" && c.Path != "" {
			p := c.Path
			if !filepath.IsAbs(p) {
				p = filepath.Join(root, p)
			}
			abs, _ := filepath.Abs(p)
			base, _ := filepath.Abs(root)
			rel, e := filepath.Rel(base, abs)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				c.Problem = "sqlite path is outside the workspace"
			}
		}
	}
	return f.Connections, nil
}
func MergeConfig(project, user []Conn) []Conn {
	m := map[string]Conn{}
	for _, c := range user {
		m[c.ID] = c
	}
	for _, c := range project {
		m[c.ID] = c
	}
	out := make([]Conn, 0, len(m))
	for _, c := range m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func ConfigTemplate() string {
	return "{\n  // Database connections. Keep passwords in the secret store.\n  \"connections\": []\n}\n"
}
func Validate(c Conn) error {
	if c.ID == "" || !idPattern.MatchString(c.ID) {
		return fmt.Errorf("invalid connection id")
	}
	driver := normDriver(c.Driver)
	if driver != "postgres" && driver != "mysql" && driver != "sqlite" && driver != "redis" {
		return fmt.Errorf("unsupported database driver")
	}
	if driver == "sqlite" && c.Path == "" {
		return fmt.Errorf("sqlite path is required")
	}
	if c.Driver != "sqlite" && (c.Host == "" || c.Port < 1 || c.Port > 65535) {
		return fmt.Errorf("host and valid port are required")
	}
	if c.Source == SourceProject && (literalPassword(c.Password) || urlHasPassword(c.URL)) {
		return fmt.Errorf("password must be a reference")
	}
	return nil
}
func literalPassword(p string) bool {
	if p == "" {
		return false
	}
	if !strings.HasPrefix(p, "${") || !strings.HasSuffix(p, "}") {
		return true
	}
	ref := p[2 : len(p)-1]
	for _, prefix := range []string{"env:", "dotenv:", "cmd:", "secret:"} {
		if strings.HasPrefix(ref, prefix) && strings.TrimSpace(strings.TrimPrefix(ref, prefix)) != "" {
			return false
		}
	}
	return true
}

func urlHasPassword(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return false
	}
	p, ok := u.User.Password()
	return ok && p != ""
}
