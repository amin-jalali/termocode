//go:build unix

package app

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// Spec (WEB-31): web_listen picks loopback (default) or tailnet; a bad
// value is loopback and one problem line (ADR 0005).
func TestWebSettingsListen(t *testing.T) {
	withConfigDir(t)
	if c := webSettings(); c.Tailnet {
		t.Fatal("tailnet by default")
	}
	writeUserConfig(t, `{"web_listen":"tailnet"}`)
	if c := webSettings(); !c.Tailnet || len(c.Problems) != 0 {
		t.Fatalf("tailnet: %+v", c)
	}
	writeUserConfig(t, `{"web_listen":"lan"}`)
	if c := webSettings(); c.Tailnet || len(c.Problems) != 1 {
		t.Fatalf("bad value: %+v", c)
	}
}

// Spec (WEB-31): in tailnet mode the gateway gets --tailnet (no --port),
// the link goes to the https tailnet URL the gateway handed back, and
// the toast names that URL so the user knows where to open it.
func TestWebStartTailnetUsesTheTailnetURL(t *testing.T) {
	r := newWebRig(t, nil)
	writeUserConfig(t, `{"web_panel":"on","web_listen":"tailnet"}`)
	links := make(chan string, 2)
	restore := SetClipboardSink(func(s string) { links <- s })
	t.Cleanup(restore)
	old := webGatewayStart
	t.Cleanup(func() { webGatewayStart = old })
	var got []string
	webGatewayStart = func(h *webHub, args []string) (*webGatewayProc, error) {
		got = args
		proc := &webGatewayProc{terminate: func() {}, alive: func() bool { return true }}
		go func() {
			_ = h.send(h.ctx, webPanelUpMsg{hub: h, proc: proc, port: 41237, url: "https://box.tail1234.ts.net/"})
		}()
		return proc, nil
	}
	r.m.startWebPanel(false, true)
	r.pumpUntil("the panel up", func() bool { return r.m.webPanel != nil && r.m.webPanel.up })
	if !slices.Contains(got, "--tailnet") || slices.Contains(got, "--port") {
		t.Fatalf("args %q", got)
	}
	select {
	case l := <-links:
		if !strings.HasPrefix(l, "https://box.tail1234.ts.net/#s=") {
			t.Fatalf("link %q", l)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no link copied")
	}
}

// Spec (WEB-31): the start card in tailnet mode says who can reach the
// panel (devices on your tailnet, never the internet) instead of
// "only on this computer".
func TestWebStartCardTailnet(t *testing.T) {
	body := webStartBody(webConfig{IdleMinutes: 15, Tailnet: true}, false)
	if strings.Contains(body, "only on this computer") || !strings.Contains(body, "tailnet") || !strings.Contains(body, "not on the internet") {
		t.Fatalf("body:\n%s", body)
	}
}
