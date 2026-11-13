package dbclient

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestOpenSQLDBPerDriverWithoutConnecting(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) { return nil, net.ErrClosed }
	init := &ConnOptions{InitSQL: "SET x = 1"}
	for name, c := range map[string]Conn{
		"postgres":            {Driver: "postgres", Host: "h", User: "u", Database: "d"},
		"postgres start sql":  {Driver: "postgres", Host: "h", User: "u", Database: "d", Options: init},
		"postgres ssl":        {Driver: "postgres", Host: "h", User: "u", SSL: &SSLConfig{Mode: "require"}},
		"postgres tunnel":     {Driver: "postgres", Host: "h", User: "u", Dial: dial},
		"mysql":               {Driver: "mysql", Host: "h", User: "u", Database: "d"},
		"mysql url":           {Driver: "mysql", URL: "mysql://u@h:3307/d"},
		"mysql socket":        {Driver: "mysql", User: "u", Options: &ConnOptions{Socket: "/tmp/mysql.sock"}},
		"mysql tls true":      {Driver: "mysql", Host: "h", Params: map[string]string{"tls": "true", "charset": "utf8mb4"}},
		"mysql tls skip":      {Driver: "mysql", Host: "h", Params: map[string]string{"tls": "skip-verify"}},
		"mysql tls preferred": {Driver: "mysql", Host: "h", Params: map[string]string{"tls": "preferred"}},
		"mysql tls disable":   {Driver: "mysql", Host: "h", Params: map[string]string{"tls": "disable"}},
		"mysql ssl block":     {Driver: "mysql", Host: "h", SSL: &SSLConfig{Mode: "require"}},
		"mysql tunnel":        {Driver: "mysql", Host: "h", Dial: dial},
		"mysql start sql":     {Driver: "mysql", Host: "h", Options: init},
		"sqlserver":           {Driver: "sqlserver", Host: "h", User: "u"},
		"sqlserver start sql": {Driver: "sqlserver", Host: "h", User: "u", Options: init},
		"oracle":              {Driver: "oracle", Host: "h", User: "u", Database: "s"},
		"oracle start sql":    {Driver: "oracle", Host: "h", User: "u", Database: "s", Options: init},
	} {
		db, release, err := openSQLDB(c, "pw")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		release()
		_ = db.Close()
	}
	for name, c := range map[string]struct {
		conn Conn
		want string
	}{
		"mysql bad tls":    {Conn{Driver: "mysql", Host: "h", Params: map[string]string{"tls": "sometimes"}}, "unknown MySQL tls value"},
		"mysql bad url":    {Conn{Driver: "mysql", URL: "mysql://%zz"}, "invalid MySQL connection settings"},
		"mysql bad ca":     {Conn{Driver: "mysql", Host: "h", SSL: &SSLConfig{Mode: "verify-full", CA: "/no/such.pem"}}, ""},
		"postgres bad ca":  {Conn{Driver: "postgres", Host: "h", SSL: &SSLConfig{Mode: "verify-full", CA: "/no/such.pem"}}, ""},
		"postgres bad dsn": {Conn{Driver: "postgres", URL: "postgres://u@h:notaport/d"}, "invalid PostgreSQL"},
		"sqlserver bad":    {Conn{Driver: "sqlserver", URL: "sqlserver://h:notaport"}, ""},
		"oracle bad tns":   {Conn{Driver: "oracle", Params: map[string]string{"tns": "x", "wallet": t.TempDir()}}, "invalid Oracle"},
	} {
		if _, _, err := openSQLDB(c.conn, ""); err == nil || (c.want != "" && !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s = %v, want %q", name, err, c.want)
		}
	}
}

func TestNewSQLSessionRefusals(t *testing.T) {
	ctx := context.Background()
	if _, err := NewSQLSession(ctx, Conn{Driver: "nope"}, ""); err == nil {
		t.Error("an unknown driver: want error")
	}
	for _, drv := range []string{"sqlserver", "oracle", "firebird", "mysql"} {
		c := Conn{Driver: drv, Host: "127.0.0.1", Port: 1, User: "u", Database: "d", Dial: func(context.Context, string, string) (net.Conn, error) { return nil, net.ErrClosed }}
		if _, err := NewSQLSession(ctx, c, "pw"); err == nil {
			t.Errorf("%s: an unreachable server must fail the ping", drv)
		}
	}
}
