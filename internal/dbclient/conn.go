// Package dbclient contains database configuration and result helpers. It does
// not open connections or start processes.
package dbclient

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/go-sql-driver/mysql"
)

type Conn struct {
	ID          string            `json:"id"`
	Name        string            `json:"name,omitempty"`
	Driver      string            `json:"driver"`
	Host        string            `json:"host,omitempty"`
	Port        int               `json:"port,omitempty"`
	User        string            `json:"user,omitempty"`
	Database    string            `json:"database,omitempty"`
	Path        string            `json:"path,omitempty"`
	DB          int               `json:"db,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
	Password    string            `json:"password,omitempty"`
	URL         string            `json:"url,omitempty"`
	ReadOnly    bool              `json:"readOnly,omitempty"`
	Environment string            `json:"environment,omitempty"`
	SSH         *SSHConfig        `json:"ssh,omitempty"`
	SSL         *SSLConfig        `json:"ssl,omitempty"`
	TLS         bool              `json:"tls,omitempty"`
	Source      string            `json:"-"`
	Problem     string            `json:"-"`
	Warning     string            `json:"-"`
}

type SSHConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port,omitempty"`
	User       string `json:"user,omitempty"`
	Auth       string `json:"auth,omitempty"`
	KeyFile    string `json:"keyFile,omitempty"`
	KnownHosts string `json:"knownHosts,omitempty"`
}

type SSLConfig struct {
	Mode string `json:"mode,omitempty"`
	CA   string `json:"ca,omitempty"`
	Cert string `json:"cert,omitempty"`
	Key  string `json:"key,omitempty"`
}

func (c Conn) DSN(password string) string {
	if normDriver(c.Driver) == "mysql" {
		if c.URL != "" {
			if parsed, err := ParseURL(c.URL); err == nil {
				c.Host, c.Port, c.User, c.Database = parsed.Host, parsed.Port, parsed.User, parsed.Database
				if len(c.Params) == 0 {
					c.Params = parsed.Params
				}
			}
		}
		cfg := mysql.NewConfig()
		cfg.User = c.User
		cfg.Passwd = password
		cfg.Net = "tcp"
		cfg.Addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
		cfg.DBName = c.Database
		cfg.Params = c.Params
		return cfg.FormatDSN()
	}
	if c.URL != "" {
		u, e := url.Parse(c.URL)
		if e == nil {
			user := c.User
			if u.User != nil && user == "" {
				user = u.User.Username()
			}
			if user != "" {
				u.User = url.User(user)
			} else {
				u.User = nil
			}
			if password != "" {
				u.User = url.UserPassword(user, password)
			}
			return u.String()
		}
	}
	switch c.Driver {
	case "sqlite":
		return c.Path
	case "redis":
		u := url.URL{Scheme: "redis", Host: netHost(c.Host, c.Port), Path: "/" + strconv.Itoa(c.DB)}
		if password != "" {
			u.User = url.UserPassword(c.User, password)
		}
		return u.String()
	case "postgres":
		u := url.URL{Scheme: "postgres", Host: netHost(c.Host, c.Port), Path: "/" + c.Database}
		if c.User != "" {
			if password != "" {
				u.User = url.UserPassword(c.User, password)
			} else {
				u.User = url.User(c.User)
			}
		}
		return u.String()
	default:
		u := url.URL{Scheme: map[string]string{"postgres": "postgres", "mysql": "mysql"}[c.Driver], Host: netHost(c.Host, c.Port), Path: "/" + c.Database}
		if c.User != "" {
			if password != "" {
				u.User = url.UserPassword(c.User, password)
			} else {
				u.User = url.User(c.User)
			}
		}
		return u.String()
	}
}
func netHost(h string, p int) string {
	if p > 0 {
		return fmt.Sprintf("%s:%d", h, p)
	}
	return h
}
func defaultPort(d string) int {
	switch d {
	case "postgres":
		return 5432
	case "mysql":
		return 3306
	case "redis":
		return 6379
	}
	return 0
}
func normDriver(d string) string {
	if d == "mariadb" {
		return "mysql"
	}
	return d
}
