//go:build browser

package web

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/performance"
	"github.com/chromedp/cdproto/profiler"
	"github.com/chromedp/chromedp"
)

// Perf records what the page costs while a test drives it: the long
// tasks Chrome reports (PerformanceObserver "longtask", over 50 ms), the
// DevTools run-time metrics (script, layout and style time and counts)
// and every WebSocket frame in and out.
type Perf struct {
	p *Page

	mu   sync.Mutex
	in   []wsFrame
	out  []wsFrame
	urls map[network.RequestID]string
	got  []fetched
}

// fetched is one finished HTTP response: its URL and the bytes that
// came over the wire (headers and body as sent, compressed or not).
type fetched struct {
	at    time.Time
	url   string
	bytes int
}

type wsFrame struct {
	at   time.Time
	size int
	head string
}

// perfScript runs before the page's own scripts: it keeps every long
// task (start and length in ms) in window.__tcLong, and counts the DOM
// changes in window.__tcMut.
const perfScript = `{
	window.__tcLong = [];
	window.__tcMut = 0;
	try {
		new PerformanceObserver((l) => { for (const e of l.getEntries()) window.__tcLong.push([Math.round(e.startTime), Math.round(e.duration)]); }).observe({ type: "longtask", buffered: true });
	} catch {}
	const mo = new MutationObserver((rs) => { window.__tcMut += rs.length; });
	const watch = () => mo.observe(document.documentElement, { subtree: true, childList: true, attributes: true, characterData: true });
	if (document.documentElement) watch(); else addEventListener("DOMContentLoaded", watch);
}`

// StartPerf starts recording on p; call it before the page opens.
func StartPerf(p *Page) *Perf {
	p.T.Helper()
	r := &Perf{p: p, urls: map[network.RequestID]string{}}
	reqs := chromedp.Events(p.Ctx, network.RequestWillBeSent)
	done := chromedp.Events(p.Ctx, network.LoadingFinished)
	go func() {
		for ev, err := range reqs {
			if err != nil {
				return
			}
			r.mu.Lock()
			r.urls[ev.RequestID] = ev.Request.URL
			r.mu.Unlock()
		}
	}()
	go func() {
		for ev, err := range done {
			if err != nil {
				return
			}
			r.mu.Lock()
			r.got = append(r.got, fetched{at: time.Now(), url: r.urls[ev.RequestID], bytes: int(ev.EncodedDataLength)})
			r.mu.Unlock()
		}
	}()
	in := chromedp.Events(p.Ctx, network.WebSocketFrameReceived)
	out := chromedp.Events(p.Ctx, network.WebSocketFrameSent)
	go func() {
		for ev, err := range in {
			if err != nil {
				return
			}
			if ev.Response != nil {
				r.add(&r.in, ev.Response.PayloadData)
			}
		}
	}()
	go func() {
		for ev, err := range out {
			if err != nil {
				return
			}
			if ev.Response != nil {
				r.add(&r.out, ev.Response.PayloadData)
			}
		}
	}()
	p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		if _, err := cdp.Call(ctx, tg, performance.Enable, performance.EnableParams{}); err != nil {
			return err
		}
		_, err := cdp.Call(ctx, tg, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: perfScript})
		return err
	}))
	return r
}

// Fetched returns the HTTP bytes over the wire from since on, and the
// URLs with their bytes.
func (r *Perf) Fetched(since time.Time) (int, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	var urls []string
	for _, f := range r.got {
		if f.at.Before(since) || strings.HasPrefix(f.url, "data:") {
			continue
		}
		n += f.bytes
		u := f.url
		if i := strings.Index(u, "/assets/"); i >= 0 {
			u = u[i+len("/assets/"):]
		}
		urls = append(urls, fmt.Sprintf("%s %d", u, f.bytes))
	}
	return n, urls
}

// Net emulates a link of mbps megabits a second each way and latency
// ms (a home Wi-Fi LAN is about 50 Mbit/s and 5 ms).
func (r *Perf) Net(mbps, latency float64) {
	r.p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		bps := mbps * 1e6 / 8
		_, err := cdp.Call(ctx, tg, network.EmulateNetworkConditionsByRule, network.EmulateNetworkConditionsByRuleParams{
			MatchedNetworkConditions: []*network.Conditions{{Latency: latency, DownloadThroughput: bps, UploadThroughput: bps}}})
		return err
	}))
}

