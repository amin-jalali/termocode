//go:build browser

package web

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// drain reads (and drops) the page's messages for d; requests stay
// unanswered.
func drain(c *HubConn, d time.Duration) int {
	n := 0
	end := time.After(d)
	for {
		select {
		case <-c.in:
			n++
		case <-end:
			return n
		}
	}
}

// autoReply answers every request whose op is in data with its value
// (and drops the rest) until the returned stop is called.
func autoReply(t *testing.T, c *HubConn, data map[string]any) (stop func()) {
	done := make(chan struct{})
	quit := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case m := <-c.in:
				if r, ok := m.(*webproto.Req); ok {
					if v, ok := data[r.Op]; ok {
						_ = c.send(&webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(v)})
					}
				}
			case <-quit:
				return
			}
		}
	}()
	return func() { close(quit); <-done }
}

// bigTree is the tree topic with a root of n folders and n files, and
// the first folder listed with n files.
func bigTree(n int) map[string]any {
	var root, sub []any
	for i := range n {
		root = append(root, entry(fmt.Sprintf("dir%03d", i), true))
	}
	for i := range n {
		root = append(root, entry(fmt.Sprintf("file%03d.go", i), false))
	}
	for i := range 1500 {
		sub = append(sub, entry(fmt.Sprintf("inner%03d.ts", i), false))
	}
	return map[string]any{"roots": []any{map[string]any{"path": "", "label": "ws-root"}}, "hidden": false,
		"dirs": map[string]any{"": map[string]any{"entries": root}, "dir000": map[string]any{"entries": sub}}}
}

func manyContainers(n int) webproto.ContainersList {
	l := webproto.ContainersList{Engine: "docker", Context: "context default"}
	for i := range n {
		l.Containers = append(l.Containers, webproto.ContainerItem{ID: fmt.Sprintf("%012x", 0xabc000+i), Name: fmt.Sprintf("svc-%d", i),
			Image: "ghcr.io/acme/svc:1", State: "running", Status: "Up 2 hours", Project: fmt.Sprintf("proj%d", i%3)})
	}
	return l
}

func manyTables(n int) webproto.DBTables {
	t := webproto.DBTables{Conn: "local", Schema: "public"}
	for i := range n {
		t.Tables = append(t.Tables, webproto.DBTable{Name: fmt.Sprintf("table_%02d", i)})
	}
	return t
}

// TestPerfReport walks the workbench (and the phone) and logs what each
// step costs. It runs only with WEB_PERF_REPORT=1 and holds no budget
// (the TestPerfGuard tests do).
func TestPerfReport(t *testing.T) {
	if os.Getenv("WEB_PERF_REPORT") == "" {
		t.Skip("set WEB_PERF_REPORT=1")
	}
	for _, vp := range []Viewport{Desktop, Phone} {
		t.Run(vp.Name, func(t *testing.T) { perfWalk(t, vp) })
	}
}

