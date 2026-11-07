//go:build unix

package webgw

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// MOB-50: the door. Frames with another workspace's ws go to that
// workspace's hub, without the field; lines from it come back with ws
// set; a project hub that ends drops only its project.

const (
	homeWS  = "00000000000000aa"
	shopWS  = "11111111111111bb"
	otherWS = "22222222222222cc"
)

// doorRig is a wsRig whose gateway is a door to project hubs by key.
type doorRig struct {
	*wsRig
	mu    sync.Mutex
	hubs  map[string]*fakeHub
	dials map[string]int
}

func newDoorRig(t *testing.T, mod func(*WSConfig)) *doorRig {
	t.Helper()
	d := &doorRig{hubs: map[string]*fakeHub{}, dials: map[string]int{}}
	d.wsRig = newWSRig(t, DefaultLimits, func(c *WSConfig) {
		c.Home = homeWS
		c.DialWS = func(ctx context.Context, ws string) (net.Conn, error) {
			d.mu.Lock()
			h := d.hubs[ws]
			d.dials[ws]++
			d.mu.Unlock()
			if h == nil {
				return nil, errors.New("not running")
			}
			return h.dial(ctx)
		}
		if mod != nil {
			mod(c)
		}
	})
	return d
}

func (d *doorRig) project(ws string) *fakeHub {
	h := newFakeHub(d.t)
	d.mu.Lock()
	d.hubs[ws] = h
	d.mu.Unlock()
	return h
}

// openProject waits for the project hub's open line and hello.
func openProject(t *testing.T, h *fakeHub) *hubPeer {
	t.Helper()
	p := h.next()
	if open := p.line(t); open["t"] != "open" || open["role"] != "controller" || open["name"] != "Phone" {
		t.Fatalf("project open line %v", open)
	}
	hello := p.line(t)
	if hello["t"] != "hello" || hello["token"] != "" || hello["resume"] != nil {
		t.Fatalf("project hello %v", hello)
	}
	return p
}

