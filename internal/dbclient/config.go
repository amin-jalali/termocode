package dbclient

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/secrets"
)

const (
	SourceUser    = "user"
	SourceProject = "project"
)

var idPattern = lazyre.New(`^[a-z0-9_-]+$`)

// podPattern is a Kubernetes object name (DNS subdomain); kubeName is a
// context or namespace: never a flag, no spaces or control characters.
var (
	podPattern = lazyre.New(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
	kubeName   = lazyre.New(`^[A-Za-z0-9_.:@/][-A-Za-z0-9_.:@/]*$`)
)

// kubeProblem is what is wrong with c's kube block ("" when fine or none).
func kubeProblem(c Conn) string {
	k := c.Kube
	if k == nil {
		return ""
	}
	switch {
	case normDriver(c.Driver) == "sqlite":
		return "kube port-forward needs a server database"
	case c.SSH != nil:
		return "use ssh or kube, not both"
	case k.Pod == "":
		return "kube.pod is required"
	case len(k.Pod) > 253 || !podPattern.MatchString(k.Pod):
		return "kube.pod is not a valid pod name"
	case k.Namespace != "" && !kubeName.MatchString(k.Namespace):
		return "kube.namespace is not valid"
	case k.Context != "" && !kubeName.MatchString(k.Context):
		return "kube.context is not valid"
	case k.Port < 0 || k.Port > 65535:
		return "kube.port is not a valid port"
	}
	return ""
}

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
		if p, ok := pgPresetForScheme(c.Driver); ok { // "driver": "cockroachdb"
			c.Driver = "postgres"
			if c.Preset == "" {
				c.Preset = p.ID
			}
		}
		c.Driver = normDriver(c.Driver)
		normFolderColor(c)
		if c.Preset != "" {
			c.Preset = strings.ToLower(c.Preset)
			if mp, ok := PresetByID(c.Preset); ok { // MySQL family (DBX-61)
				if c.Driver != mp.Driver {
					c.Problem = "preset needs the " + mp.Driver + " driver"
				} else if c.Port == 0 {
					c.Port = mp.Port
				}
			} else if p, ok := PGPresetByID(c.Preset); !ok {
				c.Problem = "unknown PostgreSQL preset"
			} else if c.Driver != "postgres" {
				c.Problem = "preset needs the postgres driver"
			} else if c.Port == 0 {
				c.Port = p.Port
			}
		}
		if c.Port == 0 {
			c.Port = defaultPort(c.Driver)
		}
		if c.Environment == "" {
			c.Environment = "development"
		}
		if source == SourceProject && literalPassword(c.Password) {
			c.Problem = "literal password in project config"
		}
		if source == SourceProject && (urlHasPassword(c.URL) || urlHasToken(c.URL)) {
			c.Problem = "literal password in project config"
		}
		if source == SourceProject && (secrets.HasCmdRef(c.Password) || secrets.HasCmdRef(c.URL)) {
			c.Problem = "invalid: " + secrets.ProjectCmdWarning
		}
		if source == SourceUser && (literalPassword(c.Password) || urlHasPassword(c.URL)) {
			c.Warning = "literal password in user config; use the secret store"
		}
		if c.Driver != "postgres" && c.Driver != "mysql" && c.Driver != "sqlite" && c.Driver != "redis" && c.Driver != "clickhouse" && c.Driver != "cassandra" && c.Driver != "libsql" && c.Driver != "mongodb" {
			c.Problem = "unsupported database driver"
		}
		if c.Options != nil && c.Problem == "" {
			if e := c.Options.Check(c.Driver); e != nil {
				c.Problem = "invalid options: " + e.Error()
			}
		}
		if c.Kube != nil {
			if c.Kube.Port == 0 {
				c.Kube.Port = defaultPort(c.Driver)
			}
			if p := kubeProblem(*c); p != "" {
				c.Problem = p
			}
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
	return `{
  // Database connections. Keep passwords in the secret store (p on a row).
  //
  // An SSH tunnel and SSL go on a connection, for example:
  //   "ssh": { "host": "bastion.example.com", "user": "deploy", "auth": "agent" },
  //   "ssl": { "mode": "verify-full", "ca": "~/.certs/db-ca.pem" }
  //
  // ssh.host may be a ~/.ssh/config alias (HostName, Port, User,
  // IdentityFile and ProxyJump are read from it; ProxyCommand is not
  // supported). "jump": ["user@gw.example.com:22"] lists jump hosts.
  // ssh.auth: "agent" | "key" (with "keyFile") | "password". The key
  // passphrase and the SSH password stay in the secret store.
  // ssl.mode: "disable" | "require" | "verify-ca" | "verify-full" (the
  // default); "cert" and "key" add a client certificate.
  //
  // A server in Kubernetes is reached through kubectl port-forward:
  //   "kube": { "context": "dev", "namespace": "shop", "pod": "pg-0", "port": 5432 }
  // (port is the pod's port; the driver's default when left out).
  "connections": []
}
`
}
func Validate(c Conn) error {
	if c.ID == "" || !idPattern.MatchString(c.ID) {
		return fmt.Errorf("invalid connection id")
	}
	driver := normDriver(c.Driver)
	if driver != "postgres" && driver != "mysql" && driver != "sqlite" && driver != "redis" && driver != "clickhouse" && driver != "cassandra" && driver != "libsql" && driver != "mongodb" {
		return fmt.Errorf("unsupported database driver")
	}
	if driver == "mongodb" {
		// A MongoDB server: a host (port 27017 when left out) or a
		// mongodb:// / mongodb+srv:// URL.
		if c.Host == "" && c.URL == "" {
			return fmt.Errorf("mongodb host or URL is required")
		}
		if c.Port < 0 || c.Port > 65535 {
			return fmt.Errorf("invalid port")
		}
		if c.Source == SourceProject && (literalPassword(c.Password) || urlHasPassword(c.URL)) {
			return fmt.Errorf("password must be a reference")
		}
		if p := kubeProblem(c); p != "" {
			return fmt.Errorf("%s", p)
		}
		return nil
	}
	if driver == "libsql" {
		// A remote libSQL server: a host (or URL) is enough, the port is
		// the HTTPS default unless the URL names one.
		if c.Host == "" && c.URL == "" {
			return fmt.Errorf("libsql server URL is required")
		}
		if c.Source == SourceProject && (literalPassword(c.Password) || urlHasPassword(c.URL) || urlHasToken(c.URL)) {
			return fmt.Errorf("password must be a reference")
		}
		return nil
	}
	if driver == "sqlite" && c.Path == "" {
		return fmt.Errorf("sqlite path is required")
	}
	if c.Options != nil {
		if e := c.Options.Check(driver); e != nil {
			return fmt.Errorf("invalid options: %v", e)
		}
	}
	if p := kubeProblem(c); p != "" {
		return fmt.Errorf("%s", p)
	}
	socket := c.Options != nil && c.Options.Socket != ""
	if c.Driver != "sqlite" && !socket && c.Kube == nil && (c.Host == "" || c.Port < 1 || c.Port > 65535) {
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

// urlHasToken reports a literal ?authToken= (libsql URLs).
func urlHasToken(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	tok := u.Query().Get("authToken")
	return tok != "" && literalPassword(tok)
}

func urlHasPassword(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return false
	}
	p, ok := u.User.Password()
	return ok && p != ""
}
