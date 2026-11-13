//go:build unix

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

	"golang.org/x/crypto/ssh"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTunnelDB is a web hub on a workspace whose Postgres connection "tun"
// goes through an SSH tunnel to srv (in-process; HOME, ~/.ssh and the
// agent are fakes). The fake session dials the database through the
// tunnel it was given, so a read only works through it; dialled lists
// what the SSH server forwarded to and pws the passwords tried.
type webTunnelDB struct {
	m       *Model
	c       *webConn
	home    string
	mu      sync.Mutex
	pws     []string
	accept  func(pw string) error // nil: every password works
	srvHits func() []string
}

func newWebTunnelDB(t *testing.T, srv, first *dbtest.SSHServer, cfg dbclient.SSHConfig) *webTunnelDB {
	t.Helper()
	home := sshTestHome(t, first)
	srv.Forward = func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		t.Cleanup(func() { _ = b.Close() })
		return a, nil
	}
	m := aiTestModel(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	conns := `{"connections":[{"id":"tun","driver":"postgres","host":"db.internal","port":5432,"user":"app","database":"app","ssh":` + webJSON(t, cfg) + `}]}`
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ws.Root = root
	m.output = newOutputStore()
	m.databases = newDBState()
	m.databases.load(root)
	w := &webTunnelDB{m: &m, home: home, srvHits: srv.Targets}
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(ctx context.Context, c dbclient.Conn, pw string) (dbclient.Session, error) {
		w.mu.Lock()
		w.pws = append(w.pws, pw)
		accept := w.accept
		w.mu.Unlock()
		if c.Dial == nil {
			return nil, errors.New("no tunnel")
		}
		nc, err := c.Dial(ctx, "tcp", "db.internal:5432")
		if err != nil {
			return nil, err
		}
		_ = nc.Close()
		if accept != nil {
			if err := accept(pw); err != nil {
				return nil, err
			}
		}
		return &aiDBSession{name: "web-tun"}, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	webTestHub(t, &m)
	w.c = webTestSub(t, &m)
	return w
}

func webJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := webData(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (w *webTunnelDB) call(t *testing.T, op, args string) map[string]any {
	t.Helper()
	return webTestCallBg(t, w.m, w.c, op, args)
}

// need is the err data of a failed op (what the page must ask for).
func webNeed(t *testing.T, f map[string]any) map[string]any {
	t.Helper()
	if webTestErr(f) != "conflict" {
		t.Fatalf("want a conflict that needs something, got %v", f)
	}
	d, _ := f["data"].(map[string]any)
	if d == nil {
		t.Fatalf("no need in %v", f)
	}
	return d
}

// Spec: the page opens a database behind an SSH tunnel by itself (the
// laptop has no tunnel open). The first time, the SSH server's key is
// unknown: the page gets the host and its fingerprint (and no login was
// sent); db.trust_host with that fingerprint saves it to known_hosts
// (another fingerprint saves nothing), and the read then works through
// the tunnel, which later reads reuse. Every trust leaves an audit line.
func TestWebDBTunnelTrustHostThenTables(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-77"
	defer srv.Close()
	w := newWebTunnelDB(t, srv, srv, dbclient.SSHConfig{Host: "bastion.example.com", User: "deploy", Auth: "password"})
	dbStoreSet(t, "db.tun.ssh", "tunnel-pw-77")
	fp := ssh.FingerprintSHA256(srv.HostKey.PublicKey())

	f := w.call(t, webproto.OpDBTables, `{"conn":"tun"}`)
	need := webNeed(t, f)
	if need["need"] != "host_key" || need["host"] != "bastion.example.com" || need["fingerprint"] != fp || need["key_type"] != "ED25519" || need["jump"] == true {
		t.Fatalf("need %v", need)
	}
	if msg, _ := f["msg"].(string); !strings.Contains(msg, "bastion.example.com") {
		t.Errorf("msg %q", msg)
	}
	if srv.AuthAttempts() != 0 {
		t.Fatal("a login was sent to an unknown host")
	}
	known := filepath.Join(w.home, ".ssh", "known_hosts")
	if f := w.call(t, "db.trust_host", `{"conn":"tun","fingerprint":"SHA256:not-this-one"}`); webTestErr(f) != "conflict" {
		t.Fatalf("another fingerprint: %v", f)
	}
	if _, err := os.Stat(known); err == nil {
		t.Fatal("known_hosts written for another fingerprint")
	}
	if f := w.call(t, "db.trust_host", `{"conn":"tun","fingerprint":"`+fp+`"}`); webTestErr(f) != "" {
		t.Fatalf("trust: %v", f)
	}
	data, err := os.ReadFile(known)
	if err != nil || !strings.Contains(string(data), "bastion.example.com ssh-ed25519 ") {
		t.Fatalf("known_hosts = %q, %v", data, err)
	}
	var tb webproto.DBTables
	webTestData(t, w.call(t, webproto.OpDBTables, `{"conn":"tun"}`), &tb)
	if len(tb.Tables) != 2 || strings.Join(w.srvHits(), ",") != "db.internal:5432" {
		t.Fatalf("tables %+v through %q", tb, w.srvHits())
	}
	webTestData(t, w.call(t, webproto.OpDBTables, `{"conn":"tun"}`), &tb)
	if srv.Logins() != 1 {
		t.Errorf("%d SSH logins for two reads, want the one tunnel reused", srv.Logins())
	}
	if a := webTestAudit(w.m); !strings.Contains(a, "db.trust_host  tun bastion.example.com "+fp) || strings.Contains(a, "tunnel-pw-77") {
		t.Errorf("audit:\n%s", a)
	}
	if f := w.call(t, "db.trust_host", `{"conn":"tun","fingerprint":"`+fp+`"}`); webTestErr(f) != "conflict" {
		t.Fatalf("a second trust with nothing asked: %v", f)
	}
	w.m.web.stop()
	if srv.WaitOpen(0) != 0 {
		t.Fatal("the page's tunnel outlived the hub")
	}
}

// Spec: an unknown jump host is asked about by its own name and key (the
// jump flag set); once trusted, the read goes through both hops.
func TestWebDBTunnelJumpHostTrusted(t *testing.T) {
	bastion := dbtest.NewSSHServer()
	bastion.Password = "tunnel-pw-77"
	defer bastion.Close()
	gw := dbtest.NewSSHServer()
	defer gw.Close()
	gw.Forward = func(ctx context.Context, addr string) (net.Conn, error) {
		if addr != "bastion.internal:22" {
			return nil, errors.New("unexpected target " + addr)
		}
		return bastion.Dial(ctx, "tcp", addr)
	}
	w := newWebTunnelDB(t, bastion, gw, dbclient.SSHConfig{Host: "bastion.internal", User: "deploy", Auth: "password", Jump: []string{"ops@gw.example.net"}})
	gw.AuthorizedKeys = []ssh.PublicKey{writeTestKey(t, filepath.Join(w.home, ".ssh", "id_ed25519"), "")}
	if err := dbclient.TrustHostKey(filepath.Join(w.home, ".ssh", "known_hosts"), "bastion.internal:22", bastion.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	dbStoreSet(t, "db.tun.ssh", "tunnel-pw-77")
	fp := ssh.FingerprintSHA256(gw.HostKey.PublicKey())
	need := webNeed(t, w.call(t, webproto.OpDBTables, `{"conn":"tun"}`))
	if need["need"] != "host_key" || need["host"] != "gw.example.net" || need["fingerprint"] != fp || need["jump"] != true {
		t.Fatalf("need %v", need)
	}
	if gw.AuthAttempts() != 0 || bastion.AuthAttempts() != 0 {
		t.Fatal("a login was sent before the jump host was trusted")
	}
	if f := w.call(t, "db.trust_host", `{"conn":"tun","fingerprint":"`+fp+`"}`); webTestErr(f) != "" {
		t.Fatalf("trust: %v", f)
	}
	var tb webproto.DBTables
	webTestData(t, w.call(t, webproto.OpDBTables, `{"conn":"tun"}`), &tb)
	if len(tb.Tables) != 2 || strings.Join(w.srvHits(), ",") != "db.internal:5432" {
		t.Fatalf("tables %+v through %q", tb, w.srvHits())
	}
}
