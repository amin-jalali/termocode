//go:build e2e && browser

package e2e

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// webLatProbe times one key on the page's clock (performance.now): the
// keydown, its term.send frame out, the first term delta and the answer
// in (when the browser got them: the events' timeStamp), and the moment
// the terminal's rows hold the text wanted.
const webLatProbe = `(() => {
  if (window.__lat) return true;
  const L = window.__lat = { k: 0, s: 0, a: 0, r: 0, d: 0, want: "", id: 0 };
  addEventListener("keydown", () => { if (!L.k) L.k = performance.now(); }, true);
  const send = WebSocket.prototype.send;
  WebSocket.prototype.send = function (data) {
    if (!this.__lat) {
      this.__lat = true;
      this.addEventListener("message", (e) => {
        if (!L.s || typeof e.data !== "string") return;
        if (!L.r && e.data.includes('"topic":"term:')) L.r = e.timeStamp;
        if (!L.a && e.data.includes('"res"') && e.data.includes('"id":' + L.id)) L.a = e.timeStamp;
      });
    }
    if (L.k && !L.s && typeof data === "string" && data.includes('"term.send"')) {
      L.s = performance.now();
      L.id = JSON.parse(data).id;
    }
    return send.call(this, data);
  };
  const pane = () => document.querySelector('[data-view="term"] .dterm-pane.is-focus .term-rows');
  new MutationObserver(() => {
    const p = pane();
    if (L.want && !L.d && p && p.textContent.includes(L.want)) L.d = performance.now();
  }).observe(document.body, { subtree: true, childList: true, characterData: true });
  return true;
})()`

// Budgets of the real path (Chrome, the real binary's gateway and hub,
// nvim and a shell). The tight budget of our own code is in
// internal/app/web_term_fast_test.go; these catch a page or a link that
// holds keys back.
const (
	webKeyBudget   = 100 * time.Millisecond // one key to its echo on the page, p95
	webBurstBudget = time.Second            // 80 keys typed at once, all on the page
)

// TestWebRealTermLatency: keys typed into the desktop's terminal show on
// the page soon, through the real binary, gateway and nvim, over 3 000
// rows of scrollback: one at a time (each within webKeyBudget, p95), and
// 80 typed at once (a held key, a fast typist: all within webBurstBudget;
// one request a key would wait behind the gateway's 20 requests a second
// for 2 s). It logs where the time goes.
//
// Run: go -C e2e test -tags e2e,browser -run TestWebRealTermLatency -v .
func TestWebRealTermLatency(t *testing.T) {
	r := newWebRig(t)
	tm := Start(t, StartOpts{Cols: 140, Rows: 40, Home: r.home, Workspace: r.ws, Env: r.env})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	link := r.startPanel()
	tm.WaitText("Web panel is on", uiTimeout)
	p := newWebPage(t, webDesktop)
	p.do(chromedp.Navigate(link))
	p.tap(".connect-foot button")
	p.waitJS(`document.documentElement.dataset.link === "online"`)
	p.goTab("term")
	open := webButton(wTerm, "Open one")
	line := wTerm + `.querySelector(".dterm-pane.is-focus .term-rows")`
	p.waitJS(open + ` || ` + line)
	if webEval[bool](p, `!!(`+open+`)`) {
		p.tapJS(open)
	}
	p.tapJS(line)
	p.waitJS(`document.activeElement === ` + wTerm + `.querySelector(".dterm-pane.is-focus .dterm-input")`)
	p.keys("seq 1 3000 | sed 's/$/ a longer row to fill the scrollback/'\r")
	p.waitJSFor(line+`.textContent.includes("3000 a longer")`, 20*time.Second)
	p.keys("echo ")
	p.waitJS(line + `.textContent.includes("echo ")`)
	webEval[bool](p, webLatProbe)
	reset := func(want string) {
		webEval[bool](p, fmt.Sprintf(`(Object.assign(window.__lat, { k: 0, s: 0, a: 0, r: 0, d: 0, want: %q }), true)`, want))
	}

	var page, trip, answer, paint, total []float64
	typed := "echo "
	for i := 0; i < 40; i++ {
		ch := string(rune('a' + i%26))
		typed += ch
		reset(typed)
		p.keys(ch)
		p.waitJSFor(`window.__lat.d > 0`, 10*time.Second)
		v := webEval[[]float64](p, `[window.__lat.k, window.__lat.s, window.__lat.a, window.__lat.r, window.__lat.d]`)
		page = append(page, v[1]-v[0])
		answer = append(answer, v[2]-v[1])
		trip = append(trip, v[3]-v[1])
		paint = append(paint, v[4]-v[3])
		total = append(total, v[4]-v[0])
	}
	q := func(name string, d []float64) float64 {
		s := append([]float64(nil), d...)
		sort.Float64s(s)
		p95 := s[len(s)*95/100]
		t.Logf("%-27s p50 %6.1f ms  p95 %6.1f ms  max %6.1f ms", name, s[len(s)/2], p95, s[len(s)-1])
		return p95
	}
	q("key -> term.send out", page)
	q("term.send out -> its answer", answer)
	q("term.send out -> delta in", trip)
	q("delta in -> rows on the page", paint)
	if p95 := q("key -> rows on the page", total); p95 > float64(webKeyBudget.Milliseconds()) {
		t.Errorf("a key took %.1f ms (p95) to show, budget %v", p95, webKeyBudget)
	}

	burst := strings.Repeat("0123456789", 8)
	typed += burst
	reset(typed)
	start := time.Now()
	p.keys(burst)
	typedIn := time.Since(start)
	p.waitJSFor(`window.__lat.d > 0`, 20*time.Second)
	v := webEval[[]float64](p, `[window.__lat.k, window.__lat.d]`)
	t.Logf("burst of %d keys: typed in %v, all on the page %.1f ms after the first key", len(burst), typedIn.Round(time.Millisecond), v[1]-v[0])
	if v[1]-v[0] > float64(webBurstBudget.Milliseconds()) {
		t.Errorf("%d keys typed at once took %.0f ms to show, budget %v", len(burst), v[1]-v[0], webBurstBudget)
	}
	if !strings.Contains(webEval[string](p, line+`.textContent`), typed) {
		t.Fatalf("the terminal lost keys: want %q", typed)
	}
	tm.Quit()
}
