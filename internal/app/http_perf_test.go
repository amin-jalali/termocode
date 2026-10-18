package app

import (
	"fmt"
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
	start := time.Now()
	p := buildHTTPPage(run, httpViewBody, false, false)
	d := time.Since(start)
	if len(p.lines) < 100000 {
		t.Fatalf("lines %d", len(p.lines))
	}
	if p.head["syntax"] != "" {
		t.Error("no json syntax above 1 MB")
	}
	if d > 150*time.Millisecond && !raceEnabled {
		t.Errorf("building the 5 MB page took %v (budget 150 ms)", d)
	}
}

func BenchmarkHTTPPageBuild5MB(b *testing.B) {
	body := big5MB()
	run := &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: "http://x/"}},
		resp: &httpclient.Response{Status: 200, StatusText: "OK", Body: body, Size: int64(len(body)), IsJSON: true}}
	b.SetBytes(int64(len(body)))
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
