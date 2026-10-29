package dbclient

import (
	"context"
	"net"
	"os"
	"testing"
)

// timeoutConn is a connection whose every read and write times out, as
// when the connection deadline passes just before the context's.
type timeoutConn struct{ net.Conn }

func (timeoutConn) Read([]byte) (int, error)  { return 0, os.ErrDeadlineExceeded }
func (timeoutConn) Write([]byte) (int, error) { return 0, os.ErrDeadlineExceeded }

// A network timeout during the SSH handshake reads the same as the
// context deadline: the server did not answer in time.
func TestTunnelHandshakeTimeoutSaysNoAnswer(t *testing.T) {
	home := sshHome(t)
	dial := func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
		return timeoutConn{a}, nil
	}
	_, err := openTestTunnel(t, SSHConfig{Host: "b", User: "u", Auth: "password"}, TunnelOptions{Home: home, Password: "pw-123456", Dial: dial})
	if err == nil || err.Error() != "SSH: b:22 did not answer in time" {
		t.Fatalf("err = %v", err)
	}
}
