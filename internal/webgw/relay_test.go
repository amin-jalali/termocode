//go:build unix

package webgw

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/relay"
	"github.com/amin-jalali/termocode/internal/relaytun"
	"github.com/amin-jalali/termocode/internal/webdevices"
)

// tapListener records every byte the relay's sockets read and write:
// exactly what the relay sees.
type tapListener struct {
	net.Listener
	mu  sync.Mutex
	all bytes.Buffer
}

func (l *tapListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &tapConn{Conn: c, l: l}, nil
}

func (l *tapListener) seen() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]byte(nil), l.all.Bytes()...)
}

type tapConn struct {
	net.Conn
	l *tapListener
}

func (c *tapConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.l.mu.Lock()
	c.l.all.Write(p[:n])
	c.l.mu.Unlock()
	return n, err
}

func (c *tapConn) Write(p []byte) (int, error) {
	c.l.mu.Lock()
	c.l.all.Write(p)
	c.l.mu.Unlock()
	return c.Conn.Write(p)
}

// relayRig is a relay on 127.0.0.1 with its tap, and a gateway that uses it.
type relayRig struct {
	*gwRig
	rl   *relay.Server
	tap  *tapListener
	url  string
	key  relaytun.Keypair
	room string
}

const relayPageMarker = "PAGE-MARKER-index-html"

func startRelayRig(t *testing.T) *relayRig {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tap := &tapListener{Listener: ln}
	rl := relay.New(relay.Config{PageWait: 5 * time.Second})
	srv := &http.Server{Handler: rl}
	go func() { _ = srv.Serve(tap) }()
	t.Cleanup(func() { rl.Close(); _ = srv.Close() })
	url := "http://" + ln.Addr().String()
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>"+relayPageMarker+"</title>")
	})
	devices := t.TempDir()
	g := startedGateway(t, func(c *RunConfig) {
		c.DevicesDir = devices
		c.Assets = page
		c.Relay = &RelayConfig{URL: url}
	})
	key, err := relaytun.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	room, _ := relay.NewRoomID()
	r := &relayRig{gwRig: g, rl: rl, tap: tap, url: url, key: key, room: room}
	r.sendKey()
	return r
}

