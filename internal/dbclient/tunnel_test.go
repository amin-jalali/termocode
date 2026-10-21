package dbclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// sshHome is a temp home with an empty ~/.ssh (never the real one).
func sshHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSH_AUTH_SOCK", "")
	return home
}

func trustServer(t *testing.T, home, host string, srv *dbtest.SSHServer) {
	t.Helper()
	if err := TrustHostKey(filepath.Join(home, ".ssh", "known_hosts"), host, srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
}

func newClientKey(t *testing.T) (ssh.Signer, ed25519.PrivateKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s, priv
}

func writeKeyFile(t *testing.T, path string, priv ed25519.PrivateKey, passphrase string, mode os.FileMode) {
	t.Helper()
	var block *pem.Block
	var err error
	if passphrase != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "test", []byte(passphrase))
	} else {
		block, err = ssh.MarshalPrivateKey(priv, "test")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func openTestTunnel(t *testing.T, cfg SSHConfig, opts TunnelOptions) (*Tunnel, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tun, err := OpenTunnel(ctx, cfg, opts)
	if tun != nil {
		t.Cleanup(func() { _ = tun.Close() })
	}
	return tun, err
}

func TestTunnelUnknownHostShowsFingerprintThenTrustSaves(t *testing.T) {
	home := sshHome(t)
	srv := dbtest.NewSSHServer()
	srv.Password = "s3cret-pw"
	defer srv.Close()
	cfg := SSHConfig{Host: "bastion.example.com", User: "deploy", Auth: "password"}
	opts := TunnelOptions{Home: home, Password: "s3cret-pw", Dial: srv.Dial}

	_, err := openTestTunnel(t, cfg, opts)
	var unknown *UnknownHostKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want UnknownHostKeyError", err)
	}
	want := ssh.FingerprintSHA256(srv.HostKey.PublicKey())
	if unknown.Fingerprint != want || !strings.HasPrefix(want, "SHA256:") {
		t.Fatalf("fingerprint = %q, want %q", unknown.Fingerprint, want)
	}
	if !strings.Contains(err.Error(), "bastion.example.com") || !strings.Contains(err.Error(), want) {
		t.Fatalf("message = %q", err.Error())
	}
	if srv.AuthAttempts() != 0 {
		t.Fatalf("the password was sent to an unverified host (%d attempts)", srv.AuthAttempts())
	}
	if err := TrustHostKey(unknown.KnownHosts, unknown.Host, unknown.Key); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("known_hosts mode = %v, want 0600", fi.Mode().Perm())
	}
	tun, err := openTestTunnel(t, cfg, opts)
	if err != nil {
		t.Fatalf("after trust: %v", err)
	}
	if !tun.Alive() || srv.Logins() != 1 {
		t.Fatalf("alive=%v logins=%d", tun.Alive(), srv.Logins())
	}
}

func TestTunnelChangedHostKeyIsRefusedBeforeAuth(t *testing.T) {
	home := sshHome(t)
	srv := dbtest.NewSSHServer()
	srv.Password = "s3cret-pw"
	defer srv.Close()
	other := dbtest.NewSSHServer() // the key the user saw before
	trustServer(t, home, "bastion.example.com:22", other)

	_, err := openTestTunnel(t, SSHConfig{Host: "bastion.example.com", User: "deploy", Auth: "password"}, TunnelOptions{Home: home, Password: "s3cret-pw", Dial: srv.Dial})
	var changed *HostKeyChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("err = %v, want HostKeyChangedError", err)
	}
	if !strings.Contains(err.Error(), "changed; check with your admin") {
		t.Fatalf("message = %q", err.Error())
	}
	var unknown *UnknownHostKeyError
	if errors.As(err, &unknown) {
		t.Fatal("a changed key must not be offered for trust")
	}
	if srv.AuthAttempts() != 0 || srv.Logins() != 0 {
		t.Fatalf("auth reached a host with a changed key (attempts %d)", srv.AuthAttempts())
	}
}

func TestTunnelRevokedHostKeyIsRefused(t *testing.T) {
	home := sshHome(t)
	srv := dbtest.NewSSHServer()
	srv.Password = "pw-123456"
	defer srv.Close()
	line := "@revoked * " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(srv.HostKey.PublicKey()))) + "\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := openTestTunnel(t, SSHConfig{Host: "b", User: "u", Auth: "password"}, TunnelOptions{Home: home, Password: "pw-123456", Dial: srv.Dial})
	var changed *HostKeyChangedError
	if !errors.As(err, &changed) || !changed.Revoked {
		t.Fatalf("err = %v, want a revoked refusal", err)
	}
}

