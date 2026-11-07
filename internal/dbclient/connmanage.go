package dbclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// More connection management (DBX-50): the edit form, templates, named
// SSH profiles and the server version statement.

// ConnEdit is what the edit form changes. Host, Port, User and Database
// are left alone on SQLite.
type ConnEdit struct {
	Name, Comment, Host, User, Database string
	Port                                int
}

// oneLine turns line breaks and other control characters into spaces.
func oneLine(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s))
}

// EditConn applies e to connection id; the result must pass Validate.
func EditConn(cs []Conn, id string, e ConnEdit) ([]Conn, error) {
	i := connIndex(cs, id)
	if i < 0 {
		return nil, fmt.Errorf("unknown connection %q", id)
	}
	out := cloneConns(cs)
	c := &out[i]
	c.Name, c.Comment = oneLine(e.Name), oneLine(e.Comment)
	if normDriver(c.Driver) != "sqlite" {
		c.Host, c.Port, c.User, c.Database = oneLine(e.Host), e.Port, oneLine(e.User), oneLine(e.Database)
	}
	if err := Validate(*c); err != nil {
		return nil, err
	}
	return out, nil
}

// BuiltinTemplates are the templates every user has.
var BuiltinTemplates = []Conn{
	{ID: "local-postgres", Name: "Local PostgreSQL", Driver: "postgres", Host: "localhost", Port: 5432, User: "postgres"},
	{ID: "local-mysql", Name: "Local MySQL", Driver: "mysql", Host: "localhost", Port: 3306, User: "root"},
	{ID: "local-redis", Name: "Local Redis", Driver: "redis", Host: "localhost", Port: 6379},
	{ID: "sqlite-file", Name: "SQLite file", Driver: "sqlite", Path: "data.db"},
	{ID: "postgres-ssh", Name: "PostgreSQL through SSH", Driver: "postgres", Host: "db.internal", Port: 5432, User: "postgres",
		SSH: &SSHConfig{Host: "bastion.example.com", Port: 22, User: "me"}},
	{ID: "postgres-kube", Name: "PostgreSQL in Kubernetes", Driver: "postgres", User: "postgres",
		Kube: &KubeConfig{Pod: "postgres-0", Port: 5432}},
}

// templateID is a template id made from its name.
func templateID(name string) string {
	b := []rune(strings.ToLower(strings.TrimSpace(name)))
	for i, r := range b {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			b[i] = '-'
		}
	}
	id := strings.Trim(string(b), "-")
	for strings.Contains(id, "--") {
		id = strings.ReplaceAll(id, "--", "-")
	}
	if id == "" {
		id = "template"
	}
	return id
}

// TemplateOf is a template made from c, named name: no literal password,
// nothing shared with c.
func TemplateOf(c Conn, name string) Conn {
	t := dropLiteralSecrets(cloneConn(c))
	t.ID, t.Name = templateID(name), oneLine(name)
	t.Source, t.Problem, t.Warning, t.Dial = "", "", "", nil
	return t
}

// FromTemplate is a new user connection from t with id (a free one when
// id is taken).
func FromTemplate(cs []Conn, t Conn, id string) (Conn, error) {
	c := cloneConn(t)
	c.ID = id
	if connIndex(cs, c.ID) >= 0 {
		c.ID = freeID(cs, c.ID)
	}
	c.Driver = normDriver(c.Driver)
	if c.Port == 0 && c.Driver != "sqlite" && c.Kube == nil {
		c.Port = defaultPort(c.Driver)
	}
	c.Source, c.Problem, c.Warning, c.Dial = SourceUser, "", "", nil
	if err := Validate(c); err != nil {
		return Conn{}, err
	}
	return c, nil
}

// ParseTemplates reads the user's templates file ({"templates": […]}).
func ParseTemplates(data []byte) ([]Conn, error) {
	var f struct {
		Templates []Conn `json:"templates"`
	}
	if err := json.Unmarshal(jsonc.Strip(data), &f); err != nil {
		return nil, fmt.Errorf("templates: %v", err)
	}
	for i := range f.Templates {
		t := &f.Templates[i]
		if !idPattern.MatchString(t.ID) {
			return nil, fmt.Errorf("template %q: invalid id", t.ID)
		}
		*t = dropLiteralSecrets(*t)
	}
	return f.Templates, nil
}

