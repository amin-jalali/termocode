package dbtest

import (
	"errors"
	"net"
	"sync"
	"time"
)

// Delayed is c whose writes reach the other side d later (one way of a
// slow link): every write is queued with its due time and sent in order,
// so writes still pipeline, as on a real link. Reads pass through. Close
// sends what is queued first, then closes c.
func Delayed(c net.Conn, d time.Duration) net.Conn {
	s := &slowConn{Conn: c, d: d, q: make(chan slowChunk, 1024), done: make(chan struct{})}
	go s.pump()
	return s
}

type slowChunk struct {
	due  time.Time
	data []byte
}

type slowConn struct {
	net.Conn
	d    time.Duration
	q    chan slowChunk
	done chan struct{}

	mu     sync.Mutex
	closed bool
}

func (s *slowConn) pump() {
	defer close(s.done)
	for ch := range s.q {
		if w := time.Until(ch.due); w > 0 {
			time.Sleep(w)
		}
		if _, err := s.Conn.Write(ch.data); err != nil {
			break
		}
	}
	_ = s.Conn.Close()
	for range s.q { // drain after a failed write
	}
}

func (s *slowConn) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("write on a closed connection")
	}
	s.q <- slowChunk{due: time.Now().Add(s.d), data: append([]byte(nil), p...)}
	return len(p), nil
}

func (s *slowConn) Close() error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.q)
	}
	s.mu.Unlock()
	return nil
}
