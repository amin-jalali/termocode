package app

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// HC-13: performance guards of the HTTP client's Go paths (allocation
// counts, like perf_guard_test.go) and the benchmarks behind the plan's
// budgets. The nvim side (redraw ≤ 5 ms with 5 MB loaded, lens redraw)
// is measured in internal/testkit (TestHTTPPerf*).

const (
	maxAllocsHTTPStatusChip = 12 // the status chip on every frame after a send (measured 8)
	maxAllocsHTTPStatusIdle = 0  // no send yet: nothing
)

func TestHTTPStatusChipAllocs(t *testing.T) {
	m := Model{w: 120}
	if n := testing.AllocsPerRun(100, func() { _ = m.httpStatusTexts() }); n > maxAllocsHTTPStatusIdle {
		t.Errorf("idle status chip: %.0f allocs (max %d)", n, maxAllocsHTTPStatusIdle)
	}
	m.http = newHTTPState()
	m.http.last = &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET"}},
		resp: &httpclient.Response{Status: 200, StatusText: "OK", Elapsed: 118 * time.Millisecond}}
	if n := testing.AllocsPerRun(100, func() { _ = m.httpStatusTexts() }); n > maxAllocsHTTPStatusChip {
		t.Errorf("status chip: %.0f allocs (max %d)", n, maxAllocsHTTPStatusChip)
	}
}

func big5MB() []byte {
	var sb strings.Builder
	sb.WriteString(`{"items":[`)
	for i := 0; sb.Len() < 5<<20; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"id":%d,"name":"user %d","tags":["a","b"]}`, i, i)
	}
	sb.WriteString("]}")
	return []byte(sb.String())
}

// The page for a 5 MB JSON answer: pretty print + split into lines.
// Budget: well under the 150 ms to the first batch on screen.
func TestHTTPBuildPage5MBBudget(t *testing.T) {
	body := big5MB()
	run := &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: "http://x/"}},
		resp: &httpclient.Response{Status: 200, StatusText: "OK", Body: body, Size: int64(len(body)), IsJSON: true, ContentType: "application/json"}}
	// Best of 5 builds (one scheduler hiccup must not decide), judged
	// against 150 ms or, on a slow or busy machine, 4 times the cost of the
	// unavoidable part (the JSON indent alone, best of 3, measured here
	// now; the build costs about 2 times that). A real slowdown of the
	// build still fails on a fast and on a slow machine.
	best := func(n int, f func()) time.Duration {
		d := time.Duration(1<<63 - 1)
		for i := 0; i < n; i++ {
			start := time.Now()
			f()
			if took := time.Since(start); took < d {
				d = took
			}
		}
		return d
	}
	var p httpPage
	d := best(5, func() { p = buildHTTPPage(run, httpViewBody, false, false) })
	base := best(3, func() { httpclient.PrettyJSON(body) })
	budget := max(150*time.Millisecond, 4*base)
	if len(p.lines) < 100000 {
		t.Fatalf("lines %d", len(p.lines))
	}
	if p.head["syntax"] != "" {
		t.Error("no json syntax above 1 MB")
	}
	if d > budget && !raceEnabled {
		t.Errorf("building the 5 MB page took %v (budget %v: 150 ms or 4 x the %v indent)", d, budget, base)
	}
}

func BenchmarkHTTPPageBuild5MB(b *testing.B) {
	body := big5MB()
	run := &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: "http://x/"}},
		resp: &httpclient.Response{Status: 200, StatusText: "OK", Body: body, Size: int64(len(body)), IsJSON: true}}
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildHTTPPage(run, httpViewBody, false, false)
	}
}

// The lens table of a 2 000-request file (the debounce re-parse).
func BenchmarkHTTPLensRows2000(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&sb, "### r%d\n# @name r%d\nGET {{host}}/users/%d\nAuthorization: Bearer {{token}}\n\n", i, i, i)
	}
	src := sb.String()
	lines := strings.Split(src, "\n")
	known := func(n string) bool { return n == "host" }
	lens := func(r *httpclient.Request) [][]string {
		return [][]string{{" ▸ send Ctrl+Alt+R ", "TermocodeHttpLens"}}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		f := httpclient.Parse(src)
		httpBufRows(f, lines, known, lens)
	}
}

// Parse + resolve + env load of the send path (budget 50 ms to the wire).
func TestHTTPSendPathBudget(t *testing.T) {
	withConfigDir(t)
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "###\n# @name r%d\nPOST {{host}}/u/%d\nContent-Type: application/json\n\n{\"id\": \"{{$guid}}\"}\n\n", i, i)
	}
	src := sb.String()
	start := time.Now()
	f := httpclient.Parse(src)
	rv := httpResolver("", []httpclient.Var{{Name: "host", Value: "http://localhost"}}, "")
	rv.Resolve(*f.AtLine(500))
	if d := time.Since(start); d > 50*time.Millisecond && !raceEnabled {
		t.Errorf("parse + resolve took %v (budget 50 ms)", d)
	}
}

// httpTextLinesRef is the old split (ReplaceAll, ReplaceAll, Split).
func httpTextLinesRef(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\x00", " ")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

func TestAppendTextLinesMatchesSplit(t *testing.T) {
	for _, s := range []string{"", "\n", "\n\n", "a", "a\n", "a\nb", "a\r\nb\r\n", "a\rb", "a\r\r\nb", "x\x00y\n", "\r\n", "a\n\nb\n\n"} {
		got := appendTextLines([]string{"head"}, []byte(s))
		want := append([]string{"head"}, httpTextLinesRef(s)...)
		if !slices.Equal(got, want) {
			t.Errorf("%q: %q, want %q", s, got, want)
		}
	}
}

// PERF-8: the 5 MB page is built with few big copies (body text, pretty
// text, line list once).
func TestHTTPBuildPage5MBBytes(t *testing.T) {
	body := big5MB()
	run := &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: "http://x/"}},
		resp: &httpclient.Response{Status: 200, StatusText: "OK", Body: body, Size: int64(len(body)), IsJSON: true, ContentType: "application/json"}}
	var ms0, ms1 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	p := buildHTTPPage(run, httpViewBody, false, false)
	runtime.ReadMemStats(&ms1)
	if len(p.lines) < 100000 {
		t.Fatal(len(p.lines))
	}
	t.Logf("%d MB", (ms1.TotalAlloc-ms0.TotalAlloc)>>20)
	if got := ms1.TotalAlloc - ms0.TotalAlloc; got > 12*uint64(len(body)) && !raceEnabled {
		t.Errorf("built the page with %d MB allocated (max %d MB)", got>>20, 12*len(body)>>20)
	}
}
