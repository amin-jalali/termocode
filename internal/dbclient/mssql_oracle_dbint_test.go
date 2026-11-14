//go:build dbint

package dbclient

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// DBX-62 / DBX-63 real-server tests. Like the other dbint tests they run
// only with -tags dbint. A server comes from DBINT_SQLSERVER_* or
// DBINT_ORACLE_* env vars; without them a local docker image (never
// pulled) is started for the test and removed after it. With neither the
// test skips; TERMOCODE_DBINT=1 makes a missing server a failure.
//
//	go test -tags dbint ./internal/dbclient -run 'TestDBIntSQLServer|TestDBIntOracle'

const (
	dbintSQLServerImage = "mcr.microsoft.com/mssql/server:2022-latest"
	dbintOracleImage    = "gvenzl/oracle-free:slim"
	dbintStrongPassword = "Termocode_dbint1"
)

func TestDBIntSQLServerSession(t *testing.T) {
	conn := Conn{Driver: "sqlserver", Host: os.Getenv("DBINT_SQLSERVER_HOST"), Port: 1433,
		User: envOr("DBINT_SQLSERVER_USER", "sa"), Password: envOr("DBINT_SQLSERVER_PASSWORD", dbintStrongPassword),
		Database: envOr("DBINT_SQLSERVER_DATABASE", "master"), Params: map[string]string{"encrypt": "disable"}}
	if port := os.Getenv("DBINT_SQLSERVER_PORT"); port != "" {
		conn.Port, _ = strconv.Atoi(port)
	}
	if conn.Host == "" {
		conn.Host, conn.Port = dbintDocker(t, dbintSQLServerImage, 1433,
			"ACCEPT_EULA=Y", "MSSQL_SA_PASSWORD="+dbintStrongPassword)
	}
	s := dbintOpen(t, conn, 2*time.Minute)
	defer s.Close()
	ctx := context.Background()

	ro := dbintReadOnly(t, s, "SELECT 'plain text'")
	defer ro.Close()

	name := dbintObject()
	if r := s.Run(ctx, "CREATE TABLE "+s.dialect.QuoteIdent(name)+" (id int PRIMARY KEY, label nvarchar(32))", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	defer s.Run(ctx, "DROP TABLE "+s.dialect.QuoteIdent(name), RunOptions{})
	dbintIntrospect(t, s, name)
	if r := s.Run(ctx, SQLServerPlanForm("SELECT * FROM "+s.dialect.QuoteIdent(name), false), RunOptions{}); r.Err != nil || len(r.Rows) == 0 {
		t.Fatalf("SHOWPLAN XML plan = %+v", r)
	}
	if r := s.Run(ctx, "SELECT TOP 1 name FROM sys.databases ORDER BY name", RunOptions{}); r.Err != nil || len(r.Rows) != 1 {
		t.Fatalf("T-SQL TOP = %+v", r)
	}
	dbintWrongPassword(t, conn, "login failed")
}

func TestDBIntOracleSession(t *testing.T) {
	conn := Conn{Driver: "oracle", Host: os.Getenv("DBINT_ORACLE_HOST"), Port: 1521,
		User: envOr("DBINT_ORACLE_USER", "termocode"), Password: envOr("DBINT_ORACLE_PASSWORD", dbintStrongPassword),
		Database: envOr("DBINT_ORACLE_SERVICE", "FREEPDB1")}
	if port := os.Getenv("DBINT_ORACLE_PORT"); port != "" {
		conn.Port, _ = strconv.Atoi(port)
	}
	if conn.Host == "" {
		conn.Host, conn.Port = dbintDocker(t, dbintOracleImage, 1521,
			"ORACLE_PASSWORD="+dbintStrongPassword, "APP_USER=termocode", "APP_USER_PASSWORD="+dbintStrongPassword)
	}
	s := dbintOpen(t, conn, 4*time.Minute)
	defer s.Close()
	ctx := context.Background()

	ro := dbintReadOnly(t, s, "SELECT 'plain text' FROM dual")
	defer ro.Close()

	// Unquoted Oracle names are upper case; the catalog keeps them so.
	name := strings.ToUpper(dbintObject())
	if r := s.Run(ctx, "CREATE TABLE "+name+" (id NUMBER PRIMARY KEY, label VARCHAR2(32))", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	defer s.Run(ctx, "DROP TABLE "+name+" PURGE", RunOptions{})
	dbintIntrospect(t, s, name)
	if r := s.Run(ctx, OraclePlanForm("SELECT * FROM "+name), RunOptions{}); r.Err != nil || len(r.Rows) == 0 {
		t.Fatalf("EXPLAIN PLAN = %+v", r)
	}

	// A PL/SQL unit runs as one statement and its source reads back.
	proc := name + "_P"
	body := "CREATE OR REPLACE PROCEDURE " + proc + " AS\nBEGIN\n  NULL;\nEND;"
	if r := s.Run(ctx, body, RunOptions{}); r.Err != nil {
		t.Fatalf("create procedure: %v", r.Err)
	}
	defer s.Run(ctx, "DROP PROCEDURE "+proc, RunOptions{})
	q, err := oracleSourceQuery(ObjectProcedure, proc)
	if err != nil {
		t.Fatal(err)
	}
	r := s.Run(ctx, q.SQL, RunOptions{Args: q.Args})
	if src, ok := q.Text(r); r.Err != nil || !ok || !strings.Contains(strings.ToUpper(src), "PROCEDURE "+proc) {
		t.Fatalf("procedure source = %q, %+v", src, r)
	}
	dbintWrongPassword(t, conn, "ora-01017")
}

// dbintOpen connects, retrying while a fresh container starts up.
func dbintOpen(t *testing.T, conn Conn, wait time.Duration) *SQLSession {
	t.Helper()
	deadline := time.Now().Add(wait)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		s, err := NewSQLSession(ctx, conn, conn.Password)
		cancel()
		if err == nil {
			return s
		}
		if time.Now().After(deadline) {
			unavailableDBInt(t, err)
		}
		time.Sleep(3 * time.Second)
	}
}

// dbintReadOnly checks the AI's read-only session: a read works, a write
// is refused before it reaches the server.
func dbintReadOnly(t *testing.T, s *SQLSession, read string) Session {
	t.Helper()
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, q := range []string{"CREATE TABLE dbint_forbidden (id int)", "DELETE FROM dbint_forbidden", "DROP TABLE dbint_forbidden"} {
		if r := ro.Run(ctx, q, RunOptions{}); r.Err == nil {
			t.Errorf("read-only session accepted %q", q)
		}
	}
	if r := ro.Run(ctx, read, RunOptions{}); r.Err != nil || len(r.Rows) != 1 || r.Rows[0][0].Text != "plain text" {
		t.Errorf("read-only text result: %+v", r)
	}
	return ro
}

func dbintIntrospect(t *testing.T, s *SQLSession, name string) {
	t.Helper()
	info, err := s.Introspect(context.Background(), IntrospectionRequest{Object: name})
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Columns) < 2 || info.DDL == "" {
		t.Fatalf("introspection missed columns or DDL: %+v", info)
	}
}

