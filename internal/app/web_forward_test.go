//go:build unix

package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/session"
)

// fakeFwdServe records tailscale serve starts and stops for previews.
type fakeFwdServe struct {
	starts  []string
	stopped int
}

func (f *fakeFwdServe) start(https int, target string) (webFwdServe, error) {
	f.starts = append(f.starts, strconv.Itoa(https)+" "+target)
	return fakeFwdStop{f}, nil
}

type fakeFwdStop struct{ f *fakeFwdServe }

func (s fakeFwdStop) Stop() { s.f.stopped++ }

// swapForward replaces the starters of web_forward.go for one test.
func swapForward(t *testing.T) (*fakeFwdServe, *[]string, *[]string) {
	t.Helper()
	sv := &fakeFwdServe{}
	var wrote, opened []string
	oldS, oldW, oldO := webForwardServe, webForwardWrite, webForwardOpenURL
	webForwardServe = sv.start
	webForwardWrite = func(s string) error { wrote = append(wrote, s); return nil }
	webForwardOpenURL = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { webForwardServe, webForwardWrite, webForwardOpenURL = oldS, oldW, oldO })
	return sv, &wrote, &opened
}

// auditOps reads the control connection until n audit lines came.
func (r *accessRig) auditOps(n int) []string {
	r.t.Helper()
	var ops []string
	for len(ops) < n {
		d := r.ctlEvent("audit")
		if op, _ := d["op"].(string); strings.HasPrefix(op, "fwd_") {
			ops = append(ops, op+" "+d["summary"].(string))
		}
	}
	return ops
}

// Spec (WEB-54): Web: Forward Port… asks only for a port, then a Risky
// card (Not now first and focused); Esc opens nothing. Open port in
// tailnet mode starts tailscale serve on its own https port for
// 127.0.0.1:<port>, lists it and audits fwd_open.
func TestForwardNeedsConfirm(t *testing.T) {
	r := newAccessRig(t)
	sv, _, _ := swapForward(t)
	r.m.dispatchWebForwardPalette("web-forward-port")
	if !r.m.promptOpen || r.m.promptKind != promptKindWebForwardTo {
		t.Fatal("no port prompt")
	}
	r.m.promptOpen = false
	r.m.onWebForwardPort("3000")
	if !r.m.confirmOpen || r.m.confirm.Buttons()[0].ID != "not-now" || r.m.confirm.SafeID() != "not-now" {
		t.Fatalf("card %+v", r.m.confirm.Buttons())
	}
	if v := ansiStrip(r.m.confirm.View()); !strings.Contains(v, "Open port 3000") || !strings.Contains(v, "can see what you type") {
		t.Fatalf("card:\n%s", v)
	}
	r.m.confirmOpen = false
	_ = r.m.onAIConfirmClosed() // Esc
	if len(sv.starts) != 0 || len(r.m.webAccess().fwds) != 0 {
		t.Fatalf("Esc opened a forward: %v", sv.starts)
	}

	r.m.onWebForwardPort("3000")
	webAccessSelect(r.m, "open")
	if len(sv.starts) != 1 || sv.starts[0] != "8443 http://127.0.0.1:3000" {
		t.Fatalf("serve %v", sv.starts)
	}
	fw := r.m.webAccess().fwds
	if len(fw) != 1 || fw[0].url != "https://box.tail1234.ts.net:8443/" || fw[0].url == r.m.webPanel.url {
		t.Fatalf("forwards %+v", fw)
	}
	if ops := r.auditOps(1); ops[0] != "fwd_open port 3000 (tailnet)" {
		t.Errorf("audit %q", ops)
	}
	// A second port gets the next https port: its own origin too.
	r.m.onWebForwardPort("5173")
	webAccessSelect(r.m, "open")
	if len(sv.starts) != 2 || sv.starts[1] != "8444 http://127.0.0.1:5173" {
		t.Fatalf("serve %v", sv.starts)
	}
}

