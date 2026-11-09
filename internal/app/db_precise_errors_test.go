package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Owner rule (2026-11-09): termocode uses only the connection settings
// the owner configured; when one fails, the message says in one sentence
// which step failed, with the configured values (never a secret), so the
// owner can fix the config. Nothing else is tried.

// The owner's BaaytonaDB: SSH to deploy@65.109.212.14 works, but on that
// server nothing listens on 127.0.0.1:5432.
const preciseRefused = "SSH to deploy@65.109.212.14 works, but nothing answers at 127.0.0.1:5432 on that server (connection refused)."

// The same, when the server runs a postgres container whose 5432 is not
// published on the host (docker ps shows "5432/tcp").
const preciseRefusedHint = preciseRefused + " A container baaytona-db-1 on that server has 5432 only inside Docker; publish it (ports: \"127.0.0.1:5432:5432\") or point this connection at it."

// preciseDockerPS answers `docker ps` on the fake server as the owner's
// server does: the database container's port only inside Docker.
func preciseDockerPS(cmd string) (string, int) {
	if strings.HasPrefix(cmd, "docker ps") {
		return "baaytona-web-1\tnginx:1.27\t0.0.0.0:80->80/tcp\nbaaytona-db-1\tpostgres:16-alpine\t5432/tcp\n", 0
	}
	return "", 127
}

// preciseOnlyConfigured fails when anything but the configured address
// was asked for through the tunnel, or a container was looked up (no
// second route).
func preciseOnlyConfigured(t *testing.T, srv *dbtest.SSHServer) {
	t.Helper()
	for _, a := range srv.Targets() {
		if a != "127.0.0.1:5432" {
			t.Fatalf("the tunnel was also asked for %s (targets %v): only the configured 127.0.0.1:5432 may be tried", a, srv.Targets())
		}
	}
	for _, c := range srv.Commands() {
		if !strings.HasPrefix(c, "docker ps") {
			t.Fatalf("a command other than the read-only docker ps ran on the server: %q", c)
		}
	}
}

// preciseDBMissing is a fake driver that reaches the database through
// the tunnel, then gets PostgreSQL's "database does not exist" (as pgx
// words it).
func preciseDBMissing(t *testing.T) {
	t.Helper()
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(ctx context.Context, c dbclient.Conn, _ string) (dbclient.Session, error) {
		addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
		if c.Dial == nil {
			return nil, errors.New("no tunnel")
		}
		nc, err := c.Dial(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		_ = nc.Close()
		return nil, fmt.Errorf("failed to connect to `user=%s database=%s`: %s (%s): server error: FATAL: database \"%s\" does not exist (SQLSTATE 3D000)", c.User, c.Database, addr, c.Host, c.Database)
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
}

// preciseSSHModel is the Databases view with the owner's connection
// through a trusted fake SSH server.
func preciseSSHModel(t *testing.T, srv *dbtest.SSHServer, pw string) *Model {
	t.Helper()
	srv.Password = pw
	home := sshTestHome(t, srv)
	if err := dbclient.TrustHostKey(filepath.Join(home, ".ssh", "known_hosts"), "65.109.212.14:22", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	m := unreachableModel(t, &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "password"})
	dbStoreSet(t, "db.baaytona.ssh", pw)
	return m
}

// ── Databases view (TUI) ───────────────────────────────────────────────

func TestDBPreciseRefusedNamesTheDockerContainer(t *testing.T) {
	srv := dbtest.NewSSHServer() // Forward nil: refused
	srv.Exec = preciseDockerPS
	defer srv.Close()
	m := preciseSSHModel(t, srv, "tunnel-pw-61")
	n := connectEnds(t, m, "baaytona", 5*time.Second)
	if n.problem != preciseRefusedHint {
		t.Fatalf("problem = %q\nwant      %q", n.problem, preciseRefusedHint)
	}
	preciseOnlyConfigured(t, srv)
}

// The hint is only information: a container that publishes the port, or
// one of another driver, adds nothing.
func TestDBPreciseNoHintWhenNoUnpublishedContainer(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Exec = func(cmd string) (string, int) {
		return "baaytona-db-1\tpostgres:16\t127.0.0.1:5433->5432/tcp\ncache\tredis:7\t6379/tcp\n", 0
	}
	defer srv.Close()
	m := preciseSSHModel(t, srv, "tunnel-pw-62")
	n := connectEnds(t, m, "baaytona", 5*time.Second)
	if n.problem != preciseRefused {
		t.Fatalf("problem = %q\nwant      %q", n.problem, preciseRefused)
	}
}

func TestDBPreciseDatabaseMissing(t *testing.T) {
	srv := dbtest.NewSSHServer()
	aiTunForward(t, srv)
	defer srv.Close()
	m := preciseSSHModel(t, srv, "tunnel-pw-63")
	preciseDBMissing(t)
	n := connectEnds(t, m, "baaytona", 5*time.Second)
	want := "SSH to deploy@65.109.212.14 works and 127.0.0.1:5432 answers, but database app does not exist there."
	if n.problem != want {
		t.Fatalf("problem = %q\nwant      %q", n.problem, want)
	}
	preciseOnlyConfigured(t, srv)
}

// ── web page ───────────────────────────────────────────────────────────

func TestWebDBPreciseRefusedNamesTheDockerContainer(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-64"
	srv.Exec = preciseDockerPS
	defer srv.Close()
	m, c, _ := webRemoteDB(t, srv, dbclient.Conn{ID: "baaytona", Driver: "postgres", Host: "127.0.0.1", Port: 5432, User: "app", Database: "app",
		SSH: &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "password"}})
	dbStoreSet(t, "db.baaytona.ssh", "tunnel-pw-64")
	f := webCallWithin(t, m, c, webproto.OpDBTables, `{"conn":"baaytona"}`, 5*time.Second)
	if webTestErr(f) != "conflict" || f["msg"] != preciseRefusedHint {
		t.Fatalf("got %v\nwant msg %q", f, preciseRefusedHint)
	}
	preciseOnlyConfigured(t, srv)
}

func TestWebDBPreciseDatabaseMissing(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-65"
	aiTunForward(t, srv)
	defer srv.Close()
	m, c, _ := webRemoteDB(t, srv, dbclient.Conn{ID: "baaytona", Driver: "postgres", Host: "127.0.0.1", Port: 5432, User: "baaytona", Database: "baaytona",
		SSH: &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "password"}})
	dbStoreSet(t, "db.baaytona.ssh", "tunnel-pw-65")
	preciseDBMissing(t)
	f := webCallWithin(t, m, c, webproto.OpDBTables, `{"conn":"baaytona"}`, 5*time.Second)
	want := "SSH to deploy@65.109.212.14 works and 127.0.0.1:5432 answers, but database baaytona does not exist there."
	if webTestErr(f) != "conflict" || f["msg"] != want {
		t.Fatalf("got %v\nwant msg %q", f, want)
	}
}

