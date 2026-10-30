//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Search under More (WEB-24, web UX 5.8) against the fake hub.

// openSearch logs in and opens More › Search.
func openSearch(t *testing.T, vp Viewport) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	c := p.Login("c-search")
	p.GoTab("more")
	p.TapJS(`document.querySelector(".more-link")`)
	p.WaitJS(`location.hash === "#/more/search" && document.querySelector(".search-input")`)
	return p, c
}

func batch(id uint64, hits ...webproto.SearchHit) *webproto.Event {
	return &webproto.Event{T: webproto.KindEv, Kind: webproto.EventSearchBatch, Data: raw(webproto.SearchBatch{ID: id, Results: hits})}
}

// The hits stream in under the count line, grouped by file with the
// match marked; batches of an older search are ignored; the summary ends
// it; a hit opens the file at its line.
func TestSearchStreams(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openSearch(t, vp)
			p.TapJS(ButtonByText("Regex"))
			p.TapJS(ButtonByText("Case"))
			p.Tap(".search-input")
			p.Do(chromedp.SendKeys(".search-input", "needle"+kb.Enter))
			r := expectOp(t, c, webproto.OpSearchRun, `{"query":"needle","regex":true,"case":true}`)
			if loc := Eval[string](p, "location.hash"); loc != "#/more/search?q=needle" {
				t.Errorf("route %q", loc)
			}
			c.Send(t, batch(r.ID, webproto.SearchHit{Path: "src/a.go", Line: 2, Col: 4, Text: "// a needle <b>here</b>", Marks: []webproto.SearchMark{{S: 5, E: 11}}}))
			c.Send(t, batch(r.ID-1, webproto.SearchHit{Path: "old.go", Line: 1, Text: "stale"}))
			c.Send(t, batch(r.ID,
				webproto.SearchHit{Path: "src/a.go", Line: 9, Text: "needle again", Marks: []webproto.SearchMark{{S: 0, E: 6}}},
				webproto.SearchHit{Path: "b.txt", Line: 1, Text: "needle", Marks: []webproto.SearchMark{{S: 0, E: 6}}}))
			p.WaitJS(`document.querySelectorAll(".search-hit").length === 3`)
			if got := Eval[[]string](p, `[...document.querySelectorAll(".search-path")].map((e) => e.textContent)`); strings.Join(got, ",") != "src/a.go,b.txt" {
				t.Errorf("files %q", got)
			}
			if got := p.Text(".search-hit mark"); got != "needle" {
				t.Errorf("mark %q", got)
			}
			if Eval[int](p, `document.querySelectorAll(".search-text b").length`) != 0 {
				t.Fatal("a hit's text became HTML")
			}
			p.WaitJS(`document.querySelector(".search-count").textContent.startsWith("2 files · 3 matches")`)
			reply(t, c, r, webproto.SearchSummary{Files: 2, Matches: 3, Ms: 412})
			p.WaitJS(`document.querySelector(".search-count").textContent === "2 files · 3 matches · 0.4 s"`)

			p.TapJS(`document.querySelectorAll(".search-hit")[1]`)
			reply(t, c, expectOp(t, c, webproto.OpSearchOpen, `{"path":"src/a.go","line":9}`), nil)
			p.WaitJS(`location.hash === "#/files/src/a.go?line=9"`)
		})
	}
}

// Stop stops the running search; the caps and the empty result say so.
func TestSearchStopAndNotes(t *testing.T) {
	p, c := openSearch(t, Phone)
	p.Tap(".search-input")
	p.Do(chromedp.SendKeys(".search-input", "x"+kb.Enter))
	r := expectOp(t, c, webproto.OpSearchRun, `{"query":"x"}`)
	p.TapJS(ButtonByText("Stop"))
	reply(t, c, expectOp(t, c, webproto.OpSearchStop, `{}`), nil)
	reply(t, c, r, webproto.SearchSummary{Stopped: "stopped", Truncated: true})
	p.WaitJS(ButtonByText("Search"))

	for _, tc := range []struct {
		sum  webproto.SearchSummary
		note string
	}{
		{webproto.SearchSummary{Matches: 500, Truncated: true}, "Stopped at 500 matches. Narrow the search."},
		{webproto.SearchSummary{Truncated: true, Stopped: "timeout"}, "Stopped after 10 seconds. Narrow the search."},
		{webproto.SearchSummary{}, "No matches."},
	} {
		p.TapJS(ButtonByText("Search"))
		r := expectOp(t, c, webproto.OpSearchRun, `{"query":"x"}`)
		reply(t, c, r, tc.sum)
		p.WaitJS(`document.querySelector(".search-note")?.textContent === "` + tc.note + `"`)
	}
	p.noOverflow("search")
}
