//go:build unix

package webgw

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The WebSocket end of the gateway (WEB-8). One browser tab is one
// WebSocket and one connection to the hub in the UI process. The gateway
// is a pipe with limits: it checks every browser message against the
// protocol and the op table before the hub sees it, and it never makes up
// a message of its own except an error for a refused request.
//
// Order on a new connection:
//   - before the upgrade: an exact Origin, the termocode.v2 subprotocol, a
//     live session to log in to, a gateway that is not stopping;
//   - after it: no HTTP deadlines (the WebSocket has its own), no
//     compression (never-do 11), a 4 KiB read limit until hello;
//   - hello within HelloWait, v 2 (else 4011), a live token (else 4001 or
//     4002);
//   - then the hub: the "open" line, the hello without its token, and from
//     there one line per message both ways.

// Subprotocol is the one WebSocket subprotocol the gateway speaks.
const Subprotocol = "termocode.v2"

// WSConfig sets up the WebSocket endpoint.
type WSConfig struct {
	// Site is the one origin the browser may connect from.
	Site Site
	// Tokens are the live sessions; hello carries one.
	Tokens *Tokens
	// Audit hears connects, disconnects and refused messages (nil: none).
	Audit *Audit
	// Dial opens a new connection to the hub (<key>.web.sock).
	Dial func(ctx context.Context) (net.Conn, error)
	// Now is the clock (nil: time.Now).
	Now func() time.Time
	// Activity hears every act of the user (a request, a ping from a
	// visible page): the gateway's idle timer. nil: nobody listens.
	Activity func()

	// HelloWait is how long a new connection may take to say hello.
	HelloWait time.Duration
	// WriteWait bounds one write to the browser; a browser that does not
	// read for this long is too slow (4008).
	WriteWait time.Duration
	// HubWait bounds one write to the hub.
	HubWait time.Duration
}

// Defaults of WSConfig.
const (
	DefaultHelloWait = 5 * time.Second
	DefaultWriteWait = 5 * time.Second
	DefaultHubWait   = 15 * time.Second
)

// closeInternal (1011) is for a fault inside the gateway. It is not in
// the protocol's table: the page shows its generic error for it, as for
// a link that just broke.
const closeInternal = webproto.CloseCode(websocket.StatusInternalError)

// maxHubLine caps one line from the hub. The hub's whole queue to one
// browser is 2 MiB, so no real line comes near it.
const maxHubLine = 4 << 20

// WS serves GET /ws.
type WS struct {
	cfg WSConfig

	mu      sync.Mutex
	conns   map[*wsConn]struct{} // past hello
	stopped bool
	wg      sync.WaitGroup
}

// NewWS makes the endpoint; zero durations get their defaults.
func NewWS(cfg WSConfig) *WS {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HelloWait <= 0 {
		cfg.HelloWait = DefaultHelloWait
	}
	if cfg.WriteWait <= 0 {
		cfg.WriteWait = DefaultWriteWait
	}
	if cfg.HubWait <= 0 {
		cfg.HubWait = DefaultHubWait
	}
	return &WS{cfg: cfg, conns: map[*wsConn]struct{}{}}
}

// Count is the number of browsers connected now (past hello).
func (s *WS) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// CloseAll closes every browser with code and refuses new ones from now
// on. It waits at most wait for the closes to finish.
func (s *WS) CloseAll(code webproto.CloseCode, wait time.Duration) {
	s.mu.Lock()
	s.stopped = true
	conns := make([]*wsConn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.close(code)
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(wait):
	}
}

// Wait waits until every connection handler returned. Call it after
// CloseAll.
func (s *WS) Wait() { s.wg.Wait() }