func dbintWrongPassword(t *testing.T, conn Conn, reason string) {
	t.Helper()
	conn.Password = "definitely-wrong-Password1"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := NewSQLSession(ctx, conn, conn.Password)
	if err == nil {
		s.Close()
		t.Fatal("wrong password connected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), reason) {
		t.Errorf("wrong-password reason was lost: %v", err)
	}
	if strings.Contains(err.Error(), conn.Password) {
		t.Errorf("error shows the password: %v", err)
	}
}

// dbintDocker starts image (only when it is already pulled: a test never
// downloads gigabytes) with port published on 127.0.0.1 and removes the
// container after the test. It skips when docker or the image is missing.
func dbintDocker(t *testing.T, image string, port int, env ...string) (string, int) {
	t.Helper()
	docker, err := dbintLookDocker()
	if err != nil {
		unavailableDBInt(t, err)
	}
	if err := dbintCmd(docker, "image", "inspect", "--format", "{{.Id}}", image).Run(); err != nil {
		unavailableDBInt(t, fmt.Errorf("docker image %s is not pulled (docker pull %s)", image, image))
	}
	args := []string{"run", "-d", "--rm", "-p", "127.0.0.1::" + strconv.Itoa(port)}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	out, err := dbintCmd(docker, append(args, image)...).Output()
	if err != nil {
		unavailableDBInt(t, fmt.Errorf("docker run %s: %v", image, err))
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = dbintCmd(docker, "rm", "-f", id).Run() })
	out, err = dbintCmd(docker, "port", id, strconv.Itoa(port)+"/tcp").Output()
	if err != nil {
		t.Fatalf("docker port %s: %v", id, err)
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	n, perr := strconv.Atoi(line[strings.LastIndex(line, ":")+1:])
	if perr != nil {
		t.Fatalf("docker port %s = %q", id, out)
	}
	return "127.0.0.1", n
}

func dbintLookDocker() (string, error) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return "", fmt.Errorf("docker is not installed: %w", err)
	}
	if err := dbintCmd(docker, "version", "--format", "{{.Server.Version}}").Run(); err != nil {
		return "", fmt.Errorf("docker daemon is not reachable: %w", err)
	}
	return docker, nil
}

func dbintCmd(docker string, args ...string) *exec.Cmd {
	return exec.Command(docker, args...) // test-only helper; product code goes through internal/spawn
}
