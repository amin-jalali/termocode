package relaytun

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/flynn/noise"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Frames inside a transport message: type (1 byte), stream id (4 bytes,
// big endian), payload. The page opens streams with odd ids, rising.
const (
	frOpen   = 1 // page → gateway: a new stream
	frData   = 2 // bytes, at most the receiver's window
	frFin    = 3 // the sender writes no more (half close)
	frClose  = 4 // the sender is done both ways: stop writing
	frWindow = 5 // 4-byte increment: the receiver read that many bytes
)

const (
	// Window is each stream's receive window, in bytes.
	Window = 256 << 10
	// chunk is the largest DATA payload, so streams take turns.
	chunk = 16 << 10
	// MaxStreams caps the open streams of one session.
	MaxStreams = 128
	// writeWait bounds one message write to the relay.
	writeWait = 30 * time.Second
)

var errProtocol = errors.New("relay: a bad frame inside the tunnel")

// ErrSessionClosed: the session ended.
var ErrSessionClosed = errors.New("relay: the session ended")

// Session is one Noise channel with its streams.
type Session struct {
	conn      MsgConn
	peer      PeerKey
	initiator bool
	ctx       context.Context
	cancel    context.CancelFunc

	wmu  sync.Mutex // one encrypt+write at a time: nonces go in order
	send *noise.CipherState
	recv *noise.CipherState

	mu      sync.Mutex
	streams map[uint32]*stream
	nextID  uint32
	lastIn  uint32
	accept  chan *stream
	done    chan struct{}
	err     error
	endOnce sync.Once
}

func newSession(c MsgConn, send, recv *noise.CipherState, peer PeerKey, initiator bool) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{conn: c, send: send, recv: recv, peer: peer, initiator: initiator, ctx: ctx, cancel: cancel,
		streams: map[uint32]*stream{}, nextID: 1, accept: make(chan *stream, MaxStreams), done: make(chan struct{})}
	crash.GoWith("relaytun: read loop", s.readLoop, s.cancel)
	return s
}

// Peer is the other end's static key, proven by the handshake.
func (s *Session) Peer() PeerKey { return s.peer }

// Done is closed when the session ends.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err says why the session ended (nil while it runs).
func (s *Session) Err() error {
	select {
	case <-s.done:
		return s.err
	default:
		return nil
	}
}

// Close ends the session and every stream in it.
func (s *Session) Close() error {
	s.end(ErrSessionClosed)
	return nil
}

func (s *Session) end(err error) {
	s.endOnce.Do(func() {
		s.mu.Lock()
		s.err = err
		streams := s.streams
		s.streams = map[uint32]*stream{}
		s.mu.Unlock()
		close(s.done)
		s.cancel()
		_ = s.conn.Close()
		for _, st := range streams {
			st.kill()
		}
	})
}

func (s *Session) readLoop() {
	for {
		msg, err := s.conn.ReadMsg(s.ctx)
		if err != nil {
			s.end(err)
			return
		}
		if len(msg) > MaxMsg {
			s.end(errProtocol)
			return
		}
		pt, err := s.recv.Decrypt(nil, nil, msg)
		if err != nil {
			s.end(ErrDecrypt)
			return
		}
		if err := s.handle(pt); err != nil {
			s.end(err)
			return
		}
	}
}

func (s *Session) handle(pt []byte) error {
	if len(pt) < 5 {
		return errProtocol
	}
	typ, id, body := pt[0], binary.BigEndian.Uint32(pt[1:5]), pt[5:]
	if typ == frOpen {
		// Only the page opens, with a new odd id above the last one.
		if s.initiator || id%2 != 1 || id <= s.lastIn || len(body) != 0 {
			return errProtocol
		}
		s.lastIn = id
		s.mu.Lock()
		if len(s.streams) >= MaxStreams {
			s.mu.Unlock()
			crash.Go("relaytun: refuse stream", func() { _ = s.writeFrame(frClose, id, nil) })
			return nil
		}
		st := newStream(s, id)
		s.streams[id] = st
		s.mu.Unlock()
		select {
		case s.accept <- st:
		default:
			st.kill()
		}
		return nil
	}
	s.mu.Lock()
	st := s.streams[id]
	s.mu.Unlock()
	if st == nil {
		return nil // a stream that ended here already: late frames are dropped
	}
	switch typ {
	case frData:
		return st.gotData(body)
	case frFin:
		st.gotFin()
	case frClose:
		st.gotClose()
	case frWindow:
		if len(body) != 4 {
			return errProtocol
		}
		return st.gotWindow(binary.BigEndian.Uint32(body))
	default:
		return errProtocol
	}
	return nil
}

