//go:build unix

package webgw

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/relay"
	"github.com/amin-jalali/termocode/internal/relaytun"
	"github.com/amin-jalali/termocode/internal/webdevices"
)

// The relay (web_relay, ADR 0022, WEB-52/53): besides its own listener
// the gateway dials out to a blind relay and keeps a waiting connection
// in its room there. A phone's page that comes pairs with it; the two
// run a Noise IK handshake (internal/relaytun) with the gateway's
// static key from the QR code, and the page's HTTP requests and
// WebSockets then run in streams inside it, into the same handlers as
// the laptop's own address. The relay sees only ciphertext.
//
// The key and the room come from the UI over the control connection
// (EvRelayKey): the private key lives in the UI's secret store, never
// in argv, env, a file of the gateway or a log.
//
// Who gets in: after the handshake the gateway knows the page's static
// key. A paired device with that relay key, or a page that brought the
// open pairing link's secret, goes on; any other is refused before any
// HTTP. Inside, the panel's own sign-in still applies: a relay pairing
// ends without a passkey (the relay's origin is not the laptop's, so a
// passkey cannot work there) and binds the device to the phone's relay
// key, which then signs in with /login/relay (tunnel requests only) and
// still asks for the one-time code with web_require_totp.

// RelayConfig turns the relay on.
type RelayConfig struct {
	// URL is the relay's base address: https://host[:port] (http only
	// for a relay on loopback, for tests and a local self-check).
	URL string
	// Dial opens the relay WebSocket (nil: websocket.Dial).
	Dial func(ctx context.Context, url string) (*websocket.Conn, error)
}

// EvRelayKey is the UI's event with the relay key: {key, room}, the
// key as 43 base64url characters of the X25519 private key.
const EvRelayKey = "relay_key"

// EvRelayOff is the UI's event that stops waiting on the relay (Web:
// Stop Relay): the dialer and every relay session end; the panel's own
// address goes on. A new EvRelayKey starts the wait again.
const EvRelayOff = "relay_off"

// maxRelaySessions caps the phones on the relay at once.
const maxRelaySessions = 4

// relayHandshakeWait bounds one handshake.
const relayHandshakeWait = 20 * time.Second

// relayPeerKey is the context key of a tunnel request's proven phone key.
type relayPeerKey struct{}

// relayPeer is the phone's relay key of a request that came through the
// relay; ok is false for a request on the laptop's own address.
func relayPeer(ctx context.Context) (relaytun.PeerKey, bool) {
	k, ok := ctx.Value(relayPeerKey{}).(relaytun.PeerKey)
	return k, ok
}

// viaRelay reports a request that came through the relay.
func viaRelay(r *http.Request) bool {
	_, ok := relayPeer(r.Context())
	return ok
}

// relayEntry makes a tunnel request look like the page's own: the
// site's Host and Origin (only our page, holding a proven key, can open
// a stream, so no other site can be behind it), no cookie and none of
// the headers a proxy in front of the laptop would set.
func relayEntry(site Site, next http.Handler) http.Handler {
	origin := site.Scheme + "://" + site.Hosts[0]
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !viaRelay(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		r.Host, r.URL.Host = site.Hosts[0], ""
		for _, h := range []string{"Origin", "Cookie", "X-Forwarded-For", "X-Forwarded-Host", "Forwarded",
			"Tailscale-User-Login", "Tailscale-User-Name", "Tailscale-User-Profile-Pic"} {
			r.Header.Del(h)
		}
		r.Header.Set("Origin", origin)
		next.ServeHTTP(w, r)
	})
}

// relayLink is the gateway's side of the relay.
type relayLink struct {
	cfg     RelayConfig
	audit   *Audit
	devices *webdevices.Store
	pairing *Pairing
	ln      *relaytun.Listener
	srv     *http.Server
	gate    *Bucket // refused handshakes logged, rate-limited

	mu       sync.Mutex
	stopLoop context.CancelFunc
	sessions map[*relaytun.Session]struct{}
	stopped  bool
	wg       sync.WaitGroup
}

func newRelayLink(cfg RelayConfig, site Site, mux http.Handler, audit *Audit, devices *webdevices.Store, pairing *Pairing) *relayLink {
	l := &relayLink{cfg: cfg, audit: audit, devices: devices, pairing: pairing, ln: relaytun.NewListener(),
		gate: NewBucket(blockedAuditPerSecond, blockedAuditBurst), sessions: map[*relaytun.Session]struct{}{}}
	srv := NewServer(site, mux, DefaultLimits, audit)
	srv.Handler = relayEntry(site, srv.Handler)
	srv.ErrorLog = log.New(io.Discard, "", 0)
	srv.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
		if p, ok := c.(interface{ Peer() relaytun.PeerKey }); ok {
			return context.WithValue(ctx, relayPeerKey{}, p.Peer())
		}
		return ctx
	}
	l.srv = srv
	crash.Go("web gateway: the relay server", func() { _ = srv.Serve(l.ln) })
	return l
}

