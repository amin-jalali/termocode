package dbclient

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

func ParseURL(raw string) (Conn, error) {
	mysqlAddr := ""
	if strings.HasPrefix(raw, "mysql://") {
		if i := strings.Index(raw, "@tcp("); i >= 0 {
			j := strings.Index(raw[i+5:], ")")
			if j >= 0 {
				mysqlAddr = raw[i+5 : i+5+j]
				raw = raw[:i] + "@" + mysqlAddr + raw[i+5+j+1:]
			}
		}
	}
	u, e := url.Parse(raw)
	var e2 error
	if e != nil {
		return Conn{}, e
	}
	c := Conn{URL: raw, Driver: normDriver(u.Scheme), Params: map[string]string{}}
	preset, isPreset := pgPresetForScheme(u.Scheme)
	switch {
	case u.Scheme == "postgres" || u.Scheme == "postgresql":
		c.Driver = "postgres"
		if id := DetectPGPreset(u.Hostname(), ""); id != "" {
			preset, isPreset = PGPresetByID(id)
		}
	case isPreset:
		c.Driver = "postgres"
	}
	if isPreset {
		c.Preset = preset.ID
	}
	switch u.Scheme {
	case "postgres", "postgresql":
	case "mysql", "mariadb":
	case "redis", "rediss":
		c.Driver = "redis"
	case "sqlite", "file":
		c.Driver = "sqlite"
	case "sqlcipher":
		// An encrypted SQLite file (DBX-64): the key is the connection
		// password, asked for and kept as a secret, never in the URL.
		c.Driver, c.Preset = "sqlite", SQLCipherPreset
		u.Scheme = "sqlite"
		raw = "sqlite" + strings.TrimPrefix(raw, "sqlcipher")
		c.URL = ""
	case "firebird", "firebirdsql", "vertica", "hdb", "hana", "saphana", "exasol", "exa", "db2", "ibmdb2":
		// DBX-71: c.Driver is the engine id (normDriver); the database is
		// the path (Firebird: a server path or alias; Exasol: the schema).
	case "libsql":
		// A remote libSQL server; the token moves from ?authToken= into
		// the password (kept as a secret, never in the saved URL).
		c.Driver = "libsql"
		q := u.Query()
		if tok := q.Get("authToken"); tok != "" {
			c.Password = tok
			q.Del("authToken")
			u.RawQuery = q.Encode()
			raw = u.String()
		}
		c.URL = raw
		c.Host = u.Hostname()
		if u.Port() != "" {
			c.Port, _ = strconv.Atoi(u.Port())
		}
		return c, nil
	case "mongodb", "mongodb+srv":
		// MongoDB (DBX-68). The URL is kept (hosts, replica set and
		// options stay in it) but without the password, which moves
		// into the secret store. A +srv address has no port.
		c.Driver = "mongodb"
		if u.User != nil {
			c.User = u.User.Username()
			c.Password, _ = u.User.Password()
			u.User = url.User(c.User)
			if c.User == "" {
				u.User = nil
			}
		}
		c.URL = u.String()
		c.Host = u.Hostname()
		if u.Scheme == "mongodb" {
			c.Port = 27017
			if p := u.Port(); p != "" {
				if c.Port, e = strconv.Atoi(p); e != nil {
					return Conn{}, fmt.Errorf("invalid port")
				}
			}
		}
		c.Database = strings.TrimPrefix(u.Path, "/")
		for k, v := range u.Query() {
			if len(v) > 0 {
				c.Params[k] = v[len(v)-1]
			}
		}
		return c, nil
	default:
		if e, tls, ok := engineForScheme(u.Scheme); ok {
			// An HTTP engine (DBX-68..71): the address, user and
			// password (kept as a secret) come out of the URL; the
			// session builds its own base from the fields.
			c = Conn{Driver: e.ID, Host: u.Hostname(), TLS: tls, Database: strings.Trim(u.Path, "/"), Params: map[string]string{}}
			if u.User != nil {
				c.User = u.User.Username()
				c.Password, _ = u.User.Password()
			}
			if u.Port() != "" {
				if c.Port, e2 = strconv.Atoi(u.Port()); e2 != nil {
					return Conn{}, fmt.Errorf("invalid port")
				}
			} else {
				c.Port = httpEngineDefaultPort(e.ID, tls)
			}
			if c.Database == "" {
				c.Database = e.Database
			}
			if e.FixURL != nil {
				e.FixURL(&c)
			}
			return c, nil
		}
		if !isPreset {
			return Conn{}, fmt.Errorf("unsupported database URL scheme %q", u.Scheme)
		}
	}
	if u.User != nil {
		c.User = u.User.Username()
		c.Password, _ = u.User.Password()
		if c.Password != "" {
			u.User = url.User(c.User)
			c.URL = u.String()
		}
	}
	c.Host = u.Hostname()
	if u.Port() != "" {
		c.Port, e = strconv.Atoi(u.Port())
		if e != nil {
			return Conn{}, fmt.Errorf("invalid port")
		}
	} else if isPreset {
		c.Port = preset.Port
	} else {
		c.Port = defaultPort(c.Driver)
	}
	if c.Driver == "sqlite" {
		p := u.Path
		if rest, ok := strings.CutPrefix(raw, "sqlite://"); ok {
			p = strings.TrimPrefix(strings.SplitN(rest, "?", 2)[0], "//")
		}
		if u.Scheme == "sqlite" && p == "" && u.Host != "" {
			p = u.Host
		} else if u.Scheme == "sqlite" && p == "" {
			p = u.Opaque
		}
		c.Path = filepath.Clean(p)
		return c, nil
	}
	c.Database = strings.TrimPrefix(u.Path, "/")
	if c.Driver == "firebird" && strings.Contains(c.Database, "/") {
		c.Database = u.Path // an absolute server path keeps its first slash
	}
	if c.Driver == "redis" && c.Database != "" {
		c.DB, e = strconv.Atoi(c.Database)
		if e != nil {
			return Conn{}, fmt.Errorf("invalid redis database index")
		}
	}
	if c.Driver == "mysql" && mysqlAddr != "" {
		h, p, err := net.SplitHostPort(mysqlAddr)
		if err == nil {
			c.Host = h
			c.Port, _ = strconv.Atoi(p)
		}
	}
	for k, v := range u.Query() {
		if len(v) > 0 {
			c.Params[k] = v[len(v)-1]
		}
	}
	return c, nil
}