func TestTunnelPasswordAuthAsksAndRejectsWrong(t *testing.T) {
	home := sshHome(t)
	srv := dbtest.NewSSHServer()
	srv.Password = "right-pw-1"
	defer srv.Close()
	trustServer(t, home, "b:22", srv)
	cfg := SSHConfig{Host: "b", User: "u", Auth: "password"}

	_, err := openTestTunnel(t, cfg, TunnelOptions{Home: home, Dial: srv.Dial})
	var need *SSHSecretError
	if !errors.As(err, &need) || need.Kind != "password" || need.Wrong {
		t.Fatalf("no password: err = %v", err)
	}
	_, err = openTestTunnel(t, cfg, TunnelOptions{Home: home, Password: "wrong-pw-1", Dial: srv.Dial})
	if !errors.As(err, &need) || !need.Wrong {
		t.Fatalf("wrong password: err = %v", err)
	}
	if strings.Contains(err.Error(), "wrong-pw-1") {
		t.Fatal("the password is in the error text")
	}
	if _, err := openTestTunnel(t, cfg, TunnelOptions{Home: home, Password: "right-pw-1", Dial: srv.Dial}); err != nil {
		t.Fatal(err)
	}
}

func TestTunnelKeyFileWithPassphrase(t *testing.T) {
	home := sshHome(t)
	signer, priv := newClientKey(t)
	srv := dbtest.NewSSHServer()
	srv.AuthorizedKeys = []ssh.PublicKey{signer.PublicKey()}
	defer srv.Close()
	trustServer(t, home, "b:22", srv)
	keyPath := filepath.Join(home, "keys", "id_test")
	_ = os.MkdirAll(filepath.Dir(keyPath), 0o700)
	writeKeyFile(t, keyPath, priv, "open sesame 1", 0o600)
	cfg := SSHConfig{Host: "b", User: "u", Auth: "key", KeyFile: keyPath}

	_, err := openTestTunnel(t, cfg, TunnelOptions{Home: home, Dial: srv.Dial})
	var need *SSHSecretError
	if !errors.As(err, &need) || need.Kind != "passphrase" || need.Wrong {
		t.Fatalf("no passphrase: err = %v", err)
	}
	_, err = openTestTunnel(t, cfg, TunnelOptions{Home: home, Passphrase: "nope nope", Dial: srv.Dial})
	if !errors.As(err, &need) || !need.Wrong {
		t.Fatalf("wrong passphrase: err = %v", err)
	}
	if srv.AuthAttempts() != 0 {
		t.Fatal("a key that could not be read was offered")
	}
	if _, err := openTestTunnel(t, cfg, TunnelOptions{Home: home, Passphrase: "open sesame 1", Dial: srv.Dial}); err != nil {
		t.Fatal(err)
	}
	if n := srv.WaitLogins(1); n != 1 {
		t.Fatalf("logins = %d", n)
	}
}

func TestTunnelKeyFileReadableByOthersIsRefused(t *testing.T) {
	home := sshHome(t)
	signer, priv := newClientKey(t)
	srv := dbtest.NewSSHServer()
	srv.AuthorizedKeys = []ssh.PublicKey{signer.PublicKey()}
	defer srv.Close()
	trustServer(t, home, "b:22", srv)
	keyPath := filepath.Join(home, "id_open")
	writeKeyFile(t, keyPath, priv, "", 0o644)
	_, err := openTestTunnel(t, SSHConfig{Host: "b", User: "u", Auth: "key", KeyFile: keyPath}, TunnelOptions{Home: home, Dial: srv.Dial})
	if err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("err = %v, want the chmod advice", err)
	}
	if srv.AuthAttempts() != 0 {
		t.Fatal("an unprotected key was used")
	}
}

