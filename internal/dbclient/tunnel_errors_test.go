package dbclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// The bastion does not answer: the user learns which address failed.
func TestTunnelBastionUnreachable(t *testing.T) {
	home := sshHome(t)
	refuse := func(context.Context, string, string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
	}
	_, err := openTestTunnel(t, SSHConfig{Host: "b", User: "u", Auth: "password"}, TunnelOptions{Home: home, Password: "pw-123456", Dial: refuse})
	if err == nil || err.Error() != "SSH: could not reach b:22: connect: connection refused" {
		t.Fatalf("err = %v", err)
	}
	// Plain TCP (no dial hook) to a closed local port.
	port := closedPort(t)
	_, err = openTestTunnel(t, SSHConfig{Host: "127.0.0.1", Port: port, User: "u", Auth: "password"}, TunnelOptions{Home: home, Password: "pw-123456"})
	if err == nil || !strings.Contains(err.Error(), "could not reach 127.0.0.1:"+strconv.Itoa(port)) || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("plain dial err = %v", err)
	}
}

// A bastion that accepts the TCP connection and then says nothing: the
// connect ends at the deadline with "did not answer in time".
func TestTunnelSilentBastionTimesOut(t *testing.T) {
	home := sshHome(t)
	var peers []net.Conn
	silent := func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		peers = append(peers, b)
		return a, nil
	}
	defer func() {
		for _, p := range peers {
			_ = p.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := OpenTunnel(ctx, SSHConfig{Host: "b", User: "u", Auth: "password"}, TunnelOptions{Home: home, Password: "pw-123456", Dial: silent})
	if err == nil || !strings.Contains(err.Error(), "did not answer in time") {
		t.Fatalf("err = %v", err)
	}
	// The dial itself hangs until the deadline.
	hang := func(ctx context.Context, _, _ string) (net.Conn, error) { <-ctx.Done(); return nil, ctx.Err() }
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	_, err = OpenTunnel(ctx2, SSHConfig{Host: "b", User: "u", Auth: "password"}, TunnelOptions{Home: home, Password: "pw-123456", Dial: hang})
	if err == nil || err.Error() != "SSH: b:22 did not answer in time" {
		t.Fatalf("hanging dial err = %v", err)
	}
}

// Something that is not an SSH server answers (a web server on the
// port): the handshake error is shown, cleaned.
func TestTunnelNotAnSSHServer(t *testing.T) {
	home := sshHome(t)
	web := func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() {
			go func() { _, _ = io.Copy(io.Discard, b) }()
			_, _ = b.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			_ = b.Close()
		}()
		return a, nil
	}
	_, err := openTestTunnel(t, SSHConfig{Host: "b", User: "u", Auth: "password"}, TunnelOptions{Home: home, Password: "pw-123456", Dial: web})
	if err == nil || !strings.HasPrefix(err.Error(), "SSH: ") || strings.Contains(err.Error(), "handshake failed: ") || strings.Contains(err.Error(), "in time") {
		t.Fatalf("err = %v", err)
	}
}

// The SSH server cannot reach the database: the message names the
// database address; only TCP is carried.
func TestTunnelTargetUnreachable(t *testing.T) {
	srv := dbtest.NewSSHServer() // Forward nil: every target is refused
	defer srv.Close()
	tun := dialThrough(t, srv)
	_, err := tun.DialContext(context.Background(), "tcp", "db.internal:5432")
	if err == nil || !strings.Contains(err.Error(), "the SSH server could not reach db.internal:5432") {
		t.Fatalf("err = %v", err)
	}
	if _, err := tun.DialContext(context.Background(), "udp", "db.internal:53"); err == nil || !strings.Contains(err.Error(), "TCP only") {
		t.Fatalf("udp err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tun.DialContext(ctx, "tcp", "db.internal:5432"); err == nil {
		t.Fatal("a cancelled dial worked")
	}
	if tun.Target().Host != "bastion" {
		t.Errorf("target = %+v", tun.Target())
	}
}

// Login refusals name the auth method that failed.
func TestTunnelLoginRefusedNamesTheMethod(t *testing.T) {
	home := sshHome(t)
	_, otherPriv := newClientKey(t)
	signer, _ := newClientKey(t)
	srv := dbtest.NewSSHServer()
	srv.AuthorizedKeys = []ssh.PublicKey{signer.PublicKey()} // not our key
	defer srv.Close()
	trustServer(t, home, "b:22", srv)
	keyPath := filepath.Join(home, "id_other")
	writeKeyFile(t, keyPath, otherPriv, "", 0o600)
	_, err := openTestTunnel(t, SSHConfig{Host: "b", User: "u", Auth: "key", KeyFile: keyPath}, TunnelOptions{Home: home, Dial: srv.Dial})
	if err == nil || err.Error() != "SSH login as u@b failed: the key was not accepted" {
		t.Fatalf("key err = %v", err)
	}
	if got := sshLoginError(errors.New("ssh: handshake failed: ssh: unable to authenticate"), SSHTarget{User: "u", Host: "b", Auth: SSHAuthAgent}); !strings.Contains(got.Error(), "the agent's keys were not accepted") {
		t.Fatalf("agent err = %v", got)
	}
}

func TestSSHSecretErrorTexts(t *testing.T) {
	for e, want := range map[*SSHSecretError]string{
		{Kind: "passphrase", Wrong: true, KeyFile: "/k/id_x"}: "wrong passphrase for the SSH key id_x",
		{Kind: "passphrase", KeyFile: "/k/id_x"}:              "the SSH key id_x needs its passphrase",
		{Kind: "password", Wrong: true}:                       "SSH login failed: the password was not accepted",
		{Kind: "password"}:                                    "the SSH tunnel needs a password",
	} {
		if e.Error() != want {
			t.Errorf("%+v = %q", e, e.Error())
		}
	}
}

func TestResolveSSHRejectsBadSettings(t *testing.T) {
	home := sshHome(t)
	for _, tc := range []struct {
		cfg  SSHConfig
		want string
	}{
		{SSHConfig{Host: " ", User: "u"}, "has no host"},
		{SSHConfig{Host: "b", User: "u", Port: 70000}, "port is not valid"},
		{SSHConfig{Host: "b", User: "u", Auth: "totp"}, `unknown SSH auth "totp"`},
	} {
		if _, err := ResolveSSH(tc.cfg, home); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: err = %v, want %q", tc.cfg, err, tc.want)
		}
	}
	// No home: no ~/.ssh files are read, the defaults stay empty.
	tg, err := ResolveSSH(SSHConfig{Host: "b", User: "u", KnownHosts: "/etc/kh_%h_%r_%%_%x"}, "")
	if err != nil || tg.KnownHosts != "/etc/kh_b_u_%_%x" || len(tg.KeyFiles) != 0 {
		t.Fatalf("no home: %+v %v", tg, err)
	}
}

