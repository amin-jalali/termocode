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
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webRemoteDB is a web hub on a workspace whose one Postgres connection
// "baaytona" is c (saved in the project file). The SSH server srv is
// in-process and trusted; HOME, ~/.ssh and the secret store are fakes.
// The fake driver dials c's host:port through the tunnel it was given
// and wraps a dial error as pgx does.
func webRemoteDB(t *testing.T, srv *dbtest.SSHServer, c dbclient.Conn) (*Model, *webConn, *[]string) {
	t.Helper()
	home := sshTestHome(t, srv)
	if c.SSH != nil {
		host := c.SSH.Host + ":22"
		if err := dbclient.TrustHostKey(filepath.Join(home, ".ssh", "known_hosts"), host, srv.HostKey.PublicKey()); err != nil {
			t.Fatal(err)
		}
	}
	m := aiTestModel(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	conns := `{"connections":[` + webJSON(t, c) + `]}`
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ws.Root = root
	m.output = newOutputStore()
	m.databases = newDBState()
	m.databases.load(root)
	var mu sync.Mutex
	dialled := &[]string{}
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(ctx context.Context, c dbclient.Conn, _ string) (dbclient.Session, error) {
		if c.Dial == nil {
			return nil, errors.New("no tunnel")
		}
		addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
		mu.Lock()
		*dialled = append(*dialled, addr)
		mu.Unlock()
		nc, err := c.Dial(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to `user=app database=app`: %s (%s): dial error: %w", addr, c.Host, err)
		}
		_ = nc.Close()
		return &aiDBSession{name: "web-remote"}, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	webTestHub(t, &m)
	return &m, webTestSub(t, &m), dialled
}

// webCallWithin is webTestCallBg that fails when no answer comes within
// limit (the page would stay on Loading… forever).
func webCallWithin(t *testing.T, m *Model, c *webConn, op, args string, limit time.Duration) map[string]any {
	t.Helper()
	got := make(chan map[string]any, 1)
	go func() { got <- webTestCallBg(t, m, c, op, args) }()
	select {
	case f := <-got:
		return f
	case <-time.After(limit):
		t.Fatalf("%s gave no answer in %v: the page stays on Loading…", op, limit)
	}
	return nil
}

// Owner's case on the page: through SSH to 65.109.212.14 nothing listens
// on the server's 127.0.0.1:5432. The page says so in the same one line
// as the laptop: where, through SSH, and what to check.
func TestWebDBTunnelNothingListensSaysWhatToCheck(t *testing.T) {
	srv := dbtest.NewSSHServer() // Forward nil: refused, as sshd says when nothing listens
	srv.Password = "tunnel-pw-51"
	defer srv.Close()
	m, c, _ := webRemoteDB(t, srv, dbclient.Conn{ID: "baaytona", Driver: "postgres", Host: "127.0.0.1", Port: 5432, User: "app", Database: "app",
		SSH: &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "password"}})
	dbStoreSet(t, "db.baaytona.ssh", "tunnel-pw-51")
	f := webCallWithin(t, m, c, webproto.OpDBTables, `{"conn":"baaytona"}`, 5*time.Second)
	want := "Nothing answers at 127.0.0.1:5432 on 65.109.212.14 (through SSH). Is the container's port published?"
	if webTestErr(f) != "conflict" || f["msg"] != want {
		t.Fatalf("got %v\nwant msg %q", f, want)
	}
}

// A driver that ignores the deadline must not keep the page on
// Loading…: the read gives up at the connect deadline (plus a short
// grace), as the laptop does, and says what did not answer.
func TestWebDBReadGivesUpWhenTheDriverIgnoresTheDeadline(t *testing.T) {
	m, c, _ := webTestDB(t, nil)
	shortConnectTimeout(t, 200*time.Millisecond)
	release, returned := make(chan struct{}), make(chan struct{})
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(context.Context, dbclient.Conn, string) (dbclient.Session, error) {
		defer close(returned)
		<-release // stuck: never looks at ctx
		return nil, errors.New("late")
	}
	t.Cleanup(func() {
		close(release)
		select {
		case <-returned:
		case <-time.After(5 * time.Second):
		}
		dbSQLSessionFactory = old
	})
	f := webCallWithin(t, m, c, webproto.OpDBTables, `{"conn":"local"}`, 5*time.Second)
	if msg, _ := f["msg"].(string); webTestErr(f) != "conflict" || !strings.HasPrefix(msg, "No answer from ") || !strings.HasSuffix(msg, " in time.") {
		t.Fatalf("got %v, want a conflict: No answer from … in time.", f)
	}
}

// A saved connection of a container on a remote engine: the page's own
// tunnel asks the server for the container's IP by its name (docker
// inspect) and reads through it; after the container was recreated with
// a new IP, the next read finds the new one.
func TestWebDBRemoteContainerFindsItsIPByName(t *testing.T) {
	var mu sync.Mutex
	ip := "172.18.0.5"
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-52"
	srv.Exec = func(cmd string) (string, int) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(cmd, "docker inspect ") && strings.HasSuffix(cmd, " baaytona-db-1") {
			return "running\nbaaytona_default " + ip + "\n", 0
		}
		return "", 1
	}
	srv.Forward = func(_ context.Context, addr string) (net.Conn, error) {
		mu.Lock()
		want := ip + ":5432"
		mu.Unlock()
		if addr != want {
			return nil, errors.New("refused")
		}
		a, b := net.Pipe()
		go func() { _ = b.Close() }()
		return a, nil
	}
	defer srv.Close()
	m, c, _ := webRemoteDB(t, srv, dbclient.Conn{ID: "baaytona", Driver: "postgres", Host: "baaytona-db-1", Port: 5432, User: "postgres", Database: "postgres",
		SSH:       &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "password"},
		Container: &dbclient.ContainerRef{Name: "baaytona-db-1", Context: "baaytona", Engine: "docker", Network: "baaytona_default"}})
	dbStoreSet(t, "db.baaytona.ssh", "tunnel-pw-52")
	var tb webproto.DBTables
	webTestData(t, webCallWithin(t, m, c, webproto.OpDBTables, `{"conn":"baaytona"}`, 5*time.Second), &tb)
	if got := strings.Join(srv.Targets(), ","); got != "172.18.0.5:5432" {
		t.Fatalf("the tunnel went to %q, want the container's IP 172.18.0.5:5432", got)
	}
	mu.Lock()
	ip = "172.18.0.9" // docker compose up recreated it
	mu.Unlock()
	webTestData(t, webCallWithin(t, m, c, webproto.OpDBTables, `{"conn":"baaytona"}`, 5*time.Second), &tb)
	if got := srv.Targets(); got[len(got)-1] != "172.18.0.9:5432" {
		t.Fatalf("after the recreate the tunnel went to %q, want 172.18.0.9:5432", got)
	}
}

