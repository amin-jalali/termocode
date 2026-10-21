package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/crypto/ssh"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// sshTestHome isolates HOME, the config dir and the agent (never the
// user's real ~/.ssh) and routes tunnels to srv.
func sshTestHome(t *testing.T, srv *dbtest.SSHServer) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("SSH_AUTH_SOCK", "")
	_ = os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	oldHome, oldDial, oldRedis := dbSSHHome, dbTunnelDial, dbRedisDial
	dbSSHHome = func() string { return home }
	dbTunnelDial = srv.Dial
	dbRedisDial = nil
	t.Cleanup(func() { dbSSHHome, dbTunnelDial, dbRedisDial = oldHome, oldDial, oldRedis })
	return home
}

// dbRunFor runs cmd (and the commands of a batch) and returns the first
// message of type T.
func dbRunFor[T any](t *testing.T, cmd tea.Cmd) (T, bool) {
	t.Helper()
	var zero T
	if cmd == nil {
		return zero, false
	}
	ch := make(chan tea.Msg, 32)
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		go func() {
			msg := c()
			if b, ok := msg.(tea.BatchMsg); ok {
				for _, x := range b {
					if x != nil {
						walk(x)
					}
				}
				return
			}
			ch <- msg
		}()
	}
	walk(cmd)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-ch:
			if v, ok := msg.(T); ok {
				return v, true
			}
		case <-deadline:
			return zero, false
		}
	}
}

// sshCacheModel is a model with one saved Redis connection behind an SSH
// tunnel (user file), served by a fake Redis through srv.
func sshCacheModel(t *testing.T, srv *dbtest.SSHServer, sshCfg dbclient.SSHConfig) (*Model, *dbtest.FakeRedis) {
	t.Helper()
	f := dbtest.NewFakeRedis()
	t.Cleanup(f.Close)
	f.SetString(0, "greeting", "hello")
	srv.Forward = func(ctx context.Context, addr string) (net.Conn, error) {
		if addr != "cache.internal:6379" {
			return nil, errors.New("unexpected target " + addr)
		}
		return f.Dial(ctx, "tcp", addr)
	}
	s := newDBState()
	s.root, s.visible, s.loaded = t.TempDir(), true, true
	cfg := sshCfg
	c := dbclient.Conn{ID: "cache", Name: "cache", Driver: "redis", Host: "cache.internal", Port: 6379, Source: dbclient.SourceUser, SSH: &cfg}
	s.conns = []dbclient.Conn{c}
	s.userConns = []dbclient.Conn{c}
	s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.ID, path: c.ID, conn: c.ID, detail: dbConnDetail(c)}
	dir, err := configDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDBConfig(filepath.Join(dir, "db", "connections.json"), s.userConns); err != nil {
		t.Fatal(err)
	}
	m := dbTestModel(s)
	m.w, m.h = 100, 30
	t.Cleanup(func() { m.databasesClose(false) })
	return m, f
}

func dbStoreSet(t *testing.T, name, value string) {
	t.Helper()
	store := databaseSecretStore()
	if store == nil {
		t.Fatal("no secret store")
	}
	if err := store.Set(name, value); err != nil {
		t.Fatal(err)
	}
}

func dbConnectAndDeliver(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	msg, ok := dbRunFor[dbConnectMsg](t, cmd)
	if !ok {
		t.Fatal("no connect result")
	}
	return m.onDBConnect(msg)
}

