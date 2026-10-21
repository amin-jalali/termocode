package dbclient

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// jumpChain is client → gw1 → gw2 → bastion → a fake database, all
// in-process. Every hop forwards only to the next one.
type jumpChain struct {
	home              string
	gw1, gw2, bastion *dbtest.SSHServer
	dbTargets         chan string
	firstDial         string
	keyPath           string
	clientKey         ssh.Signer
	bastionCfg        SSHConfig
	bastionPw         string
	dbAddr            string
	dial              DialFunc
	gw1Addr, gw2Addr  string
	bastionAddr       string
}

const jumpSSHConfig = `Host db-bastion
  HostName bastion.internal
  ProxyJump gw1,jumper@gw2.internal:2222
Host gw1
  HostName gw1.example.net
  User jumper
  IdentityFile ~/.ssh/id_jump
Host gw2.internal
  IdentityFile ~/.ssh/id_jump
`

func newJumpChain(t *testing.T, sshConfig string) *jumpChain {
	t.Helper()
	home := sshHome(t)
	signer, priv := newClientKey(t)
	keyPath := filepath.Join(home, ".ssh", "id_jump")
	writeKeyFile(t, keyPath, priv, "", 0o600)
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(sshConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &jumpChain{
		home: home, keyPath: keyPath, clientKey: signer,
		gw1: dbtest.NewSSHServer(), gw2: dbtest.NewSSHServer(), bastion: dbtest.NewSSHServer(),
		dbTargets: make(chan string, 8), bastionPw: "bastion-pw-1",
		gw1Addr: "gw1.example.net:22", gw2Addr: "gw2.internal:2222", bastionAddr: "bastion.internal:22", dbAddr: "db.internal:5432",
	}
	c.gw1.AuthorizedKeys = []ssh.PublicKey{signer.PublicKey()}
	c.gw2.AuthorizedKeys = []ssh.PublicKey{signer.PublicKey()}
	c.bastion.Password = c.bastionPw
	c.gw1.Forward = func(ctx context.Context, addr string) (net.Conn, error) {
		if addr != c.gw2Addr {
			return nil, errors.New("gw1: unexpected target " + addr)
		}
		return c.gw2.Dial(ctx, "tcp", addr)
	}
	c.gw2.Forward = func(ctx context.Context, addr string) (net.Conn, error) {
		if addr != c.bastionAddr {
			return nil, errors.New("gw2: unexpected target " + addr)
		}
		return c.bastion.Dial(ctx, "tcp", addr)
	}
	c.bastion.Forward = func(_ context.Context, addr string) (net.Conn, error) {
		c.dbTargets <- addr
		a, b := net.Pipe()
		go func() { _, _ = io.Copy(b, b); _ = b.Close() }() // echo
		return a, nil
	}
	c.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if c.firstDial != "" {
			return nil, errors.New("dialled twice from the client: " + addr)
		}
		c.firstDial = addr
		return c.gw1.Dial(ctx, network, addr)
	}
	c.bastionCfg = SSHConfig{Host: "db-bastion", User: "deploy", Auth: "password"}
	t.Cleanup(c.gw1.Close)
	t.Cleanup(c.gw2.Close)
	t.Cleanup(c.bastion.Close)
	return c
}

func (c *jumpChain) trustAll(t *testing.T) {
	t.Helper()
	trustServer(t, c.home, c.gw1Addr, c.gw1)
	trustServer(t, c.home, c.gw2Addr, c.gw2)
	trustServer(t, c.home, c.bastionAddr, c.bastion)
}

func (c *jumpChain) open(t *testing.T) (*Tunnel, error) {
	t.Helper()
	c.firstDial = ""
	return openTestTunnel(t, c.bastionCfg, TunnelOptions{Home: c.home, Password: c.bastionPw, Dial: c.dial})
}

