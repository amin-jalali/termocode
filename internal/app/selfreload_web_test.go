//go:build unix

package app

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (reload on a new build): a reload hands the web panel over. The
// running gateway gets the bye 1012 and is gone before the exec; the
// browser sees 1012 (the page retries). The next image starts the
// gateway again by itself, on the same port, and the same browser gets
// back in with the token it holds: no new link, no new pairing. Its
// meta names the running build, so a page of an older build reloads.
func TestReloadHandsTheWebPanelOver(t *testing.T) {
	r := newWebPanelRig(t)
	c, tok, meta := r.browserTok(r.started())
	port := r.m.webPanel.port
	if b, _ := meta["build"].(string); b == "" {
		t.Errorf("meta %v has no build", meta)
	}
	var execs int
	old := selfReloadDepsFor
	t.Cleanup(func() { selfReloadDepsFor = old })
	selfReloadDepsFor = func() selfReloadDeps {
		d := old()
		d.exe = func() (string, error) { return "/opt/bin/termocode", nil }
		d.exec = func(string, []string, []string) error {
			execs++
			if st := r.m.webPanel; st != nil && st.proc.alive() {
				t.Error("exec while the gateway still runs")
			}
			return nil
		}
		d.allowed = func() bool { return true }
		return d
	}
	// The page reads all the time, as a browser does (it answers the
	// close frame at once).
	closed := make(chan websocket.StatusCode, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				closed <- websocket.CloseStatus(err)
				return
			}
		}
	}()
	start := time.Now()
	r.m.execReload()
	if execs != 1 {
		t.Fatalf("execs %d", execs)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the hand-over took %v", d)
	}
	if got := <-closed; got != websocket.StatusCode(webproto.CloseRestarting) {
		t.Fatalf("browser closed with %d, want 1012", got)
	}
	r.pumpUntil("the old panel gone", func() bool { return r.m.webPanel == nil })

	// The next image: its start finds the hand-over and starts the panel.
	cmd := r.m.webResumeInitCmd()
	if cmd == nil {
		t.Fatal("no web resume at start")
	}
	if next, ok := r.m.handleWebMsg(cmd()); ok && next != nil {
		_ = next
	}
	r.pumpUntil("the panel back", func() bool { return r.m.webPanel != nil && r.m.webPanel.up })
	if r.m.webPanel.port != port {
		t.Fatalf("the panel came back on port %d, want %d", r.m.webPanel.port, port)
	}
	r.mu.Lock()
	args := r.args[len(r.args)-1]
	r.mu.Unlock()
	if !hasArgs(args, "--resume") || !hasArgs(args, "--port", strconv.Itoa(port)) {
		t.Errorf("the next gateway's args %q lack --resume and --port %d", args, port)
	}
	r.browserWith("http://127.0.0.1:"+strconv.Itoa(port), tok) // the old token: no new link
	if r.m.webResumeInitCmd() != nil {
		t.Error("the hand-over was used twice")
	}
}

func hasArgs(args []string, want ...string) bool {
	for i := range args {
		if i+len(want) <= len(args) {
			ok := true
			for j, w := range want {
				ok = ok && args[i+j] == w
			}
			if ok {
				return true
			}
		}
	}
	return false
}