// setKey starts (or restarts) waiting in room with key.
func (l *relayLink) setKey(data json.RawMessage) {
	var d struct {
		Key  string `json:"key"`
		Room string `json:"room"`
	}
	if json.Unmarshal(data, &d) != nil || !relay.ValidRoom(d.Room) {
		return
	}
	b, err := base64.RawURLEncoding.DecodeString(d.Key)
	if err != nil || len(b) != 32 {
		return
	}
	key, err := relaytun.KeypairFromPrivate([32]byte(b))
	if err != nil {
		return
	}
	u, err := relay.WSURL(l.cfg.URL, d.Room, relay.SideGateway)
	if err != nil {
		_ = l.audit.Log(Event{Kind: EvOp, Op: "relay", Why: "bad_url"})
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stopped {
		return
	}
	if l.stopLoop != nil {
		l.stopLoop() // a new key: the old room's sessions end too
		for s := range l.sessions {
			_ = s.Close()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.stopLoop = cancel
	l.wg.Add(1)
	crash.Go("web gateway: the relay dialer", func() {
		defer l.wg.Done()
		l.loop(ctx, u, key)
	})
	_ = l.audit.Log(Event{Kind: EvOp, Op: "relay", Summary: "waiting at " + relayHost(l.cfg.URL)})
}

func relayHost(base string) string {
	if u, err := url.Parse(base); err == nil {
		return u.Host
	}
	return "the relay"
}

func (l *relayLink) dial(ctx context.Context, u string) (*websocket.Conn, error) {
	if l.cfg.Dial != nil {
		return l.cfg.Dial(ctx, u)
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(dctx, u, &websocket.DialOptions{CompressionMode: websocket.CompressionDisabled})
	return c, err
}

// loop keeps one connection waiting in the room. Each that a page takes
// is served on its own; the loop opens the next one at once.
func (l *relayLink) loop(ctx context.Context, u string, key relaytun.Keypair) {
	backoff := time.Second
	for ctx.Err() == nil {
		c, err := l.dial(ctx, u)
		if err != nil {
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(2*backoff, 30*time.Second)
			continue
		}
		backoff = time.Second
		typ, b, err := c.Read(ctx) // a cancelled ctx drops the connection
		if err != nil || typ != websocket.MessageText || string(b) != relay.MsgPaired {
			_ = c.CloseNow()
			if err != nil && websocket.CloseStatus(err) != relay.CloseIdle && !sleepCtx(ctx, time.Second) {
				return
			}
			continue
		}
		l.wg.Add(1)
		crash.Go("web gateway: a relay session", func() {
			defer l.wg.Done()
			l.serve(ctx, c, key)
		})
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// serve runs the handshake on a paired connection and hands its streams
// to the gateway's HTTP server until the session ends.
func (l *relayLink) serve(ctx context.Context, c *websocket.Conn, key relaytun.Keypair) {
	l.mu.Lock()
	full := len(l.sessions) >= maxRelaySessions
	l.mu.Unlock()
	if full {
		_ = c.Close(websocket.StatusTryAgainLater, "busy")
		return
	}
	hctx, cancel := context.WithTimeout(ctx, relayHandshakeWait)
	sess, err := relaytun.Accept(hctx, relaytun.WSConn(c), key, l.allow)
	cancel()
	if err != nil {
		if l.gate.Allow(time.Now()) {
			why := "handshake"
			if errors.Is(err, relaytun.ErrNotPaired) {
				why = "not_paired"
			}
			_ = l.audit.Log(Event{Kind: EvOp, Op: "relay", Why: why, Summary: "a phone was refused"})
		}
		return
	}
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		_ = sess.Close()
		return
	}
	l.sessions[sess] = struct{}{}
	l.mu.Unlock()
	_ = l.audit.Log(Event{Kind: EvOp, Op: "relay", Summary: "a phone came through the relay"})
	stop := context.AfterFunc(ctx, func() { _ = sess.Close() })
	l.ln.Serve(sess)
	stop()
	<-sess.Done()
	l.mu.Lock()
	delete(l.sessions, sess)
	l.mu.Unlock()
}

// allow lets in a paired device's relay key, or a page that brought the
// open pairing link's secret (it pairs next, inside the tunnel).
func (l *relayLink) allow(peer relaytun.PeerKey, hello relaytun.Hello) error {
	if _, ok := l.deviceFor(peer); ok {
		return nil
	}
	if hello.Pair != "" && l.pairing != nil && l.pairing.Open(hello.Pair) {
		return nil
	}
	return relaytun.ErrNotPaired
}

func (l *relayLink) deviceFor(peer relaytun.PeerKey) (webdevices.Device, bool) {
	return relayDevice(l.devices, peer)
}

// relayDevice is the paired, non-guest device bound to peer's key.
func relayDevice(st *webdevices.Store, peer relaytun.PeerKey) (webdevices.Device, bool) {
	if st == nil {
		return webdevices.Device{}, false
	}
	devs, err := st.List()
	if err != nil {
		return webdevices.Device{}, false
	}
	want := peer.String()
	for _, d := range devs {
		if d.RelayKey != "" && d.RelayKey == want && !d.Guest {
			return d, true
		}
	}
	return webdevices.Device{}, false
}

// recheck ends the relay sessions whose key no longer belongs to a
// paired device (one was removed on the laptop).
func (l *relayLink) recheck() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for s := range l.sessions {
		if _, ok := relayDevice(l.devices, s.Peer()); !ok {
			_ = s.Close()
		}
	}
}

// pause ends the dialer and every session; the tunnel's server stays
// for a later setKey.
func (l *relayLink) pause() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stopLoop == nil {
		return
	}
	l.stopLoop()
	l.stopLoop = nil
	for s := range l.sessions {
		_ = s.Close()
	}
	_ = l.audit.Log(Event{Kind: EvOp, Op: "relay", Summary: "stopped"})
}

// stop ends the dialer, every session and the tunnel's server.
func (l *relayLink) stop() {
	l.mu.Lock()
	l.stopped = true
	if l.stopLoop != nil {
		l.stopLoop()
	}
	for s := range l.sessions {
		_ = s.Close()
	}
	l.mu.Unlock()
	_ = l.ln.Close()
	_ = l.srv.Close()
	l.wg.Wait()
}