func echoThrough(t *testing.T, tun *Tunnel, addr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := tun.DialContext(ctx, "tcp", addr)
	if err != nil {
		t.Fatalf("dial %s through the chain: %v", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("echo = %q, %v", buf, err)
	}
}

func TestTunnelProxyJumpTwoHops(t *testing.T) {
	c := newJumpChain(t, jumpSSHConfig)
	c.trustAll(t)
	tun, err := c.open(t)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if c.firstDial != c.gw1Addr {
		t.Fatalf("the client dialled %q, want the first jump host %q", c.firstDial, c.gw1Addr)
	}
	echoThrough(t, tun, c.dbAddr)
	if got := <-c.dbTargets; got != c.dbAddr {
		t.Fatalf("bastion forwarded to %q", got)
	}
	if g := c.gw1.Targets(); len(g) != 1 || g[0] != c.gw2Addr {
		t.Fatalf("gw1 targets = %v", g)
	}
	if g := c.gw2.Targets(); len(g) != 1 || g[0] != c.bastionAddr {
		t.Fatalf("gw2 targets = %v", g)
	}
	if c.gw1.WaitLogins(1) != 1 || c.gw2.WaitLogins(1) != 1 || c.bastion.Logins() != 1 {
		t.Fatalf("logins gw1=%d gw2=%d bastion=%d", c.gw1.Logins(), c.gw2.Logins(), c.bastion.Logins())
	}
	if hops := tun.Target().Jumps; len(hops) != 2 || hops[0].Addr() != c.gw1Addr || hops[1].Addr() != c.gw2Addr || hops[0].User != "jumper" || hops[1].User != "jumper" {
		t.Fatalf("resolved jumps = %+v", hops)
	}
}

func TestTunnelProxyJumpUnknownInnerHopAsksForThatHop(t *testing.T) {
	c := newJumpChain(t, jumpSSHConfig)
	trustServer(t, c.home, c.gw1Addr, c.gw1)
	trustServer(t, c.home, c.bastionAddr, c.bastion)
	_, err := c.open(t)
	var unknown *UnknownHostKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want UnknownHostKeyError for gw2", err)
	}
	want := ssh.FingerprintSHA256(c.gw2.HostKey.PublicKey())
	if unknown.Host != c.gw2Addr || unknown.Fingerprint != want || unknown.Jump == "" {
		t.Fatalf("unknown = host %q jump %q fp %q, want %q %q", unknown.Host, unknown.Jump, unknown.Fingerprint, c.gw2Addr, want)
	}
	if !strings.Contains(err.Error(), "gw2.internal") || !strings.Contains(err.Error(), "jump host") {
		t.Fatalf("message = %q", err.Error())
	}
	if c.gw2.AuthAttempts() != 0 || len(c.gw2.Targets()) != 0 || c.bastion.AuthAttempts() != 0 {
		t.Fatal("the chain went on past an unverified hop")
	}
	if err := TrustHostKey(unknown.KnownHosts, unknown.Host, unknown.Key); err != nil {
		t.Fatal(err)
	}
	tun, err := c.open(t)
	if err != nil {
		t.Fatalf("after trust: %v", err)
	}
	echoThrough(t, tun, c.dbAddr)
}

func TestTunnelProxyJumpChangedInnerHopIsRefused(t *testing.T) {
	c := newJumpChain(t, jumpSSHConfig)
	trustServer(t, c.home, c.gw1Addr, c.gw1)
	trustServer(t, c.home, c.gw2Addr, dbtest.NewSSHServer()) // the key seen before
	trustServer(t, c.home, c.bastionAddr, c.bastion)
	_, err := c.open(t)
	var changed *HostKeyChangedError
	if !errors.As(err, &changed) || changed.Host != c.gw2Addr {
		t.Fatalf("err = %v, want HostKeyChangedError for gw2", err)
	}
	var unknown *UnknownHostKeyError
	if errors.As(err, &unknown) {
		t.Fatal("a changed key must not be offered for trust")
	}
	if !strings.Contains(err.Error(), "gw2.internal") || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("message = %q", err.Error())
	}
	if c.gw2.AuthAttempts() != 0 || c.bastion.AuthAttempts() != 0 {
		t.Fatal("auth went past a hop with a changed key")
	}
}

