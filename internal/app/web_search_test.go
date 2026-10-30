package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/search"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// webTestSearchFrames waits until the answer of request id arrives and
// returns the batches before it and the answer.
func webTestSearchFrames(t *testing.T, c *webConn, id uint64) ([]webproto.SearchBatch, map[string]any) {
	t.Helper()
	var batches []webproto.SearchBatch
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, f := range webTestNow(t, c) {
			switch {
			case f["t"] == "ev" && f["kind"] == webproto.EventSearchBatch:
				var b webproto.SearchBatch
				webTestData(t, f, &b)
				if b.ID == id {
					batches = append(batches, b)
				}
			case f["id"] == float64(id):
				return batches, f
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("request %d gave no answer", id)
	return nil, nil
}

// webTestSearchStart sends search.run (or another search op) and returns
// its request id; the answer comes later from the search goroutine.
func webTestSearchStart(t *testing.T, m *Model, c *webConn, op, args string) uint64 {
	t.Helper()
	a, err := webproto.DecodeArgs(op, []byte(args))
	if err != nil {
		t.Fatalf("%s %s: %v", op, args, err)
	}
	c.lastID++
	call := &webCall{ctx: c.ctx, conn: c, id: c.lastID, op: op, args: a}
	c.inflight.Add(1)
	m.webRunOp(m.web.ops[op], call)
	return call.id
}

// webTestFakeStream swaps the search stream for one the test feeds; it
// returns the options each search was asked with.
func webTestFakeStream(t *testing.T, feed func(ctx context.Context, ch chan<- search.Batch)) *[]search.Options {
	t.Helper()
	var asked []search.Options
	var mu sync.Mutex
	old := webSearchStream
	webSearchStream = func(ctx context.Context, dirs []string, q string, o search.Options) <-chan search.Batch {
		mu.Lock()
		asked = append(asked, o)
		mu.Unlock()
		ch := make(chan search.Batch)
		go func() {
			defer close(ch)
			feed(ctx, ch)
		}()
		return ch
	}
	t.Cleanup(func() { webSearchStream = old })
	return &asked
}

// Spec (WEB-24): search.run streams real hits from the workspace as
// batches with the request's id, paths relative, one context line, the
// matches marked in UTF-16; the summary answers last. The query is
// audited, cut short.
func TestSearchStreamsHits(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir := t.TempDir()
	webTestWrite(t, dir, "src/a.go", "package a\n// ✦ find the needle here\nfunc A() {}\n")
	webTestWrite(t, dir, "b.txt", "no\nneedle\n")
	webTestWrite(t, dir, "c.txt", "nothing\n")
	m.ws = workspace.Workspace{Root: dir}
	c := webTestSub(t, &m)

	id := webTestSearchStart(t, &m, c, webproto.OpSearchRun, `{"query":"needle"}`)
	batches, res := webTestSearchFrames(t, c, id)
	var sum webproto.SearchSummary
	webTestData(t, res, &sum)
	hits := map[string]webproto.SearchHit{}
	for _, b := range batches {
		for _, h := range b.Results {
			hits[h.Path] = h
		}
	}
	if sum.Files != 2 || sum.Matches != 2 || sum.Truncated || sum.Stopped != "" || len(hits) != 2 {
		t.Fatalf("summary %+v hits %+v", sum, hits)
	}
	a := hits["src/a.go"]
	if a.Line != 2 || a.Text != "// ✦ find the needle here" || len(a.Marks) != 1 || a.Marks[0] != (webproto.SearchMark{S: 14, E: 20}) {
		t.Errorf("hit %+v", a)
	}
	if len(a.Before) != 1 || a.Before[0] != "package a" || len(a.After) != 1 || a.After[0] != "func A() {}" {
		t.Errorf("context %q / %q", a.Before, a.After)
	}
	if got := webTestAudit(&m); !strings.Contains(got, "search.run  needle  by=web:phone") {
		t.Errorf("audit:\n%s", got)
	}
	// A bad regex is the user's mistake.
	id = webTestSearchStart(t, &m, c, webproto.OpSearchRun, `{"query":"(","regex":true}`)
	if _, res := webTestSearchFrames(t, c, id); webTestErr(res) != "bad_request" {
		t.Errorf("bad regex: %v", res)
	}
}

// Spec (WEB-24): one search per connection: a new one stops the old
// (which answers stopped "replaced", exactly once), search.stop stops the
// running one, and a search past SearchTimeout stops with "timeout".
func TestSearchStreamsAndCancels(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	m.ws = workspace.Workspace{Root: t.TempDir()}
	webTestFakeStream(t, func(ctx context.Context, ch chan<- search.Batch) {
		select { // one hit, then wait until stopped
		case ch <- search.Batch{Results: []search.Result{{Path: filepath.Join(m.ws.Root, "a.go"), Line: 1, Col: 1, Preview: "x"}}}:
		case <-ctx.Done():
			return
		}
		<-ctx.Done()
	})
	c := webTestSub(t, &m)
	first := webTestSearchStart(t, &m, c, webproto.OpSearchRun, `{"query":"x"}`)
	second := webTestSearchStart(t, &m, c, webproto.OpSearchRun, `{"query":"y"}`)
	_, res := webTestSearchFrames(t, c, first)
	var sum webproto.SearchSummary
	webTestData(t, res, &sum)
	if sum.Stopped != "replaced" {
		t.Fatalf("the replaced search: %+v", sum)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpSearchStop, `{}`); f["t"] != "res" {
		t.Fatalf("stop: %v", f)
	}
	_, res = webTestSearchFrames(t, c, second)
	sum = webproto.SearchSummary{}
	webTestData(t, res, &sum)
	if sum.Stopped != "stopped" {
		t.Fatalf("the stopped search: %+v", sum)
	}
	if n := webTestAnswers(t, c, first, second); n != 0 {
		t.Fatalf("%d more answers for done searches", n)
	}

	old := webSearchTimeout
	webSearchTimeout = 50 * time.Millisecond
	t.Cleanup(func() { webSearchTimeout = old })
	id := webTestSearchStart(t, &m, c, webproto.OpSearchRun, `{"query":"z"}`)
	_, res = webTestSearchFrames(t, c, id)
	sum = webproto.SearchSummary{}
	webTestData(t, res, &sum)
	if sum.Stopped != "timeout" || sum.Matches != 1 {
		t.Fatalf("the slow search: %+v", sum)
	}
}

// webTestAnswers counts answers to ids that arrive within a short while.
func webTestAnswers(t *testing.T, c *webConn, ids ...uint64) int {
	t.Helper()
	n := 0
	end := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(end) {
		for _, f := range webTestNow(t, c) {
			for _, id := range ids {
				if f["id"] == float64(id) {
					n++
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return n
}

// Spec (WEB-24): at most max (≤ 500) hits go out, in batches of at most
// MaxSearchBatch; texts are cut at MaxSearchText bytes on a rune edge; all
// texts of one search stay under MaxSearchBytes ("limit").
func TestSearchCaps(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	m.ws = workspace.Workspace{Root: t.TempDir()}
	long := strings.Repeat("é", 300) // 600 bytes
	var results []search.Result
	for i := 0; i < 1200; i++ {
		results = append(results, search.Result{Path: filepath.Join(m.ws.Root, "f"+itoa(i/10)+".txt"), Line: i%10 + 1, Col: 1, Preview: long,
			Matches: []search.MatchRange{{Start: 0, End: 2}, {Start: 590, End: 600}}})
	}
	asked := webTestFakeStream(t, func(ctx context.Context, ch chan<- search.Batch) {
		for i := 0; i < len(results); i += 200 {
			select {
			case ch <- search.Batch{Results: results[i : i+200]}:
			case <-ctx.Done():
				return
			}
		}
		ch <- search.Batch{Done: true, Summary: search.Summary{Shown: 1200, Total: 1300, Files: 120}}
	})
	c := webTestSub(t, &m)
	id := webTestSearchStart(t, &m, c, webproto.OpSearchRun, `{"query":"é","max":250,"case":true,"word":true,"include":"*.txt"}`)
	batches, res := webTestSearchFrames(t, c, id)
	var sum webproto.SearchSummary
	webTestData(t, res, &sum)
	n := 0
	for _, b := range batches {
		if len(b.Results) > webproto.MaxSearchBatch {
			t.Fatalf("batch of %d", len(b.Results))
		}
		for _, h := range b.Results {
			n++
			if len(h.Text) > webproto.MaxSearchText || !strings.HasPrefix(h.Text, "é") || strings.ContainsRune(h.Text, '�') {
				t.Fatalf("text of %d bytes: %q", len(h.Text), h.Text[len(h.Text)-4:])
			}
			if len(h.Marks) != 1 || h.Marks[0] != (webproto.SearchMark{S: 0, E: 1}) {
				t.Fatalf("marks %+v (one past the cut must go)", h.Marks)
			}
		}
	}
	if n != 250 || sum.Matches != 250 || !sum.Truncated {
		t.Fatalf("%d hits, summary %+v", n, sum)
	}
	o := (*asked)[0]
	if o.MaxResults != 250 || !o.CaseSensitive || !o.WholeWord || o.Regex || o.Context != webproto.SearchContext || len(o.Include) != 1 {
		t.Errorf("options %+v", o)
	}

	// The byte cap (small here, so the test stays fast).
	oldMax := webSearchMaxBytes
	webSearchMaxBytes = 100 << 10
	t.Cleanup(func() { webSearchMaxBytes = oldMax })
	for i := range results {
		results[i].Before = []search.ContextLine{{Line: 1, Text: long}}
		results[i].After = []search.ContextLine{{Line: 3, Text: long}}
	}
	id = webTestSearchStart(t, &m, c, webproto.OpSearchRun, `{"query":"é","max":500}`)
	batches, res = webTestSearchFrames(t, c, id)
	sum = webproto.SearchSummary{}
	webTestData(t, res, &sum)
	size := 0
	for _, b := range batches {
		for _, h := range b.Results {
			size += len(h.Text) + len(strings.Join(h.Before, "")) + len(strings.Join(h.After, ""))
		}
	}
	if sum.Stopped != "limit" || !sum.Truncated || size > webSearchMaxBytes || sum.Matches >= 500 || sum.Matches == 0 {
		t.Fatalf("byte cap: %d bytes, summary %+v", size, sum)
	}
}

// Spec (WEB-24): a search stays inside the workspace: globs that leave it
// are refused by the args rules, a linked folder that points out is not
// followed, and every path that goes out is relative.
func TestSearchGlobsInsideRoot(t *testing.T) {
	for _, args := range []string{`{"query":"a","include":"../**"}`, `{"query":"a","exclude":"/etc"}`} {
		if _, err := webproto.DecodeArgs(webproto.OpSearchRun, []byte(args)); err == nil {
			t.Errorf("%s decoded", args)
		}
	}
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir := t.TempDir()
	outside := t.TempDir()
	webTestWrite(t, outside, "secret.txt", "needle outside\n")
	webTestWrite(t, dir, "in.go", "needle inside\n")
	webTestWrite(t, dir, "in.txt", "needle in text\n")
	if err := os.Symlink(outside, filepath.Join(dir, "out")); err != nil {
		t.Skip("no symlinks")
	}
	m.ws = workspace.Workspace{Root: dir}
	c := webTestSub(t, &m)
	id := webTestSearchStart(t, &m, c, webproto.OpSearchRun, `{"query":"needle","include":"*.go"}`)
	batches, _ := webTestSearchFrames(t, c, id)
	var paths []string
	for _, b := range batches {
		for _, h := range b.Results {
			paths = append(paths, h.Path)
		}
	}
	if strings.Join(paths, ",") != "in.go" {
		t.Fatalf("paths %q", paths)
	}
	id = webTestSearchStart(t, &m, c, webproto.OpSearchRun, `{"query":"needle"}`)
	batches, _ = webTestSearchFrames(t, c, id)
	for _, b := range batches {
		for _, h := range b.Results {
			if strings.HasPrefix(h.Path, "out") || filepath.IsAbs(h.Path) {
				t.Fatalf("a hit outside the workspace: %q", h.Path)
			}
		}
	}
	// search.open checks the file is in the workspace.
	if f, _ := webTestCall(t, &m, c, webproto.OpSearchOpen, `{"path":"in.go","line":1}`); f["t"] != "res" {
		t.Errorf("open: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpSearchOpen, `{"path":"out/secret.txt","line":1}`); webTestErr(f) != "not_found" {
		t.Errorf("open through a link: %v", f)
	}
}
