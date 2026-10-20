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
	if e != nil {
		return Conn{}, e
	}
	c := Conn{URL: raw, Driver: normDriver(u.Scheme), Params: map[string]string{}}
	switch u.Scheme {
	case "postgres", "postgresql":
		c.Driver = "postgres"
	case "mysql", "mariadb":
	case "redis", "rediss":
		c.Driver = "redis"
	case "sqlite", "file":
		c.Driver = "sqlite"
	default:
		return Conn{}, fmt.Errorf("unsupported database URL scheme %q", u.Scheme)
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