func TestTunnelAgentAuth(t *testing.T) {
	home := sshHome(t)
	signer, priv := newClientKey(t)
	srv := dbtest.NewSSHServer()
	srv.AuthorizedKeys = []ssh.PublicKey{signer.PublicKey()}
	defer srv.Close()
	trustServer(t, home, "b:22", srv)

	dir, err := os.MkdirTemp("", "tcag")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("no unix sockets: %v", err)
	}
	defer ln.Close()
	keyring := agent.NewKeyring()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = agent.ServeAgent(keyring, c); _ = c.Close() }()
		}
	}()
	cfg := SSHConfig{Host: "b", User: "u", Auth: "agent"}
	_, err = openTestTunnel(t, cfg, TunnelOptions{Home: home, AgentSocket: sock, Dial: srv.Dial})
	if err == nil || !strings.Contains(err.Error(), "ssh-add") {
		t.Fatalf("empty agent: err = %v", err)
	}
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	if _, err := openTestTunnel(t, cfg, TunnelOptions{Home: home, AgentSocket: sock, Dial: srv.Dial}); err != nil {
		t.Fatal(err)
	}
	_, err = openTestTunnel(t, cfg, TunnelOptions{Home: home, Dial: srv.Dial})
	if err == nil || !strings.Contains(err.Error(), "SSH_AUTH_SOCK") {
		t.Fatalf("no agent: err = %v", err)
	}
}

func TestTunnelReadsSSHConfigSubset(t *testing.T) {
	home := sshHome(t)
	signer, priv := newClientKey(t)
	keyPath := filepath.Join(home, ".ssh", "id_bastion")
	writeKeyFile(t, keyPath, priv, "", 0o600)
	conf := `# global
Host *.other
  User nobody
Host bastion !bad
  HostName real.example.net
  Port 2222
  User Deploy
  IdentityFile ~/.ssh/id_bastion
  Port 9999
Host jumpy
  ProxyJump gw.example.net
Host *
  User fallback
`
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	tg, err := ResolveSSH(SSHConfig{Host: "bastion", Auth: "key"}, home)
	if err != nil {
		t.Fatal(err)
	}
	if tg.Host != "real.example.net" || tg.Port != 2222 || tg.User != "Deploy" || len(tg.KeyFiles) != 1 || tg.KeyFiles[0] != keyPath {
		t.Fatalf("resolved = %+v", tg)
	}
	// The JSON object wins over the config.
	tg, _ = ResolveSSH(SSHConfig{Host: "bastion", Port: 22, User: "me"}, home)
	if tg.Port != 22 || tg.User != "me" {
		t.Fatalf("explicit fields lost: %+v", tg)
	}
	tg, _ = ResolveSSH(SSHConfig{Host: "elsewhere"}, home)
	if tg.User != "fallback" || tg.Host != "elsewhere" {
		t.Fatalf("Host * not applied: %+v", tg)
	}

	srv := dbtest.NewSSHServer()
	srv.AuthorizedKeys = []ssh.PublicKey{signer.PublicKey()}
	defer srv.Close()
	trustServer(t, home, "[real.example.net]:2222", srv)
	var dialled string
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialled = addr
		return srv.Dial(ctx, network, addr)
	}
	if _, err := openTestTunnel(t, SSHConfig{Host: "bastion", Auth: "key"}, TunnelOptions{Home: home, Dial: dial}); err != nil {
		t.Fatal(err)
	}
	if dialled != "real.example.net:2222" {
		t.Fatalf("dialled %q, want the config's HostName and Port", dialled)
	}

	// ProxyJump is followed now: the jump host is dialled first and gets
	// its own host key check (unknown here, so the trust card names it).
	_, err = openTestTunnel(t, SSHConfig{Host: "jumpy", Auth: "key"}, TunnelOptions{Home: home, Dial: dial})
	var unknown *UnknownHostKeyError
	if !errors.As(err, &unknown) || unknown.Host != "gw.example.net:22" || unknown.Jump == "" || dialled != "gw.example.net:22" {
		t.Fatalf("ProxyJump: err = %v dialled %q, want the jump host's trust card", err, dialled)
	}
}

// dialThrough opens a tunnel to srv (trusted, password auth).
func dialThrough(t *testing.T, srv *dbtest.SSHServer) *Tunnel {
	t.Helper()
	home := sshHome(t)
	srv.Password = "tunnel-pw-1"
	trustServer(t, home, "bastion:22", srv)
	tun, err := openTestTunnel(t, SSHConfig{Host: "bastion", User: "u", Auth: "password"}, TunnelOptions{Home: home, Password: "tunnel-pw-1", Dial: srv.Dial})
	if err != nil {
		t.Fatal(err)
	}
	return tun
}