func (s *WS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.upgradeOK(w, r) {
		return
	}
	defer s.wg.Done()
	// The server's request deadlines would cut the WebSocket off after
	// ReadTimeout; the WebSocket has its own (hello, writes, pings).
	rc := http.NewResponseController(w)
	if rc.SetReadDeadline(time.Time{}) != nil || rc.SetWriteDeadline(time.Time{}) != nil {
		refuse(w, http.StatusInternalServerError, "cannot serve a WebSocket here")
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:    []string{Subprotocol},
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &wsConn{s: s, ws: ws, ctx: ctx, cancel: cancel, closed: make(chan struct{})}
	c.serve()
}

// upgradeOK refuses, before the upgrade, what can never become a
// session. The guard checked Host and Origin already; the Origin check is
// repeated here so this handler is safe on its own.
func (s *WS) upgradeOK(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet || !headerHasToken(r.Header, "Upgrade", "websocket") {
		refuse(w, http.StatusBadRequest, "need a WebSocket")
		return false
	}
	origins := r.Header.Values("Origin")
	if !s.cfg.Site.hasHost(r.Host) || len(origins) != 1 || origins[0] != s.cfg.Site.Scheme+"://"+r.Host {
		refuse(w, http.StatusForbidden, "wrong origin")
		return false
	}
	if !headerHasToken(r.Header, "Sec-WebSocket-Protocol", Subprotocol) {
		refuse(w, http.StatusBadRequest, "need the "+Subprotocol+" subprotocol")
		return false
	}
	if s.cfg.Tokens == nil || s.cfg.Tokens.Len() == 0 {
		refuse(w, http.StatusUnauthorized, "no web session")
		return false
	}
	// Counted under the lock, so Wait after CloseAll misses no handler.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		refuse(w, http.StatusServiceUnavailable, "the web panel is stopping")
		return false
	}
	s.wg.Add(1)
	return true
}

// headerHasToken reports whether a comma-separated header lists tok
// (case-insensitive, as HTTP tokens are).
func headerHasToken(h http.Header, key, tok string) bool {
	for _, v := range h.Values(key) {
		for _, part := range splitComma(v) {
			if equalFoldASCII(part, tok) {
				return true
			}
		}
	}
	return false
}

