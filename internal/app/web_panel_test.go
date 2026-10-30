//go:build unix

package app

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webgw"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webPanelRig is a hub rig whose gateway runs in this process (the real
// webgw.Run, dialling the real hub socket) instead of a child process.
type webPanelRig struct {
	*webRig
	mu    sync.Mutex
	args  [][]string
	links chan string // links that reached the clipboard
	audit string
}

func newWebPanelRig(t *testing.T) *webPanelRig {
	t.Helper()
	r := &webPanelRig{webRig: newWebRig(t, nil), links: make(chan string, 4)}
	writeUserConfig(t, `{"web_panel":"on"}`)
	dir := t.TempDir()
	r.audit = filepath.Join(dir, "audit.jsonl")
	restore := SetClipboardSink(func(s string) { r.links <- s })
	t.Cleanup(restore)
	old := webGatewayStart
	t.Cleanup(func() { webGatewayStart = old })
	webGatewayStart = func(h *webHub, args []string) (*webGatewayProc, error) {
		r.mu.Lock()
		r.args = append(r.args, args)
		r.mu.Unlock()
		ctx, cancel := context.WithCancel(context.Background())
		pr, pw := io.Pipe()
		done := make(chan struct{})
		proc := &webGatewayProc{terminate: cancel, alive: func() bool {
			select {
			case <-done:
				return false
			default:
				return true
			}
		}}
		go func() {
			_, _ = webgw.Run(ctx, webgw.RunConfig{
				Dial: func(ctx context.Context) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", r.path)
				},
				AuditPath: r.audit, StatusPath: filepath.Join(dir, "web.json"),
				LockPath: filepath.Join(dir, "web.lock"), Handoff: pw,
			})
			_ = pw.Close()
			close(done)
			_ = h.send(h.ctx, webPanelExitMsg{hub: h, proc: proc})
		}()
		go func() {
			var hand struct {
				Port int `json:"port"`
			}
			if err := json.NewDecoder(pr).Decode(&hand); err != nil {
				_ = h.send(h.ctx, webPanelFailMsg{hub: h, proc: proc, err: err})
				return
			}
			_ = h.send(h.ctx, webPanelUpMsg{hub: h, proc: proc, port: hand.Port})
		}()
		t.Cleanup(func() {
			cancel()
			<-done
		})
		return proc, nil
	}
	return r
}

// started starts the panel with Copy link and returns the link.
func (r *webPanelRig) started() string {
	r.t.Helper()
	r.m.startWebPanel(false, true)
	if r.m.webPanel == nil {
		r.t.Fatal("panel not started")
	}
	r.pumpUntil("the panel up", func() bool { return r.m.webPanel != nil && r.m.webPanel.up })
	select {
	case l := <-r.links:
		return l
	case <-time.After(10 * time.Second):
		r.t.Fatal("no link copied")
	}
	return ""
}

// browser redeems the link and says hello on the WebSocket; it returns
// the connection once the hub's meta came back through the gateway.
func (r *webPanelRig) browser(link string) *websocket.Conn {
	r.t.Helper()
	base, secret, ok := strings.Cut(link, "/#s=")
	if !ok {
		r.t.Fatalf("link %q", link)
	}
	req, _ := http.NewRequest(http.MethodPost, base+"/auth", strings.NewReader(`{"secret":"`+secret+`"}`))
	req.Header.Set("Origin", base)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	var tok struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()
	if tok.Token == "" {
		r.t.Fatalf("auth %d", resp.StatusCode)
	}
	h := http.Header{}
	h.Set("Origin", base)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, strings.Replace(base, "http", "ws", 1)+"/ws",
		&websocket.DialOptions{HTTPHeader: h, Subprotocols: []string{webgw.Subprotocol}})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.CloseNow() })
	hello, _ := json.Marshal(map[string]any{"t": "hello", "v": webproto.Version, "token": tok.Token})
	if err := c.Write(ctx, websocket.MessageText, hello); err != nil {
		r.t.Fatal(err)
	}
	got := make(chan map[string]any, 1)
	go func() {
		_, b, err := c.Read(context.Background())
		var m map[string]any
		if err == nil {
			_ = json.Unmarshal(b, &m)
		}
		got <- m
	}()
	var meta map[string]any
	r.pumpUntil("meta through the gateway", func() bool {
		select {
		case meta = <-got:
			return true
		default:
			return false
		}
	})
	if meta["t"] != "meta" || meta["role"] != "controller" {
		r.t.Fatalf("first message %v", meta)
	}
	return c
}

