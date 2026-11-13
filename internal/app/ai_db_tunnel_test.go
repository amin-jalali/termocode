//go:build unix

package app

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// aiTunEnv is a workspace whose one Postgres connection goes through an
// SSH tunnel to srv (in-process; HOME, ~/.ssh and the secret store are
// fakes). The Databases view has no tunnel open and no web page is on:
// the AI's db_* tools must open it themselves. The fake driver dials
// the connection's host:port through the dialer it was given.
type aiTunEnv struct {
	m      *Model
	mu     sync.Mutex
	call   aiIDECaller
	tools  map[string]ai.Tool
	home   string
	root   string
	dials  atomic.Int32 // TCP dials to the SSH server (one per tunnel)
	logins atomic.Int32 // database logins
}

func newAITunEnv(t *testing.T, srv *dbtest.SSHServer, c dbclient.Conn, trusted bool) *aiTunEnv {
	t.Helper()
	e := &aiTunEnv{home: sshTestHome(t, srv)}
	dbTunnelDial = func(ctx context.Context, n, a string) (net.Conn, error) {
		e.dials.Add(1)
		return srv.Dial(ctx, n, a)
	}
	if trusted {
		if err := dbclient.TrustHostKey(filepath.Join(e.home, ".ssh", "known_hosts"), c.SSH.Host+":22", srv.HostKey.PublicKey()); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	e.root = root
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(`{"connections":[`+webJSON(t, c)+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e.m = &Model{w: 100, h: 30}
	e.m.databases = newDBState()
	e.m.databases.load(root)
	e.call = func(ctx context.Context, fn aiIDEFunc) (string, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		out, _, err := fn(e.m)
		if e.m.web != nil {
			e.m.webAfterUpdate(nil) // as Update does after every message
		}
		return out, err
	}
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(ctx context.Context, c dbclient.Conn, _ string) (dbclient.Session, error) {
		if c.Dial == nil {
			return nil, errors.New("no tunnel")
		}
		nc, err := c.Dial(ctx, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
		if err != nil {
			return nil, err
		}
		_ = nc.Close()
		e.logins.Add(1)
		return &aiDBSession{name: "ai-tun"}, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	e.tools = map[string]ai.Tool{}
	for _, tl := range aiRedactResults(aiDBTools(root, e.call)) {
		e.tools[tl.Name] = tl
	}
	return e
}

// dbNoSSHForTest makes SSH tunnels read ~/.ssh from a temp home, serves
// a test ssh-agent with one key (the same on a laptop with an agent and
// on CI without one) and refuses every dial to an SSH server: a db_*
// tool that opens a tunnel on its own never reaches a real host from a
// test, and it fails at the dial, not at the agent.
func dbNoSSHForTest(t *testing.T) {
	t.Helper()
	webTestAgent(t)
	home := t.TempDir()
	oldHome, oldDial := dbSSHHome, dbTunnelDial
	dbSSHHome = func() string { return home }
	dbTunnelDial = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("test: no SSH server here")
	}
	t.Cleanup(func() { dbSSHHome, dbTunnelDial = oldHome, oldDial })
}

// ui runs fn on the model as Update would.
func (e *aiTunEnv) ui(fn func(m *Model)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fn(e.m)
}

// waitFor waits until cond holds on the model.
func (e *aiTunEnv) waitFor(t *testing.T, what string, cond func(m *Model) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ok := false
		e.ui(func(m *Model) { ok = cond(m) })
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type aiTunResult struct {
	out string
	err error
}

// runBg runs a tool off the test goroutine (it may wait for the user);
// the test ends only after it did (its context is cancelled first).
func (e *aiTunEnv) runBg(t *testing.T, name string, a map[string]any) <-chan aiTunResult {
	ch, done := make(chan aiTunResult, 1), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(done)
		out, err := e.tools[name].Run(ctx, a)
		ch <- aiTunResult{out, err}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	})
	return ch
}

func aiTunWait(t *testing.T, ch <-chan aiTunResult) aiTunResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("the tool gave no answer")
	}
	return aiTunResult{}
}

// aiTunForward answers every tunnel channel (a database that listens).
func aiTunForward(t *testing.T, srv *dbtest.SSHServer) {
	srv.Forward = func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		t.Cleanup(func() { _ = b.Close() })
		return a, nil
	}
}

func aiTunBaaytona() dbclient.Conn {
	return dbclient.Conn{ID: "baaytonadb", Name: "BaaytonaDB", Driver: "postgres", Host: "127.0.0.1", Port: 5432, User: "app", Database: "baaytona",
		SSH: &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "password"}}
}

// Owner's bug: db_schema on a connection behind an SSH tunnel (trusted
// host, the view never connected) opens the tunnel itself and answers;
// a second call reuses the same tunnel (one dial to the SSH server).
func TestAIDBSchemaOpensTheSSHTunnelAndReusesIt(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-81"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAITunEnv(t, srv, aiTunBaaytona(), true)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-81")

	for i := range 2 {
		r := aiTunWait(t, e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"}))
		if r.err != nil || !strings.Contains(r.out, "users") {
			t.Fatalf("call %d: out %q, err %v", i+1, r.out, r.err)
		}
	}
	if n := e.dials.Load(); n != 1 {
		t.Fatalf("%d dials to the SSH server for two calls, want 1 (the tunnel is reused)", n)
	}
	if got := strings.Join(srv.Targets(), ","); !strings.Contains(got, "127.0.0.1:5432") {
		t.Fatalf("the tunnel went to %q", got)
	}
	r := aiTunWait(t, e.runBg(t, "db_connections", nil))
	if r.err != nil || strings.Contains(r.out, "SSH tunnel closed") || strings.Contains(r.out, "Databases view (one key there)") ||
		!strings.Contains(r.out, "where: via ssh deploy@65.109.212.14") {
		t.Fatalf("db_connections: %q, %v", r.out, r.err)
	}
}