// Throttle slows the page's CPU by rate (4 is a slow laptop).
func (r *Perf) Throttle(rate float64) {
	r.p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		_, err := cdp.Call(ctx, tg, emulation.SetCPUThrottlingRate, emulation.SetCPUThrottlingRateParams{Rate: rate})
		return err
	}))
}

func (r *Perf) add(into *[]wsFrame, b []byte) {
	head := string(b)
	if len(head) > 60 {
		head = head[:60]
	}
	r.mu.Lock()
	*into = append(*into, wsFrame{at: time.Now(), size: len(b), head: head})
	r.mu.Unlock()
}

// Frames returns the frames in and out from since on, pings and pongs
// left out (the keep-alive is not traffic a view causes).
func (r *Perf) Frames(since time.Time) (in, out []wsFrame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	keep := func(fs []wsFrame) []wsFrame {
		var got []wsFrame
		for _, f := range fs {
			if f.at.Before(since) || strings.Contains(f.head, `"t":"ping"`) || strings.Contains(f.head, `"t":"pong"`) {
				continue
			}
			got = append(got, f)
		}
		return got
	}
	return keep(r.in), keep(r.out)
}

// Metrics returns the DevTools run-time metrics now (ScriptDuration,
// LayoutDuration, RecalcStyleDuration and TaskDuration in seconds,
// LayoutCount, RecalcStyleCount, Nodes, JSHeapUsedSize).
func (r *Perf) Metrics() map[string]float64 {
	r.p.T.Helper()
	var m map[string]float64
	r.p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		res, err := cdp.Call(ctx, tg, performance.GetMetrics, cdp.Empty{})
		if err != nil {
			return err
		}
		m = map[string]float64{}
		for _, x := range res.Metrics {
			m[x.Name] = x.Value
		}
		return nil
	}))
	return m
}

// Span is what one step cost: the metrics' growth, the long tasks in it
// and the DOM changes.
type Span struct {
	Name                        string
	Wall                        time.Duration
	ScriptMs, LayoutMs, StyleMs float64
	TaskMs                      float64
	Layouts, Styles             int
	Long                        []int // ms of each long task
	Mutations                   int
}

func (s Span) String() string {
	return fmt.Sprintf("%-28s wall %4dms task %6.1fms script %6.1fms layout %5.1fms(%d) style %5.1fms(%d) long %v mut %d",
		s.Name, s.Wall.Milliseconds(), s.TaskMs, s.ScriptMs, s.LayoutMs, s.Layouts, s.StyleMs, s.Styles, s.Long, s.Mutations)
}

// Measure runs step and returns what it cost. The step should wait for
// its own end (a WaitJS); the span then waits two frames so the paint is
// counted.
func (r *Perf) Measure(name string, step func()) Span {
	r.p.T.Helper()
	settle := func() {
		r.p.T.Helper()
		ctx, cancel := context.WithTimeout(r.p.Ctx, wait)
		defer cancel()
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`new Promise((ok) => requestAnimationFrame(() => requestAnimationFrame(() => setTimeout(() => ok(true)))))`, chromedp.EvalAwaitPromise)); err != nil {
			r.p.T.Fatalf("settle: %v", err)
		}
	}
	settle()
	type mark struct {
		Now  float64
		Long int
		Mut  int
	}
	read := func() mark {
		return Eval[mark](r.p, `({Now: performance.now(), Long: window.__tcLong.length, Mut: window.__tcMut})`)
	}
	m0, k0, t0 := r.Metrics(), read(), time.Now()
	if want := os.Getenv("WEB_PERF_PROFILE"); want != "" && strings.Contains(name, want) {
		r.p.T.Logf("%s: %s", name, r.Profile(step))
	} else {
		step()
	}
	settle()
	m1, k1 := r.Metrics(), read()
	long := Eval[[][2]int](r.p, fmt.Sprintf(`window.__tcLong.slice(%d)`, k0.Long))
	s := Span{Name: name, Wall: time.Since(t0), Mutations: k1.Mut - k0.Mut}
	s.ScriptMs = (m1["ScriptDuration"] - m0["ScriptDuration"]) * 1000
	s.LayoutMs = (m1["LayoutDuration"] - m0["LayoutDuration"]) * 1000
	s.StyleMs = (m1["RecalcStyleDuration"] - m0["RecalcStyleDuration"]) * 1000
	s.TaskMs = (m1["TaskDuration"] - m0["TaskDuration"]) * 1000
	s.Layouts = int(m1["LayoutCount"] - m0["LayoutCount"])
	s.Styles = int(m1["RecalcStyleCount"] - m0["RecalcStyleCount"])
	for _, l := range long {
		s.Long = append(s.Long, l[1])
	}
	return s
}