func TestTunnelCarriesRedisSession(t *testing.T) {
	f := dbtest.NewFakeRedis()
	defer f.Close()
	f.SetString(0, "greeting", "hello")
	srv := dbtest.NewSSHServer()
	srv.Forward = func(ctx context.Context, addr string) (net.Conn, error) { return f.Dial(ctx, "tcp", addr) }
	defer srv.Close()
	tun := dialThrough(t, srv)

	c := Conn{ID: "cache", Driver: "redis", Host: "cache.internal", Port: 6379, Dial: tun.DialContext}
	s, err := NewRedisSession(context.Background(), c, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Do(context.Background(), []string{"GET", "greeting"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := got.([]byte); string(b) != "hello" {
		t.Fatalf("GET = %#v", got)
	}
	if tg := srv.Targets(); len(tg) == 0 || tg[0] != "cache.internal:6379" {
		t.Fatalf("tunnel targets = %v", tg)
	}
}

// recordTarget is a Forward that answers nothing and closes: enough to
// see which address the driver asked the SSH server for.
func recordTarget(context.Context, string) (net.Conn, error) {
	a, b := net.Pipe()
	go func() { _, _ = io.Copy(io.Discard, b); _ = b.Close() }()
	go func() { time.Sleep(50 * time.Millisecond); _ = a.Close() }()
	return a, nil
}

func TestTunnelDialHookPostgresAndMySQL(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Forward = recordTarget
	defer srv.Close()
	tun := dialThrough(t, srv)
	for _, c := range []Conn{
		// Unresolvable here: the name must reach the SSH server as is.
		{ID: "pg", Driver: "postgres", Host: "db.internal.invalid", Port: 5432, User: "app", Database: "app", Params: map[string]string{"sslmode": "disable"}},
		{ID: "my", Driver: "mysql", Host: "db.internal.invalid", Port: 3306, User: "app", Database: "app"},
	} {
		c.Dial = tun.DialContext
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		s, err := NewSQLSession(ctx, c, "pw")
		cancel()
		if err == nil {
			_ = s.Close()
			t.Fatalf("%s: a closed target connected", c.Driver)
		}
	}
	got := srv.Targets()
	want := []string{"db.internal.invalid:5432", "db.internal.invalid:3306"}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Fatalf("tunnel targets = %v, want %v", got, want)
		}
	}
}

func TestTunnelClosedDialSaysReconnect(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Forward = recordTarget
	defer srv.Close()
	tun := dialThrough(t, srv)
	srv.DropAll() // the bastion goes away
	deadline := time.Now().Add(3 * time.Second)
	for tun.Alive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if tun.Alive() {
		t.Fatal("the tunnel did not notice the server closing")
	}
	_, err := tun.DialContext(context.Background(), "tcp", "db:5432")
	if !errors.Is(err, ErrTunnelClosed) || !strings.Contains(err.Error(), "R reconnect") {
		t.Fatalf("err = %v", err)
	}

	tun2 := dialThrough(t, srv2(t))
	_ = tun2.Close()
	if _, err := tun2.DialContext(context.Background(), "tcp", "db:5432"); !errors.Is(err, ErrTunnelClosed) {
		t.Fatalf("after Close: err = %v", err)
	}
}

func srv2(t *testing.T) *dbtest.SSHServer {
	s := dbtest.NewSSHServer()
	s.Forward = recordTarget
	t.Cleanup(s.Close)
	return s
}

func TestTunnelConnDeadlineInterruptsRead(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := &tunnelConn{Conn: a}
	if err := c.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err := c.Read(make([]byte, 1))
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("err = %v, want a timeout", err)
	}
	d := &tunnelConn{Conn: b}
	_ = d.SetDeadline(time.Now().Add(time.Hour))
	_ = d.SetDeadline(time.Time{}) // cleared: no timer left
	if d.rt != nil || d.wt != nil {
		t.Fatal("a cleared deadline kept its timer")
	}
}

func TestTrustHostKeyAppendsNormalisedLine(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sub", "known_hosts")
	srv := dbtest.NewSSHServer()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(knownhosts.Line([]string{"other.example"}, dbtest.NewSSHServer().HostKey.PublicKey())), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := TrustHostKey(file, "bastion:2222", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "[bastion]:2222 ssh-ed25519 ") {
		t.Fatalf("known_hosts = %q", data)
	}
	cb, err := knownhosts.New(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb("bastion:2222", hostAddr("bastion:2222"), srv.HostKey.PublicKey()); err != nil {
		t.Fatalf("saved line does not verify: %v", err)
	}
}