func splitComma(v string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(v); i++ {
		if i == len(v) || v[i] == ',' {
			p := v[start:i]
			for len(p) > 0 && (p[0] == ' ' || p[0] == '\t') {
				p = p[1:]
			}
			for len(p) > 0 && (p[len(p)-1] == ' ' || p[len(p)-1] == '\t') {
				p = p[:len(p)-1]
			}
			out = append(out, p)
			start = i + 1
		}
	}
	return out
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// wsConn is one browser connection.
type wsConn struct {
	s      *WS
	ws     *websocket.Conn
	ctx    context.Context // ends when the connection closes
	cancel context.CancelFunc

	id    string
	sess  Sess
	token string // to re-check the session's idle and max time; never logged

	hub   net.Conn
	hubMu sync.Mutex // one writer to the hub at a time
	hubW  *bufio.Writer

	once   sync.Once
	closed chan struct{}
	code   webproto.CloseCode // why it ended (set once, in close)

	hubErr bool // hub reader only: the hub sent an error for the connection

	lastID uint64 // browser reader only: the last request id
}

// serve runs one connection until it ends.
func (c *wsConn) serve() {
	defer c.close(webproto.CloseNormal) // no-op when something closed it first
	if !c.hello() {
		return
	}
	go c.fromHub()
	c.fromBrowser()
}

// hello reads and checks the first message, then opens the hub
// connection. false: the connection was closed.
func (c *wsConn) hello() bool {
	c.ws.SetReadLimit(webproto.MaxHello)
	// A timer, not a read deadline: a cancelled read drops the link
	// without the close frame the page needs.
	late := time.AfterFunc(c.s.cfg.HelloWait, func() { c.protoError("hello_late", webproto.ClosePolicy) })
	typ, b, err := c.ws.Read(c.ctx)
	if !late.Stop() {
		return false
	}
	if err != nil {
		c.close(readEndCode(err))
		return false
	}
	if typ != websocket.MessageText {
		c.protoError("binary", webproto.CloseUnsupportedData)
		return false
	}
	msg, err := webproto.DecodeClient(b)
	if err != nil {
		if helloVersion(b) != webproto.Version {
			c.protoError("version", webproto.CloseUnsupportedVersion)
			return false
		}
		c.protoError("bad_hello", webproto.ClosePolicy)
		return false
	}
	h, ok := msg.(*webproto.Hello)
	if !ok {
		c.protoError("not_hello", webproto.ClosePolicy)
		return false
	}
	if h.V != webproto.Version {
		c.protoError("version", webproto.CloseUnsupportedVersion)
		return false
	}
	sess, err := c.s.cfg.Tokens.Check(h.Token, c.s.cfg.Now())
	if err != nil {
		code := webproto.CloseAuthFailed
		if errors.Is(err, ErrTokenExpired) {
			code = webproto.CloseTokenExpired
		}
		_ = c.s.cfg.Audit.Log(Event{Kind: EvAuthFail, Code: int(code), Why: "token"})
		c.close(code)
		return false
	}
	c.sess, c.token = sess, h.Token
	if c.id, err = connID(); err != nil {
		c.close(closeInternal)
		return false
	}
	if !c.openHub(h) {
		return false
	}
	c.ws.SetReadLimit(webproto.MaxFrame)
	if !c.s.add(c) {
		c.close(webproto.CloseStopped)
		return false
	}
	_ = c.s.cfg.Audit.Log(Event{Kind: EvConnect, Conn: c.id, Device: c.sess.Device})
	c.activity()
	return true
}

// helloVersion reads "v" of a hello the strict decoder refused (a newer
// page may carry fields this binary does not know); 0 if it is not one.
func helloVersion(b []byte) int {
	var head struct {
		T string `json:"t"`
		V int    `json:"v"`
	}
	if json.Unmarshal(b, &head) != nil || head.T != webproto.KindHello || head.V == 0 {
		return webproto.Version
	}
	return head.V
}

// openHub dials the hub and sends the "open" line and the hello (with its
// token taken out: the hub does not check tokens and should not hold
// them). false: the connection was closed with 4012.
func (c *wsConn) openHub(h *webproto.Hello) bool {
	dctx, cancel := context.WithTimeout(c.ctx, c.s.cfg.HubWait)
	hub, err := c.s.cfg.Dial(dctx)
	cancel()
	if err != nil {
		c.close(webproto.CloseHubGone)
		return false
	}
	c.hub = hub
	c.hubW = bufio.NewWriterSize(hub, 64<<10)
	open, err1 := webproto.Encode(&webproto.Open{T: webproto.KindOpen, Conn: c.id,
		Role: hubRole(c.sess.Role), Device: c.sess.Device, Name: h.DeviceName})
	hello := *h
	hello.Token = ""
	fwd, err2 := webproto.Encode(&hello)
	if err1 != nil || err2 != nil || !c.toHub(open) || !c.toHub(fwd) {
		c.close(webproto.CloseHubGone)
		return false
	}
	return true
}

// hubRole is the hub's name for a session role. Anything unknown is a
// viewer: the hub then refuses every op that changes something.
func hubRole(r Role) webproto.Role {
	if r == RoleControl {
		return webproto.RoleController
	}
	return webproto.RoleViewer
}

// add registers a connection past hello; false once the gateway stops.
func (s *WS) add(c *wsConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

// fromBrowser reads browser messages until the connection ends.
func (c *wsConn) fromBrowser() {
	for {
		typ, b, err := c.ws.Read(c.ctx)
		if err != nil {
			c.close(readEndCode(err))
			return
		}
		if typ != websocket.MessageText {
			c.protoError("binary", webproto.CloseUnsupportedData)
			return
		}
		msg, err := webproto.DecodeClient(b)
		if err != nil {
			c.protoError("bad_message", webproto.ClosePolicy)
			return
		}
		switch m := msg.(type) {
		case *webproto.Hello:
			c.protoError("hello_twice", webproto.ClosePolicy)
			return
		case *webproto.Bye:
			_ = c.toHub(b)
			c.close(webproto.CloseNormal)
			return
		case *webproto.Ping:
			if m.Here {
				c.activity()
			}
		case *webproto.Req:
			switch c.checkReq(m) {
			case reqClosed:
				return
			case reqAnswered:
				continue
			}
			c.activity()
		}
		if !c.toHub(b) {
			c.close(webproto.CloseHubGone)
			return
		}
	}
}

// activity marks the session used now; a session past its own idle or
// max time ends with 4002.
func (c *wsConn) activity() {
	if _, err := c.s.cfg.Tokens.Check(c.token, c.s.cfg.Now()); err != nil {
		go c.close(webproto.CloseTokenExpired)
		return
	}
	if c.s.cfg.Activity != nil {
		c.s.cfg.Activity()
	}
}

// toHub writes one line to the hub; false when the hub is gone or stalls.
func (c *wsConn) toHub(line []byte) bool {
	c.hubMu.Lock()
	defer c.hubMu.Unlock()
	_ = c.hub.SetWriteDeadline(time.Now().Add(c.s.cfg.HubWait))
	_, _ = c.hubW.Write(line)
	_ = c.hubW.WriteByte('\n')
	return c.hubW.Flush() == nil
}

// fromHub copies hub lines to the browser until either side ends. It
// reads the hub only as fast as the browser takes the lines, so a slow
// browser fills the hub's queue (which resyncs or drops it), never the
// gateway's memory.
func (c *wsConn) fromHub() {
	br := bufio.NewReaderSize(c.hub, 64<<10)
	for {
		line, err := readLine(br, maxHubLine)
		if err != nil {
			c.hubEnded()
			return
		}
		var head struct {
			T    string          `json:"t"`
			ID   uint64          `json:"id"`
			Code json.RawMessage `json:"code"` // a number in bye, a name in err
		}
		if json.Unmarshal(line, &head) != nil {
			c.close(closeInternal)
			return
		}
		if head.T == webproto.KindErr && head.ID == 0 {
			c.hubErr = true // the hub closes this connection next
		}
		wctx, cancel := context.WithTimeout(c.ctx, c.s.cfg.WriteWait)
		err = c.ws.Write(wctx, websocket.MessageText, line)
		late := wctx.Err() != nil && c.ctx.Err() == nil
		cancel()
		if err != nil {
			if late {
				c.close(webproto.CloseSlowClient)
			} else {
				c.close(webproto.CloseNormal)
			}
			return
		}
		if head.T == webproto.KindBye {
			var code webproto.CloseCode
			if json.Unmarshal(head.Code, &code) != nil || code.Reason() == "" { // only codes of the protocol
				code = webproto.CloseNormal
			}
			c.close(code)
			return
		}
	}
}

// hubEnded maps the end of the hub connection to a close code: after an
// error for the connection it was a protocol error (1008); if the hub
// still answers it dropped only this browser, which happens when the
// browser is too slow (4008); else the UI process is gone (4012).
func (c *wsConn) hubEnded() {
	select {
	case <-c.closed:
		return
	default:
	}
	switch {
	case c.hubErr:
		c.close(webproto.ClosePolicy)
	case c.s.hubAlive(c.ctx):
		c.close(webproto.CloseSlowClient)
	default:
		c.close(webproto.CloseHubGone)
	}
}

// hubAlive reports whether the hub takes a new connection. The probe
// sends nothing; the hub drops it quietly.
func (s *WS) hubAlive(ctx context.Context) bool {
	pctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	nc, err := s.cfg.Dial(pctx)
	if err != nil {
		return false
	}
	_ = nc.Close()
	return true
}

// protoError logs a refused browser message by its rule and closes.
func (c *wsConn) protoError(why string, code webproto.CloseCode) {
	_ = c.s.cfg.Audit.Log(Event{Kind: EvProtoError, Conn: c.id, Device: c.sess.Device, Why: why, Code: int(code)})
	c.close(code)
}

// close ends the connection once: the close frame with code, the hub
// connection, the context. Safe from any goroutine.
func (c *wsConn) close(code webproto.CloseCode) {
	c.once.Do(func() {
		c.code = code
		close(c.closed)
		if c.hub != nil {
			_ = c.hub.Close()
		}
		c.s.mu.Lock()
		_, live := c.s.conns[c]
		delete(c.s.conns, c)
		c.s.mu.Unlock()
		// The close frame first: a cancelled read would drop the link
		// without one, and the page needs the code.
		_ = c.ws.Close(websocket.StatusCode(code), code.Reason())
		c.cancel()
		if live {
			_ = c.s.cfg.Audit.Log(Event{Kind: EvDisconnect, Conn: c.id, Device: c.sess.Device, Code: int(code)})
		}
	})
}

// readEndCode is the close code for a read that failed: the browser's own
// close code, or 1006 for a link that just broke.
func readEndCode(err error) webproto.CloseCode {
	if st := websocket.CloseStatus(err); st != -1 {
		return webproto.CloseCode(st)
	}
	return webproto.CloseCode(websocket.StatusAbnormalClosure)
}

// connID names one WebSocket for the hub and the audit log.
func connID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "c-" + hex.EncodeToString(b), nil
}

// errLineTooLong: a hub line over the cap.
var errLineTooLong = errors.New("line too long")

// readLine reads one newline-framed line of at most max bytes (without
// the newline). The result does not alias br's buffer.
func readLine(br *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		part, err := br.ReadSlice('\n')
		if len(buf)+len(part) > max+1 {
			return nil, errLineTooLong
		}
		buf = append(buf, part...)
		switch {
		case err == nil:
			return buf[:len(buf)-1], nil
		case !errors.Is(err, bufio.ErrBufferFull):
			return nil, err
		}
	}
}
