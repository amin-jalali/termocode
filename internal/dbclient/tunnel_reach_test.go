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
// 127.0.0.1:5432. The reason is one plain line that names the address,
// the SSH server and what to check.
func TestTunnelNothingListensSaysWhatToCheck(t *testing.T) {
	srv := dbtest.NewSSHServer() // Forward nil: the server refuses every target
	defer srv.Close()
	tun := dialThrough(t, srv)
	_, err := tun.DialContext(context.Background(), "tcp", "127.0.0.1:5432")
	want := "Nothing answers at 127.0.0.1:5432 on bastion (through SSH). Is the container's port published?"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v\nwant  %s", err, want)
	}
	_, err = tun.DialContext(context.Background(), "tcp", "db.internal:5432")
	want = "Nothing answers at db.internal:5432 on bastion (through SSH). Is the database running there?"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v\nwant  %s", err, want)
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