func perfWalk(t *testing.T, vp Viewport) {
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	r := StartPerf(p)
	var calls *Calls
	if os.Getenv("WEB_PERF_CALLS") != "" {
		calls = StartCalls(p)
	}
	if v, err := strconv.ParseFloat(os.Getenv("WEB_PERF_CPU"), 64); err == nil && v > 1 {
		r.Throttle(v)
	}
	if v, err := strconv.ParseFloat(os.Getenv("WEB_PERF_NET"), 64); err == nil && v > 0 {
		r.Net(v, 5)
	}
	add := func(sp Span) { t.Log(sp) }

	t0 := time.Now()
	p.Open()
	t.Logf("open → connect screen: %s", time.Since(t0))
	type load struct {
		DCL, Load, FP, FCP float64
		Req, Bytes, Body   int
		Res                []string
	}
	ld := Eval[load](p, `(() => {
		const n = performance.getEntriesByType("navigation")[0];
		const p = Object.fromEntries(performance.getEntriesByType("paint").map((e) => [e.name, e.startTime]));
		const rs = performance.getEntriesByType("resource");
		return {DCL: n.domContentLoadedEventEnd, Load: n.loadEventEnd, FP: p["first-paint"] ?? 0, FCP: p["first-contentful-paint"] ?? 0,
			Req: rs.length + 1, Bytes: rs.reduce((a, e) => a + e.transferSize, n.transferSize), Body: rs.reduce((a, e) => a + e.decodedBodySize, n.decodedBodySize),
			Res: rs.map((e) => e.name.replace(/^.*\/assets\//, "") + " " + Math.round(e.duration) + "ms " + e.decodedBodySize)};
	})()`)
	t.Logf("load: DCL %.0fms load %.0fms FP %.0fms FCP %.0fms, %d requests, %d B transferred, %d B decoded", ld.DCL, ld.Load, ld.FP, ld.FCP, ld.Req, ld.Bytes, ld.Body)
	for _, x := range ld.Res {
		t.Logf("  %s", x)
	}
	n, urls := r.Fetched(time.Time{})
	t.Logf("wire bytes to the connect screen: %d (%v)", n, urls)
	var c *HubConn
	add(r.Measure("login → workbench", func() { c = p.LoginHere("c-perf") }))
	t.Logf("DOM after login: %s", Eval[string](p, `(() => { const all = document.querySelectorAll("*"); const by = {}; for (const e of all) { const k = e.tagName + "." + (typeof e.className === "string" ? e.className.split(" ")[0] : ""); by[k] = (by[k] || 0) + 1; } return all.length + " elements; " + Object.entries(by).sort((a, b) => b[1] - a[1]).slice(0, 15).map(([k, n]) => k + " " + n).join(", "); })()`))
	if vp.W >= 1200 {
		add(r.Measure("big tree (400 rows)", func() {
			c.Send(t, Snap("tree", 1, bigTree(200)))
			p.WaitJS(`document.querySelectorAll('[data-view="files"] .tree-row').length > 50`)
		}))
		if os.Getenv("WEB_PERF_CV") != "" {
			Eval[bool](p, `(() => { const s = new CSSStyleSheet(); s.replaceSync(".wb-side .tree-line{content-visibility:auto;contain-intrinsic-size:auto 22px}"); document.adoptedStyleSheets = [...document.adoptedStyleSheets, s]; return true; })()`)
		}
		add(r.Measure("open a folder of 1500", func() {
			p.TapJS(treeRow("dir000"))
			p.WaitJS(treeRow("dir000/inner1499.ts"))
		}))
		t.Logf("tree rows in DOM: %d", Eval[int](p, `document.querySelectorAll('[data-view="files"] .tree-row').length`))
		drain(c, 100*time.Millisecond)
		add(r.Measure("tree delta ×10 (other folder)", func() {
			for i := range 10 {
				c.Send(t, Delta("tree", uint64(2+i), dirDelta("dir001", false, entry(fmt.Sprintf("x%d.go", i), false))))
			}
			time.Sleep(300 * time.Millisecond)
		}))
		add(r.Measure("bufs delta ×10", func() {
			for i := range 10 {
				c.Send(t, Delta("bufs", uint64(2+i), map[string]any{"bufs": []any{map[string]any{"buf": 1, "path": fmt.Sprintf("a%d.go", i), "dirty": i%2 == 0, "active": true}}}))
			}
			time.Sleep(300 * time.Millisecond)
		}))
	}
	add(r.Measure("chat snap (80 msgs)", func() {
		var msgs []map[string]any
		for i := range 40 {
			msgs = append(msgs, userMsg(2*i, "question "+fmt.Sprint(i)), aiMsg(2*i+1, mdPara(strings.Repeat("answer words ", 40))))
		}
		c.Send(t, Snap("chat", 1, chatWith(false, msgs...)))
		p.WaitJS(chatView + `?.querySelectorAll(".msg").length >= 40`)
	}))
	if vp.W >= 1200 {
		add(r.Measure("type 20 keys in composer", func() {
			p.TapJS(composer + `.querySelector("textarea")`)
			p.Key("abcdefghijklmnopqrst")
			p.WaitJS(composer + `.querySelector("textarea").value === "abcdefghijklmnopqrst"`)
		}))
		lines := make([]string, 2000)
		for i := range lines {
			lines[i] = fmt.Sprintf("func f%d() int { return %d } // line", i, i)
		}
		drain(c, 200*time.Millisecond)
		before := time.Now()
		add(r.Measure("open file (2000 lines)", func() {
			openBufAs(t, p, c, 2, "big.go", nil, lines...)
			p.WaitJS(deskDoc + `?.querySelector(".view-lines .view-line")`)
		}))
		n, urls := r.Fetched(before)
		t.Logf("wire bytes of the first file open: %d (%v)", n, urls)
		add(r.Measure("type 20 keys in editor", func() {
			deskType(p, true, "abcdefghijklmnopqrst")
			p.WaitJS(deskDoc + `.querySelector(".view-lines").textContent.includes("abcdefghijklmnopqrst")`)
		}))
		add(r.Measure("open 2nd file (2000 lines)", func() {
			openBufAs(t, p, c, 3, "big2.go", nil, lines...)
			p.WaitJS(`document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent === "big2.go"`)
			p.WaitJS(deskDoc + `?.querySelector(".view-lines .view-line")`)
		}))
		drain(c, 200*time.Millisecond)
		add(r.Measure("back to the 1st tab", func() {
			Eval[bool](p, `(location.hash = "#/files/big.go", true)`)
			p.WaitJS(`document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent === "big.go"`)
		}))
		drain(c, 200*time.Millisecond)
		stop := autoReply(t, c, map[string]any{
			webproto.OpContainersList:    manyContainers(30),
			webproto.OpContainersHost:    ctrHost(),
			webproto.OpContainersServers: webproto.ServersList{Engine: "docker", Hosts: []webproto.ServerHost{{Name: "default", Local: true, Current: true}}},
			webproto.OpDBConns:           dbConns(),
			webproto.OpDBTables:          manyTables(36),
		})
		add(r.Measure("view: scm", func() { p.GoTab("git") }))
		add(r.Measure("view: containers (30)", func() {
			Eval[bool](p, `(location.hash = "#/more/containers", true)`)
			p.WaitJS(`document.querySelectorAll('.wb-side [data-view="containers"] .tr[data-id]').length === 30`)
		}))
		add(r.Measure("view: db + 36 tables", func() {
			Eval[bool](p, `(location.hash = "#/more/db", true)`)
			const side = `document.querySelector('.wb-side [data-view="db"]')`
			p.WaitJS(side + `?.querySelectorAll(".tr[data-conn]").length === 3`)
			p.TapJS(side + `.querySelector('.tr[data-conn="local"]')`)
			p.WaitJS(side + `.querySelectorAll(".tr[data-table]").length === 36`)
		}))
		add(r.Measure("view: files", func() { p.GoTab("files") }))
		add(r.Measure("view: chat", func() { p.GoTab("chat") }))
		add(r.Measure("view: containers again", func() {
			Eval[bool](p, `(location.hash = "#/more/containers", true)`)
			p.WaitJS(`document.querySelectorAll('.wb-side [data-view="containers"] .tr[data-id]').length === 30`)
		}))
		add(r.Measure("view: files again", func() { p.GoTab("files") }))
		stop()
		drain(c, 300*time.Millisecond)
		add(r.Measure("status delta ×20", func() {
			for i := range 20 {
				c.Send(t, Delta("status", uint64(2+i), statusData("main", i)))
			}
			time.Sleep(300 * time.Millisecond)
		}))
		if calls != nil {
			marks := map[string]string{"App": "shell layout-desktop", "Workbench": "wb-center", "chat msg": "msg-text", "tree row": treeRowFn}
			seq := uint64(22)
			countAll := func(name string, step func()) {
				// one pass per marker would repeat the step: count all at once
				calls.take()
				step()
				res := calls.take()
				var parts []string
				for k, m := range marks {
					parts = append(parts, fmt.Sprintf("%s %d", k, calls.countIn(res, m)))
				}
				t.Logf("renders during %s: %v", name, parts)
			}
			countAll("status delta ×20", func() {
				for i := range 20 {
					seq++
					c.Send(t, Delta("status", seq, statusData("main", i)))
				}
				time.Sleep(300 * time.Millisecond)
			})
			countAll("20 keys in the editor", func() {
				deskType(p, true, "ABCDEFGHIJKLMNOPQRST")
				p.WaitJS(deskDoc + `.querySelector(".view-lines").textContent.includes("ABCDEFGHIJKLMNOPQRST")`)
				time.Sleep(300 * time.Millisecond)
			})
			countAll("20 keys in the composer", func() {
				p.TapJS(composer + `.querySelector("textarea")`)
				p.Key("klmnopqrstuvwxyzabcd")
				time.Sleep(300 * time.Millisecond)
			})
			countAll("idle 3s", func() { time.Sleep(3 * time.Second) })
			countAll("view: scm and back", func() {
				p.GoTab("git")
				p.GoTab("files")
			})
			countAll("chat append ×20", func() {
				c.Send(t, Snap("chat", 1, chatWith(true, userMsg(0, "q"), aiMsg(1, mdPara("a")))))
				p.WaitJS(chatView + `?.querySelectorAll(".msg").length === 2`)
				for i := range 20 {
					c.Send(t, Delta("chat", uint64(2+i), map[string]any{"op": "append", "i": 1, "text": " more"}))
				}
				time.Sleep(300 * time.Millisecond)
			})
		}
	}
	drain(c, 200*time.Millisecond)
	since := time.Now()
	mut := Eval[int](p, `window.__tcMut`)
	time.Sleep(5 * time.Second)
	in, out := r.Frames(since)
	t.Logf("idle 5s: %d frames in (%d B), %d out (%d B), %d DOM changes", len(in), sumBytes(in), len(out), sumBytes(out), Eval[int](p, `window.__tcMut`)-mut)
	t.Logf("idle out:\n%s", frameStats(out))
	allIn, allOut := r.Frames(time.Time{})
	t.Logf("whole walk: %d frames in (%d B), %d out (%d B)\nin:\n%sout:\n%s", len(allIn), sumBytes(allIn), len(allOut), sumBytes(allOut), frameStats(allIn), frameStats(allOut))
}