func TestDBTunnelUnknownHostCardTrustThenConnect(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-77"
	defer srv.Close()
	home := sshTestHome(t, srv)
	m, _ := sshCacheModel(t, srv, dbclient.SSHConfig{Host: "bastion.example.com", User: "deploy", Auth: "password"})
	dbStoreSet(t, "db.cache.ssh", "tunnel-pw-77")

	dbConnectAndDeliver(t, m, m.databaseConnect("cache"))
	if !m.confirmOpen || m.confirmKind != confirmKindDBTrustHost {
		t.Fatalf("no unknown-host card (open=%v kind=%d); row: %q", m.confirmOpen, m.confirmKind, m.databases.nodes["cache"].problem)
	}
	card := ansi.Strip(m.confirm.View())
	fp := ssh.FingerprintSHA256(srv.HostKey.PublicKey())
	for _, want := range []string{"Unknown host bastion.example.com", fp, "Trust and save to known_hosts", "ED25519"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card lacks %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, "tunnel-pw-77") || srv.AuthAttempts() != 0 {
		t.Fatal("the SSH password left before the host was trusted")
	}
	if m.databases.tunnel("cache") != nil {
		t.Fatal("a tunnel was kept for an untrusted host")
	}

	cmd, ok := m.handleDBConfirm("trust")
	if !ok {
		t.Fatal("trust not handled")
	}
	data, err := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil || !strings.Contains(string(data), "bastion.example.com ssh-ed25519 ") {
		t.Fatalf("known_hosts = %q, %v", data, err)
	}
	dbConnectAndDeliver(t, m, cmd)
	if m.databases.sessions["cache"] == nil {
		t.Fatalf("not connected after trust; row: %q", m.databases.nodes["cache"].problem)
	}
	tun := m.databases.tunnel("cache")
	if !tun.Alive() {
		t.Fatal("the tunnel is not kept with the connection")
	}
	if got := m.databases.rowFooter(dbTreeRow{kind: dbNodeConnection, path: "cache", conn: "cache"}); !strings.Contains(got, "tunnel up") {
		t.Fatalf("footer = %q", got)
	}
	if !strings.Contains(m.databases.nodes["cache"].detail, "ssh") {
		t.Fatalf("row detail %q has no ssh tag", m.databases.nodes["cache"].detail)
	}

	// Disconnect closes the tunnel; so does hiding the view.
	_ = m.databaseDisconnectNow("cache")
	if tun.Alive() || m.databases.tunnel("cache") != nil {
		t.Fatal("disconnect left the SSH tunnel open")
	}
	dbConnectAndDeliver(t, m, m.databaseConnect("cache"))
	tun = m.databases.tunnel("cache")
	if !tun.Alive() {
		t.Fatal("reconnect did not open a tunnel")
	}
	m.databasesClose(true)
	if tun.Alive() {
		t.Fatal("hiding the view left the SSH tunnel open")
	}
}

func TestDBTunnelCancelTrustLeavesNoKeyAndSaysSo(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-77"
	defer srv.Close()
	home := sshTestHome(t, srv)
	m, _ := sshCacheModel(t, srv, dbclient.SSHConfig{Host: "bastion", User: "deploy", Auth: "password"})
	dbStoreSet(t, "db.cache.ssh", "tunnel-pw-77")
	dbConnectAndDeliver(t, m, m.databaseConnect("cache"))
	if _, ok := m.handleDBConfirm("cancel"); !ok {
		t.Fatal("cancel not handled")
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "known_hosts")); !os.IsNotExist(err) {
		t.Fatal("known_hosts written without trust")
	}
	if p := m.databases.nodes["cache"].problem; !strings.Contains(p, "not trusted") {
		t.Fatalf("row problem = %q", p)
	}
}

