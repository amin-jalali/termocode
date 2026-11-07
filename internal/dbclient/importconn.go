package dbclient

import (
	"bufio"
	"bytes"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// ImportedConn is one connection found in another tool's file. Conn never
// holds the password: Secret has the plain password from the file ("" when
// there is none), and the caller puts it in the secret store (internal/
// secrets) and sets Conn.Password to its ${secret:...} reference.
type ImportedConn struct {
	Conn   Conn
	Secret string
	From   string
}

// String and GoString never print the secret.
func (i ImportedConn) String() string {
	has := "no password"
	if i.Secret != "" {
		has = "password set"
	}
	return fmt.Sprintf("%s %s %s@%s:%d/%s (%s, from %s)", i.Conn.ID, i.Conn.Driver, i.Conn.User, i.Conn.Host, i.Conn.Port, i.Conn.Database, has, i.From)
}

func (i ImportedConn) GoString() string { return i.String() }

// Format makes every verb (%v, %+v, %#v, %s) use String.
func (i ImportedConn) Format(f fmt.State, _ rune) { fmt.Fprint(f, i.String()) }

// ImportConnections reads a file of another tool and returns its
// connections with unique, valid ids that are not in taken. The format
// comes from the file name (pgpass, my.cnf / .ini) or else from the
// content.
func ImportConnections(name string, data []byte, taken []string) []ImportedConn {
	var out []ImportedConn
	base := strings.ToLower(filepath.Base(name))
	switch {
	case strings.Contains(base, "pgpass"):
		out = ParsePgpass(data)
	case strings.HasSuffix(base, ".cnf") || strings.HasSuffix(base, ".ini") || strings.Contains(base, "my.cnf"):
		out = ParseMyCnf(data)
	default:
		if out = ParseURLList(data); len(out) == 0 {
			if out = ParseMyCnf(data); len(out) == 0 {
				out = ParsePgpass(data)
			}
		}
	}
	used := map[string]bool{}
	for _, id := range taken {
		used[id] = true
	}
	for i := range out {
		out[i].Conn.ID = importID(out[i].Conn, used)
		used[out[i].Conn.ID] = true
	}
	return out
}

func importID(c Conn, used map[string]bool) string {
	prefix := map[string]string{"postgres": "pg", "mysql": "mysql", "redis": "redis", "sqlite": "sqlite"}[c.Driver]
	if prefix == "" {
		prefix = "db"
	}
	parts := []string{prefix}
	for _, p := range []string{c.Host, c.Database} {
		if s := slug(p); s != "" {
			parts = append(parts, s)
		}
	}
	base := strings.Join(parts, "-")
	if len(base) > 48 {
		base = strings.TrimRight(base[:48], "-")
	}
	id := base
	for n := 2; used[id]; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	return id
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
		if ok {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// ParsePgpass reads a PostgreSQL password file
// (host:port:database:user:password, with \: and \\ escapes). Lines with
// a wildcard host cannot be connected to and are skipped; a wildcard port
// is 5432, a wildcard database or user is empty.
func ParsePgpass(data []byte) []ImportedConn {
	var out []ImportedConn
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.TrimSpace(line) == "" {
			continue
		}
		f := splitPgpass(line)
		if len(f) != 5 || f[0] == "" || f[0] == "*" {
			continue
		}
		c := Conn{Driver: "postgres", Host: f[0], Port: 5432, Environment: "development"}
		if f[1] != "*" && f[1] != "" {
			p, err := strconv.Atoi(f[1])
			if err != nil || p < 1 || p > 65535 {
				continue
			}
			c.Port = p
		}
		if f[2] != "*" {
			c.Database = f[2]
		}
		if f[3] != "*" {
			c.User = f[3]
		}
		out = append(out, ImportedConn{Conn: c, Secret: f[4], From: "pgpass"})
	}
	return out
}

func splitPgpass(line string) []string {
	var fields []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch ch := line[i]; {
		case ch == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
		case ch == ':' && len(fields) < 4:
			fields = append(fields, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	return append(fields, cur.String())
}

// ParseMyCnf reads a MySQL option file. [client] and [mysql] together make
// one connection; each [client<suffix>] or [mysql<suffix>] group makes one
// more, on top of [client]. Server groups and !include lines are ignored.
func ParseMyCnf(data []byte) []ImportedConn {
	groups := map[string]map[string]string{}
	var order []string
	cur := ""
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' || line[0] == '!' {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if _, ok := groups[cur]; !ok {
				groups[cur] = map[string]string{}
				order = append(order, cur)
			}
			continue
		}
		if cur == "" {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		k = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(k)), "-", "_")
		groups[cur][k] = unquoteCnf(strings.TrimSpace(v))
	}
	merge := func(names ...string) map[string]string {
		m := map[string]string{}
		for _, n := range names {
			for k, v := range groups[n] {
				m[k] = v
			}
		}
		return m
	}
	var out []ImportedConn
	add := func(m map[string]string) {
		if m["host"] == "" && m["user"] == "" && m["password"] == "" && m["database"] == "" {
			return
		}
		c := Conn{Driver: "mysql", Host: m["host"], Port: 3306, User: m["user"], Database: m["database"], Environment: "development"}
		if c.Host == "" {
			c.Host = "localhost"
		}
		if p, err := strconv.Atoi(m["port"]); err == nil && p > 0 && p < 65536 {
			c.Port = p
		}
		out = append(out, ImportedConn{Conn: c, Secret: m["password"], From: "my.cnf"})
	}
	add(merge("client", "mysql"))
	for _, g := range order {
		if g == "client" || g == "mysql" {
			continue
		}
		if strings.HasPrefix(g, "client") || (strings.HasPrefix(g, "mysql") && !strings.HasPrefix(g, "mysqld") && g != "mysqldump" && g != "mysqladmin") {
			add(merge("client", g))
		}
	}
	return out
}

func unquoteCnf(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

// ParseURLList reads a file with one database URL per line (postgres://,
// mysql://, redis://). The password is moved from the URL to Secret.
func ParseURLList(data []byte) []ImportedConn {
	var out []ImportedConn
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || !strings.Contains(line, "://") {
			continue
		}
		c, err := ParseURL(line)
		if err != nil || c.Driver == "sqlite" || c.Host == "" {
			continue
		}
		secret := c.Password
		c.Password = ""
		if u, err := url.Parse(c.URL); err == nil && u.User != nil {
			if _, has := u.User.Password(); has {
				u.User = url.User(u.User.Username())
				c.URL = u.String()
			}
		}
		if c.Environment == "" {
			c.Environment = "development"
		}
		out = append(out, ImportedConn{Conn: c, Secret: secret, From: "url list"})
	}
	return out
}
