// Package relay is the web panel's blind relay (ADR 0022, WEB-52): a
// small server that pairs a laptop's gateway and a phone's page by a
// random room id and copies WebSocket messages between them. It never
// parses what it copies: the two ends speak Noise inside (WEB-53), so
// the relay sees only ciphertext, sizes and timing.
//
// A room is a rendezvous, not a session. The gateway keeps a waiting
// connection in its room; a page that comes pairs with one waiting
// connection and the relay tells that gateway "paired" (the only text
// message it ever sends), so the gateway opens the next waiting one.
//
// Limits (every one has a default): message size, bytes per second per
// pair, idle time and lifetime of a pair, waiting gateways and pairs per
// room, rooms in all, open connections and new connections per second
// per client address. The log has connects, ends, byte counts and a
// short hash of the room id; never the room id, never a payload.
package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/crash"
)

// PathPrefix is where the relay answers: PathPrefix + room + "?side=gw|page".
const PathPrefix = "/relay/v1/"

// The two sides of a room.
const (
	SideGateway = "gw"
	SidePage    = "page"
)

// MsgPaired is the text the relay sends a waiting gateway when a page
// pairs with it. Everything else on the wire is a binary message.
const MsgPaired = "paired"

// Close codes of the relay (4000-4999 are the application's own).
const (
	CloseNoPeer  websocket.StatusCode = 4404 // no gateway waited in the room
	CloseLimit   websocket.StatusCode = 4429 // a limit refused this connection
	CloseIdle    websocket.StatusCode = 4408 // the pair was idle too long
	CloseMaxLife websocket.StatusCode = 4410 // the pair reached its lifetime
	CloseGone    websocket.StatusCode = 4503 // the relay stops
)

// Event kinds of the log.
const (
	EvConnect = "connect" // a side came; Side is set
	EvPair    = "pair"    // a page paired with a waiting gateway
	EvEnd     = "end"     // a pair ended; Up/Down are the bytes, Why the reason
	EvRefuse  = "refuse"  // a connection was refused; Why says which limit
)

// Event is one line of the relay's log. It never holds a payload nor
// the room id: Room is the first 8 bytes of SHA-256(room id), in hex.
type Event struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Room string    `json:"room,omitempty"`
	Side string    `json:"side,omitempty"`
	Up   int64     `json:"up,omitempty"`   // page → gateway bytes
	Down int64     `json:"down,omitempty"` // gateway → page bytes
	Why  string    `json:"why,omitempty"`
}

// Config is the relay's limits; a zero field is its default.
type Config struct {
	MaxMessage     int64         // bytes in one message (default 64 KiB + 64)
	BytesPerSecond int           // per pair, both ways together (default 4 MiB)
	Idle           time.Duration // a pair with no message ends (default 15 min)
	MaxLife        time.Duration // a pair ends at the latest (default 12 h)
	PageWait       time.Duration // a page waits this long for a gateway (default 10 s)
	GatewayWait    time.Duration // a waiting gateway is closed after this (default 5 min)
	MaxWaiting     int           // waiting gateways per room (default 2)
	MaxPairs       int           // pairs per room (default 4)
	MaxRooms       int           // rooms in all (default 10000)
	MaxConnsPerIP  int           // open connections per client address (default 20)
	ConnsPerSecond float64       // new connections per client address (default 2, burst 20)
	// TrustProxy takes the client address from the last X-Forwarded-For
	// value: only behind the operator's own TLS proxy.
	TrustProxy bool
	Log        func(Event) // nil: no log
}

func (c Config) withDefaults() Config {
	if c.MaxMessage <= 0 {
		c.MaxMessage = 64<<10 + 64
	}
	if c.BytesPerSecond <= 0 {
		c.BytesPerSecond = 4 << 20
	}
	if c.Idle <= 0 {
		c.Idle = 15 * time.Minute
	}
	if c.MaxLife <= 0 {
		c.MaxLife = 12 * time.Hour
	}
	if c.PageWait <= 0 {
		c.PageWait = 10 * time.Second
	}
	if c.GatewayWait <= 0 {
		c.GatewayWait = 5 * time.Minute
	}
	if c.MaxWaiting <= 0 {
		c.MaxWaiting = 2
	}
	if c.MaxPairs <= 0 {
		c.MaxPairs = 4
	}
	if c.MaxRooms <= 0 {
		c.MaxRooms = 10000
	}
	if c.MaxConnsPerIP <= 0 {
		c.MaxConnsPerIP = 20
	}
	if c.ConnsPerSecond <= 0 {
		c.ConnsPerSecond = 2
	}
	return c
}

var roomRE = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)

// ValidRoom reports whether id is a room id: 22 base64url characters
// (128 bits), nothing else.
func ValidRoom(id string) bool { return roomRE.MatchString(id) }