// TemplatesJSON is the templates file.
func TemplatesJSON(ts []Conn) ([]byte, error) {
	clean := make([]Conn, 0, len(ts))
	for _, t := range ts {
		clean = append(clean, dropLiteralSecrets(cloneConn(t)))
	}
	b, err := json.MarshalIndent(struct {
		Templates []Conn `json:"templates"`
	}{clean}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// SSHProfile is a named SSH setting many connections can use.
type SSHProfile struct {
	Name string    `json:"name"`
	SSH  SSHConfig `json:"ssh"`
}

func cloneSSH(s SSHConfig) SSHConfig {
	s.Jump = append([]string(nil), s.Jump...)
	return s
}

// SaveSSHProfile adds p, or replaces the profile with p's name.
func SaveSSHProfile(ps []SSHProfile, p SSHProfile) []SSHProfile {
	out := make([]SSHProfile, 0, len(ps)+1)
	replaced := false
	for _, x := range ps {
		if x.Name == p.Name {
			x, replaced = SSHProfile{Name: p.Name, SSH: cloneSSH(p.SSH)}, true
		} else {
			x.SSH = cloneSSH(x.SSH)
		}
		out = append(out, x)
	}
	if !replaced {
		out = append(out, SSHProfile{Name: p.Name, SSH: cloneSSH(p.SSH)})
	}
	return out
}

// ParseSSHProfiles reads the profiles file ({"profiles": […]}).
func ParseSSHProfiles(data []byte) ([]SSHProfile, error) {
	var f struct {
		Profiles []SSHProfile `json:"profiles"`
	}
	if err := json.Unmarshal(jsonc.Strip(data), &f); err != nil {
		return nil, fmt.Errorf("SSH profiles: %v", err)
	}
	for _, p := range f.Profiles {
		if oneLine(p.Name) == "" || p.Name != oneLine(p.Name) {
			return nil, errors.New("SSH profiles: every profile needs a one-line name")
		}
		if p.SSH.Host == "" {
			return nil, fmt.Errorf("SSH profile %q has no host", p.Name)
		}
	}
	return f.Profiles, nil
}

// SSHProfilesJSON is the profiles file.
func SSHProfilesJSON(ps []SSHProfile) ([]byte, error) {
	b, err := json.MarshalIndent(struct {
		Profiles []SSHProfile `json:"profiles"`
	}{ps}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// UseSSHProfile gives connection id a copy of p's SSH settings.
func UseSSHProfile(cs []Conn, id string, p SSHProfile) ([]Conn, error) {
	i := connIndex(cs, id)
	if i < 0 {
		return nil, fmt.Errorf("unknown connection %q", id)
	}
	switch {
	case normDriver(cs[i].Driver) == "sqlite":
		return nil, errors.New("a SQLite file needs no SSH tunnel")
	case cs[i].Kube != nil:
		return nil, errors.New("use ssh or kube, not both")
	}
	out := cloneConns(cs)
	s := cloneSSH(p.SSH)
	out[i].SSH = &s
	return out, nil
}

// ServerVersionQuery reads the server version on driver ("" none: Redis
// answers INFO instead).
func ServerVersionQuery(driver string) string {
	switch normDriver(driver) {
	case "postgres", "mysql", "clickhouse":
		return "SELECT version()"
	case "sqlite", "libsql":
		return "SELECT sqlite_version()"
	case "cassandra":
		return "SELECT release_version FROM system.local"
	case "sqlserver":
		return "SELECT @@VERSION"
	case "firebird":
		return "SELECT rdb$get_context('SYSTEM', 'ENGINE_VERSION') FROM rdb$database"
	case "vertica":
		return "SELECT version()"
	case "exasol":
		return "SELECT PARAM_VALUE FROM SYS.EXA_METADATA WHERE PARAM_NAME = 'databaseProductVersion'"
	case "hana":
		return "SELECT VERSION FROM SYS.M_DATABASE"
	}
	return ""
}