// An unknown SSH host key shows the same trust card the Databases view
// shows (no login sent before it); after yes the key is saved and the
// tool goes on and answers.
func TestAIDBUnknownHostKeyShowsTheTrustCardThenAnswers(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-82"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAITunEnv(t, srv, aiTunBaaytona(), false)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-82")

	ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"})
	e.waitFor(t, "the trust card", func(m *Model) bool { return m.confirmOpen && m.confirmKind == confirmKindDBTrustHost })
	if srv.AuthAttempts() != 0 {
		t.Fatal("a login was sent to an unknown host")
	}
	e.ui(func(m *Model) {
		if !strings.Contains(m.confirm.Title(), "65.109.212.14") {
			t.Errorf("card title %q", m.confirm.Title())
		}
		m.confirmOpen = false
		_ = m.handleConfirmSelect("trust")
	})
	r := aiTunWait(t, ch)
	if r.err != nil || !strings.Contains(r.out, "users") {
		t.Fatalf("after trust: out %q, err %v", r.out, r.err)
	}
	data, err := os.ReadFile(filepath.Join(e.home, ".ssh", "known_hosts"))
	if err != nil || !strings.Contains(string(data), "65.109.212.14 ssh-ed25519 ") {
		t.Fatalf("known_hosts = %q, %v", data, err)
	}
}

// Cancel on the trust card (or Esc) ends the tool with a short error;
// nothing is trusted and no login was sent.
func TestAIDBTrustCardCancelReturnsAnError(t *testing.T) {
	for _, how := range []string{"cancel", "esc"} {
		t.Run(how, func(t *testing.T) {
			srv := dbtest.NewSSHServer()
			srv.Password = "tunnel-pw-83"
			defer srv.Close()
			aiTunForward(t, srv)
			e := newAITunEnv(t, srv, aiTunBaaytona(), false)
			dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-83")
			ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb"})
			e.waitFor(t, "the trust card", func(m *Model) bool { return m.confirmOpen && m.confirmKind == confirmKindDBTrustHost })
			e.ui(func(m *Model) {
				m.confirmOpen = false
				if how == "cancel" {
					_ = m.handleConfirmSelect("cancel")
				} else {
					nm, _ := m.Update(confirm.CloseMsg{}) // Esc
					*m = nm.(Model)
				}
			})
			r := aiTunWait(t, ch)
			if r.err == nil || !strings.Contains(r.err.Error(), "not trusted") || len(r.err.Error()) > 200 {
				t.Fatalf("cancel: out %q, err %v", r.out, r.err)
			}
			if srv.AuthAttempts() != 0 || e.logins.Load() != 0 {
				t.Fatal("a login was sent after cancel")
			}
			if _, err := os.Stat(filepath.Join(e.home, ".ssh", "known_hosts")); err == nil {
				data, _ := os.ReadFile(filepath.Join(e.home, ".ssh", "known_hosts"))
				if strings.Contains(string(data), "65.109.212.14") {
					t.Fatal("the key was saved after cancel")
				}
			}
		})
	}
}

// A connection of a container on an ssh:// docker context: the tool
// opens the tunnel, asks the server for the container's IP by its name
// and reads through it.
func TestAIDBContainerOnSSHContextResolvesAndAnswers(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-84"
	srv.Exec = func(cmd string) (string, int) {
		if strings.HasPrefix(cmd, "docker inspect ") && strings.HasSuffix(cmd, " baaytona-db-1") {
			return "running\nbaaytona_default 172.18.0.5\n", 0
		}
		return "", 1
	}
	srv.Forward = func(_ context.Context, addr string) (net.Conn, error) {
		if addr != "172.18.0.5:5432" {
			return nil, errors.New("refused")
		}
		a, b := net.Pipe()
		t.Cleanup(func() { _ = b.Close() })
		return a, nil
	}
	defer srv.Close()
	e := newAITunEnv(t, srv, dbclient.Conn{ID: "baaytona-db-1", Driver: "postgres", Host: "baaytona-db-1", Port: 5432, User: "postgres", Database: "postgres",
		SSH:       &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "password"},
		Container: &dbclient.ContainerRef{Name: "baaytona-db-1", Context: "baaytona", Engine: "docker", Network: "baaytona_default"}}, true)
	dbStoreSet(t, "db.baaytona-db-1.ssh", "tunnel-pw-84")
	r := aiTunWait(t, e.runBg(t, "db_schema", map[string]any{"conn": "baaytona-db-1", "schema": "public"}))
	if r.err != nil || !strings.Contains(r.out, "users") {
		t.Fatalf("out %q, err %v", r.out, r.err)
	}
	if got := strings.Join(srv.Targets(), ","); got != "172.18.0.5:5432" {
		t.Fatalf("the tunnel went to %q, want the container's IP", got)
	}
}

// A Codex / Claude Code step with Tools: all gets db_connection (and the
// db_* tools) on its termocode MCP server: the model can open a
// connection itself.
func TestAICLIStepToolsHaveDBConnection(t *testing.T) {
	m := aiTestModel(t)
	root := t.TempDir()
	tools := aiFilterTools(m.aiChatToolsBy(root, nil, aiRunCaller{}), agentdag.ToolRule{Preset: agentdag.PresetAll})
	names := map[string]bool{}
	for _, s := range aiStepMCPSpecs(tools) {
		names[s.Name] = true
	}
	for _, want := range []string{"db_connection", "db_connections", "db_schema", "db_query"} {
		if !names[want] {
			t.Errorf("a step with Tools: all lacks %s", want)
		}
	}
}
