//go:build e2e && browser

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// The web panel against the real binary, measured: what the hub sends
// while nothing happens, how long the page waits for the ops it asks,
// and which ops the hub logged as slow on its UI thread. It opens no
// view that would ask a real engine (Containers, Databases).
//
// Run: WEB_PERF_REPORT=1 go -C e2e test -tags e2e,browser -run TestWebRealPerf .

// wsLog keeps every WebSocket frame of a page.
type wsLog struct {
	mu  sync.Mutex
	all []wsMsg
}

type wsMsg struct {
	at   time.Time
	out  bool
	size int
	T    string          `json:"t"`
	ID   uint64          `json:"id"`
	Op   string          `json:"op"`
	Top  string          `json:"topic"`
	Data json.RawMessage `json:"data"`
}

func recordWS(t *testing.T, p *webPage) *wsLog {
	l := &wsLog{}
	add := func(out bool, b []byte) {
		m := wsMsg{at: time.Now(), out: out, size: len(b)}
		_ = json.Unmarshal(b, &m)
		m.Data = nil
		l.mu.Lock()
		l.all = append(l.all, m)
		l.mu.Unlock()
	}
	in := chromedp.Events(p.ctx, network.WebSocketFrameReceived)
	sent := chromedp.Events(p.ctx, network.WebSocketFrameSent)
	go func() {
		for ev, err := range in {
			if err != nil {
				return
			}
			if ev.Response != nil {
				add(false, ev.Response.PayloadData)
			}
		}
	}()
	go func() {
		for ev, err := range sent {
			if err != nil {
				return
			}
			if ev.Response != nil {
				add(true, ev.Response.PayloadData)
			}
		}
	}()
	p.do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		_, err := cdp.Call(ctx, tg, network.Enable, network.EnableParams{})
		return err
	}))
	return l
}

func (l *wsLog) since(t0 time.Time) []wsMsg {
	l.mu.Lock()
	defer l.mu.Unlock()
	var got []wsMsg
	for _, m := range l.all {
		if !m.at.Before(t0) && m.T != "ping" && m.T != "pong" {
			got = append(got, m)
		}
	}
	return got
}

// summary groups frames by direction, kind and topic or op.
func wsSummary(ms []wsMsg) string {
	type agg struct{ n, b int }
	by := map[string]*agg{}
	for _, m := range ms {
		k := "in  "
		if m.out {
			k = "out "
		}
		k += m.T + " " + m.Top + m.Op
		a := by[k]
		if a == nil {
			a = &agg{}
			by[k] = a
		}
		a.n++
		a.b += m.size
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return by[keys[i]].b > by[keys[j]].b })
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "  %5d × %8d B  %s\n", by[k].n, by[k].b, k)
	}
	return b.String()
}

// latencies pairs each req with its res (or err) by id.
func wsLatencies(ms []wsMsg) string {
	reqs := map[uint64]wsMsg{}
	var b strings.Builder
	for _, m := range ms {
		switch {
		case m.out && m.T == "req":
			reqs[m.ID] = m
		case !m.out && (m.T == "res" || m.T == "err") && m.ID != 0:
			if r, ok := reqs[m.ID]; ok {
				fmt.Fprintf(&b, "  %6dms  %-28s %s %d B\n", m.at.Sub(r.at).Milliseconds(), r.Op, m.T, m.size)
				delete(reqs, m.ID)
			}
		}
	}
	for _, r := range reqs {
		fmt.Fprintf(&b, "  no answer  %s\n", r.Op)
	}
	return b.String()
}

func TestWebRealPerf(t *testing.T) {
	if os.Getenv("WEB_PERF_REPORT") == "" {
		t.Skip("set WEB_PERF_REPORT=1")
	}
	model := newWebModel(t)
	var extra []string
	if p := os.Getenv("WEB_PERF_CPUPROFILE"); p != "" {
		extra = append(extra, "TERMOCODE_CPUPROFILE="+p)
	}
	r := newWebRig(t, append(model.env(), extra...)...)
	// A workspace of some size: 300 files in 30 folders.
	for d := range 30 {
		for f := range 10 {
			writeFile(t, filepath.Join(r.ws, fmt.Sprintf("pkg%02d", d), fmt.Sprintf("file%02d.go", f)), fmt.Sprintf("package pkg%02d\n\nfunc F%d() int { return %d }\n", d, f, f))
		}
	}
	r.git("add", "-A")
	r.git("-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-qm", "many files")
	tm := Start(t, StartOpts{Cols: 140, Rows: 40, Home: r.home, Workspace: r.ws, Env: r.env})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	link := r.startPanel()
	tm.WaitText("Web panel is on", uiTimeout)
	p := newWebPage(t, webDesktop)
	ws := recordWS(t, p)
	t0 := time.Now()
	p.do(chromedp.Navigate(link))
	p.tap(".connect-foot button")
	p.waitJS(`document.documentElement.dataset.link === "online"`)
	p.waitJS(`document.querySelector(".strip .branch")?.textContent.includes("main")`)
	p.waitJS(wFiles + `?.querySelector('.tree-row[data-path="pkg00"]')`)
	t.Logf("navigate → workbench with the tree: %s", time.Since(t0))
	t.Logf("start:\n%s", wsSummary(ws.since(t0)))

	idle := func(name string, d time.Duration) {
		s := time.Now()
		time.Sleep(d)
		ms := ws.since(s)
		bytes := 0
		for _, m := range ms {
			bytes += m.size
		}
		t.Logf("idle %s %s: %d frames (%.1f/s), %d B\n%s", name, d, len(ms), float64(len(ms))/d.Seconds(), bytes, wsSummary(ms))
	}
	idle("after start", 10*time.Second)

	step := func(name string, do func()) {
		s := time.Now()
		do()
		t.Logf("%s: %s\n%s%s", name, time.Since(s).Round(time.Millisecond), wsSummary(ws.since(s)), wsLatencies(ws.since(s)))
	}
	step("open pkg00", func() {
		p.tapJS(wFiles + `.querySelector('.tree-row[data-path="pkg00"]')`)
		p.waitJS(wFiles + `.querySelector('.tree-row[data-path="pkg00/file00.go"]')`)
	})
	step("open file", func() {
		p.tapJS(wFiles + `.querySelector('.tree-row[data-path="pkg00/file00.go"]')`)
		p.waitJS(wFiles + `.querySelector(".desk-editor")?.contentDocument?.querySelector(".view-lines")?.textContent.replace(/\u00a0/g, " ").includes("package pkg00")`)
	})
	for _, v := range []string{"git", "chat", "files"} {
		step("view "+v, func() { p.goTab(v) })
	}
	idle("at the end", 10*time.Second)
	b, _ := os.ReadFile(filepath.Join(r.home, ".local", "state", "termocode", "errors.log"))
	if len(b) == 0 {
		matches, _ := filepath.Glob(filepath.Join(r.home, "*", "*", "termocode", "errors.log"))
		matches2, _ := filepath.Glob(filepath.Join(r.home, "*", "termocode", "errors.log"))
		for _, f := range append(matches, matches2...) {
			x, _ := os.ReadFile(f)
			b = append(b, x...)
		}
	}
	t.Logf("errors.log:\n%s", b)
}