// sendKey gives the gateway its relay key and room, as the UI does.
func (r *relayRig) sendKey() {
	r.t.Helper()
	data, _ := json.Marshal(map[string]string{"key": base64.RawURLEncoding.EncodeToString(r.key.Private[:]), "room": r.room})
	r.ctl.send(r.t, `{"t":"ev","kind":"relay_key","data":`+string(data)+`}`)
	deadline := time.Now().Add(10 * time.Second)
	for r.rl.Waiting(r.room) < 1 {
		if time.Now().After(deadline) {
			r.t.Fatal("the gateway never waited in its relay room")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// phone is a headless page: its own key, a Noise session through the
// relay, and an HTTP client over the session's streams.
type phone struct {
	t    *testing.T
	key  relaytun.Keypair
	sess *relaytun.Session
	http *http.Client
}

func (r *relayRig) newPhone() *phone {
	r.t.Helper()
	k, err := relaytun.GenerateKeypair()
	if err != nil {
		r.t.Fatal(err)
	}
	return &phone{t: r.t, key: k}
}

// connect opens a session to the gateway through the relay.
func (r *relayRig) connect(p *phone, pair string) error {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws := "ws" + strings.TrimPrefix(r.url, "http") + relay.PathPrefix + r.room + "?side=page"
	c, _, err := websocket.Dial(ctx, ws, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	sess, err := relaytun.Dial(ctx, relaytun.WSConn(c), p.key, r.key.Public, relaytun.Hello{Pair: pair})
	if err != nil {
		return err
	}
	r.t.Cleanup(func() { _ = sess.Close() })
	p.sess = sess
	p.http = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) { return sess.OpenStream() },
	}}
	return nil
}

// post posts JSON as the page's shim does: no Origin of its own (the
// gateway sets its own origin on every tunnel request).
func (p *phone) post(path, body string) (int, map[string]any) {
	p.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "http://laptop.invalid"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		p.t.Fatalf("%s: %v", path, err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

// pairOverRelay runs a whole pairing over the relay; the owner picks role.
func (r *relayRig) pairOverRelay(p *phone, role string) string {
	r.t.Helper()
	plain := r.openPairing(false)
	var err error
	for i := 0; i < 100; i++ { // the pairing event may come after the first try
		if err = r.connect(p, plain); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		r.t.Fatalf("connect with the pairing secret: %v", err)
	}
	code, m := p.post("/pair/start", `{"p":"`+plain+`","name":"Phone · relay"}`)
	if code != http.StatusOK {
		r.t.Fatalf("start: %d %v", code, m)
	}
	ask := r.ctl.line(r.t)
	if ask["op"] != "web.pair_ask" {
		r.t.Fatalf("ask = %v", ask)
	}
	r.ctl.send(r.t, `{"t":"res","id":`+jsonNum(ask["id"])+`,"data":{"role":"`+role+`"}}`)
	id := m["pair_id"].(string)
	for i := 0; i < 100; i++ {
		_, w := p.post("/pair/wait", `{"pair_id":"`+id+`"}`)
		if w["state"] == "approved" {
			if w["options"] != nil {
				r.t.Fatalf("a relay pairing got passkey options: %v", w)
			}
			break
		}
	}
	code, m = p.post("/pair/finish", `{"pair_id":"`+id+`","remember":true}`)
	if code != http.StatusOK {
		r.t.Fatalf("finish: %d %v", code, m)
	}
	return m["token"].(string)
}

// Spec (ADR 0022, WEB-52/53): the phone reaches the gateway through the
// relay: it pairs with the QR code's secret (no passkey: the relay's
// origin is not the laptop's), signs in later with its relay key alone,
// gets the page and the WebSocket, and the relay sees none of it.
func TestRelayEndToEndAndTheRelaySeesOnlyCiphertext(t *testing.T) {
	r := startRelayRig(t)
	p := r.newPhone()
	tok := r.pairOverRelay(p, webdevices.RoleControl)

	devs, _ := webdevices.Open(r.cfg.DevicesDir).List()
	if len(devs) != 1 || devs[0].RelayKey != p.key.Public.String() || devs[0].Bookmark != nil {
		t.Fatalf("devices = %+v, want one with the phone's relay key and no bookmark", devs)
	}

	// The page itself, over a stream.
	resp, err := p.http.Get("http://laptop.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(page), relayPageMarker) || resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("page = %d %q", resp.StatusCode, page)
	}
	_ = p.sess.Close()

	// Later: a new session with no secret, the relay key alone signs in.
	p2 := &phone{t: t, key: p.key}
	if err := r.connect(p2, ""); err != nil {
		t.Fatalf("a paired phone's key was refused: %v", err)
	}
	code, m := p2.post("/login/relay", `{}`)
	tok2, _ := m["token"].(string)
	if code != http.StatusOK || tok2 == "" || tok2 == tok || m["role"] != webdevices.RoleControl {
		t.Fatalf("relay login: %d %v", code, m)
	}

	// The panel's WebSocket through the tunnel.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://laptop.invalid/ws", &websocket.DialOptions{HTTPClient: p2.http, Subprotocols: []string{Subprotocol}})
	if err != nil {
		t.Fatalf("ws over the relay: %v", err)
	}
	defer func() { _ = c.CloseNow() }()
	writeText(t, c, helloMsg(tok2))
	hp := r.hub.next()
	if open := hp.line(t); open["t"] != "open" {
		t.Fatalf("hub got %v", open)
	}
	hp.line(t) // hello
	hp.send(t, `{"t":"snap","topic":"status","seq":1,"data":{"line":"CHAT-MARKER-secret-line"}}`)
	got := readMsg(t, c)
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), "CHAT-MARKER-secret-line") {
		t.Fatalf("page got %s", b)
	}

	seen := r.tap.seen()
	for _, plain := range []string{relayPageMarker, "CHAT-MARKER", tok, tok2, "/pair/start", "/login/relay", "Phone · relay", "Content-Security-Policy"} {
		if bytes.Contains(seen, []byte(plain)) {
			t.Fatalf("the relay saw the plaintext %q", plain)
		}
	}
	if len(seen) < 1000 {
		t.Fatalf("the tap saw only %d bytes: the test did not go through the relay", len(seen))
	}
}

// A phone that knows the gateway key but is not paired is refused after
// the handshake, before any HTTP; a used or wrong secret is no way in.
func TestRelayRefusesAnUnpairedPhone(t *testing.T) {
	r := startRelayRig(t)
	p := r.newPhone()
	var re *relaytun.RefusedError
	if err := r.connect(p, ""); !errors.As(err, &re) || re.Why != "not_paired" {
		t.Fatalf("unpaired phone: %v, want refused not_paired", err)
	}
	if err := r.connect(r.newPhone(), "not-the-secret-not-the-secret-not-the-secr"); !errors.As(err, &re) {
		t.Fatalf("wrong secret: %v, want refused", err)
	}
}

// A removed device's relay key no longer gets in.
func TestRelayForgetsARemovedDevice(t *testing.T) {
	r := startRelayRig(t)
	p := r.newPhone()
	r.pairOverRelay(p, webdevices.RoleViewer)
	_ = p.sess.Close()
	devs, _ := webdevices.Open(r.cfg.DevicesDir).List()
	if _, _, err := webdevices.Open(r.cfg.DevicesDir).Remove(devs[0].ID); err != nil {
		t.Fatal(err)
	}
	var re *relaytun.RefusedError
	if err := r.connect(&phone{t: t, key: p.key}, ""); !errors.As(err, &re) {
		t.Fatalf("removed device: %v, want refused", err)
	}
}

// Over the relay the passkey sign-in is not offered, and /login/relay
// is only for tunnel requests.
func TestRelayLoginOnlyInsideTheTunnel(t *testing.T) {
	r := startRelayRig(t)
	if code, _ := r.postJSON("/login/relay", `{}`); code != http.StatusNotFound {
		t.Fatalf("/login/relay on the laptop's own address = %d, want 404", code)
	}
	p := r.newPhone()
	r.pairOverRelay(p, webdevices.RoleViewer)
	if code, m := p.post("/login/start", `{}`); code != http.StatusNotFound {
		t.Fatalf("/login/start over the relay = %d %v, want 404 no_passkeys", code, m)
	}
	st, err := ReadStatus(r.cfg.StatusPath)
	if err != nil || st.Relay != r.url {
		t.Fatalf("status relay = %q (%v), want %q", st.Relay, err, r.url)
	}
}

// The kill switch closes the relay sessions with the panel.
func TestRelayEndsWithThePanel(t *testing.T) {
	r := startRelayRig(t)
	p := r.newPhone()
	r.pairOverRelay(p, webdevices.RoleViewer)
	r.ctl.send(t, `{"t":"bye","code":4009}`)
	r.wait()
	select {
	case <-p.sess.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the relay session outlived the panel")
	}
	deadline := time.Now().Add(10 * time.Second)
	for r.rl.Waiting(r.room) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the gateway still waits in the relay room after it stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Spec (NEW-12): relay_off from the UI ends the gateway's wait in its
// room and every session; the panel's own address keeps working, and a
// new relay_key starts the wait again.
func TestRelayOffStopsWaiting(t *testing.T) {
	r := startRelayRig(t)
	r.ctl.send(r.t, `{"t":"ev","kind":"relay_off"}`)
	deadline := time.Now().Add(10 * time.Second)
	for r.rl.Waiting(r.room) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("the gateway still waits in its relay room after relay_off")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if n := r.rl.Waiting(r.room); n != 0 {
		t.Fatalf("the gateway dialed the relay again (%d waiting)", n)
	}
	r.sendKey() // waits until it is back in the room
}
