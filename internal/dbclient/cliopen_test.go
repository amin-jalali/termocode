package dbclient

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func noSecretInArgs(t *testing.T, c CLICmd, secret string) {
	t.Helper()
	for _, a := range c.Args {
		if strings.Contains(a, secret) {
			t.Fatalf("secret in argv: %q", c.Args)
		}
	}
}

func TestCLICommandPostgres(t *testing.T) {
	pw := "pw-" + "test"
	c := Conn{ID: "p", Driver: "postgres", Host: "db", Port: 5432, User: "app", Database: "shop", ReadOnly: true,
		SSL: &SSLConfig{Mode: "verify-full", CA: "/ca.pem"}}
	got, err := CLICommand(c, CLIOptions{Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	if got.Tool != "psql" || !reflect.DeepEqual(got.Args, []string{"-h", "db", "-p", "5432", "-U", "app", "-d", "shop"}) {
		t.Fatalf("psql = %+v", got)
	}
	noSecretInArgs(t, got, pw)
	for _, want := range []string{"PGPASSWORD=" + pw, "PGSSLMODE=verify-full", "PGSSLROOTCERT=/ca.pem", "PGOPTIONS=-c default_transaction_read_only=on"} {
		if !hasEnv(got.Env, want) {
			t.Fatalf("env misses %q: %v", want, got.Env)
		}
	}
}

func TestCLICommandTunnelAddressWins(t *testing.T) {
	c := Conn{ID: "p", Driver: "postgres", Host: "db.internal", Port: 5432, User: "app"}
	got, err := CLICommand(c, CLIOptions{Host: "127.0.0.1", Port: 40123})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Args[:4], []string{"-h", "127.0.0.1", "-p", "40123"}) {
		t.Fatalf("tunnel addr not used: %v", got.Args)
	}
	if len(got.Env) != 0 {
		t.Fatalf("no password, no env: %v", got.Env)
	}
}

func TestCLICommandMySQLRedisSQLite(t *testing.T) {
	pw := "pw-" + "test"
	m, err := CLICommand(Conn{ID: "m", Driver: "mariadb", Host: "h", Port: 3306, User: "u", Database: "d"}, CLIOptions{Password: pw})
	if err != nil || m.Tool != "mysql" || !reflect.DeepEqual(m.Args, []string{"--protocol=TCP", "-h", "h", "-P", "3306", "-u", "u", "d"}) || !hasEnv(m.Env, "MYSQL_PWD="+pw) {
		t.Fatalf("mysql = %+v %v", m, err)
	}
	noSecretInArgs(t, m, pw)

	r, err := CLICommand(Conn{ID: "r", Driver: "redis", Host: "h", Port: 6379, User: "u", DB: 2, TLS: true}, CLIOptions{Password: pw})
	if err != nil || r.Tool != "redis-cli" || !reflect.DeepEqual(r.Args, []string{"-h", "h", "-p", "6379", "--user", "u", "-n", "2", "--tls"}) || !hasEnv(r.Env, "REDISCLI_AUTH="+pw) {
		t.Fatalf("redis = %+v %v", r, err)
	}
	noSecretInArgs(t, r, pw)

	root := t.TempDir()
	s, err := CLICommand(Conn{ID: "s", Driver: "sqlite", Path: "data/app.db", ReadOnly: true}, CLIOptions{Root: root})
	if err != nil || s.Tool != "sqlite3" || !reflect.DeepEqual(s.Args, []string{"-readonly", filepath.Join(root, "data/app.db")}) {
		t.Fatalf("sqlite = %+v %v", s, err)
	}
}

func TestCLICommandFromURLAndUnknown(t *testing.T) {
	got, err := CLICommand(Conn{ID: "p", Driver: "postgres", URL: "postgres://app@db:6000/shop"}, CLIOptions{})
	if err != nil || !reflect.DeepEqual(got.Args, []string{"-h", "db", "-p", "6000", "-U", "app", "-d", "shop"}) {
		t.Fatalf("url = %+v %v", got, err)
	}
	if _, err := CLICommand(Conn{ID: "x", Driver: "oracle"}, CLIOptions{}); err == nil {
		t.Fatal("unknown driver must fail")
	}
	if CLITool("postgres") != "psql" || CLITool("mariadb") != "mysql" || CLITool("nope") != "" {
		t.Fatal("CLITool names")
	}
}

func hasEnv(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
