//go:build unix

package app

import (
	"net"
	"os"
	"slices"
	"strings"
	"testing"
)

// lanSwitchRig is an access rig in loopback mode: the panel runs on
// 127.0.0.1, this laptop has a Wi-Fi address (or not), and every gateway
// start is recorded.
type lanSwitchRig struct {
	*accessRig
	starts [][]string
	procs  []*webGatewayProc
}

func newLANSwitchRig(t *testing.T, wifi bool) *lanSwitchRig {
	t.Helper()
	r := &lanSwitchRig{accessRig: newAccessRig(t)}
	writeUserConfig(t, `{"web_panel":"on","theme":"dark"}`)
	r.m.webPanel.url = ""
	oldAddr, oldStart := webLANAddr, webGatewayStart
	t.Cleanup(func() { webLANAddr, webGatewayStart = oldAddr, oldStart })
	webLANAddr = func() (net.IP, bool) {
		if !wifi {
			return nil, false
		}
		return net.IPv4(192, 168, 1, 20), true
	}
	webGatewayStart = func(_ *webHub, args []string) (*webGatewayProc, error) {
		p := &webGatewayProc{terminate: func() {}, alive: func() bool { return false }}
		r.starts = append(r.starts, args)
		r.procs = append(r.procs, p)
		return p, nil
	}
	return r
}

// Spec (pair-normal-flow): Web: Pair a Device… in loopback mode asks to
// use this Wi-Fi on a Risky card: Not now first and the safe answer
// (Enter and Esc), Use this Wi-Fi second; the card says what changes and
// names tailnet mode for outside the home. Esc changes nothing.
func TestWebPairLoopbackAsksForWiFi(t *testing.T) {
	r := newLANSwitchRig(t, true)
	r.m.openWebPair(false)
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindWebUseLAN {
		t.Fatalf("no Wi-Fi card: kind %v toast %q", r.m.confirmKind, webTestToasts(*r.m))
	}
	b := r.m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "not-now" || b[1].ID != "use-lan" || r.m.confirm.SafeID() != "not-now" {
		t.Fatalf("buttons %+v safe %q", b, r.m.confirm.SafeID())
	}
	view := ansiStrip(r.m.confirm.View())
	for _, want := range []string{"Pair a phone on this Wi-Fi?", "private address", "certificate", "your yes", "tailnet", "Not now", "Use this Wi-Fi"} {
		if !strings.Contains(view, want) {
			t.Errorf("card misses %q:\n%s", want, view)
		}
	}
	webAccessSelect(r.m, "") // Esc
	if webSettings().LAN || len(r.starts) != 0 || r.m.webPanel.stopping {
		t.Fatalf("Esc changed something: lan %v starts %d stopping %v", webSettings().LAN, len(r.starts), r.m.webPanel.stopping)
	}
	r.m.openWebPair(false)
	webAccessSelect(r.m, "not-now")
	if webSettings().LAN || len(r.starts) != 0 {
		t.Fatal("Not now changed something")
	}
}

// Spec (pair-normal-flow): Use this Wi-Fi saves "web_listen": "lan" in
// the user's config.json (other keys kept), stops the loopback panel,
// starts it again with --lan and shows the QR code as soon as it is up,
// with the address under the code.
func TestWebUseLANSavesRestartsAndShowsQR(t *testing.T) {
	r := newLANSwitchRig(t, true)
	old := r.m.webPanel.proc
	r.m.openWebPair(false)
	webAccessSelect(r.m, "use-lan")
	if !webSettings().LAN {
		t.Fatal(`"web_listen": "lan" not saved`)
	}
	path, _ := settingsConfigPath()
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"theme"`) {
		t.Fatalf("other keys lost: %s", b)
	}
	if !r.m.webPanel.stopping {
		t.Fatal("the loopback panel was not stopped")
	}
	r.m.onWebPanelExit(webPanelExitMsg{hub: r.hub, proc: old})
	if len(r.starts) != 1 || !slices.Contains(r.starts[0], "--lan") {
		t.Fatalf("not started in LAN mode: %q", r.starts)
	}
	r.m.onWebPanelUp(webPanelUpMsg{hub: r.hub, proc: r.procs[0], port: 47443,
		url: "https://192.168.1.20:47443/", fp: "q2Hc0Vg8gkN0Hq9nD7oQ1yRj4wM5uY6tZ3xC2bA1aB0"})
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindWebPairQR {
		t.Fatalf("no QR card after the restart: %q", webTestToasts(*r.m))
	}
	view := ansiStrip(r.m.confirm.View())
	if !strings.Contains(view, "192.168.1.20:47443") || !strings.Contains(view, "2 minutes") {
		t.Fatalf("QR card:\n%s", view)
	}
	if at, qr := strings.Index(view, "192.168.1.20:47443"), strings.LastIndex(view, "█"); at < qr {
		t.Fatalf("the address is not under the code:\n%s", view)
	}
}

// Spec (pair-normal-flow): with no Wi-Fi or LAN address, Use this Wi-Fi
// says so plainly and changes nothing.
func TestWebUseLANNoWiFi(t *testing.T) {
	r := newLANSwitchRig(t, false)
	r.m.openWebPair(false)
	webAccessSelect(r.m, "use-lan")
	if !strings.Contains(webTestToasts(*r.m), "Connect this laptop to Wi-Fi first") {
		t.Fatalf("toast %q", webTestToasts(*r.m))
	}
	if webSettings().LAN || r.m.webPanel.stopping || len(r.starts) != 0 {
		t.Fatal("something changed without a Wi-Fi address")
	}
}

// Spec (pair-normal-flow): in LAN mode with the panel not running, Pair
// a Device starts it and shows the QR code once it is up.
func TestWebPairStartsLANPanel(t *testing.T) {
	r := newLANSwitchRig(t, true)
	writeUserConfig(t, `{"web_panel":"on","web_listen":"lan"}`)
	r.m.webPanel = nil
	r.m.openWebPair(false)
	if len(r.starts) != 1 || !slices.Contains(r.starts[0], "--lan") {
		t.Fatalf("not started: %q (toast %q)", r.starts, webTestToasts(*r.m))
	}
	r.m.onWebPanelUp(webPanelUpMsg{hub: r.hub, proc: r.procs[0], port: 47443,
		url: "https://192.168.1.20:47443/", fp: "q2Hc0Vg8gkN0Hq9nD7oQ1yRj4wM5uY6tZ3xC2bA1aB0"})
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindWebPairQR {
		t.Fatalf("no QR card: %q", webTestToasts(*r.m))
	}
}