// MaxLong is the longest task in the span (0 if none over 50 ms).
func (s Span) MaxLong() int {
	m := 0
	for _, l := range s.Long {
		m = max(m, l)
	}
	return m
}

// frameStats sums frames per kind ("t" and topic), largest first.
func frameStats(fs []wsFrame) string {
	type agg struct {
		n, bytes int
	}
	by := map[string]*agg{}
	for _, f := range fs {
		k := f.head
		if i := strings.Index(k, `"seq"`); i > 0 {
			k = k[:i]
		}
		if len(k) > 40 {
			k = k[:40]
		}
		a := by[k]
		if a == nil {
			a = &agg{}
			by[k] = a
		}
		a.n++
		a.bytes += f.size
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return by[keys[i]].bytes > by[keys[j]].bytes })
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "  %5d × %7d B  %s\n", by[k].n, by[k].bytes, k)
	}
	return b.String()
}

func sumBytes(fs []wsFrame) int {
	n := 0
	for _, f := range fs {
		n += f.size
	}
	return n
}

// Profile runs step under the V8 sampling profiler and returns the
// functions that took most time, self and with their callees.
func (r *Perf) Profile(step func()) string {
	r.p.T.Helper()
	r.p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		if _, err := cdp.Call(ctx, tg, profiler.Enable, cdp.Empty{}); err != nil {
			return err
		}
		if _, err := cdp.Call(ctx, tg, profiler.SetSamplingInterval, profiler.SetSamplingIntervalParams{Interval: 200}); err != nil {
			return err
		}
		_, err := cdp.Call(ctx, tg, profiler.Start, cdp.Empty{})
		return err
	}))
	step()
	var prof *profiler.Profile
	r.p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		res, err := cdp.Call(ctx, tg, profiler.Stop, cdp.Empty{})
		prof = res.Profile
		return err
	}))
	if prof == nil || len(prof.Samples) == 0 {
		return "no samples"
	}
	dt := map[int64]float64{}
	for i, id := range prof.Samples {
		if i+1 < len(prof.TimeDeltas) {
			dt[id] += float64(prof.TimeDeltas[i+1]) / 1000
		}
	}
	byID := map[int64]*profiler.ProfileNode{}
	for _, n := range prof.Nodes {
		byID[n.ID] = n
	}
	name := func(n *profiler.ProfileNode) string {
		f := n.CallFrame
		u := f.URL
		if i := strings.LastIndex(u, "/"); i >= 0 {
			u = u[i+1:]
		}
		fn := f.FunctionName
		if fn == "" {
			fn = "(anon)"
		}
		return fmt.Sprintf("%s %s:%d:%d", fn, u, f.LineNumber+1, f.ColumnNumber+1)
	}
	self := map[string]float64{}
	total := map[string]float64{}
	var walk func(id int64, onStack map[string]bool) float64
	walk = func(id int64, onStack map[string]bool) float64 {
		n := byID[id]
		k := name(n)
		t := dt[id]
		self[k] += dt[id]
		fresh := !onStack[k]
		if fresh {
			onStack[k] = true
		}
		for _, c := range n.Children {
			t += walk(c, onStack)
		}
		if fresh {
			total[k] += t
			delete(onStack, k)
		}
		return t
	}
	all := walk(prof.Nodes[0].ID, map[string]bool{})
	top := func(m map[string]float64, n int) string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return m[keys[i]] > m[keys[j]] })
		var b strings.Builder
		for i, k := range keys {
			if i >= n {
				break
			}
			fmt.Fprintf(&b, "  %7.1fms  %s\n", m[k], k)
		}
		return b.String()
	}
	return fmt.Sprintf("profile %.0fms\nself:\n%stotal:\n%s", all, top(self, 25), top(total, 40))
}