func TestTunnelProxyJumpUnknownBastionBehindJumps(t *testing.T) {
	c := newJumpChain(t, jumpSSHConfig)
	trustServer(t, c.home, c.gw1Addr, c.gw1)
	trustServer(t, c.home, c.gw2Addr, c.gw2)
	_, err := c.open(t)
	var unknown *UnknownHostKeyError
	if !errors.As(err, &unknown) || unknown.Host != c.bastionAddr || unknown.Jump != "" {
		t.Fatalf("err = %v, want the bastion's own trust card", err)
	}
	if c.bastion.AuthAttempts() != 0 {
		t.Fatal("the password went to an unverified bastion")
	}
}

func TestTunnelProxyJumpAliasLoopIsRefused(t *testing.T) {
	c := newJumpChain(t, `Host db-bastion
  ProxyJump a
Host a
  ProxyJump b
Host b
  ProxyJump a
`)
	_, err := c.open(t)
	if !errors.Is(err, ErrProxyJumpLoop) {
		t.Fatalf("err = %v, want ErrProxyJumpLoop", err)
	}
	if c.firstDial != "" {
		t.Fatal("a looping chain was dialled")
	}
	// Too many hops (no loop) is refused too.
	hops := make([]string, 12)
	for i := range hops {
		hops[i] = "h" + string(rune('a'+i))
	}
	c2 := newJumpChain(t, "Host db-bastion\n  ProxyJump "+strings.Join(hops, ",")+"\n")
	_, err = c2.open(t)
	if !errors.Is(err, ErrTooManyJumps) || c2.firstDial != "" {
		t.Fatalf("err = %v, want ErrTooManyJumps", err)
	}
}

func TestTunnelProxyJumpNone(t *testing.T) {
	c := newJumpChain(t, `Host db-bastion
  HostName bastion.internal
  ProxyJump none
Host *
  ProxyJump gw1
`)
	trustServer(t, c.home, c.bastionAddr, c.bastion)
	c.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		c.firstDial = addr
		return c.bastion.Dial(ctx, network, addr)
	}
	tun, err := c.open(t)
	if err != nil {
		t.Fatal(err)
	}
	if c.firstDial != c.bastionAddr || len(tun.Target().Jumps) != 0 {
		t.Fatalf("dialled %q jumps %v, want the bastion directly", c.firstDial, tun.Target().Jumps)
	}
	echoThrough(t, tun, c.dbAddr)
}

func TestTunnelProxyCommandStillRefused(t *testing.T) {
	c := newJumpChain(t, `Host db-bastion
  ProxyCommand nc %h %p
Host viacmd
  ProxyJump gwcmd
Host gwcmd
  ProxyCommand ssh -W %h:%p elsewhere
`)
	_, err := c.open(t)
	if !errors.Is(err, ErrProxyCommandUnsupported) || !strings.Contains(err.Error(), "ProxyCommand") {
		t.Fatalf("err = %v, want ErrProxyCommandUnsupported", err)
	}
	c.bastionCfg.Host = "viacmd"
	_, err = c.open(t)
	if !errors.Is(err, ErrProxyCommandUnsupported) || !strings.Contains(err.Error(), "gwcmd") {
		t.Fatalf("jump host with ProxyCommand: err = %v", err)
	}
	if c.firstDial != "" {
		t.Fatal("a ProxyCommand host was dialled")
	}
}