func frame(typ byte, id uint32, body []byte) []byte {
	pt := make([]byte, 5+len(body))
	pt[0] = typ
	binary.BigEndian.PutUint32(pt[1:5], id)
	copy(pt[5:], body)
	return pt
}

// writeFrame encrypts and sends one frame.
func (s *Session) writeFrame(typ byte, id uint32, body []byte) error {
	pt := frame(typ, id, body)
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return s.writeLocked(pt)
}

// writeLocked sends pt; the caller holds wmu.
func (s *Session) writeLocked(pt []byte) error {
	select {
	case <-s.done:
		return ErrSessionClosed
	default:
	}
	ct, err := s.send.Encrypt(nil, nil, pt)
	if err != nil {
		s.end(err)
		return err
	}
	ctx, cancel := context.WithTimeout(s.ctx, writeWait)
	defer cancel()
	if err := s.conn.WriteMsg(ctx, ct); err != nil {
		s.end(err)
		return ErrSessionClosed
	}
	return nil
}

// OpenStream opens a stream to the gateway (the page's side).
func (s *Session) OpenStream() (net.Conn, error) {
	if !s.initiator {
		return nil, errors.New("relay: only the page opens streams")
	}
	// The id and its OPEN go out under the write lock: ids rise on the wire.
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		return nil, ErrSessionClosed
	default:
	}
	if len(s.streams) >= MaxStreams {
		s.mu.Unlock()
		return nil, errors.New("relay: too many streams")
	}
	id := s.nextID
	s.nextID += 2
	st := newStream(s, id)
	s.streams[id] = st
	s.mu.Unlock()
	if err := s.writeLocked(frame(frOpen, id, nil)); err != nil {
		return nil, err
	}
	return st, nil
}

// AcceptStream waits for the page's next stream (the gateway's side).
func (s *Session) AcceptStream() (net.Conn, error) {
	select {
	case st := <-s.accept:
		return st, nil
	case <-s.done:
		return nil, ErrSessionClosed
	}
}

func (s *Session) forget(id uint32) {
	s.mu.Lock()
	delete(s.streams, id)
	s.mu.Unlock()
}

// stream is one byte stream: a net.Conn.
type stream struct {
	s  *Session
	id uint32

	mu       sync.Mutex
	cond     *sync.Cond
	buf      []byte
	unacked  int  // bytes read, not yet given back as window
	finIn    bool // the peer writes no more
	finOut   bool // we write no more
	closedIn bool // the peer is done both ways
	closed   bool // Close was called here
	dead     bool // the session ended
	sendWin  int
	rdl, wdl time.Time
	timers   [2]*time.Timer
}

func newStream(s *Session, id uint32) *stream {
	st := &stream{s: s, id: id, sendWin: Window}
	st.cond = sync.NewCond(&st.mu)
	return st
}

func (st *stream) kill() {
	st.mu.Lock()
	st.dead = true
	st.cond.Broadcast()
	st.mu.Unlock()
}

func (st *stream) gotData(b []byte) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed || st.finIn {
		return nil // we stopped reading: drop it
	}
	if len(st.buf)+len(b) > Window {
		return errProtocol // the sender broke its window
	}
	st.buf = append(st.buf, b...)
	st.cond.Broadcast()
	return nil
}

func (st *stream) gotFin() {
	st.mu.Lock()
	st.finIn = true
	done := st.finOut || st.closed
	st.cond.Broadcast()
	st.mu.Unlock()
	if done {
		st.s.forget(st.id)
	}
}

func (st *stream) gotClose() {
	st.mu.Lock()
	st.finIn, st.closedIn = true, true
	st.cond.Broadcast()
	st.mu.Unlock()
	st.s.forget(st.id)
}

func (st *stream) gotWindow(n uint32) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if n == 0 || st.sendWin+int(n) > Window {
		return errProtocol
	}
	st.sendWin += int(n)
	st.cond.Broadcast()
	return nil
}

func past(t time.Time) bool { return !t.IsZero() && !time.Now().Before(t) }