// NewRoomID makes a room id from 128 random bits.
func NewRoomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// RoomHash is what the log says for a room: never the id itself.
func RoomHash(id string) string {
	s := sha256.Sum256([]byte(id))
	return hex.EncodeToString(s[:8])
}

// Server is the relay. Mount it at PathPrefix (or at the root).
type Server struct {
	cfg    Config
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu    sync.Mutex
	rooms map[string]*room
	ips   map[string]*client
	// closing is set by Close: new connections are refused (join).
	closing bool
}

type room struct {
	waiting []*waiter
	pairs   int
	wake    chan struct{} // closed and remade when a gateway starts to wait
}

// waiter is a gateway connection waiting in its room.
type waiter struct {
	c    *websocket.Conn
	took chan pairing // the page it pairs with
}

// pairing hands a page to a waiting gateway; done closes when the pipe
// is over, so the page's handler (which owns its request) returns then.
type pairing struct {
	page *websocket.Conn // nil: the gateway was dead, give up
	done chan struct{}
}

type client struct {
	open   int
	bucket bucket
	seen   time.Time
}

// New makes a relay with cfg's limits.
func New(cfg Config) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{cfg: cfg.withDefaults(), ctx: ctx, cancel: cancel,
		rooms: map[string]*room{}, ips: map[string]*client{}}
}

// Close ends every connection and waits for them; a connection that
// comes after is refused (join).
func (s *Server) Close() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
}

// join counts a new connection for Close to wait for; false once the
// relay is closing (the Add and Close's flag share the lock, so no Add
// comes while Close waits).
func (s *Server) join() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.wg.Add(1)
	return true
}

// Waiting is the number of gateways waiting in room (for tests and the
// operator's status).
func (s *Server) Waiting(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.rooms[id]; r != nil {
		return len(r.waiting)
	}
	return 0
}

// Pairs is the number of live pairs in room.
func (s *Server) Pairs(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.rooms[id]; r != nil {
		return r.pairs
	}
	return 0
}

func (s *Server) log(e Event) {
	if s.cfg.Log != nil {
		e.At = time.Now().UTC()
		s.cfg.Log(e)
	}
}

// ServeHTTP answers PathPrefix + room + "?side=gw|page" with a WebSocket.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := strings.CutPrefix(r.URL.Path, PathPrefix)
	if !ok {
		http.NotFound(w, r)
		return
	}
	side := r.URL.Query().Get("side")
	if r.Method != http.MethodGet || !ValidRoom(id) || (side != SideGateway && side != SidePage) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ip := s.clientIP(r)
	if why := s.admit(ip); why != "" {
		s.log(Event{Kind: EvRefuse, Room: RoomHash(id), Side: side, Why: why})
		w.Header().Set("Retry-After", "5")
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer s.release(ip)
	if !s.join() {
		http.Error(w, "the relay is stopping", http.StatusServiceUnavailable)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		s.wg.Done()
		return
	}
	defer s.wg.Done() // added in join, before the upgrade
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(s.cfg.MaxMessage)
	s.log(Event{Kind: EvConnect, Room: RoomHash(id), Side: side})
	if side == SideGateway {
		s.serveGateway(id, c)
	} else {
		s.servePage(id, c)
	}
}

