package dbclient

import (
	"context"
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

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// The owner's case: Postgres runs in a container on the server and its
// port is not published, so nothing listens on the server's
// 127.0.0.1:5432. The reason is one plain sentence: the SSH login that
// worked (user@host), the address on that server and how it failed.
func TestTunnelNothingListensSaysWhatToCheck(t *testing.T) {
	srv := dbtest.NewSSHServer() // Forward nil: the server refuses every target
	defer srv.Close()
	tun := dialThrough(t, srv)
	_, err := tun.DialContext(context.Background(), "tcp", "127.0.0.1:5432")
	want := "SSH to u@bastion works, but nothing answers at 127.0.0.1:5432 on that server (connection refused)."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v\nwant  %s", err, want)
	}
	_, err = tun.DialContext(context.Background(), "tcp", "db.internal:5432")
	want = "SSH to u@bastion works, but nothing answers at db.internal:5432 on that server (connection refused)."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v\nwant  %s", err, want)
	}
}

// The SSH server takes the tunnel channel but its dial to the target
// never ends (a firewall that drops packets): at the deadline the reason
// says the target on that server timed out, and it is still a deadline.
func TestTunnelTargetTimeoutSaysTimedOut(t *testing.T) {
	srv := dbtest.NewSSHServer()
	defer srv.Close()
	block := make(chan struct{})
	defer close(block) // runs before srv.Close
	srv.Forward = func(context.Context, string) (net.Conn, error) {
		<-block
		return nil, errors.New("stopped")
	}
	tun := dialThrough(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := tun.DialContext(ctx, "tcp", "127.0.0.1:5432")
	want := "SSH to u@bastion works, but 127.0.0.1:5432 on that server did not answer in time (timed out)."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v\nwant  %s", err, want)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v is not a deadline", err)
	}
	if ReachText(fmtWrap(err)) != want {
		t.Fatalf("ReachText of a wrapped error = %q", ReachText(fmtWrap(err)))
	}
}

// fmtWrap flattens err the way a driver does (only its text is kept).
func fmtWrap(err error) error {
	return errors.New("failed to connect to `user=app database=app`: 127.0.0.1:5432 (127.0.0.1): dial error: " + err.Error())
}

// A cancel (not a deadline) stays a plain cancel.
func TestTunnelTargetCancelStaysCancel(t *testing.T) {
	srv := dbtest.NewSSHServer()
	defer srv.Close()
	block := make(chan struct{})
	defer close(block) // runs before srv.Close
	srv.Forward = func(context.Context, string) (net.Conn, error) {
		<-block
		return nil, errors.New("stopped")
	}
	tun := dialThrough(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := tun.DialContext(ctx, "tcp", "127.0.0.1:5432")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// The other reasons name the same SSH login.
func TestReachErrorTexts(t *testing.T) {
	for _, tc := range []struct {
		e    ReachError
		want string
	}{
		{ReachError{Addr: "127.0.0.1:5432", Via: "deploy@65.109.212.14", Prohibited: true}, "SSH to deploy@65.109.212.14 works, but that server does not allow tunnels (AllowTcpForwarding is off there)."},
		{ReachError{Addr: "127.0.0.1:5432", Via: "deploy@65.109.212.14", Reason: "no route to host"}, "SSH to deploy@65.109.212.14 works, but 127.0.0.1:5432 on that server could not be reached (no route to host)."},
	} {
		if got := tc.e.Error(); got != tc.want {
			t.Errorf("got  %q\nwant %q", got, tc.want)
		}
	}
}

// agentSocket serves a on a unix socket in a short temp dir.
func agentSocket(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tcag")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("no unix sockets: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() })
			go serve(c)
		}
	}()
	return sock
}

// slowSignAgent lists its keys but never finishes a signature (an agent
// waiting for a confirmation nobody gives).
type slowSignAgent struct {
	agent.Agent
	block chan struct{}
}

func (a slowSignAgent) Sign(ssh.PublicKey, []byte) (*ssh.Signature, error) {
	<-a.block
	return nil, errors.New("stopped")
}

func (a slowSignAgent) SignWithFlags(ssh.PublicKey, []byte, agent.SignatureFlags) (*ssh.Signature, error) {
	<-a.block
	return nil, errors.New("stopped")
}

// An ssh-agent that never answers must not hold the connect forever:
// it ends at the connect deadline with a plain reason.
func TestTunnelAgentThatNeverAnswersEndsAtTheDeadline(t *testing.T) {
	home := sshHome(t)
	signer, priv := newClientKey(t)
	srv := dbtest.NewSSHServer()
	srv.AuthorizedKeys = []ssh.PublicKey{signer.PublicKey()}
	defer srv.Close()
	trustServer(t, home, "b:22", srv)

	mute := agentSocket(t, func(c net.Conn) { _, _ = io.Copy(io.Discard, c) })
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	slow := agentSocket(t, func(c net.Conn) { _ = agent.ServeAgent(slowSignAgent{Agent: keyring, block: block}, c) })

	for _, tc := range []struct{ name, sock, want string }{
		{"no reply at all", mute, "ssh-agent did not answer in time"},
		{"no signature", slow, "did not answer in time"},
	} {
		done := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			tun, err := OpenTunnel(ctx, SSHConfig{Host: "b", User: "u", Auth: "agent"}, TunnelOptions{Home: home, AgentSocket: tc.sock, Dial: srv.Dial})
			if tun != nil {
				_ = tun.Close()
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s: err = %v, want %q", tc.name, err, tc.want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: the connect still waits for the agent after 3 s (deadline 300 ms)", tc.name)
		}
	}
}