func TestDBTunnelChangedHostKeyRefusesWithoutCard(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-77"
	defer srv.Close()
	home := sshTestHome(t, srv)
	m, _ := sshCacheModel(t, srv, dbclient.SSHConfig{Host: "bastion", User: "deploy", Auth: "password"})
	dbStoreSet(t, "db.cache.ssh", "tunnel-pw-77")
	if err := dbclient.TrustHostKey(filepath.Join(home, ".ssh", "known_hosts"), "bastion:22", dbtest.NewSSHServer().HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	dbConnectAndDeliver(t, m, m.databaseConnect("cache"))
	if m.confirmOpen {
		t.Fatal("a changed host key offered a trust card")
	}
	if p := m.databases.nodes["cache"].problem; !strings.Contains(p, "changed; check with your admin") {
		t.Fatalf("row problem = %q", p)
	}
	if m.databases.sessions["cache"] != nil || m.databases.tunnel("cache") != nil || srv.AuthAttempts() != 0 {
		t.Fatal("connected (or logged in) to a host whose key changed")
	}
}

func writeTestKey(t *testing.T, path, passphrase string) ssh.PublicKey {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKey(priv, "t") // "": no passphrase
	if passphrase != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "t", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(priv)
	return signer.PublicKey()
}

func TestDBTunnelAsksKeyPassphraseAndStoresItOnlyInSecrets(t *testing.T) {
	srv := dbtest.NewSSHServer()
	defer srv.Close()
	home := sshTestHome(t, srv)
	keyPath := filepath.Join(home, ".ssh", "id_db")
	srv.AuthorizedKeys = []ssh.PublicKey{writeTestKey(t, keyPath, "correct horse 9")}
	if err := dbclient.TrustHostKey(filepath.Join(home, ".ssh", "known_hosts"), "bastion:22", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	m, _ := sshCacheModel(t, srv, dbclient.SSHConfig{Host: "bastion", User: "deploy", Auth: "key", KeyFile: "~/.ssh/id_db"})

	dbConnectAndDeliver(t, m, m.databaseConnect("cache"))
	if !m.promptOpen || m.promptKind != promptKindDBSSHSecret {
		t.Fatalf("no passphrase prompt; row: %q", m.databases.nodes["cache"].problem)
	}
	if !strings.Contains(ansi.Strip(m.prompt.View()), "id_db") {
		t.Fatalf("prompt does not name the key:\n%s", ansi.Strip(m.prompt.View()))
	}
	// A wrong one asks again.
	cmd, _ := m.databaseSSHPrompt(promptKindDBSSHSecret, "wrong one 1")
	dbConnectAndDeliver(t, m, cmd)
	if !m.promptOpen || m.promptKind != promptKindDBSSHSecret || !strings.Contains(ansi.Strip(m.prompt.View()), "not accepted") {
		t.Fatal("a wrong passphrase did not ask again")
	}
	cmd, _ = m.databaseSSHPrompt(promptKindDBSSHSecret, "correct horse 9")
	dbConnectAndDeliver(t, m, cmd)
	if m.databases.sessions["cache"] == nil {
		t.Fatalf("not connected; row: %q", m.databases.nodes["cache"].problem)
	}
	if v, ok, _ := databaseSecretStore().Get("db.cache.sshkey"); !ok || v != "correct horse 9" {
		t.Fatal("the passphrase is not in the secret store as db.cache.sshkey")
	}
	dir, _ := configDir()
	conf, _ := os.ReadFile(filepath.Join(dir, "db", "connections.json"))
	if strings.Contains(string(conf), "correct horse") || strings.Contains(string(conf), "wrong one") {
		t.Fatal("a passphrase reached the connection file")
	}
}

func TestDBAddSSHTunnelFlowSavesObjectAndTests(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "flow-pw-123"
	defer srv.Close()
	home := sshTestHome(t, srv)
	if err := dbclient.TrustHostKey(filepath.Join(home, ".ssh", "known_hosts"), "[jump.example.net]:2200", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	m, _ := sshCacheModel(t, srv, dbclient.SSHConfig{})
	// Start from a row without a tunnel.
	m.databases.conns[0].SSH, m.databases.userConns[0].SSH = nil, nil

	if cmd, ok := m.dispatchDatabasesPalette("db-add-tunnel"); !ok || cmd == nil && !m.promptOpen {
		t.Fatal("palette item did nothing")
	}
	if m.promptKind != promptKindDBSSHHost {
		t.Fatalf("first prompt = %d", m.promptKind)
	}
	m.databaseSSHPrompt(promptKindDBSSHHost, "jump.example.net")
	m.databaseSSHPrompt(promptKindDBSSHPort, "2200")
	m.databaseSSHPrompt(promptKindDBSSHUser, "ops")
	if !m.pickerOpen || m.pickerKind != pickerKindDBSSHAuth {
		t.Fatal("no auth picker")
	}
	m.databaseSSHPicker(dbclient.SSHAuthPassword)
	if !m.promptOpen || m.promptKind != promptKindDBSSHSecret {
		t.Fatal("no masked password prompt")
	}
	cmd, _ := m.databaseSSHPrompt(promptKindDBSSHSecret, "flow-pw-123")
	msg, ok := dbRunFor[dbTestMsg](t, cmd)
	if !ok {
		t.Fatal("no test ran after the tunnel was added")
	}
	if msg.err != nil || msg.via != "jump.example.net" || !msg.existing {
		t.Fatalf("test through the tunnel: err=%v via=%q", msg.err, msg.via)
	}
	m.onDBTest(msg)

	dir, _ := configDir()
	conf, _ := os.ReadFile(filepath.Join(dir, "db", "connections.json"))
	for _, want := range []string{`"host": "jump.example.net"`, `"port": 2200`, `"user": "ops"`, `"auth": "password"`} {
		if !strings.Contains(string(conf), want) {
			t.Fatalf("connections.json lacks %s:\n%s", want, conf)
		}
	}
	if strings.Contains(string(conf), "flow-pw-123") {
		t.Fatal("the SSH password reached the connection file")
	}
	if v, _, _ := databaseSecretStore().Get("db.cache.ssh"); v != "flow-pw-123" {
		t.Fatal("the SSH password is not in the secret store")
	}
	if !strings.Contains(m.databases.nodes["cache"].detail, "ssh") {
		t.Fatal("row lacks the ssh tag")
	}
	if srv.Logins() != 1 || len(srv.Targets()) == 0 || srv.Targets()[0] != "cache.internal:6379" {
		t.Fatalf("logins=%d targets=%v", srv.Logins(), srv.Targets())
	}
}

func TestDBSSLSettingsFlowWritesObject(t *testing.T) {
	srv := dbtest.NewSSHServer()
	defer srv.Close()
	sshTestHome(t, srv)
	m, _ := sshCacheModel(t, srv, dbclient.SSHConfig{})
	m.databases.conns[0].SSH, m.databases.userConns[0].SSH = nil, nil

	m.dispatchDatabasesPalette("db-ssl")
	if !m.pickerOpen || m.pickerKind != pickerKindDBSSLMode {
		t.Fatal("no SSL mode picker")
	}
	m.databaseSSHPicker(dbclient.SSLVerifyCA)
	m.databaseSSHPrompt(promptKindDBSSLCA, "certs/ca.pem")
	m.databaseSSHPrompt(promptKindDBSSLCert, "certs/client.pem")
	cmd, _ := m.databaseSSHPrompt(promptKindDBSSLKey, "certs/client.key")
	msg, ok := dbRunFor[dbTestMsg](t, cmd)
	if !ok || msg.err == nil || !strings.Contains(msg.err.Error(), "CA file") {
		t.Fatalf("test with a missing CA: ok=%v err=%v", ok, msg.err)
	}
	dir, _ := configDir()
	conf, _ := os.ReadFile(filepath.Join(dir, "db", "connections.json"))
	for _, want := range []string{`"mode": "verify-ca"`, `"ca": "certs/ca.pem"`, `"cert": "certs/client.pem"`, `"key": "certs/client.key"`} {
		if !strings.Contains(string(conf), want) {
			t.Fatalf("connections.json lacks %s:\n%s", want, conf)
		}
	}
	if !strings.Contains(m.databases.nodes["cache"].detail, "ssl") {
		t.Fatalf("row detail %q lacks the ssl tag", m.databases.nodes["cache"].detail)
	}
}

func TestDBAIRidesTheOpenTunnelOnly(t *testing.T) {
	c := dbclient.Conn{ID: "cache", Driver: "redis", SSH: &dbclient.SSHConfig{Host: "b"}}
	if _, err := dbAIRoute(t.TempDir(), c, &dbAISnapshot{}); err == nil || !strings.Contains(err.Error(), "ask the user to connect") {
		t.Fatalf("closed tunnel: err = %v", err)
	}
	called := false
	snap := &dbAISnapshot{dial: func(context.Context, string, string) (net.Conn, error) { called = true; return nil, errors.New("x") }}
	got, err := dbAIRoute(t.TempDir(), c, snap)
	if err != nil || got.Dial == nil {
		t.Fatalf("open tunnel: %v", err)
	}
	_, _ = got.Dial(context.Background(), "tcp", "a:1")
	if !called {
		t.Fatal("the AI session does not dial through the user's tunnel")
	}
	s := newDBState()
	if _, err := s.dbDialFor(c); !errors.Is(err, dbclient.ErrTunnelClosed) {
		t.Fatalf("no tunnel: err = %v", err)
	}
}

func TestDBResolveConnPathsCopiesObjects(t *testing.T) {
	orig := dbclient.Conn{SSH: &dbclient.SSHConfig{Host: "b", KeyFile: "~/.ssh/k"}, SSL: &dbclient.SSLConfig{CA: "ca.pem"}}
	got := dbResolveConnPaths(orig, "/ws", "/home/u")
	if got.SSH.KeyFile != "/home/u/.ssh/k" || got.SSL.CA != "/ws/ca.pem" {
		t.Fatalf("resolved = %+v %+v", got.SSH, got.SSL)
	}
	if orig.SSH.KeyFile != "~/.ssh/k" || orig.SSL.CA != "ca.pem" {
		t.Fatal("the saved connection was changed")
	}
}

func TestDBPerDatabaseSessionUsesTheOpenTunnel(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "pw-per-db-1"
	srv.Forward = func(context.Context, string) (net.Conn, error) { return nil, errors.New("closed") }
	defer srv.Close()
	home := sshTestHome(t, srv)
	if err := dbclient.TrustHostKey(filepath.Join(home, ".ssh", "known_hosts"), "b:22", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := dbclient.Conn{ID: "pg", Driver: "postgres", Host: "db.internal", Port: 5432, Database: "app", SSH: &dbclient.SSHConfig{Host: "b", User: "u", Auth: "password"}}
	tun, err := dbOpenTunnel(ctx, c, dbSSHSecrets{password: "pw-per-db-1"})
	if err != nil {
		t.Fatal(err)
	}
	s := newDBState()
	s.visible = true
	s.conns = []dbclient.Conn{c}
	s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.ID, path: c.ID, conn: c.ID}
	s.addIntrospection(c.ID, dbclient.IntrospectionResult{Databases: []string{"other"}})
	s.sessions[c.ID] = &fakeDBSession{}
	s.setTunnel(c.ID, tun)
	m := dbTestModel(s)
	var dialled []bool
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(_ context.Context, c dbclient.Conn, _ string) (dbclient.Session, error) {
		dialled = append(dialled, c.Dial != nil)
		return &fakeDBSession{}, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	cmd := m.databaseExpand("pg/db/other")
	if _, ok := dbRunFor[dbConnectMsg](t, cmd); !ok || len(dialled) != 1 || !dialled[0] {
		t.Fatalf("per-database session did not get the tunnel dialer: %v", dialled)
	}
	// A dead tunnel: no session is opened around it.
	_ = tun.Close()
	n := s.nodes["pg/db/other"]
	n.loading = false
	s.nodes["pg/db/other"] = n
	delete(s.sessions, "pg/db/other")
	_ = m.databaseExpand("pg/db/other")
	if n := s.nodes["pg/db/other"]; n.loading || !strings.Contains(n.problem, "SSH tunnel closed") {
		t.Fatalf("dead tunnel: loading=%v problem=%q", n.loading, n.problem)
	}
}

func TestDBTunnelJumpHostCardNamesTheHopThenConnects(t *testing.T) {
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
	home := sshTestHome(t, gw) // the client dials the jump host only
	gw.AuthorizedKeys = []ssh.PublicKey{writeTestKey(t, filepath.Join(home, ".ssh", "id_ed25519"), "")}
	known := filepath.Join(home, ".ssh", "known_hosts")
	if err := dbclient.TrustHostKey(known, "bastion.internal:22", bastion.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	m, _ := sshCacheModel(t, bastion, dbclient.SSHConfig{Host: "bastion.internal", User: "deploy", Auth: "password", Jump: []string{"ops@gw.example.net"}})
	dbStoreSet(t, "db.cache.ssh", "tunnel-pw-77")

	dbConnectAndDeliver(t, m, m.databaseConnect("cache"))
	if !m.confirmOpen || m.confirmKind != confirmKindDBTrustHost {
		t.Fatalf("no unknown-host card; row: %q", m.databases.nodes["cache"].problem)
	}
	card := ansi.Strip(m.confirm.View())
	for _, want := range []string{"Unknown jump host gw.example.net", ssh.FingerprintSHA256(gw.HostKey.PublicKey())} {
		if !strings.Contains(card, want) {
			t.Fatalf("card lacks %q:\n%s", want, card)
		}
	}
	if gw.AuthAttempts() != 0 || bastion.AuthAttempts() != 0 {
		t.Fatal("a login was sent before the jump host was trusted")
	}
	cmd, ok := m.handleDBConfirm("trust")
	if !ok {
		t.Fatal("trust not handled")
	}
	dbConnectAndDeliver(t, m, cmd)
	tun := m.databases.tunnel("cache")
	if m.databases.sessions["cache"] == nil || !tun.Alive() {
		t.Fatalf("not connected through the jump host; row: %q", m.databases.nodes["cache"].problem)
	}
	if j := tun.Target().Jumps; len(j) != 1 || j[0].Addr() != "gw.example.net:22" {
		t.Fatalf("jumps = %+v", j)
	}
	_ = m.databaseDisconnectNow("cache")
	if tun.Alive() || gw.WaitOpen(0) != 0 || bastion.WaitOpen(0) != 0 {
		t.Fatal("disconnect left a hop open")
	}
}
