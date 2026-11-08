//go:build browser

package web

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/profiler"
	"github.com/chromedp/chromedp"
)

// Calls counts exactly how often the page's functions run (V8 precise
// coverage with call counts), so a test can say "this component rendered
// n times" without a hook in the page: a function is found by a piece of
// its own source text (minified names change, its strings do not).
type Calls struct {
	p *Page

	mu   sync.Mutex
	src  map[string]string // script URL → its text
	seen map[string]bool   // markers found in a script once
}

// StartCalls starts counting; call it before the page opens, so every
// function is compiled with its counter.
func StartCalls(p *Page) *Calls {
	p.T.Helper()
	yes := true
	p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		if _, err := cdp.Call(ctx, tg, profiler.Enable, cdp.Empty{}); err != nil {
			return err
		}
		_, err := cdp.Call(ctx, tg, profiler.StartPreciseCoverage, profiler.StartPreciseCoverageParams{CallCount: &yes, Detailed: &yes})
		return err
	}))
	return &Calls{p: p, src: map[string]string{}, seen: map[string]bool{}}
}

func (c *Calls) take() []*profiler.ScriptCoverage {
	c.p.T.Helper()
	var got []*profiler.ScriptCoverage
	c.p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		res, err := cdp.Call(ctx, tg, profiler.TakePreciseCoverage, cdp.Empty{})
		got = res.Result
		return err
	}))
	return got
}

func (c *Calls) source(url string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.src[url]; ok {
		return s
	}
	s := ""
	if strings.HasPrefix(url, "http") {
		if res, err := http.Get(url); err == nil {
			b, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			s = string(b)
		}
	}
	c.src[url] = s
	return s
}

// Count runs step and returns how many times the function whose own
// body holds marker (the innermost one) ran during it.
func (c *Calls) Count(marker string, step func()) int {
	c.p.T.Helper()
	c.take() // resets the counters
	step()
	n, found := 0, false
	for _, sc := range c.take() {
		if os.Getenv("WEB_PERF_CALLS_DEBUG") != "" {
			c.p.T.Logf("coverage: %s (%d functions)", sc.URL, len(sc.Functions))
		}
		src := c.source(sc.URL)
		at := strings.Index(src, marker)
		if at < 0 {
			continue
		}
		var best *profiler.FunctionCoverage
		for _, f := range sc.Functions {
			if len(f.Ranges) == 0 {
				continue
			}
			r := f.Ranges[0]
			// A function as long as a module is the script itself (the
			// one asked for did not run, so it is not in the list).
			if r.EndOffset-r.StartOffset > 20000 {
				continue
			}
			if int(r.StartOffset) <= at && at < int(r.EndOffset) && (best == nil || r.EndOffset-r.StartOffset < best.Ranges[0].EndOffset-best.Ranges[0].StartOffset) {
				best = f
			}
		}
		found = true
		if best != nil {
			n += int(best.Ranges[0].Count)
		}
	}
	// A script none of whose functions ran is left out of the counts.
	if found {
		c.seen[marker] = true
	} else if !c.seen[marker] {
		c.p.T.Fatalf("no function of the page holds %q", marker)
	}
	return n
}
