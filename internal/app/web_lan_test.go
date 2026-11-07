//go:build unix

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
)

// Spec (LAN mode): "web_listen": "lan" turns LAN mode on; it is not
// tailnet mode, and it is never on by default.
func TestWebSettingsLAN(t *testing.T) {
	withConfigDir(t)
	if c := webSettings(); c.LAN {
		t.Fatal("lan by default")
	}
	writeUserConfig(t, `{"web_listen":"lan"}`)
	if c := webSettings(); !c.LAN || c.Tailnet || len(c.Problems) != 0 {
		t.Fatalf("lan: %+v", c)
	}
	writeUserConfig(t, `{"web_listen":"public"}`)
	if c := webSettings(); c.LAN || c.Tailnet || len(c.Problems) != 1 {
		t.Fatalf("bad value: %+v", c)
	}
}

// Spec (LAN mode): the gateway gets --lan and a fixed port (the tablet
// saves the origin with its port), web_port when set; never --tailnet.
func TestWebGatewayArgsLAN(t *testing.T) {
	cfg := webConfig{IdleMinutes: 15, MaxHours: 12, MaxViewers: 4, LAN: true}
	args := webGatewayArgs(cfg, "/w", "/a", "/d", 0)
	if !slices.Contains(args, "--lan") || slices.Contains(args, "--tailnet") {
		t.Fatalf("args %q", args)
	}
	if i := slices.Index(args, "--port"); i < 0 || args[i+1] != "47443" {
		t.Fatalf("default port: %q", args)
	}
	cfg.Port = 9443
	args = webGatewayArgs(cfg, "/w", "/a", "/d", 0)
	if i := slices.Index(args, "--port"); i < 0 || args[i+1] != "9443" {
		t.Fatalf("web_port: %q", args)
	}
}

// Spec (LAN mode): the start card says the panel is reachable on this
// Wi-Fi, not only on this computer.
func TestWebStartCardLAN(t *testing.T) {
	body := webStartBody(webConfig{IdleMinutes: 15, LAN: true}, false)
	if strings.Contains(body, "only on this computer") || !strings.Contains(body, "same Wi-Fi") {
		t.Fatalf("body:\n%s", body)
	}
}

// Spec (LAN mode): pairing works in LAN mode, and the link carries the
// certificate's fingerprint: https://<lan ip>:<port>/#p=<secret>&fp=<fp>.
func TestWebPairLANLinkHasFingerprint(t *testing.T) {
	r := newAccessRig(t)
	writeUserConfig(t, `{"web_panel":"on","web_listen":"lan"}`)
	const fp = "q2Hc0Vg8gkN0Hq9nD7oQ1yRj4wM5uY6tZ3xC2bA1aB0"
	r.m.webPanel.url, r.m.webPanel.fp = "https://192.168.1.20:47443/", fp
	r.m.openWebPair(false)
	ev := r.ctlEvent("pair")
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindWebPairQR {
		t.Fatalf("no QR card: %q", webTestToasts(*r.m))
	}
	webAccessSelect(r.m, "copy")
	link := <-r.links
	rest, ok := strings.CutPrefix(link, "https://192.168.1.20:47443/#p=")
	plain, gotFP, ok2 := strings.Cut(rest, "&fp=")
	sum := sha256.Sum256([]byte(plain))
	if !ok || !ok2 || gotFP != fp || hex.EncodeToString(sum[:]) != ev["sha256"] {
		t.Fatalf("link %q", link)
	}
}

// Spec (LAN mode): a share link (sent to another person) is not made in
// LAN mode, and loopback mode still refuses pairing and names both modes.
func TestWebPairLANRefusals(t *testing.T) {
	r := newAccessRig(t)
	r.m.webPanel.url = ""
	r.m.openWebPair(false)
	if toasts := webTestToasts(*r.m); r.m.confirmOpen || !strings.Contains(toasts, `"lan"`) || !strings.Contains(toasts, `"tailnet"`) {
		t.Fatalf("loopback: card %v toast %q", r.m.confirmOpen, toasts)
	}
	r.m.webPanel.url, r.m.webPanel.fp = "https://192.168.1.20:47443/", "q2Hc0Vg8gkN0Hq9nD7oQ1yRj4wM5uY6tZ3xC2bA1aB0"
	r.m.openWebPair(true)
	if r.m.confirmOpen || !strings.Contains(webTestToasts(*r.m), "Wi-Fi") {
		t.Fatalf("lan share: card %v toast %q", r.m.confirmOpen, webTestToasts(*r.m))
	}
}