// ── AI db_* tools ──────────────────────────────────────────────────────

func TestAIDBPreciseRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		exec func(string) (string, int)
		want string
	}{
		{"no docker", nil, preciseRefused},
		{"unpublished container", preciseDockerPS, preciseRefusedHint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := dbtest.NewSSHServer() // Forward nil: refused
			srv.Password = "tunnel-pw-66"
			srv.Exec = tc.exec
			defer srv.Close()
			e := newAITunEnv(t, srv, aiTunBaaytona(), true)
			dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-66")
			r := aiTunWait(t, e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"}))
			if r.err == nil || r.err.Error() != tc.want {
				t.Fatalf("err = %v\nwant  %s", r.err, tc.want)
			}
			preciseOnlyConfigured(t, srv)
			if n := e.dials.Load(); n != 1 {
				t.Fatalf("%d dials to the SSH server, want 1 (no other route)", n)
			}
		})
	}
}

// The SSH server takes the channel, then the database never says a word:
// the tool ends at the deadline and says the address timed out there.
func TestAIDBPreciseTimeout(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-67"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAITunEnv(t, srv, aiTunBaaytona(), true)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-67")
	// The AI's login waits db_timeout (1 s here, the least there is).
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`{"db_timeout":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(ctx context.Context, c dbclient.Conn, _ string) (dbclient.Session, error) {
		nc, err := c.Dial(ctx, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
		if err != nil {
			return nil, err
		}
		defer nc.Close()
		<-ctx.Done() // the server never answers the startup message
		return nil, fmt.Errorf("failed to connect to `user=app database=baaytona`: 127.0.0.1:5432 (127.0.0.1): timeout: %w", ctx.Err())
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	r := aiTunWait(t, e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"}))
	want := "SSH to deploy@65.109.212.14 works, but 127.0.0.1:5432 on that server did not answer in time (timed out)."
	if r.err == nil || r.err.Error() != want {
		t.Fatalf("err = %v\nwant  %s", r.err, want)
	}
}

func TestAIDBPreciseDatabaseMissing(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-68"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAITunEnv(t, srv, aiTunBaaytona(), true)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-68")
	preciseDBMissing(t)
	r := aiTunWait(t, e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"}))
	want := "SSH to deploy@65.109.212.14 works and 127.0.0.1:5432 answers, but database baaytona does not exist there."
	if r.err == nil || r.err.Error() != want {
		t.Fatalf("err = %v\nwant  %s", r.err, want)
	}
	preciseOnlyConfigured(t, srv)
}

// ── one sentence per kind of failure ───────────────────────────────────

func TestDBConnectProblemSentences(t *testing.T) {
	ssh := &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "agent"}
	pg := dbclient.Conn{ID: "b", Driver: "postgres", Host: "127.0.0.1", Port: 5432, User: "baaytona", Database: "baaytona", SSH: ssh}
	direct := pg
	direct.SSH = nil
	my := dbclient.Conn{ID: "m", Driver: "mysql", Host: "db.internal", Port: 3306, User: "shop", Database: "shop"}
	tls := pg
	tls.Params = map[string]string{"sslmode": "require"}
	pgx := func(reason string) error {
		return errors.New("failed to connect to `user=baaytona database=baaytona`: 127.0.0.1:5432 (127.0.0.1): server error: " + reason)
	}
	via := "SSH to deploy@65.109.212.14 works and 127.0.0.1:5432 answers, but "
	for _, tc := range []struct {
		name string
		c    dbclient.Conn
		err  error
		want string
	}{
		{"wrong password", pg, pgx(`FATAL: password authentication failed for user "baaytona" (SQLSTATE 28P01)`),
			via + "PostgreSQL did not accept the password of user baaytona."},
		{"wrong password direct", direct, pgx(`FATAL: password authentication failed for user "baaytona" (SQLSTATE 28P01)`),
			"127.0.0.1:5432 answers, but PostgreSQL did not accept the password of user baaytona."},
		{"no such user", pg, pgx(`FATAL: role "baaytona" does not exist (SQLSTATE 28000)`),
			via + "user baaytona does not exist there."},
		{"no such database", pg, pgx(`FATAL: database "baaytona" does not exist (SQLSTATE 3D000)`),
			via + "database baaytona does not exist there."},
		{"pg_hba", pg, pgx(`FATAL: no pg_hba.conf entry for host "172.18.0.1", user "baaytona", database "baaytona", no encryption (SQLSTATE 28000)`),
			via + "PostgreSQL does not let user baaytona in from there (no pg_hba.conf entry)."},
		{"server without ssl", tls, errors.New("failed to connect to `user=baaytona database=baaytona`: 127.0.0.1:5432 (127.0.0.1): tls error: server refused TLS connection"),
			via + "the server does not accept SSL (sslmode require)."},
		{"bad certificate", tls, errors.New("failed to connect to `user=baaytona database=baaytona`: 127.0.0.1:5432 (127.0.0.1): tls error: tls: failed to verify certificate: x509: certificate signed by unknown authority"),
			via + "the SSL check failed (x509: certificate signed by unknown authority)."},
		{"mysql password", my, errors.New("Error 1045 (28000): Access denied for user 'shop'@'10.0.0.5' (using password: YES)"),
			"db.internal:3306 answers, but MySQL did not accept the password of user shop."},
		{"mysql database", my, errors.New("Error 1049 (42000): Unknown database 'shop'"),
			"db.internal:3306 answers, but database shop does not exist there."},
		{"reach", pg, fmt.Errorf("dial error: %w", &dbclient.ReachError{Addr: "127.0.0.1:5432", Via: "deploy@65.109.212.14"}), preciseRefused},
		{"other", pg, errors.New("ERROR: relation \"x\" does not exist (SQLSTATE 42P01)"), ""},
		{"a statement's error", pg, errors.New("ERROR: role \"bob\" does not exist (SQLSTATE 42704)"), ""},
	} {
		if got := dbConnectProblem(tc.c, tc.err); got != tc.want {
			t.Errorf("%s:\ngot  %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

func TestDBDockerHint(t *testing.T) {
	for _, tc := range []struct {
		name, ps, driver string
		port             int
		want             string
	}{
		{"unpublished postgres", "baaytona-db-1\tpostgres:16-alpine\t5432/tcp\n", "postgres", 5432,
			"A container baaytona-db-1 on that server has 5432 only inside Docker; publish it (ports: \"127.0.0.1:5432:5432\") or point this connection at it."},
		{"published elsewhere", "baaytona-db-1\tpostgres:16\t127.0.0.1:5433->5432/tcp\n", "postgres", 5432, ""},
		{"other driver", "cache\tredis:7\t6379/tcp\n", "postgres", 5432, ""},
		{"name says postgres", "pg-main\tghcr.io/acme/db:3\t5432/tcp, 8008/tcp\n", "postgres", 5432,
			"A container pg-main on that server has 5432 only inside Docker; publish it (ports: \"127.0.0.1:5432:5432\") or point this connection at it."},
		{"mysql", "shop-db\tmariadb:11\t3306/tcp\n", "mysql", 3306,
			"A container shop-db on that server has 3306 only inside Docker; publish it (ports: \"127.0.0.1:3306:3306\") or point this connection at it."},
		{"garbage", "permission denied while trying to connect to the Docker daemon socket", "postgres", 5432, ""},
	} {
		if got := dbDockerHint(tc.ps, tc.driver, tc.port); got != tc.want {
			t.Errorf("%s:\ngot  %q\nwant %q", tc.name, got, tc.want)
		}
	}
}
