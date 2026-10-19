//go:build dbint

package dbclient

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func dbintEnabled() bool { return os.Getenv("TERMOCODE_DBINT") == "1" }

func unavailableDBInt(t *testing.T, err error) {
	t.Helper()
	if dbintEnabled() {
		t.Fatalf("database integration server is required but unavailable: %v", err)
	}
	t.Skipf("database integration server is unavailable: %v", err)
}

func dbintObject() string { return fmt.Sprintf("dbint_probe_%d", time.Now().UnixNano()) }

func verifySQLServer(t *testing.T, s *SQLSession, mysql bool) {
	t.Helper()
	ctx := context.Background()
	roSession, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	defer roSession.Close()
	ro := roSession.(*SQLSession)
	blocked := []string{"CREATE TABLE dbint_forbidden (id int)", "TRUNCATE TABLE dbint_forbidden", "RENAME TABLE dbint_forbidden TO dbint_forbidden2", "SELECT GET_LOCK('dbint', 0)", "PREPARE dbint_stmt FROM 'SELECT 1'", "SET SESSION max_execution_time=1", "LOCK TABLES dbint_forbidden READ"}
	blocked = append(blocked, "COPY dbint_forbidden TO '/tmp/dbint-copy'")
	for _, query := range blocked {
		if r := ro.Run(ctx, query, RunOptions{}); r.Err == nil {
			t.Errorf("read-only session accepted %q", query)
		}
	}
	if mysql {
		outfile := filepath.Join(os.TempDir(), dbintObject()+".out")
		for _, query := range []string{
			"SELECT 1 INTO OUTFILE '" + outfile + "'",
			"TABLE ro_t INTO OUTFILE '" + outfile + "'",
			"SELECT 1 /*!50000 INTO OUTFILE '" + outfile + "' */",
			"SELECT 1 /*! INTO OUTFILE '" + outfile + "' */",
			"SELECT 1 /*M! INTO OUTFILE '" + outfile + "' */",
			"SELECT RELEASE_LOCK('dbint')", "EXECUTE dbint_stmt", "DEALLOCATE PREPARE dbint_stmt",
		} {
			if r := ro.Run(ctx, query, RunOptions{}); r.Err == nil {
				t.Errorf("read-only session accepted %q", query)
			}
		}
		if _, err := os.Stat(outfile); !os.IsNotExist(err) {
			t.Errorf("read-only outfile statement created %q (stat error: %v)", outfile, err)
			_ = os.Remove(outfile)
		}
		timeoutVariable := "max_execution_time"
		if s.mariadb {
			timeoutVariable = "max_statement_time"
		}
		if r := s.Run(ctx, "SELECT @@SESSION."+timeoutVariable, RunOptions{}); r.Err != nil || len(r.Rows) != 1 || !isZeroNumber(r.Rows[0][0].Text) {
			t.Errorf("user session %s changed: %+v", timeoutVariable, r)
		}
	}
	name := dbintObject()
	create := "CREATE TABLE " + s.dialect.QuoteIdent(name) + " (id integer PRIMARY KEY, label varchar(32))"
	if r := s.Run(ctx, create, RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	defer s.Run(ctx, "DROP TABLE "+s.dialect.QuoteIdent(name), RunOptions{})
	info, err := s.Introspect(ctx, IntrospectionRequest{Object: name})
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Columns) < 2 || info.DDL == "" {
		t.Fatalf("introspection missed columns or DDL: %+v", info)
	}
	if r := ro.Run(ctx, "SELECT 'plain text'", RunOptions{}); r.Err != nil || len(r.Rows) != 1 || strings.HasPrefix(r.Rows[0][0].Text, "0x") {
		t.Errorf("read-only text result: %+v", r)
	}
}

