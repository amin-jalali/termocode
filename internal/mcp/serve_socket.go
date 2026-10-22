package mcp

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// The MCP server's listener (M1 MS-1): the running UI listens on a unix
// socket in the private session dir; `termocode mcp` (MS-2) relays an
// agent's stdio to it. Every accepted connection passes a peer check
// (same uid) before it gets its own MCPServer over newline framing.

// ErrSocketBusy: another process answers on the socket path.
var ErrSocketBusy = errors.New("another termocode already serves MCP for this folder")

// MaxSocketConns caps the connections served at once; more are closed.
const MaxSocketConns = 16

// ListenUnix listens on path with mode 0600. A socket file nobody answers
// on (a crashed run) is removed first; one that answers is ErrSocketBusy;
// any other kind of file is left alone and is an error. The folder must
// be private (0700) already: the socket is reachable there until the
// chmod.
func ListenUnix(path string) (net.Listener, error) {
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			_ = c.Close()
			return nil, ErrSocketBusy
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

// SocketServer serves MCP on a listener until Close.
type SocketServer struct {
	ln     net.Listener
	source ToolSource
	peer   func(net.Conn) error
	onErr  func(error)

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

// Serve accepts connections on ln in the background. peer must accept a
// connection before anything is read from it (nil refuses all). onErr
// (optional) hears refused connections; it runs on a server goroutine.
func Serve(ln net.Listener, source ToolSource, peer func(net.Conn) error, onErr func(error)) *SocketServer {
	s := &SocketServer{ln: ln, source: source, peer: peer, onErr: onErr, conns: map[net.Conn]struct{}{}}
	s.wg.Add(1)
	go s.accept()
	return s
}

func (s *SocketServer) report(err error) {
	if s.onErr != nil {
		s.onErr(err)
	}
}

func (s *SocketServer) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || errors.Is(err, net.ErrClosed) {
				return
			}
			s.report(err)
			time.Sleep(50 * time.Millisecond) // EMFILE and friends: do not spin
			continue
		}
		s.mu.Lock()
		if s.closed || len(s.conns) >= MaxSocketConns {
			s.mu.Unlock()
			_ = c.Close()
			continue
		}
		s.conns[c] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go s.serve(c)
	}
}

func (s *SocketServer) serve(c net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
		_ = c.Close()
	}()
	if s.peer == nil {
		s.report(errors.New("connection refused: no peer check"))
		return
	}
	if err := s.peer(c); err != nil {
		s.report(fmt.Errorf("connection refused: %w", err))
		return
	}
	conn := extproc.NewConnWith(c, c, NewServer(s.source).Handle, extproc.ConnOptions{Framing: extproc.FramingNewline})
	<-conn.Done()
	conn.Close()
}

// Conns is how many connections are served now.
func (s *SocketServer) Conns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// Close stops accepting, closes every connection (clients see EOF) and
// waits for the server goroutines. A unix listener removes its socket
// file. Calls still running end when their tools see the closed
// connection's context.
func (s *SocketServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	err := s.ln.Close()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return err
}