// Spec (WEB-54): there is no host field. Only a port number passes the
// prompt; a host, an address, a URL, 0 or a too big number is refused,
// and so is the panel's own port.
func TestForwardNoHostField(t *testing.T) {
	for _, bad := range []string{"", "evil.example:3000", "127.0.0.1:3000", "http://localhost:3000", "[::1]:3000", "0", "70000", "3000 4000", "-1"} {
		if _, err := parseWebForwardPort(bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
	if p, err := parseWebForwardPort(" 3000 "); err != nil || p != 3000 {
		t.Errorf("3000: %d %v", p, err)
	}
	r := newAccessRig(t)
	swapForward(t)
	r.m.onWebForwardPort("41237")
	if r.m.confirmOpen || !strings.Contains(webTestToasts(*r.m), "own port") {
		t.Fatalf("own port: card %v toast %q", r.m.confirmOpen, webTestToasts(*r.m))
	}
}

// Spec (WEB-54): in SSH mode the yes sends termocode ssh one in-band
// request for 127.0.0.1:<port>; Web: Forwarded Ports… lists it and Enter
// stops it (a close request, fwd_close audited).
func TestForwardSSHModeRequestsAndStops(t *testing.T) {
	r := newAccessRig(t)
	_, wrote, _ := swapForward(t)
	r.m.webPanel.url, r.m.webPanel.sshPort = "", 41237
	r.m.onWebForwardPort("3000")
	webAccessSelect(r.m, "open")
	fw := r.m.webAccess().fwds
	if len(fw) != 1 || len(*wrote) != 1 {
		t.Fatalf("forwards %+v wrote %q", fw, *wrote)
	}
	if want := session.PortForwardAPC(session.PortForward{Open: true, Port: 3000, ID: fw[0].id}); (*wrote)[0] != want {
		t.Fatalf("wrote %q, want %q", (*wrote)[0], want)
	}
	r.m.dispatchWebForwardPalette("web-forwarded-ports")
	if !r.m.pickerOpen || r.m.pickerKind != pickerKindWebForwards || !strings.Contains(r.m.picker.View(), "Port 3000") {
		t.Fatal("no list")
	}
	r.m.pickerOpen = false
	r.m.handleWebAccessPicker(fw[0].id)
	if len(r.m.webAccess().fwds) != 0 || len(*wrote) != 2 ||
		(*wrote)[1] != session.PortForwardAPC(session.PortForward{ID: fw[0].id}) {
		t.Fatalf("stop: forwards %d wrote %q", len(r.m.webAccess().fwds), *wrote)
	}
	if ops := r.auditOps(2); ops[0] != "fwd_open port 3000 (ssh)" || ops[1] != "fwd_close port 3000 (ssh)" {
		t.Errorf("audit %q", ops)
	}
}

// Spec (WEB-54): forwards end with the panel; they never survive it.
func TestForwardsEndWithPanel(t *testing.T) {
	r := newAccessRig(t)
	sv, _, _ := swapForward(t)
	r.m.onWebForwardPort("3000")
	webAccessSelect(r.m, "open")
	r.m.stopWebPanel()
	if sv.stopped != 1 || len(r.m.webAccess().fwds) != 0 {
		t.Fatalf("after stop: %d stopped, %d left", sv.stopped, len(r.m.webAccess().fwds))
	}
}

// Spec (WEB-54): with the panel on this computer nothing is forwarded:
// the browser here opens 127.0.0.1:<port> after the card.
func TestForwardLocalModeOpensHere(t *testing.T) {
	r := newAccessRig(t)
	sv, wrote, opened := swapForward(t)
	r.m.webPanel.url = ""
	r.m.onWebForwardPort("3000")
	webAccessSelect(r.m, "open")
	if len(*opened) != 1 || (*opened)[0] != "http://127.0.0.1:3000/" || len(sv.starts) != 0 || len(*wrote) != 0 {
		t.Fatalf("opened %q serve %v wrote %q", *opened, sv.starts, *wrote)
	}
}

// Spec (WEB-54): no panel, no forward: one plain sentence, no prompt.
func TestForwardNeedsPanel(t *testing.T) {
	r := newAccessRig(t)
	r.m.webPanel = nil
	r.m.dispatchWebForwardPalette("web-forward-port")
	if r.m.promptOpen || !strings.Contains(webTestToasts(*r.m), "Web: Open in Browser") {
		t.Fatalf("prompt %v toast %q", r.m.promptOpen, webTestToasts(*r.m))
	}
}

// Spec (WEB-54): AI agents, MCP and project files can never create or
// list a forward: both ids are never in the action policy.
func TestAIActionsNeverForward(t *testing.T) {
	for _, it := range webForwardPaletteItems() {
		p, ok := aiActionPolicyFor(it.ID)
		if !ok || p.Gate != aiGateNever || p.Reason == "" {
			t.Errorf("%s: policy %+v %v", it.ID, p, ok)
		}
	}
}
