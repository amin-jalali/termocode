//go:build unix

package webgw

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// controlGateway is a paired gateway on a fake clock.
func controlGateway(t *testing.T) (*gwRig, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Now()}
	devs := filepath.Join(t.TempDir(), "web")
	r := startedGateway(t, func(c *RunConfig) { c.DevicesDir = devs; c.Now = clock.Now })
	return r, clock
}

// askControl runs control.challenge and control.ask with k on c and
// returns the id of the ask.
func askControl(t *testing.T, c *websocket.Conn, k *softKey, id int) int {
	t.Helper()
	writeText(t, c, `{"t":"req","id":`+strconv.Itoa(id)+`,"op":"control.challenge"}`)
	m := readMsg(t, c)
	if m["t"] != "res" {
		t.Fatalf("challenge: %v", m)
	}
	opts, _ := json.Marshal(m["data"].(map[string]any)["options"])
	cred, _ := json.Marshal(string(k.get(opts)))
	writeText(t, c, `{"t":"req","id":`+strconv.Itoa(id+1)+`,"op":"control.ask","args":{"credential":`+string(cred)+`}}`)
	return id + 1
}

// Spec (WEB-34, T32): a viewer gets control only with a fresh passkey
// assertion of its own device AND the owner's yes on the laptop; then
// its links close with 4013 and it comes back as a controller (and stays
// one: the role is stored).
func TestViewerAsksForControl(t *testing.T) {
	r, _ := controlGateway(t)
	tok, k := r.pairPhoneKey(webdevices.RoleViewer)
	c, _, open := r.connectToken(tok)
	if open["role"] != "viewer" {
		t.Fatalf("open %v", open)
	}
	askID := askControl(t, c, k, 1)
	ask := r.ctl.line(t)
	args, _ := ask["args"].(map[string]any)
	if ask["op"] != "web.grant_ask" || args["device"] != open["device"] {
		t.Fatalf("laptop asked %v", ask)
	}
	r.ctl.send(t, `{"t":"res","id":`+jsonNum(ask["id"])+`}`)
	if m := readMsg(t, c); m["t"] != "res" || m["id"] != float64(askID) {
		t.Fatalf("answer %v", m)
	}
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseRoleChanged) {
		t.Fatalf("closed with %d, want 4013", got)
	}
	_, _, open = r.connectToken(tok)
	if open["role"] != "controller" {
		t.Fatalf("after the grant: %v", open)
	}
	d, _, _ := webdevices.Open(r.cfg.DevicesDir).Get(open["device"].(string))
	if d.Role != webdevices.RoleControl {
		t.Fatalf("stored role %q", d.Role)
	}
	if a := auditText(t, r.cfg.AuditPath); !strings.Contains(a, `"ev":"role"`) {
		t.Errorf("no role event in the audit")
	}
}

// Spec (T32): Not now on the laptop, or an owner answer that comes more
// than 5 minutes after the passkey, gives nothing; the viewer stays.
func TestGrantNeedsOwnerAndFreshAssertion(t *testing.T) {
	r, clock := controlGateway(t)
	tok, k := r.pairPhoneKey(webdevices.RoleViewer)
	c, _, _ := r.connectToken(tok)

	askID := askControl(t, c, k, 1)
	ask := r.ctl.line(t)
	r.ctl.send(t, `{"t":"err","id":`+jsonNum(ask["id"])+`,"code":"forbidden","msg":"Not now"}`)
	wantErr(t, c, float64(askID), webproto.CodeForbidden)

	clock.Add(6 * time.Second) // the ops' own rate: one per 5 s
	askID = askControl(t, c, k, 10)
	ask = r.ctl.line(t)
	clock.Add(GrantFresh + time.Second)
	r.ctl.send(t, `{"t":"res","id":`+jsonNum(ask["id"])+`}`)
	wantErr(t, c, float64(askID), webproto.CodeForbidden)

	ds, _ := webdevices.Open(r.cfg.DevicesDir).List()
	if ds[0].Role != webdevices.RoleViewer {
		t.Fatalf("role %q after refusals", ds[0].Role)
	}
}

// Another device's passkey cannot win control for this session, and a
// shared viewer (guest) never gets control at all.
func TestGrantOnlyWithOwnPasskey(t *testing.T) {
	r, clock := controlGateway(t)
	tokA, _ := r.pairPhoneKey(webdevices.RoleViewer)
	_, kB := r.pairPhoneKey(webdevices.RoleViewer)
	c, _, _ := r.connectToken(tokA)
	askID := askControl(t, c, kB, 1)
	wantErr(t, c, float64(askID), webproto.CodeForbidden)
	noHubLine(t, r.ctl)

	ds, _ := webdevices.Open(r.cfg.DevicesDir).List()
	if _, err := webdevices.Open(r.cfg.DevicesDir).Change(ds[0].ID, func(d *webdevices.Device) { d.Guest = true }); err != nil {
		t.Fatal(err)
	}
	clock.Add(6 * time.Second)
	writeText(t, c, `{"t":"req","id":5,"op":"control.challenge"}`)
	wantErr(t, c, 5, webproto.CodeForbidden)
}

// Spec (WEB-34): the laptop takes control back (role event): the
// device's links close with 4013 and come back as a viewer.
func TestLaptopTakesControl(t *testing.T) {
	r, _ := controlGateway(t)
	tok, _ := r.pairPhoneKey(webdevices.RoleControl)
	c, _, open := r.connectToken(tok)
	dev := open["device"].(string)
	if _, err := webdevices.Open(r.cfg.DevicesDir).Change(dev, func(d *webdevices.Device) { d.Role = webdevices.RoleViewer }); err != nil {
		t.Fatal(err)
	}
	r.ctl.send(t, `{"t":"ev","kind":"role","data":{"device":"`+dev+`","role":"viewer"}}`)
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseRoleChanged) {
		t.Fatalf("closed with %d", got)
	}
	if _, _, open := r.connectToken(tok); open["role"] != "viewer" {
		t.Fatalf("after take: %v", open)
	}
}