// The page's SSH and SSL forms change a saved connection of a remote
// container and keep its container (the IP lookup needs it).
func TestWebDBFormsKeepTheContainer(t *testing.T) {
	srv := dbtest.NewSSHServer()
	defer srv.Close()
	m, c, _ := webRemoteDB(t, srv, dbclient.Conn{ID: "baaytona", Driver: "postgres", Host: "baaytona-db-1", Port: 5432, User: "postgres", Database: "postgres",
		SSH:       &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "agent"},
		Container: &dbclient.ContainerRef{Name: "baaytona-db-1", Context: "baaytona", Engine: "docker", Network: "baaytona_default"}})
	file := filepath.Join(m.ws.Root, ".termocode", "databases.json")
	webDBAsk(t, m, c, webproto.OpDBSSH, `{"conn":"baaytona","host":"65.109.212.14","user":"ops","auth":"agent"}`, "ops@65.109.212.14")
	webTestLaptopSelect(t, m, "confirm")
	webDBAsk(t, m, c, webproto.OpDBSSL, `{"conn":"baaytona","mode":"disable"}`, "disable")
	webTestLaptopSelect(t, m, "confirm")
	_, _, list := dbLoadConfigs(m.ws.Root)
	if len(list) != 1 || list[0].SSH == nil || list[0].SSH.User != "ops" || list[0].SSL == nil {
		b, _ := os.ReadFile(file)
		t.Fatalf("not saved:\n%s", b)
	}
	want := dbclient.ContainerRef{Name: "baaytona-db-1", Context: "baaytona", Engine: "docker", Network: "baaytona_default"}
	if list[0].Container == nil || *list[0].Container != want {
		b, _ := os.ReadFile(file)
		t.Fatalf("the container is gone:\n%s", b)
	}
}
