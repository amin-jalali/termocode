//go:build unix

package webgw

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// gwRig runs Run against a fake hub.
type gwRig struct {
	t      *testing.T
	hub    *fakeHub
	ctl    *hubPeer
	dir    string
	cfg    RunConfig
	port   int
	secret string
	done   chan struct{}
	code   webproto.CloseCode
	err    error
}

func startGateway(t *testing.T, mod func(*RunConfig)) *gwRig {
	t.Helper()
	dir := t.TempDir()
	r := &gwRig{t: t, hub: newFakeHub(t), dir: dir, done: make(chan struct{})}
	r.cfg = RunConfig{Dial: r.hub.dial, AuditPath: filepath.Join(dir, "audit.jsonl"),
		StatusPath: filepath.Join(dir, "web.json"), LockPath: filepath.Join(dir, "web.lock")}
	if mod != nil {
		mod(&r.cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(r.done)
		r.code, r.err = Run(ctx, r.cfg)
		if c, ok := r.cfg.Handoff.(io.Closer); ok {
			_ = c.Close()
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-r.done
	})
	r.ctl = r.hub.next()
	if m := r.ctl.line(t); m["t"] != "ctl" {
		t.Fatalf("first line %v", m)
	}
	return r
}

// link sends a new launch link and returns once the handoff came.
func (r *gwRig) link() {
	r.t.Helper()
	plain, digest, err := NewLinkSecret()
	if err != nil {
		r.t.Fatal(err)
	}
	r.secret = plain
	r.ctl.send(r.t, `{"t":"ev","kind":"link","data":{"sha256":"`+hex.EncodeToString(digest[:])+`"}}`)
}

func (r *gwRig) handoff(pr io.Reader) {
	r.t.Helper()
	var h struct {
		Port int `json:"port"`
	}
	if err := json.NewDecoder(pr).Decode(&h); err != nil || h.Port == 0 {
		r.t.Fatalf("handoff %v %v", h, err)
	}
	r.port = h.Port
}

// started sends the first link and reads the port from the handoff.
func startedGateway(t *testing.T, mod func(*RunConfig)) *gwRig {
	t.Helper()
	pr, pw := io.Pipe()
	r := startGateway(t, func(c *RunConfig) {
		c.Handoff = pw
		if mod != nil {
			mod(c)
		}
	})
	r.link()
	r.handoff(pr)
	return r
}

// browser logs in with the secret and opens the WebSocket.
func (r *gwRig) browser() (*websocket.Conn, *hubPeer) {
	r.t.Helper()
	origin := "http://127.0.0.1:" + strconv.Itoa(r.port)
	req, _ := http.NewRequest(http.MethodPost, origin+"/auth", strings.NewReader(`{"secret":"`+r.secret+`"}`))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	var got struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || got.Token == "" {
		r.t.Fatalf("auth: %d", resp.StatusCode)
	}
	h := http.Header{}
	h.Set("Origin", origin)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(r.port)+"/ws",
		&websocket.DialOptions{HTTPHeader: h, Subprotocols: []string{Subprotocol}})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.CloseNow() })
	writeText(r.t, c, helloMsg(got.Token))
	p := r.hub.next()
	p.line(r.t) // open
	p.line(r.t) // hello
	return c, p
}

func (r *gwRig) wait() webproto.CloseCode {
	r.t.Helper()
	select {
	case <-r.done:
	case <-time.After(15 * time.Second):
		r.t.Fatal("gateway did not stop")
	}
	if r.err != nil {
		r.t.Fatalf("run: %v", r.err)
	}
	return r.code
}

// Spec (5.4, WEB-9): the kill switch (the hub's bye 4009 on the control
// connection) closes every browser with 4009, revokes the tokens,
// removes the status file and is audited.
func TestGatewayKillSwitch(t *testing.T) {
	r := startedGateway(t, nil)
	st, err := ReadStatus(r.cfg.StatusPath)
	if err != nil || st.Port != r.port || st.PID != os.Getpid() {
		t.Fatalf("status %+v %v", st, err)
	}
	c, _ := r.browser()
	r.ctl.send(t, `{"t":"bye","code":4009}`)
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseStopped) {
		t.Fatalf("browser closed with %d, want 4009", got)
	}
	if code := r.wait(); code != webproto.CloseStopped {
		t.Fatalf("stopped with %d", code)
	}
	if _, err := os.Stat(r.cfg.StatusPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("status file left: %v", err)
	}
	a := auditText(t, r.cfg.AuditPath)
	for _, ev := range []string{`"ev":"start"`, `"ev":"connect"`, `"ev":"kill_switch"`, `"ev":"stop"`} {
		if !strings.Contains(a, ev) {
			t.Errorf("audit has no %s", ev)
		}
	}
	if strings.Contains(a, r.secret) {
		t.Fatal("secret in the audit log")
	}
}

// Spec (3.5): the gateway dies with the session: the UI going away
// closes the browsers with 4012 and ends the gateway.
func TestGatewayStopsWhenSessionEnds(t *testing.T) {
	r := startedGateway(t, nil)
	c, _ := r.browser()
	_ = r.ctl.nc.Close()
	r.hub.setDown()
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseHubGone) {
		t.Fatalf("browser closed with %d, want 4012", got)
	}
	if code := r.wait(); code != webproto.CloseHubGone {
		t.Fatalf("stopped with %d", code)
	}
}