func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if v := r.Header.Values("X-Forwarded-For"); len(v) > 0 {
			parts := strings.Split(v[len(v)-1], ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// admit counts a new connection from ip, or says which limit refuses it.
func (s *Server) admit(ip string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	cl := s.ips[ip]
	if cl == nil {
		if len(s.ips) > 4*s.cfg.MaxRooms {
			s.sweepLocked(now)
		}
		cl = &client{bucket: newBucket(s.cfg.ConnsPerSecond, 20)}
		s.ips[ip] = cl
	}
	cl.seen = now
	if cl.open >= s.cfg.MaxConnsPerIP {
		return "conns_per_ip"
	}
	if !cl.bucket.take(now, 1) {
		return "conn_rate"
	}
	cl.open++
	return ""
}

func (s *Server) release(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cl := s.ips[ip]; cl != nil {
		cl.open--
	}
}

// sweepLocked forgets addresses with nothing open for a minute.
func (s *Server) sweepLocked(now time.Time) {
	for ip, cl := range s.ips {
		if cl.open <= 0 && now.Sub(cl.seen) > time.Minute {
			delete(s.ips, ip)
		}
	}
}

// roomLocked returns the room, made when make is true and there is room.
func (s *Server) roomLocked(id string, create bool) *room {
	r := s.rooms[id]
	if r == nil && create && len(s.rooms) < s.cfg.MaxRooms {
		r = &room{wake: make(chan struct{})}
		s.rooms[id] = r
	}
	return r
}

func (s *Server) dropIfEmptyLocked(id string, r *room) {
	if len(r.waiting) == 0 && r.pairs == 0 && s.rooms[id] == r {
		delete(s.rooms, id)
	}
}

func (s *Server) serveGateway(id string, c *websocket.Conn) {
	wt := &waiter{c: c, took: make(chan pairing, 1)}
	s.mu.Lock()
	r := s.roomLocked(id, true)
	if r == nil || len(r.waiting) >= s.cfg.MaxWaiting {
		s.mu.Unlock()
		s.log(Event{Kind: EvRefuse, Room: RoomHash(id), Side: SideGateway, Why: "waiting"})
		_ = c.Close(CloseLimit, "limit")
		return
	}
	r.waiting = append(r.waiting, wt)
	close(r.wake)
	r.wake = make(chan struct{})
	s.mu.Unlock()

	// One reader for the gateway's whole life: while it waits, it notices
	// a gateway that went away (or one that sends early); paired, it
	// feeds the pipe.
	done := make(chan struct{})
	defer close(done)
	msgs := readAll(c, done)
	t := time.NewTimer(s.cfg.GatewayWait)
	defer t.Stop()
	code := CloseIdle
	select {
	case p := <-wt.took:
		s.pipe(id, c, msgs, p)
		return
	case m := <-msgs:
		code = websocket.StatusPolicyViolation // a message before a page came
		if m.err != nil {
			code = 0 // it went away
		}
	case <-t.C:
	case <-s.ctx.Done():
		code = CloseGone
	}
	s.mu.Lock()
	taken := !removeWaiter(r, wt)
	s.dropIfEmptyLocked(id, r)
	s.mu.Unlock()
	if taken { // a page took it in the same moment
		s.pipe(id, c, msgs, <-wt.took)
		return
	}
	if code == 0 {
		_ = c.CloseNow()
		return
	}
	_ = c.Close(code, "")
}

// msg is one read of a connection.
type msg struct {
	typ websocket.MessageType
	b   []byte
	err error
}

// readAll reads c until an error, which is the last message, or done.
func readAll(c *websocket.Conn, done <-chan struct{}) <-chan msg {
	ch := make(chan msg)
	crash.Go("relay: read", func() {
		for {
			typ, b, err := c.Read(context.Background())
			select {
			case ch <- msg{typ, b, err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	})
	return ch
}

func removeWaiter(r *room, wt *waiter) bool {
	for i, w := range r.waiting {
		if w == wt {
			r.waiting = append(r.waiting[:i], r.waiting[i+1:]...)
			return true
		}
	}
	return false
}

func (s *Server) servePage(id string, c *websocket.Conn) {
	deadline := time.NewTimer(s.cfg.PageWait)
	defer deadline.Stop()
	for {
		s.mu.Lock()
		r := s.roomLocked(id, false)
		var wake chan struct{}
		if r != nil {
			if r.pairs >= s.cfg.MaxPairs {
				s.mu.Unlock()
				s.log(Event{Kind: EvRefuse, Room: RoomHash(id), Side: SidePage, Why: "pairs"})
				_ = c.Close(CloseLimit, "limit")
				return
			}
			if len(r.waiting) > 0 {
				wt := r.waiting[0]
				r.waiting = r.waiting[1:]
				r.pairs++
				s.mu.Unlock()
				// The paired note goes first, before any byte of the page.
				ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
				err := wt.c.Write(ctx, websocket.MessageText, []byte(MsgPaired))
				cancel()
				if err != nil { // a dead waiting gateway: try the next one
					s.mu.Lock()
					r.pairs--
					s.mu.Unlock()
					wt.took <- pairing{}
					continue
				}
				s.log(Event{Kind: EvPair, Room: RoomHash(id)})
				done := make(chan struct{})
				wt.took <- pairing{page: c, done: done}
				<-done
				return
			}
			wake = r.wake
		}
		s.mu.Unlock()
		if wake == nil {
			wake = make(chan struct{}) // no room yet: wait for the deadline or a retry
		}
		retry := time.NewTimer(50 * time.Millisecond)
		select {
		case <-wake:
		case <-retry.C:
		case <-deadline.C:
			retry.Stop()
			_ = c.Close(CloseNoPeer, "no laptop")
			return
		case <-s.ctx.Done():
			retry.Stop()
			_ = c.Close(CloseGone, "")
			return
		}
		retry.Stop()
	}
}

// pipe copies messages between a gateway and a page until one ends, a
// limit is hit or the relay stops. It runs on the gateway's handler.
func (s *Server) pipe(id string, gw *websocket.Conn, gwMsgs <-chan msg, p pairing) {
	if p.page == nil {
		_ = gw.CloseNow()
		return
	}
	page := p.page
	defer func() {
		close(p.done)
		s.mu.Lock()
		if r := s.rooms[id]; r != nil {
			r.pairs--
			s.dropIfEmptyLocked(id, r)
		}
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(s.ctx, s.cfg.MaxLife)
	defer cancel()
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	pace := &sharedBucket{}
	pace.b = newBucket(float64(s.cfg.BytesPerSecond), int(maxInt64(s.cfg.MaxMessage, int64(s.cfg.BytesPerSecond/5))))
	type result struct {
		code websocket.StatusCode
		why  string
	}
	res := make(chan result, 3)
	var up, down atomic.Int64
	// Reads and writes never take ctx: coder/websocket drops a connection
	// whose context ends, and an end here is a close with a code.
	pageDone := make(chan struct{})
	defer close(pageDone)
	pageMsgs := readAll(page, pageDone)
	copyOne := func(src <-chan msg, dst *websocket.Conn, n *atomic.Int64) {
		for {
			var m msg
			select {
			case m = <-src:
			case <-ctx.Done(): // the pipe ended: its readers stop too
				res <- result{}
				return
			}
			typ, b, err := m.typ, m.b, m.err
			if err != nil {
				switch {
				case ctx.Err() != nil:
					res <- result{}
				case websocket.CloseStatus(err) == websocket.StatusMessageTooBig || strings.Contains(err.Error(), "read limited"):
					res <- result{websocket.StatusMessageTooBig, "too_big"}
				default:
					res <- result{websocket.StatusNormalClosure, "closed"}
				}
				return
			}
			if typ != websocket.MessageBinary {
				res <- result{websocket.StatusPolicyViolation, "text"}
				return
			}
			if err := pace.wait(ctx, len(b)); err != nil {
				res <- result{}
				return
			}
			last.Store(time.Now().UnixNano())
			wctx, wcancel := context.WithTimeout(context.Background(), 30*time.Second)
			err = dst.Write(wctx, websocket.MessageBinary, b)
			wcancel()
			if err != nil {
				res <- result{websocket.StatusNormalClosure, "closed"}
				return
			}
			n.Add(int64(len(b)))
		}
	}
	panicked := func() { res <- result{websocket.StatusInternalError, "panic"} } // res has room for all three
	crash.GoWith("relay: page to gateway", func() { copyOne(pageMsgs, gw, &up) }, panicked)
	crash.GoWith("relay: gateway to page", func() { copyOne(gwMsgs, page, &down) }, panicked)
	crash.GoWith("relay: idle watch", func() { // the idle watch, the lifetime and the relay's stop
		t := time.NewTicker(maxDur(s.cfg.Idle/4, 10*time.Millisecond))
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				res <- result{}
				return
			case <-t.C:
				if time.Since(time.Unix(0, last.Load())) >= s.cfg.Idle {
					res <- result{CloseIdle, "idle"}
					return
				}
			}
		}
	}, panicked)
	r := <-res
	switch {
	case r.code != 0:
	case s.ctx.Err() != nil:
		r = result{CloseGone, "relay_stop"}
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		r = result{CloseMaxLife, "max_life"}
	default:
		r = result{websocket.StatusNormalClosure, "closed"}
	}
	cancel()
	s.log(Event{Kind: EvEnd, Room: RoomHash(id), Up: up.Load(), Down: down.Load(), Why: r.why})
	// Both closes at once: each waits for its own peer's answer.
	var wg sync.WaitGroup
	for _, c := range []*websocket.Conn{page, gw} {
		wg.Add(1)
		crash.Go("relay: close", func() {
			defer wg.Done()
			_ = c.Close(r.code, "")
		})
	}
	wg.Wait()
}

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// bucket is a token bucket over bytes or connections: burst at once,
// then rate per second.
type bucket struct {
	rate   float64
	burst  float64
	tokens float64
	at     time.Time
}

func newBucket(rate float64, burst int) bucket {
	return bucket{rate: rate, burst: float64(burst), tokens: float64(burst)}
}

func (b *bucket) refill(now time.Time) {
	if !b.at.IsZero() && now.After(b.at) {
		b.tokens += now.Sub(b.at).Seconds() * b.rate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
	}
	b.at = now
}

// take takes n tokens or none.
func (b *bucket) take(now time.Time, n float64) bool {
	b.refill(now)
	if b.tokens < n {
		return false
	}
	b.tokens -= n
	return true
}

// sharedBucket paces both directions of one pair.
type sharedBucket struct {
	mu sync.Mutex
	b  bucket
}

// wait takes n bytes, going into debt and sleeping it off, so a message
// larger than the burst still passes, only later.
func (p *sharedBucket) wait(ctx context.Context, n int) error {
	p.mu.Lock()
	now := time.Now()
	p.b.refill(now)
	p.b.tokens -= float64(n)
	var d time.Duration
	if p.b.tokens < 0 {
		d = time.Duration(-p.b.tokens / p.b.rate * float64(time.Second))
	}
	p.mu.Unlock()
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
