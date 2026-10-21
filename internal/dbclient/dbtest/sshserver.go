package dbtest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHServer is an in-process SSH server for tunnel tests: Dial hands the
// client one end of a BufferedPipe and serves the other. It accepts a
// password, public keys, or both, and forwards "direct-tcpip" channels to
// Forward (nil: refused). Nothing listens on a port.
type SSHServer struct {
	HostKey        ssh.Signer
	Password       string
	AuthorizedKeys []ssh.PublicKey
	// Forward opens the target of a tunnel channel (host:port as the
	// client asked for it).
	Forward func(ctx context.Context, addr string) (net.Conn, error)

	logins   atomic.Int32 // successful logins
	attempts atomic.Int32 // auth attempts of any kind
	open     atomic.Int32 // client connections being served

	mu      sync.Mutex
	targets []string
	conns   []net.Conn
	wg      sync.WaitGroup
	closed  bool
}

// NewSSHServer is a server with a fresh ed25519 host key.
func NewSSHServer() *SSHServer {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		panic(err)
	}
	return &SSHServer{HostKey: signer}
}

// Logins is how many clients logged in.
func (s *SSHServer) Logins() int { return int(s.logins.Load()) }

// WaitLogins waits up to 2 s for n logins (a key login is counted on
// the server's goroutine) and returns the count.
func (s *SSHServer) WaitLogins(n int) int {
	deadline := time.Now().Add(2 * time.Second)
	for s.Logins() < n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return s.Logins()
}

// Open is how many client connections the server still serves.
func (s *SSHServer) Open() int { return int(s.open.Load()) }

// WaitOpen waits up to 2 s until at most n client connections are open
// and returns the count.
func (s *SSHServer) WaitOpen(n int) int {
	deadline := time.Now().Add(2 * time.Second)
	for s.Open() > n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return s.Open()
}

// AuthAttempts is how many auth requests reached the server.
func (s *SSHServer) AuthAttempts() int { return int(s.attempts.Load()) }

// Targets are the addresses tunnel channels asked for, in order.
func (s *SSHServer) Targets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.targets...)
}

// Dial is the client's dialer (addr is ignored: there is one server).
func (s *SSHServer) Dial(_ context.Context, _, _ string) (net.Conn, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("connection refused")
	}
	client, server := BufferedPipe()
	s.conns = append(s.conns, server)
	s.wg.Add(1)
	s.open.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer s.open.Add(-1)
		s.serve(server)
	}()
	return client, nil
}

// DropAll closes every client connection (the server goes away).
func (s *SSHServer) DropAll() {
	s.mu.Lock()
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// Close drops every connection, refuses new ones and waits for the
// handlers.
func (s *SSHServer) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.DropAll()
	s.wg.Wait()
}

func (s *SSHServer) config() *ssh.ServerConfig {
	cfg := &ssh.ServerConfig{}
	if s.Password != "" {
		cfg.PasswordCallback = func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			s.attempts.Add(1)
			if string(pw) == s.Password {
				s.logins.Add(1) // counted before the client hears of it
				return nil, nil
			}
			return nil, errors.New("wrong password")
		}
	}
	if len(s.AuthorizedKeys) > 0 {
		cfg.PublicKeyCallback = func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			s.attempts.Add(1)
			for _, k := range s.AuthorizedKeys {
				if string(k.Marshal()) == string(key.Marshal()) {
					return &ssh.Permissions{Extensions: map[string]string{"key": "1"}}, nil
				}
			}
			return nil, errors.New("unknown key")
		}
	}
	cfg.AddHostKey(s.HostKey)
	return cfg
}

func (s *SSHServer) serve(c net.Conn) {
	defer c.Close()
	sc, chans, reqs, err := ssh.NewServerConn(c, s.config())
	if err != nil {
		return
	}
	defer sc.Close()
	if sc.Permissions != nil && sc.Permissions.Extensions["key"] == "1" {
		s.logins.Add(1)
	}
	go ssh.DiscardRequests(reqs)
	var wg sync.WaitGroup
	defer wg.Wait()
	for nc := range chans {
		if nc.ChannelType() != "direct-tcpip" {
			_ = nc.Reject(ssh.UnknownChannelType, "only direct-tcpip")
			continue
		}
		var p struct {
			Host     string
			Port     uint32
			OrigHost string
			OrigPort uint32
		}
		if err := ssh.Unmarshal(nc.ExtraData(), &p); err != nil {
			_ = nc.Reject(ssh.ConnectionFailed, "bad payload")
			continue
		}
		addr := net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port)))
		s.mu.Lock()
		s.targets = append(s.targets, addr)
		s.mu.Unlock()
		if s.Forward == nil {
			_ = nc.Reject(ssh.ConnectionFailed, "connect failed")
			continue
		}
		target, err := s.Forward(context.Background(), addr)
		if err != nil {
			_ = nc.Reject(ssh.ConnectionFailed, "connect failed")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			_ = target.Close()
			continue
		}
		go ssh.DiscardRequests(creqs)
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = io.Copy(ch, target)
			_ = ch.CloseWrite()
			_ = ch.Close()
		}()
		go func() {
			defer wg.Done()
			_, _ = io.Copy(target, ch)
			_ = target.Close()
		}()
	}
}