func TestTunnelJumpFromJSON(t *testing.T) {
	c := newJumpChain(t, "")
	c.trustAll(t)
	// No ssh config: the hops' key is the default ~/.ssh/id_ed25519.
	if err := os.Rename(c.keyPath, filepath.Join(c.home, ".ssh", "id_ed25519")); err != nil {
		t.Fatal(err)
	}
	c.bastionCfg = SSHConfig{Host: "bastion.internal", User: "deploy", Auth: "password", Jump: []string{"jumper@gw1.example.net", "jumper@gw2.internal:2222"}}
	tun, err := c.open(t)
	if err != nil {
		t.Fatal(err)
	}
	echoThrough(t, tun, c.dbAddr)
	// The JSON list wins over the ssh config's ProxyJump.
	if err := os.WriteFile(filepath.Join(c.home, ".ssh", "config"), []byte("Host bastion.internal\n  ProxyJump nowhere.invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.open(t); err != nil {
		t.Fatalf("with a config ProxyJump: %v", err)
	}
	if _, err := ResolveSSH(SSHConfig{Host: "b", Jump: []string{"u@:22"}}, c.home); err == nil {
		t.Fatal("an empty jump host was accepted")
	}
}

func TestTunnelJumpHopPasswordIsRefusedWithItsName(t *testing.T) {
	c := newJumpChain(t, jumpSSHConfig)
	c.trustAll(t)
	c.gw2.AuthorizedKeys = nil
	c.gw2.Password = "hop-pw"
	_, err := c.open(t)
	if err == nil || !strings.Contains(err.Error(), "jump host") || !strings.Contains(err.Error(), "gw2.internal") {
		t.Fatalf("err = %v, want a refusal naming gw2", err)
	}
	var need *SSHSecretError
	if errors.As(err, &need) {
		t.Fatal("a jump host password must not use the bastion's secret prompt")
	}
	if c.bastion.AuthAttempts() != 0 {
		t.Fatal("the bastion was reached after a hop failed")
	}
}

func TestTunnelProxyJumpTeardownClosesEveryHop(t *testing.T) {
	before := runtime.NumGoroutine()
	func() {
		c := newJumpChain(t, jumpSSHConfig)
		c.trustAll(t)
		tun, err := c.open(t)
		if err != nil {
			t.Fatal(err)
		}
		echoThrough(t, tun, c.dbAddr)
		if err := tun.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		for name, s := range map[string]*dbtest.SSHServer{"gw1": c.gw1, "gw2": c.gw2, "bastion": c.bastion} {
			if n := s.WaitOpen(0); n != 0 {
				t.Fatalf("%s still has %d client connections after Close", name, n)
			}
		}
		if _, err := tun.DialContext(context.Background(), "tcp", c.dbAddr); !errors.Is(err, ErrTunnelClosed) {
			t.Fatalf("after close: %v", err)
		}
		c.gw1.Close()
		c.gw2.Close()
		c.bastion.Close()
	}()
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines %d -> %d:\n%s", before, n, buf[:runtime.Stack(buf, true)])
	}
}

func TestTunnelProxyJumpDeadHopTearsDownChainAndNamesIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		drop func(c *jumpChain)
		want string
	}{
		{"first hop", func(c *jumpChain) { c.gw1.DropAll() }, "gw1"},
		{"inner hop", func(c *jumpChain) { c.gw2.DropAll() }, "gw2.internal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newJumpChain(t, jumpSSHConfig)
			c.trustAll(t)
			tun, err := c.open(t)
			if err != nil {
				t.Fatal(err)
			}
			tc.drop(c)
			deadline := time.Now().Add(3 * time.Second)
			for tun.Alive() && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if tun.Alive() {
				t.Fatal("the chain did not notice a hop going away")
			}
			_, err = tun.DialContext(context.Background(), "tcp", c.dbAddr)
			if !errors.Is(err, ErrTunnelClosed) || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "R reconnect") {
				t.Fatalf("err = %v, want a closed tunnel naming %s", err, tc.want)
			}
			for name, s := range map[string]*dbtest.SSHServer{"gw1": c.gw1, "gw2": c.gw2, "bastion": c.bastion} {
				if n := s.WaitOpen(0); n != 0 {
					t.Fatalf("%s still has %d client connections after the chain broke", name, n)
				}
			}
		})
	}
}
