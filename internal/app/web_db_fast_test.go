package app

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webSlowDB is the owner's setup made small: a Postgres in a container
// on an SSH server across a slow link (each packet waits lag each way),
// reached from the page with the real driver. docker inspect costs
// inspect (the docker CLI starting on the server). Everything is in
// process: the SSH server, the database and the link are fakes.
type webSlowDB struct {
	m   *Model
	c   *webConn
	srv *dbtest.SSHServer
	pg  *dbtest.PGServer

	mu sync.Mutex
	ip string // the container's IP now
}

func newWebSlowDB(t *testing.T, lag, inspect time.Duration) *webSlowDB {
	t.Helper()
	w := &webSlowDB{ip: "172.18.0.5", pg: &dbtest.PGServer{Password: "pg-pw-31", Tables: []string{"users", "orders"}}}
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-31"
	srv.Latency = lag
	srv.Exec = func(cmd string) (string, int) {
		if !strings.HasPrefix(cmd, "docker inspect ") || !strings.HasSuffix(cmd, " pg-1") {
			return "", 1
		}
		time.Sleep(inspect)
		w.mu.Lock()
		defer w.mu.Unlock()
		return "running\napp_default " + w.ip + "\n", 0
	}
	srv.Forward = func(_ context.Context, addr string) (net.Conn, error) {
		w.mu.Lock()
		ok := addr == w.ip+":5432"
		w.mu.Unlock()
		if !ok {
			return nil, errors.New("connection refused")
		}
		return w.pg.Conn(), nil
	}
	t.Cleanup(srv.Close)
	w.srv = srv
	home := sshTestHome(t, srv)
	if err := dbclient.TrustHostKey(filepath.Join(home, ".ssh", "known_hosts"), "srv.example.com:22", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	m := aiTestModel(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	conns := `{"connections":[{"id":"pg","driver":"postgres","host":"pg-1","port":5432,"user":"app","database":"app",` +
		`"container":{"name":"pg-1"},"ssh":{"host":"srv.example.com","user":"deploy","auth":"password"}}]}`
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ws.Root = root
	m.output = newOutputStore()
	m.databases = newDBState()
	m.databases.load(root)
	dbStoreSet(t, "db.pg.ssh", "tunnel-pw-31")
	dbStoreSet(t, "db.pg", "pg-pw-31")
	webTestHub(t, &m)
	w.m = &m
	w.c = webTestSub(t, &m)
	return w
}

// webSlowCost is what one read cost: its time, the SSH commands it ran,
// the database logins and the round trips the driver waited for.
type webSlowCost struct {
	took                  time.Duration
	execs, logins, rounds int
}

func (w *webSlowDB) read(t *testing.T, op, args string) (map[string]any, webSlowCost) {
	t.Helper()
	execs, logins, rounds := len(w.srv.Commands()), w.pg.Logins(), w.pg.Rounds()
	start := time.Now()
	f := webTestCallBg(t, w.m, w.c, op, args)
	c := webSlowCost{took: time.Since(start), execs: len(w.srv.Commands()) - execs, logins: w.pg.Logins() - logins, rounds: w.pg.Rounds() - rounds}
	if webTestErr(f) != "" {
		t.Fatalf("%s: %v", op, f)
	}
	t.Logf("%-9s %6.0f ms  %d docker inspect, %d postgres logins, %d postgres round trips", op, float64(c.took.Microseconds())/1000, c.execs, c.logins, c.rounds)
	return f, c
}

// Spec (perf): the page asks the server for the container's IP once per
// tunnel (docker inspect is a new SSH session and the docker CLI
// starting, often a second), not on every read. When the container was
// recreated, the dial to its old IP fails, the IP is asked again once
// and the read works.
func TestWebDBContainerIPFoundOncePerTunnel(t *testing.T) {
	w := newWebSlowDB(t, 0, 0)
	if _, c := w.read(t, webproto.OpDBTables, `{"conn":"pg"}`); c.execs != 1 {
		t.Fatalf("first open ran %d docker inspect, want 1", c.execs)
	}
	if _, c := w.read(t, webproto.OpDBTables, `{"conn":"pg"}`); c.execs != 0 {
		t.Errorf("a second read ran %d docker inspect, want none", c.execs)
	}
	if _, c := w.read(t, webproto.OpDBRows, `{"conn":"pg","table":"users"}`); c.execs != 0 {
		t.Errorf("rows ran %d docker inspect, want none", c.execs)
	}
	w.mu.Lock()
	w.ip = "172.18.0.9" // the container was recreated
	w.mu.Unlock()
	w.pg.DropAll()
	w.pg.WaitOpen(0)
	time.Sleep(50 * time.Millisecond) // the channels' close reaches the client
	if _, c := w.read(t, webproto.OpDBTables, `{"conn":"pg"}`); c.execs != 1 {
		t.Errorf("after the container was recreated: %d docker inspect, want 1", c.execs)
	}
	tg := w.srv.Targets()
	if last := tg[len(tg)-1]; last != "172.18.0.9:5432" {
		t.Fatalf("the last read went to %s, want the new IP (targets %q)", last, tg)
	}
}
