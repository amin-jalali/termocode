//go:build browser

package web

import (
	"testing"
	"time"
)

// Perf guards (web perf): budgets the page must hold in a real Chrome.
// TestPerfReport (perfwalk_test.go) logs the numbers behind them.

// What goes over the wire: the page on a desktop and on a phone, and the
// editor the desktop loads at its first file. The page is sent anew on
// every load (no-store), so it must be packed: unpacked it was 0.84 MB
// (desktop) and 0.57 MB (phone), and the editor 4.6 MB, 1.2 s on a
// 50 Mbit/s Wi-Fi.
const (
	wireDesktopPage = 300 << 10
	wirePhonePage   = 220 << 10
	wireEditor      = 1400 << 10
)

func TestPerfGuardWireBytes(t *testing.T) {
	for _, vp := range []Viewport{Desktop, Phone} {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			r := StartPerf(p)
			p.Open()
			n, urls := r.Fetched(time.Time{})
			budget := wirePhonePage
			if vp.W >= 1200 {
				budget = wireDesktopPage
			}
			t.Logf("page: %d B over the wire", n)
			if n > budget {
				t.Errorf("the page took %d B over the wire, budget %d B: %v", n, budget, urls)
			}
			if vp.W < 1200 {
				return
			}
			c := p.LoginHere("c-wire")
			since := time.Now()
			openBufAs(t, p, c, 2, "a.go", nil, "package a")
			p.WaitJS(deskDoc + `?.querySelector(".view-lines .view-line")`)
			n, urls = r.Fetched(since)
			t.Logf("first file open: %d B over the wire", n)
			if n > wireEditor {
				t.Errorf("the first file open took %d B over the wire, budget %d B: %v", n, wireEditor, urls)
			}
		})
	}
}
