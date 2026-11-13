package dbclient

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOracleConnStringAndDSN(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tnsnames.ora"), []byte("PROD =\n  (DESCRIPTION =\n    (ADDRESS = (PROTOCOL = TCPS)(HOST = db.prod)(PORT = 2484))\n    (CONNECT_DATA = (SERVICE_NAME = svc.prod)))\nLEGACY =\n  (DESCRIPTION =\n    (ADDRESS = (PROTOCOL = TCP)(HOST = old)(PORT = 0))\n    (CONNECT_DATA = (SID = ORCL)))\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		conn Conn
		want []string
		bad  string
	}{
		"plain":     {Conn{Host: "h", Port: 1522, Database: "svc", User: "u"}, []string{"oracle://u:pw@h:1522/svc"}, ""},
		"host only": {Conn{Host: "h", Database: "svc"}, []string{"oracle://h/svc"}, ""},
		"url":       {Conn{URL: "oracle://me@x:1/y?a=b"}, []string{"me:pw@x:1/y", "a=b"}, ""},
		"bad url":   {Conn{URL: "oracle://%zz"}, []string{"%zz"}, ""},
		"wallet":    {Conn{Host: "h", Database: "s", Params: map[string]string{"wallet": "/w", "x": "y"}}, []string{"WALLET=%2Fw", "SSL=true", "x=y"}, ""},
		"tns":       {Conn{Params: map[string]string{"tns": "prod", "wallet": dir}}, []string{"db.prod:2484", "/svc.prod", "SSL=true"}, ""},
		"tns sid":   {Conn{Params: map[string]string{"tns": "legacy", "wallet": dir}}, []string{"old:1521", "SID=ORCL"}, ""},
		"tns miss":  {Conn{Params: map[string]string{"tns": "nope", "wallet": dir}}, nil, "is not in"},
		"ssl block": {Conn{Host: "h", Database: "s", SSL: &SSLConfig{Mode: "require"}}, []string{"SSL=true"}, ""},
	} {
		got, err := oracleConnString(c.conn, "pw")
		if c.bad != "" {
			if err == nil || !strings.Contains(err.Error(), c.bad) {
				t.Errorf("%s: %v, want %q", name, err, c.bad)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", name, got, w)
			}
		}
	}
	t.Setenv("TNS_ADMIN", t.TempDir())
	t.Setenv("ORACLE_HOME", "")
	if _, err := oracleConnString(Conn{Params: map[string]string{"tns": "prod"}}, ""); err == nil || !strings.Contains(err.Error(), "no tnsnames.ora") {
		t.Errorf("no tnsnames.ora = %v", err)
	}
}

func TestOracleAndSQLServerConnectors(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) { return nil, os.ErrNotExist }
	if c, err := oracleConnector(Conn{Host: "h", Database: "s", Dial: dial, SSL: &SSLConfig{Mode: "require"}}, "pw"); err != nil || c == nil {
		t.Errorf("oracle connector = %v %v", c, err)
	}
	if _, err := oracleConnector(Conn{Host: "h", SSL: &SSLConfig{Mode: "verify-full", CA: "/no/such.pem"}}, ""); err == nil {
		t.Error("oracle with an unreadable CA: want error")
	}
	if _, err := oracleConnector(Conn{Params: map[string]string{"tns": "x", "wallet": t.TempDir()}}, ""); err == nil {
		t.Error("oracle with a missing tnsnames.ora: want error")
	}
	for name, c := range map[string]Conn{
		"plain":     {Host: "h", Port: 1433, User: "u", Database: "d"},
		"tunnel":    {Host: "h", User: "u", Dial: dial},
		"tls":       {Host: "h", User: "u", SSL: &SSLConfig{Mode: "require"}},
		"start sql": {Host: "h", User: "u", Options: &ConnOptions{InitSQL: "SET NOCOUNT ON"}},
	} {
		conn, err := sqlServerConnector(c, "pw")
		if err != nil || conn == nil {
			t.Errorf("%s = %v %v", name, conn, err)
		}
	}
	if _, err := sqlServerConnector(Conn{Host: "h", SSL: &SSLConfig{Mode: "verify-full", CA: "/no/such.pem"}}, ""); err == nil {
		t.Error("sql server with an unreadable CA: want error")
	}
	if _, err := sqlServerConnector(Conn{URL: "sqlserver://h:notaport"}, ""); err == nil {
		t.Error("a bad DSN: want error")
	}
	if got := sqlServerParam(Conn{Params: map[string]string{"FedAuth": "x"}}, "fedauth"); got != "x" {
		t.Errorf("param any case = %q", got)
	}
	if got := sqlServerParam(Conn{URL: "sqlserver://h?Fedauth=y"}, "fedauth"); got != "y" {
		t.Errorf("param in the URL = %q", got)
	}
	if got := sqlServerParam(Conn{URL: "sqlserver://h"}, "fedauth"); got != "" {
		t.Errorf("no param = %q", got)
	}
}