func (st *stream) Read(p []byte) (int, error) {
	st.mu.Lock()
	for len(st.buf) == 0 {
		switch {
		case st.closed:
			st.mu.Unlock()
			return 0, net.ErrClosed
		case st.finIn:
			st.mu.Unlock()
			return 0, io.EOF
		case st.dead:
			st.mu.Unlock()
			return 0, ErrSessionClosed
		case past(st.rdl):
			st.mu.Unlock()
			return 0, os.ErrDeadlineExceeded
		}
		st.cond.Wait()
	}
	n := copy(p, st.buf)
	st.buf = st.buf[n:]
	if len(st.buf) == 0 {
		st.buf = nil
	}
	st.unacked += n
	var credit int
	if st.unacked >= Window/4 || (len(st.buf) == 0 && st.unacked > 0) {
		credit, st.unacked = st.unacked, 0
	}
	st.mu.Unlock()
	if credit > 0 {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(credit))
		_ = st.s.writeFrame(frWindow, st.id, b[:])
	}
	return n, nil
}

func (st *stream) Write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		st.mu.Lock()
		for st.sendWin == 0 && !st.closed && !st.closedIn && !st.dead && !st.finOut && !past(st.wdl) {
			st.cond.Wait()
		}
		switch {
		case st.closed || st.finOut:
			st.mu.Unlock()
			return written, net.ErrClosed
		case st.closedIn:
			st.mu.Unlock()
			return written, io.ErrClosedPipe
		case st.dead:
			st.mu.Unlock()
			return written, ErrSessionClosed
		case past(st.wdl):
			st.mu.Unlock()
			return written, os.ErrDeadlineExceeded
		}
		n := min(len(p)-written, chunk, st.sendWin)
		st.sendWin -= n
		st.mu.Unlock()
		if err := st.s.writeFrame(frData, st.id, p[written:written+n]); err != nil {
			return written, err
		}
		written += n
	}
	return written, nil
}

// CloseWrite sends a half close: the peer reads to EOF, and may still write.
func (st *stream) CloseWrite() error {
	st.mu.Lock()
	if st.finOut || st.closed {
		st.mu.Unlock()
		return nil
	}
	st.finOut = true
	done := st.finIn
	st.cond.Broadcast()
	st.mu.Unlock()
	err := st.s.writeFrame(frFin, st.id, nil)
	if done {
		st.s.forget(st.id)
	}
	return err
}

// Close ends the stream both ways. The peer still reads what it got.
func (st *stream) Close() error {
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		return nil
	}
	st.closed = true
	quiet := st.closedIn || st.dead
	for _, t := range st.timers {
		if t != nil {
			t.Stop()
		}
	}
	st.cond.Broadcast()
	st.mu.Unlock()
	st.s.forget(st.id)
	if quiet {
		return nil
	}
	return st.s.writeFrame(frClose, st.id, nil)
}

// Peer is the other end's proven static key (the gateway's HTTP server
// reads it to know which phone a request came from).
func (st *stream) Peer() PeerKey { return st.s.peer }

func (st *stream) LocalAddr() net.Addr  { return addr{} }
func (st *stream) RemoteAddr() net.Addr { return addr{} }

func (st *stream) SetDeadline(t time.Time) error {
	_ = st.SetReadDeadline(t)
	return st.SetWriteDeadline(t)
}

func (st *stream) SetReadDeadline(t time.Time) error  { st.setDeadline(0, t); return nil }
func (st *stream) SetWriteDeadline(t time.Time) error { st.setDeadline(1, t); return nil }

func (st *stream) setDeadline(i int, t time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if i == 0 {
		st.rdl = t
	} else {
		st.wdl = t
	}
	if st.timers[i] != nil {
		st.timers[i].Stop()
		st.timers[i] = nil
	}
	if !t.IsZero() {
		st.timers[i] = time.AfterFunc(time.Until(t), func() {
			st.mu.Lock()
			st.cond.Broadcast()
			st.mu.Unlock()
		})
	}
	st.cond.Broadcast()
}

// addr is a stream's address: the relay, never a network address.
type addr struct{}

func (addr) Network() string { return "relay" }
func (addr) String() string  { return "relay" }

// Listener hands the streams of every session it serves to an
// http.Server, as one net.Listener.
type Listener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

// NewListener makes an empty listener.
func NewListener() *Listener {
	return &Listener{conns: make(chan net.Conn), done: make(chan struct{})}
}

// Serve feeds s's streams to the listener until s or the listener ends.
func (l *Listener) Serve(s *Session) {
	for {
		c, err := s.AcceptStream()
		if err != nil {
			return
		}
		select {
		case l.conns <- c:
		case <-l.done:
			_ = c.Close()
			_ = s.Close()
			return
		}
	}
}

// Accept waits for the next stream.
func (l *Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

// Close stops the listener; Serve then closes its sessions.
func (l *Listener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

// Addr is the relay.
func (l *Listener) Addr() net.Addr { return addr{} }