func TestDBIntPostgresSession(t *testing.T) {
	dsn := os.Getenv("DBINT_POSTGRES_DSN")
	if dsn == "" && dbintEnabled() {
		dsn = "postgres://termocode:termocode@localhost:54329/termocode"
	}
	if dsn == "" {
		t.Skip("DBINT_POSTGRES_DSN is not set")
	}
	conn, err := ParseURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewPostgresSession(context.Background(), conn, conn.Password)
	if err != nil {
		unavailableDBInt(t, err)
	}
	defer s.Close()
	verifySQLServer(t, s, false)
	if r := s.Run(context.Background(), "INSERT INTO dbint_probe_returning (id) VALUES (1) RETURNING id", RunOptions{}); r.Err == nil {
		t.Fatal("expected missing table error")
	}
	name := dbintObject()
	if r := s.Run(context.Background(), "CREATE SEQUENCE "+s.dialect.QuoteIdent(name), RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	defer s.Run(context.Background(), "DROP SEQUENCE "+s.dialect.QuoteIdent(name), RunOptions{})
	if r := s.Run(context.Background(), "SELECT nextval('"+name+"')", RunOptions{}); r.Err != nil || len(r.Rows) != 1 {
		t.Fatalf("SELECT nextval result: %+v", r)
	}
	bad := conn
	bad.Password = "definitely-wrong-password"
	if _, err := NewPostgresSession(context.Background(), bad, bad.Password); err == nil || !strings.Contains(strings.ToLower(err.Error()), "password authentication failed") {
		t.Errorf("wrong-password reason was lost: %v", err)
	}
}

func TestDBIntMySQLSession(t *testing.T) {
	host := os.Getenv("DBINT_MYSQL_HOST")
	if host == "" && dbintEnabled() {
		host = "127.0.0.1"
	}
	if host == "" {
		t.Skip("DBINT_MYSQL_HOST is not set")
	}
	conn := Conn{Driver: "mysql", Host: host, Port: 3306, User: envOr("DBINT_MYSQL_USER", "termocode"), Password: os.Getenv("DBINT_MYSQL_PASSWORD"), Database: envOr("DBINT_MYSQL_DATABASE", "termocode")}
	if dbintEnabled() && os.Getenv("DBINT_MYSQL_PORT") == "" {
		conn.Port = 33069
	}
	if port := os.Getenv("DBINT_MYSQL_PORT"); port != "" {
		conn.Port, _ = strconv.Atoi(port)
	}
	s, err := NewMySQLSession(context.Background(), conn, conn.Password)
	if err != nil {
		unavailableDBInt(t, err)
	}
	defer s.Close()
	verifySQLServer(t, s, true)
}

func TestDBIntMariaDBSession(t *testing.T) {
	host := os.Getenv("DBINT_MARIADB_HOST")
	if host == "" && dbintEnabled() {
		host = "127.0.0.1"
	}
	if host == "" {
		t.Skip("DBINT_MARIADB_HOST is not set")
	}
	conn := Conn{Driver: "mysql", Host: host, Port: 3306, User: envOr("DBINT_MARIADB_USER", "termocode"), Password: envOr("DBINT_MARIADB_PASSWORD", "termocode"), Database: envOr("DBINT_MARIADB_DATABASE", "termocode")}
	if dbintEnabled() && os.Getenv("DBINT_MARIADB_PORT") == "" {
		conn.Port = 33079
	}
	if port := os.Getenv("DBINT_MARIADB_PORT"); port != "" {
		conn.Port, _ = strconv.Atoi(port)
	}
	s, err := NewMySQLSession(context.Background(), conn, conn.Password)
	if err != nil {
		unavailableDBInt(t, err)
	}
	defer s.Close()
	if !s.mariadb {
		t.Fatalf("server flavor was not detected")
	}
	verifySQLServer(t, s, true)
	if r := s.Explain(context.Background(), "SELECT 1", true); r.Err != nil || len(r.Rows) == 0 {
		t.Fatalf("MariaDB analyze result = %+v", r)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func isZeroNumber(value string) bool {
	n, err := strconv.ParseFloat(value, 64)
	return err == nil && n == 0
}
