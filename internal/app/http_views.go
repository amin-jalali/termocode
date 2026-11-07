package app

import (
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/httpclient"
)

// The Response page's inspection views (API-10): Cookies, Timing
// (waterfall + size breakdown), Wire (the raw exchange), Diff (this
// answer against the one before for the same request) and Text (an
// HTML page as plain text). Secrets stay hidden unless x reveals them.

// httpRow is one line of an inspection view: k is the row kind the
// page Lua colours ("" = plain text), n the length of a header name.
type httpRow struct {
	k, text string
	n       int
}

// httpPrevMaxBody: a previous answer bigger than this is not kept for
// the Diff view (the ring keeps big bodies for the newest few only).
const httpPrevMaxBody = 1 << 20

// httpTimingWidth is the waterfall's bar width in cells.
const httpTimingWidth = 40

func httpInspectRows(run *httpRun, view int, reveal bool) []httpRow {
	resp := run.resp
	switch view {
	case httpViewCookies:
		return httpCookieRows(resp, reveal)
	case httpViewTiming:
		return httpTimingRows(run)
	case httpViewWire:
		return httpWireRows(run, reveal)
	case httpViewDiff:
		return httpDiffRows(run)
	case httpViewText:
		if resp.Kind() != "html" {
			return []httpRow{{k: "note", text: "Text shows an HTML answer as plain text; this answer is " + httpKindWord(resp) + "."}}
		}
		var rows []httpRow
		for _, l := range strings.Split(httpclient.HTMLText(resp.TextBytes()), "\n") {
			rows = append(rows, httpRow{text: l})
		}
		return rows
	}
	return nil
}

func httpKindWord(r *httpclient.Response) string {
	if k := r.Kind(); k != "" {
		return k
	}
	return "not HTML"
}

func httpCookieRows(resp *httpclient.Response, reveal bool) []httpRow {
	cs := resp.Cookies(reveal)
	if len(cs) == 0 {
		return []httpRow{{k: "note", text: "No cookies were set by this answer."}}
	}
	rows := []httpRow{{k: "t", text: fmt.Sprintf("COOKIES  %d set", len(cs)), n: len("COOKIES")}}
	for _, c := range cs {
		rows = append(rows, httpRow{k: "h", text: c.Name + ": " + c.Value, n: len(c.Name) + 1})
		var attrs []string
		if c.Domain != "" {
			attrs = append(attrs, "domain "+c.Domain)
		}
		if c.Path != "" {
			attrs = append(attrs, "path "+c.Path)
		}
		attrs = append(attrs, c.Expires)
		if c.SameSite != "" {
			attrs = append(attrs, "SameSite "+c.SameSite)
		}
		attrs = append(attrs, c.Flags...)
		rows = append(rows, httpRow{k: "note", text: "  " + strings.Join(attrs, " · ")})
	}
	return rows
}

func httpTimingRows(run *httpRun) []httpRow {
	resp := run.resp
	rows := []httpRow{{k: "t", text: "TIMING", n: len("TIMING")}}
	for _, l := range httpclient.Waterfall(resp.Timing, httpTimingWidth) {
		rows = append(rows, httpRow{text: l})
	}
	rows = append(rows, httpRow{}, httpRow{k: "t", text: "SIZE", n: len("SIZE")})
	var total int64
	for _, s := range resp.SizeBreakdown(len(run.rr.BodyText())) {
		rows = append(rows, httpRow{text: fmt.Sprintf("%-24s %10s", s.Part, httpclient.FormatSize(s.Bytes))})
		if !strings.HasPrefix(s.Part, "response body (") {
			total += s.Bytes
		}
	}
	return append(rows, httpRow{text: fmt.Sprintf("%-24s %10s", "total", httpclient.FormatSize(total))})
}

func httpWireRows(run *httpRun, reveal bool) []httpRow {
	body := run.rr.BodyText()
	if !reveal {
		body = ai.Redact(body)
	}
	var rows []httpRow
	for _, l := range run.resp.WireLog(run.rr.Method, run.rr.URL, body, reveal) {
		rows = append(rows, httpRow{text: l})
	}
	return rows
}

func httpDiffRows(run *httpRun) []httpRow {
	if run.prev == nil {
		return []httpRow{{k: "note", text: "No earlier answer to compare with. Send this request again (r), then Diff shows what changed."}}
	}
	old, cur := layoutHTTPBody(run.prev), run.bodyLines()
	rows := []httpRow{{k: "t", text: "DIFF  " + run.prev.StatusLine() + " → " + run.resp.StatusLine(), n: len("DIFF")}}
	changed := false
	for _, l := range httpclient.DiffLines(old.lines, cur.lines) {
		switch {
		case strings.HasPrefix(l, "+ "):
			rows, changed = append(rows, httpRow{k: "add", text: l}), true
		case strings.HasPrefix(l, "- "):
			rows, changed = append(rows, httpRow{k: "del", text: l}), true
		default:
			rows = append(rows, httpRow{text: l})
		}
	}
	if !changed {
		rows = append(rows[:1], httpRow{k: "note", text: "The body is the same as the answer before."})
	}
	return rows
}

// previousAnswer is the newest answer in the ring for the same request
// as r (same file and line, or the same method and URL for a quick
// request), when it is small enough to compare.
func (s *httpState) previousAnswer(r *httpRun) *httpclient.Response {
	for i := len(s.ring) - 1; i >= 0; i-- {
		o := s.ring[i]
		if o == r || o.resp == nil || o.resp.Err != "" {
			continue
		}
		same := o.file == r.file && o.line == r.line && o.file != ""
		if r.file == "" {
			same = o.file == "" && o.rr.Method == r.rr.Method && o.rr.URL == r.rr.URL
		}
		if !same {
			continue
		}
		if len(o.resp.Body) > httpPrevMaxBody || o.resp.Truncated {
			return nil
		}
		return o.resp
	}
	return nil
}