// Spec (WEB-9): Start Panel runs the gateway, the gateway gets the link's
// digest over the control connection, and a browser with the link
// reaches the hub (meta comes back through the gateway).
func TestWebPanelStartsAndBrowserReachesHub(t *testing.T) {
	r := newWebPanelRig(t)
	link := r.started()
	if !strings.HasPrefix(link, "http://127.0.0.1:"+strconv.Itoa(r.m.webPanel.port)+"/#s=") {
		t.Fatalf("link %q", link)
	}
	r.browser(link)
	r.pumpUntil("one browser", func() bool { return r.hub.browsers() == 1 })
}

// Spec (5.4): the kill switch closes every browser with 4009 in about a
// second and the gateway ends; nothing asks first.
func TestWebKillSwitchClosesBrowsers4009(t *testing.T) {
	r := newWebPanelRig(t)
	c := r.browser(r.started())
	r.m.stopWebPanel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			if got := websocket.CloseStatus(err); got != websocket.StatusCode(webproto.CloseStopped) {
				t.Fatalf("closed with %d (%v), want 4009", got, err)
			}
			break
		}
	}
	r.pumpUntil("the gateway gone", func() bool { return r.m.webPanel == nil })
	b, _ := os.ReadFile(r.audit)
	if !strings.Contains(string(b), `"ev":"kill_switch"`) {
		t.Fatalf("audit: %s", b)
	}
}

// Spec (T9, leak_test): the secret is never in the gateway's argv or
// env, nor in its audit log; argv holds only paths and limits.
func TestWebSecretNeverInArgvOrEnv(t *testing.T) {
	r := newWebPanelRig(t)
	link := r.started()
	_, secret, _ := strings.Cut(link, "#s=")
	r.mu.Lock()
	args := strings.Join(r.args[0], " ")
	r.mu.Unlock()
	if strings.Contains(args, secret) || strings.Contains(args, "#s=") {
		t.Fatalf("secret in argv: %s", args)
	}
	for _, kv := range os.Environ() {
		if strings.Contains(kv, secret) {
			t.Fatal("secret in the environment")
		}
	}
	for _, want := range []string{"--dir", "--audit", "--idle-min 15", "--max-hours 12", "--viewers 4", "--port 0"} {
		if !strings.Contains(args, want) {
			t.Errorf("argv has no %q: %s", want, args)
		}
	}
	r.browser(link)
	b, _ := os.ReadFile(r.audit)
	if strings.Contains(string(b), secret) {
		t.Fatal("secret in the audit log")
	}
}

// Spec (T52): `termocode web` without --yes never starts the panel by
// itself; with --yes it does and gets the port.
func TestWebCLIOpenNeedsTheLaptop(t *testing.T) {
	r := newWebPanelRig(t)
	ctl := r.dial(`{"t":"ctl"}`)
	ctl.send(`{"t":"req","id":1,"op":"web.open"}`)
	if m := r.nextMsg(ctl); m["t"] != "err" || m["code"] != "forbidden" {
		t.Fatalf("got %v", m)
	}
	if r.m.webPanel != nil {
		t.Fatal("the panel started without the laptop")
	}
	ctl.send(`{"t":"req","id":2,"op":"web.open","args":{"yes":true}}`)
	var res map[string]any
	for res == nil {
		m := r.nextMsg(ctl)
		if m["t"] == "res" {
			res = m
		}
	}
	data, _ := res["data"].(map[string]any)
	if res["id"] != float64(2) || data["port"] != float64(r.m.webPanel.port) {
		t.Fatalf("res %v", res)
	}
	ctl.send(`{"t":"req","id":3,"op":"web.stop"}`)
	for {
		m := r.nextMsg(ctl)
		if m["t"] == "res" && m["id"] == float64(3) {
			break
		}
	}
	r.pumpUntil("the gateway gone", func() bool { return r.m.webPanel == nil })
}

// Spec (E11): with web_panel off nothing starts and the hub does not
// listen; turning it on starts the hub, off stops it.
func TestWebPanelOffToastAndHubFollowsSetting(t *testing.T) {
	m := aiTestModel(t)
	t.Setenv("XDG_RUNTIME_DIR", webTestDir(t))
	t.Chdir(t.TempDir())
	calls := 0
	old := webGatewayStart
	t.Cleanup(func() { webGatewayStart = old })
	webGatewayStart = func(*webHub, []string) (*webGatewayProc, error) { calls++; return nil, io.EOF }
	m.startWebPanel(true, false)
	if calls != 0 || m.web != nil {
		t.Fatal("started with web_panel off")
	}
	if v := strings.Join(m.toast.View(200, 10), "\n"); !strings.Contains(v, "web_panel") {
		t.Fatalf("toast %q", v)
	}
	writeUserConfig(t, `{"web_panel":"on"}`)
	m.syncWebHub()
	if m.web == nil {
		t.Fatal("hub not started with web_panel on")
	}
	writeUserConfig(t, `{"web_panel":"off"}`)
	m.syncWebHub()
	if m.web != nil {
		t.Fatal("hub still runs with web_panel off")
	}
}