// Spec (3.5): no use for Idle stops the gateway (4006); use keeps it on.
func TestGatewayIdleStops(t *testing.T) {
	r := startedGateway(t, func(c *RunConfig) { c.Idle = 400 * time.Millisecond })
	c, p := r.browser()
	for i := 1; i <= 4; i++ { // use for longer than Idle
		<-time.After(150 * time.Millisecond)
		writeText(t, c, `{"t":"ping","n":"`+itoa(i)+`","here":true}`)
		p.line(t)
	}
	select {
	case <-r.done:
		t.Fatal("stopped while in use")
	default:
	}
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseIdle) {
		t.Fatalf("browser closed with %d, want 4006", got)
	}
	if code := r.wait(); code != webproto.CloseIdle {
		t.Fatalf("stopped with %d", code)
	}
	if !strings.Contains(auditText(t, r.cfg.AuditPath), `"ev":"idle_stop"`) {
		t.Fatal("no idle_stop in the audit log")
	}
}

// Spec (3.5): Max after the start the gateway stops (4007), in use or not.
func TestGatewayMaxStops(t *testing.T) {
	r := startedGateway(t, func(c *RunConfig) { c.Max = 300 * time.Millisecond })
	c, _ := r.browser()
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseMaxLifetime) {
		t.Fatalf("browser closed with %d, want 4007", got)
	}
	if code := r.wait(); code != webproto.CloseMaxLifetime {
		t.Fatalf("stopped with %d", code)
	}
}

// Spec: without the first launch link from the UI the start fails; a
// second gateway for the same folder is busy.
func TestGatewayStartFailures(t *testing.T) {
	r := startGateway(t, func(c *RunConfig) { c.LinkWait = 200 * time.Millisecond })
	select {
	case <-r.done:
	case <-time.After(10 * time.Second):
		t.Fatal("no stop")
	}
	if !errors.Is(r.err, ErrNoLink) {
		t.Fatalf("err %v, want ErrNoLink", r.err)
	}

	r2 := startedGateway(t, nil)
	_, err := Run(context.Background(), RunConfig{Dial: r2.hub.dial, LockPath: r2.cfg.LockPath,
		AuditPath: filepath.Join(r2.dir, "a2.jsonl"), StatusPath: filepath.Join(r2.dir, "s2.json")})
	if !errors.Is(err, ErrGatewayBusy) {
		t.Fatalf("second gateway: %v, want busy", err)
	}
}

// Spec (T9): the status file is 0600 and holds no secret.
func TestStatusFileMode0600(t *testing.T) {
	r := startedGateway(t, nil)
	fi, err := os.Stat(r.cfg.StatusPath)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("status file %v %v", fi, err)
	}
	b, _ := os.ReadFile(r.cfg.StatusPath)
	if strings.Contains(string(b), r.secret) || strings.Contains(string(b), "#s=") {
		t.Fatal("secret in the status file")
	}
}

// Spec (WEB-29): in SSH mode the gateway serves on a 0600 unix socket and
// answers to the port the user's ssh forwards on their computer: the
// hand-off and the status file name that port, and /auth works through
// the socket with Host and Origin 127.0.0.1:<port>.
func TestGatewayUnixMode(t *testing.T) {
	sock := filepath.Join(shortDir(t), "k.webhttp.sock")
	r := startedGateway(t, func(c *RunConfig) { c.UnixPath, c.PortHint = sock, 41237 })
	if r.port != 41237 {
		t.Fatalf("hand-off port %d, want the hint 41237", r.port)
	}
	st, err := ReadStatus(r.cfg.StatusPath)
	if err != nil || st.Port != 41237 || st.URL != "http://127.0.0.1:41237/" || st.Sock != sock {
		t.Fatalf("status %+v %v", st, err)
	}
	if fi, err := os.Lstat(sock); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket %v %v", fi, err)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sock)
	}}}
	const origin = "http://127.0.0.1:41237"
	req, _ := http.NewRequest(http.MethodPost, origin+"/auth", strings.NewReader(`{"secret":"`+r.secret+`"}`))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auth through the socket: %d", resp.StatusCode)
	}
	r.ctl.send(t, `{"t":"bye","code":4009}`)
	if code := r.wait(); code != webproto.CloseStopped {
		t.Errorf("code %d", code)
	}
	if _, err := os.Lstat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the socket is left after the stop: %v", err)
	}
}

// An installable (https) site serves the app manifest; a loopback site
// does not (WEB-35). /auth, /ws and the page are routed the same on both.
func TestSiteMuxManifestOnlyWhenInstallable(t *testing.T) {
	mark := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(name)) })
	}
	for _, tc := range []struct {
		site Site
		want int
	}{{LoopbackSite(41237), http.StatusNotFound}, {Site{Scheme: "https", Hosts: []string{"box.tail1234.ts.net"}}, http.StatusOK}} {
		mux := siteMux(tc.site, mark("auth"), mark("ws"), Assets())
		if got := getAsset(t, mux, http.MethodGet, ManifestPath).StatusCode; got != tc.want {
			t.Errorf("%s: GET %s = %d, want %d", tc.site.Scheme, ManifestPath, got, tc.want)
		}
		for path, want := range map[string]string{"/auth": "auth", "/ws": "ws"} {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Body.String() != want {
				t.Errorf("%s: %s went to %q", tc.site.Scheme, path, rec.Body.String())
			}
		}
		if got := getAsset(t, mux, http.MethodGet, "/").StatusCode; got != http.StatusOK {
			t.Errorf("%s: GET / = %d", tc.site.Scheme, got)
		}
	}
}