func TestValidWS(t *testing.T) {
	for _, ok := range []string{homeWS, "0123456789abcdef"} {
		if !webproto.ValidWS(ok) {
			t.Errorf("ValidWS(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "abc", "0123456789ABCDEF", "0123456789abcdeg", "../../etc/passwd", "0123456789abcdef0", "/tmp/x.web.sock"} {
		if webproto.ValidWS(bad) {
			t.Errorf("ValidWS(%q) = true", bad)
		}
	}
}

func TestDoorRoutesFramesByWS(t *testing.T) {
	d := newDoorRig(t, nil)
	shop := d.project(shopWS)
	c, home := d.connect()

	// The home meta gets the home key.
	home.send(t, `{"t":"meta","v":2,"conn":"c-1","role":"controller","device":"loc-test","idle_s":1,"max_s":1,"viewers":1,"ui":{"theme":"dark"},"caps":[]}`)
	if m := readMsg(t, c); m["t"] != "meta" || m["ws"] != homeWS {
		t.Fatalf("home meta %v", m)
	}

	// A sub for shop opens shop's hub; the hub sees no ws.
	writeText(t, c, `{"t":"sub","topics":["status"],"ws":"`+shopWS+`"}`)
	sp := openProject(t, shop)
	if m := sp.line(t); m["t"] != "sub" || m["ws"] != nil {
		t.Fatalf("shop hub got %v", m)
	}
	sp.send(t, `{"t":"snap","topic":"status","seq":1,"data":{"x":1}}`)
	if m := readMsg(t, c); m["t"] != "snap" || m["ws"] != shopWS || m["topic"] != "status" {
		t.Fatalf("browser got %v", m)
	}

	// A request for shop goes to shop; its answer comes back with ws.
	writeText(t, c, `{"t":"req","id":1,"op":"tree.list","args":{"path":""},"ws":"`+shopWS+`"}`)
	if m := sp.line(t); m["t"] != "req" || m["ws"] != nil || m["id"] != float64(1) {
		t.Fatalf("shop hub got %v", m)
	}
	sp.send(t, `{"t":"res","id":1,"data":{}}`)
	if m := readMsg(t, c); m["t"] != "res" || m["ws"] != shopWS {
		t.Fatalf("browser got %v", m)
	}

	// The home key and no key both mean the home hub, without the field.
	writeText(t, c, `{"t":"sub","topics":["git"],"ws":"`+homeWS+`"}`)
	if m := home.line(t); m["t"] != "sub" || m["ws"] != nil {
		t.Fatalf("home hub got %v", m)
	}
	writeText(t, c, `{"t":"sub","topics":["tasks"]}`)
	if m := home.line(t); m["t"] != "sub" {
		t.Fatalf("home hub got %v", m)
	}
	// Pings go to the home hub only.
	writeText(t, c, `{"t":"ping","n":"1"}`)
	if m := home.line(t); m["t"] != "ping" {
		t.Fatalf("home hub got %v", m)
	}
	if d.ws.Sides() != 1 {
		t.Fatalf("Sides = %d, want 1", d.ws.Sides())
	}
}

func TestDoorBadWSCloses(t *testing.T) {
	d := newDoorRig(t, nil)
	c, _ := d.connect()
	writeText(t, c, `{"t":"sub","topics":["status"],"ws":"../../x"}`)
	if code := closeCode(t, c); code != 1008 {
		t.Fatalf("close %d, want 1008", code)
	}
}

func TestDoorProjectNotRunning(t *testing.T) {
	d := newDoorRig(t, nil)
	c, home := d.connect()
	writeText(t, c, `{"t":"sub","topics":["status"],"ws":"`+otherWS+`"}`)
	m := readMsg(t, c)
	if m["t"] != "ev" || m["kind"] != "ws.gone" || m["ws"] != otherWS {
		t.Fatalf("browser got %v", m)
	}
	if data, _ := m["data"].(map[string]any); data["why"] != "not_running" {
		t.Fatalf("why %v", m["data"])
	}
	// A request for it gets err gone, and the home link still works.
	writeText(t, c, `{"t":"req","id":1,"op":"tree.list","args":{"path":""},"ws":"`+otherWS+`"}`)
	for {
		m = readMsg(t, c)
		if m["t"] == "err" {
			break
		}
	}
	if m["id"] != float64(1) || m["ws"] != otherWS || (m["code"] != "gone") {
		t.Fatalf("err %v", m)
	}
	writeText(t, c, `{"t":"sub","topics":["status"]}`)
	if m := home.line(t); m["t"] != "sub" {
		t.Fatalf("home hub got %v", m)
	}
}

func TestDoorOffWithoutDialWS(t *testing.T) {
	r := newWSRig(t, DefaultLimits, func(c *WSConfig) { c.Home = homeWS })
	c, _ := r.connect()
	writeText(t, c, `{"t":"req","id":1,"op":"tree.list","args":{"path":""},"ws":"`+shopWS+`"}`)
	m := readMsg(t, c)
	if m["t"] != "err" || m["code"] != "not_found" || m["ws"] != shopWS {
		t.Fatalf("browser got %v", m)
	}
	r.hub.none()
}

func TestDoorCapsProjects(t *testing.T) {
	d := newDoorRig(t, func(c *WSConfig) { c.MaxHubs = 1 })
	d.project(shopWS)
	d.project(otherWS)
	c, _ := d.connect()
	writeText(t, c, `{"t":"sub","topics":["status"],"ws":"`+shopWS+`"}`)
	openProject(t, d.hubs[shopWS])
	writeText(t, c, `{"t":"req","id":1,"op":"tree.list","args":{"path":""},"ws":"`+otherWS+`"}`)
	m := readMsg(t, c)
	if m["t"] != "err" || m["code"] != "limit" || m["ws"] != otherWS {
		t.Fatalf("browser got %v", m)
	}
}

func TestDoorProjectByeEndsOnlyThatProject(t *testing.T) {
	d := newDoorRig(t, nil)
	shop := d.project(shopWS)
	c, home := d.connect()
	writeText(t, c, `{"t":"req","id":1,"op":"tree.list","args":{"path":""},"ws":"`+shopWS+`"}`)
	sp := openProject(t, shop)
	if m := sp.line(t); m["t"] != "req" {
		t.Fatalf("shop hub got %v", m)
	}
	// The hub says bye with the request open: the request gets err gone,
	// the page ev ws.gone, and the link stays up.
	sp.send(t, `{"t":"bye","code":4012}`)
	sawErr, sawGone := false, false
	for !sawErr || !sawGone {
		m := readMsg(t, c)
		switch {
		case m["t"] == "err" && m["id"] == float64(1) && m["code"] == "gone" && m["ws"] == shopWS:
			sawErr = true
		case m["t"] == "ev" && m["kind"] == "ws.gone" && m["ws"] == shopWS:
			sawGone = true
		default:
			t.Fatalf("browser got %v", m)
		}
	}
	sp.eof(t)
	writeText(t, c, `{"t":"sub","topics":["status"]}`)
	if m := home.line(t); m["t"] != "sub" {
		t.Fatalf("home hub got %v", m)
	}
	waitFor(t, "no project hubs", func() bool { return d.ws.Sides() == 0 })
	if !strings.Contains(auditText(t, d.alog), `"ev":"project"`) {
		t.Fatal("no project line in the audit log")
	}
}

func TestDoorHubSettingWSIsDropped(t *testing.T) {
	d := newDoorRig(t, nil)
	shop := d.project(shopWS)
	c, _ := d.connect()
	writeText(t, c, `{"t":"sub","topics":["status"],"ws":"`+shopWS+`"}`)
	sp := openProject(t, shop)
	sp.line(t)
	// A hub may not name a workspace: it would speak for another one.
	sp.send(t, `{"t":"snap","topic":"status","seq":1,"data":{},"ws":"`+otherWS+`"}`)
	m := readMsg(t, c)
	if m["t"] != "ev" || m["kind"] != "ws.gone" || m["ws"] != shopWS {
		t.Fatalf("browser got %v", m)
	}
	if data, _ := m["data"].(map[string]any); data["why"] != "bad" {
		t.Fatalf("why %v", m["data"])
	}
}

func TestDoorBrowserCloseClosesProjectHubs(t *testing.T) {
	d := newDoorRig(t, nil)
	shop := d.project(shopWS)
	c, _ := d.connect()
	writeText(t, c, `{"t":"sub","topics":["status"],"ws":"`+shopWS+`"}`)
	sp := openProject(t, shop)
	sp.line(t)
	_ = c.CloseNow()
	sp.eof(t)
	waitFor(t, "no project hubs", func() bool { return d.ws.Sides() == 0 })
}

func TestWithWS(t *testing.T) {
	out, ok := withWS([]byte(`{"t":"res","id":1}`), shopWS)
	if !ok || string(out) != `{"ws":"`+shopWS+`","t":"res","id":1}` {
		t.Fatalf("withWS = %s, %v", out, ok)
	}
	for _, bad := range []string{``, `{}`, `[1]`, `"x"`} {
		if _, ok := withWS([]byte(bad), shopWS); ok {
			t.Errorf("withWS(%q) ok", bad)
		}
	}
}
