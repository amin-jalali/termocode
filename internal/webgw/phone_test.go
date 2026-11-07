//go:build unix

package webgw

import (
	"context"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// phoneRes sends op with args on c and returns the answer's data.
func phoneRes(t *testing.T, c *websocket.Conn, id int, op, args string) map[string]any {
	t.Helper()
	writeText(t, c, `{"t":"req","id":`+jsonNum(float64(id))+`,"op":"`+op+`","args":`+args+`}`)
	m := readMsg(t, c)
	if m["t"] != "res" || m["id"] != float64(id) {
		t.Fatalf("%s: %v", op, m)
	}
	d, _ := m["data"].(map[string]any)
	return d
}

// Spec (MOBUX-20): This phone reads the device's own record from the
// laptop: its id, name, role, passkey, the laptop it is paired with, and
// its stored choices (none yet: alerts off). The hub never sees it.
func TestPhoneGetShowsThisPhone(t *testing.T) {
	r, _ := controlGateway(t)
	tok := r.pairPhone(webdevices.RoleViewer)
	c, p, open := r.connectToken(tok)
	info := phoneRes(t, c, 1, webproto.OpPhoneGet, `{}`)
	if info["device"] != open["device"] || info["role"] != "viewer" || info["passkey"] != true {
		t.Fatalf("phone.get %v (open %v)", info, open)
	}
	if ms, _ := info["paired_ms"].(float64); ms <= 0 {
		t.Errorf("paired_ms %v", info["paired_ms"])
	}
	laptops, _ := info["laptops"].([]any)
	if len(laptops) != 1 || laptops[0].(map[string]any)["this"] != true || laptops[0].(map[string]any)["name"] == "" {
		t.Errorf("laptops %v", info["laptops"])
	}
	if prefs, _ := info["prefs"].(map[string]any); prefs["alerts"] != false {
		t.Errorf("prefs %v", info["prefs"])
	}
	noHubLine(t, p)
}

// Spec (MOBUX-20): the phone's choices (alerts, quiet hours, lock) are
// kept with its record on the laptop, read back by phone.get, and
// audited as switches and times only.
func TestPhonePrefsKeptOnLaptop(t *testing.T) {
	r, clock := controlGateway(t)
	tok := r.pairPhone(webdevices.RoleViewer)
	c, p, open := r.connectToken(tok)
	phoneRes(t, c, 1, webproto.OpPhonePrefs, `{"alerts":true,"quiet_from":"22:00","quiet_to":"07:00","utc_offset_min":60,"lock":true}`)
	d, ok, err := webdevices.Open(r.cfg.DevicesDir).Get(open["device"].(string))
	if err != nil || !ok || d.Phone == nil {
		t.Fatalf("stored %+v %v %v", d, ok, err)
	}
	if ph := *d.Phone; !ph.Alerts || ph.QuietFrom != "22:00" || ph.QuietTo != "07:00" || ph.UTCOffsetMin != 60 || !ph.Lock || !ph.Changed.Equal(clock.Now()) {
		t.Errorf("stored phone %+v", ph)
	}
	prefs, _ := phoneRes(t, c, 2, webproto.OpPhoneGet, `{}`)["prefs"].(map[string]any)
	if prefs["alerts"] != true || prefs["quiet_from"] != "22:00" || prefs["lock"] != true {
		t.Errorf("read back %v", prefs)
	}
	if a := auditText(t, r.cfg.AuditPath); !strings.Contains(a, `"op":"phone.prefs"`) || !strings.Contains(a, "quiet 22:00-07:00") {
		t.Errorf("no prefs line in the audit:\n%s", a)
	}
	writeText(t, c, `{"t":"req","id":3,"op":"phone.prefs","args":{"alerts":true,"quiet_from":"22:00"}}`)
	wantErr(t, c, 3, webproto.CodeBadRequest)
	noHubLine(t, p)
}

// Spec (MOBUX-20): a shared view (guest) is for one visit: it keeps no
// choices on the laptop.
func TestPhonePrefsNotForGuest(t *testing.T) {
	r, _ := controlGateway(t)
	tok := r.pairPhone(webdevices.RoleViewer)
	c, _, open := r.connectToken(tok)
	dev := open["device"].(string)
	if _, err := webdevices.Open(r.cfg.DevicesDir).Change(dev, func(d *webdevices.Device) { d.Guest = true }); err != nil {
		t.Fatal(err)
	}
	writeText(t, c, `{"t":"req","id":1,"op":"phone.prefs","args":{"alerts":true}}`)
	wantErr(t, c, 1, webproto.CodeForbidden)
	if d, _, _ := webdevices.Open(r.cfg.DevicesDir).Get(dev); d.Phone != nil {
		t.Errorf("a guest kept %+v", d.Phone)
	}
}

// Spec (MOBUX-20): "Forget this phone" removes the device from the
// laptop: the answer comes first, then its links close with 4003, its
// record is gone, its token no longer opens anything, and the audit has
// the revoke. Other browsers stay.
func TestForgetThisPhone(t *testing.T) {
	r, _ := controlGateway(t)
	tok := r.pairPhone(webdevices.RoleControl)
	c, _, open := r.connectToken(tok)
	other, _ := r.browser()
	otherAlive := other.CloseRead(context.Background())
	dev := open["device"].(string)
	phoneRes(t, c, 1, webproto.OpPhoneForget, `{}`)
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseDeviceRevoked) {
		t.Fatalf("closed with %d, want 4003", got)
	}
	if _, ok, _ := webdevices.Open(r.cfg.DevicesDir).Get(dev); ok {
		t.Error("the device is still paired")
	}
	again := r.dialWS()
	writeText(t, again, helloMsg(tok))
	if got := closeCode(t, again); got != websocket.StatusCode(webproto.CloseAuthFailed) {
		t.Fatalf("the forgotten phone's token got %d, want 4001", got)
	}
	if otherAlive.Err() != nil {
		t.Fatal("the other browser was closed too")
	}
	if a := auditText(t, r.cfg.AuditPath); !strings.Contains(a, `"ev":"revoke"`) || !strings.Contains(a, `"why":"phone"`) {
		t.Errorf("no revoke in the audit:\n%s", a)
	}
}

// Spec (MOBUX-20): a tab that is not a paired device (the laptop's own
// launch link) has no This phone record: every op is refused.
func TestPhoneOpsNeedAPairedDevice(t *testing.T) {
	r, _ := controlGateway(t)
	c, p := r.browser()
	for i, op := range []string{webproto.OpPhoneGet, webproto.OpPhonePrefs, webproto.OpPhoneForget} {
		args := `{}`
		if op == webproto.OpPhonePrefs {
			args = `{"alerts":true}`
		}
		writeText(t, c, `{"t":"req","id":`+jsonNum(float64(i+1))+`,"op":"`+op+`","args":`+args+`}`)
		wantErr(t, c, float64(i+1), webproto.CodeForbidden)
	}
	noHubLine(t, p)
}
