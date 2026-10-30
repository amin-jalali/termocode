//go:build unix

package webgw

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// postJSON posts body to the gateway as its own page would.
func (r *gwRig) postJSON(path, body string) (int, map[string]any) {
	r.t.Helper()
	origin := "http://127.0.0.1:" + strconv.Itoa(r.port)
	req, _ := http.NewRequest(http.MethodPost, origin+path, strings.NewReader(body))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

// openPairing sends a pairing link's digest, as the UI does.
func (r *gwRig) openPairing(guest bool) string {
	r.t.Helper()
	plain, digest, err := NewLinkSecret()
	if err != nil {
		r.t.Fatal(err)
	}
	r.ctl.send(r.t, `{"t":"ev","kind":"pair","data":{"sha256":"`+hex.EncodeToString(digest[:])+`","ttl_s":120,"guest":`+strconv.FormatBool(guest)+`}}`)
	return plain
}

// pairPhone runs a whole pairing in which the owner picks role; it
// returns the phone's token.
func (r *gwRig) pairPhone(role string) string {
	r.t.Helper()
	tok, _ := r.pairPhoneKey(role)
	return tok
}

// pairPhoneKey is pairPhone that also returns the phone's passkey.
func (r *gwRig) pairPhoneKey(role string) (string, *softKey) {
	r.t.Helper()
	k := newSoftKey(r.t, "localhost", "http://localhost:"+strconv.Itoa(r.port))
	plain := r.openPairing(false)
	var code int
	var m map[string]any
	for i := 0; i < 100; i++ { // the event may come after the first try
		if code, m = r.postJSON("/pair/start", `{"p":"`+plain+`","name":"iPhone · Safari"}`); code == http.StatusOK {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if code != http.StatusOK {
		r.t.Fatalf("start: %d %v", code, m)
	}
	ask := r.ctl.line(r.t)
	args, _ := ask["args"].(map[string]any)
	if ask["t"] != "req" || ask["op"] != "web.pair_ask" || args["code"] != m["code"] || args["pair_id"] != m["pair_id"] {
		r.t.Fatalf("ask %v for %v", ask, m)
	}
	r.ctl.send(r.t, `{"t":"res","id":`+jsonNum(ask["id"])+`,"data":{"role":"`+role+`"}}`)
	id := m["pair_id"].(string)
	var opts []byte
	for i := 0; i < 100; i++ {
		if _, w := r.postJSON("/pair/wait", `{"pair_id":"`+id+`"}`); w["state"] == "approved" {
			opts, _ = json.Marshal(w["options"])
			break
		}
	}
	code, m = r.postJSON("/pair/finish", `{"pair_id":"`+id+`","credential":`+string(k.create(opts))+`}`)
	if code != http.StatusOK {
		r.t.Fatalf("finish: %d %v", code, m)
	}
	return m["token"].(string), k
}

// dialWS opens a WebSocket to the gateway without saying hello.
func (r *gwRig) dialWS() *websocket.Conn {
	r.t.Helper()
	h := http.Header{}
	h.Set("Origin", "http://127.0.0.1:"+strconv.Itoa(r.port))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(r.port)+"/ws",
		&websocket.DialOptions{HTTPHeader: h, Subprotocols: []string{Subprotocol}})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

// connectToken opens the WebSocket with tok and returns the hub's open line.
func (r *gwRig) connectToken(tok string) (*websocket.Conn, *hubPeer, map[string]any) {
	r.t.Helper()
	origin := "http://127.0.0.1:" + strconv.Itoa(r.port)
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
	writeText(r.t, c, helloMsg(tok))
	p := r.hub.next()
	open := p.line(r.t)
	p.line(r.t) // hello
	return c, p, open
}

func pairedGateway(t *testing.T) *gwRig {
	devs := filepath.Join(t.TempDir(), "web")
	return startedGateway(t, func(c *RunConfig) { c.DevicesDir = devs })
}

// Spec (WEB-32): the whole pairing through the real gateway: the QR
// secret's digest comes from the UI, the laptop is asked on the control
// connection with the phone's code, and the paired phone's WebSocket
// reaches the hub with the role the owner picked and its device id.
func TestGatewayPairsAPhone(t *testing.T) {
	r := pairedGateway(t)
	tok := r.pairPhone(webdevices.RoleViewer)
	_, _, open := r.connectToken(tok)
	if open["role"] != "viewer" || !strings.HasPrefix(open["device"].(string), "dev-") {
		t.Fatalf("open %v", open)
	}
	ds, _ := webdevices.Open(r.cfg.DevicesDir).List()
	if len(ds) != 1 || ds[0].Role != webdevices.RoleViewer {
		t.Fatalf("devices %+v", ds)
	}
	a := auditText(t, r.cfg.AuditPath)
	for _, ev := range []string{`"ev":"pair_start"`, `"ev":"pair_ok"`} {
		if !strings.Contains(a, ev) {
			t.Errorf("audit has no %s", ev)
		}
	}
}

// Spec (WEB-32, T30): removing a device on the laptop closes its live
// WebSocket with 4003 at once, drops its tokens, and a reconnect with
// the old token is refused; other browsers stay.
func TestRevokeDropsTokensAndConns(t *testing.T) {
	r := pairedGateway(t)
	tok := r.pairPhone(webdevices.RoleControl)
	c, _, open := r.connectToken(tok)
	other, _ := r.browser() // the laptop's own tab
	otherAlive := other.CloseRead(context.Background())
	dev := open["device"].(string)
	if _, _, err := webdevices.Open(r.cfg.DevicesDir).Remove(dev); err != nil {
		t.Fatal(err)
	}
	r.ctl.send(t, `{"t":"ev","kind":"revoke","data":{"device":"`+dev+`"}}`)
	if got := closeCode(t, c); got != websocket.StatusCode(4003) {
		t.Fatalf("revoked browser closed with %d, want 4003", got)
	}
	again := r.dialWS()
	writeText(t, again, helloMsg(tok))
	if got := closeCode(t, again); got != websocket.StatusCode(4001) {
		t.Fatalf("the revoked token got %d, want 4001", got)
	}
	if otherAlive.Err() != nil {
		t.Fatal("the other browser was closed too")
	}
	if !strings.Contains(auditText(t, r.cfg.AuditPath), `"ev":"revoke"`) {
		t.Error("no revoke in the audit")
	}
}

// A paired device removed while the gateway did not hear it (no event):
// its next hello is refused with 4003 all the same (the store is the
// truth, checked on every hello).
func TestRemovedDeviceRefusedAtHello(t *testing.T) {
	r := pairedGateway(t)
	tok := r.pairPhone(webdevices.RoleViewer)
	ds, _ := webdevices.Open(r.cfg.DevicesDir).List()
	if _, _, err := webdevices.Open(r.cfg.DevicesDir).Remove(ds[0].ID); err != nil {
		t.Fatal(err)
	}
	c := r.dialWS()
	writeText(t, c, helloMsg(tok))
	if got := closeCode(t, c); got != websocket.StatusCode(4003) {
		t.Fatalf("closed with %d, want 4003", got)
	}
}

// Spec (WEB-33): a paired phone comes back later with its passkey alone
// (a new tab, no QR), through the real gateway, with its stored role.
func TestGatewayPasskeyLogin(t *testing.T) {
	r := pairedGateway(t)
	_, k := r.pairPhoneKey(webdevices.RoleControl)
	code, m := r.postJSON("/login/start", `{}`)
	if code != http.StatusOK {
		t.Fatalf("start %d %v", code, m)
	}
	opts, _ := json.Marshal(m["options"])
	code, f := r.postJSON("/login/finish", `{"login_id":"`+m["login_id"].(string)+`","credential":`+string(k.get(opts))+`}`)
	if code != http.StatusOK {
		t.Fatalf("finish %d %v", code, f)
	}
	_, _, open := r.connectToken(f["token"].(string))
	if open["role"] != "controller" {
		t.Fatalf("open %v", open)
	}
	if !strings.Contains(auditText(t, r.cfg.AuditPath), `"ev":"login_ok"`) {
		t.Error("no login_ok in the audit")
	}
}