// IdentityFile and known_hosts paths expand like OpenSSH.
func TestExpandSSHPath(t *testing.T) {
	for in, want := range map[string]string{
		"~":                  "/h",
		"~/.ssh/id":          "/h/.ssh/id",
		"%d/keys/%h-%r":      "/h/keys/db.example-amy",
		"/k/100%%":           "/k/100%",
		"/k/%":               "/k/%",
		"/plain/path/id_rsa": "/plain/path/id_rsa",
	} {
		if got := expandSSHPath(in, "/h", "db.example", "amy"); got != want {
			t.Errorf("expandSSHPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// Key file problems are explained before anything is sent.
func TestLoadSSHKeyProblems(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadSSHKey([]string{filepath.Join(dir, "a"), filepath.Join(dir, "b")}, ""); err == nil || !strings.Contains(err.Error(), "no SSH key file found") {
		t.Errorf("none found = %v", err)
	}
	if _, err := loadSSHKey([]string{dir}, ""); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("directory = %v", err)
	}
	junk := filepath.Join(dir, "junk")
	if err := os.WriteFile(junk, []byte("-----BEGIN NOTHING-----\nxx\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSSHKey([]string{junk}, ""); err == nil || !strings.Contains(err.Error(), "could not read the SSH key junk") {
		t.Errorf("junk = %v", err)
	}
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, make([]byte, maxKeyFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSSHKey([]string{big}, ""); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("big = %v", err)
	}
}

// known_hosts that cannot be read stops the connect before any login.
func TestTunnelUnreadableKnownHosts(t *testing.T) {
	home := sshHome(t)
	kh := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.Mkdir(kh, 0o700); err != nil { // a directory, not a file
		t.Fatal(err)
	}
	srv := dbtest.NewSSHServer()
	srv.Password = "pw-123456"
	defer srv.Close()
	_, err := openTestTunnel(t, SSHConfig{Host: "b", User: "u", Auth: "password"}, TunnelOptions{Home: home, Password: "pw-123456", Dial: srv.Dial})
	if err == nil || !strings.Contains(err.Error(), "could not read known_hosts") || srv.AuthAttempts() != 0 {
		t.Fatalf("err = %v (attempts %d)", err, srv.AuthAttempts())
	}
}

// A trusted RSA host key is checked with the RSA SHA-2 algorithms.
func TestTunnelTrustedRSAHostKey(t *testing.T) {
	home := sshHome(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	srv := dbtest.NewSSHServer()
	srv.HostKey = signer
	srv.Password = "pw-123456"
	defer srv.Close()
	trustServer(t, home, "b:22", srv)
	if _, err := openTestTunnel(t, SSHConfig{Host: "b", User: "u", Auth: "password"}, TunnelOptions{Home: home, Password: "pw-123456", Dial: srv.Dial}); err != nil {
		t.Fatal(err)
	}
	if got := hostKeyAlgos(ssh.KeyAlgoRSA); len(got) != 3 || got[0] != ssh.KeyAlgoRSASHA512 {
		t.Errorf("algos = %v", got)
	}
}

func TestTrustHostKeyErrors(t *testing.T) {
	if err := TrustHostKey("", "b:22", nil); err == nil {
		t.Error("no file accepted")
	}
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := dbtest.NewSSHServer()
	if err := TrustHostKey(filepath.Join(blocker, "known_hosts"), "b:22", srv.HostKey.PublicKey()); err == nil {
		t.Error("saved under a file")
	}
}

func TestTunnelNilAndConnTimeouts(t *testing.T) {
	var tun *Tunnel
	if tun.Alive() || tun.Close() != nil {
		t.Error("nil tunnel")
	}
	a, b := net.Pipe()
	defer b.Close()
	c := &tunnelConn{Conn: a}
	_ = c.SetWriteDeadline(time.Now().Add(-time.Second)) // already past: closes at once
	_, err := c.Write([]byte("x"))
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() || err.Error() != "i/o timeout" {
		t.Fatalf("write err = %v", err)
	}
	if !(tunnelTimeout{}).Temporary() || hostAddr("h:1").Network() != "tcp" {
		t.Error("timeout/addr")
	}
	if got := hostForDisplay("bad\x1bhost"); strings.Contains(got, "\x1b") {
		t.Errorf("hostForDisplay = %q", got)
	}
}
