package httpclient

import (
	"strings"
	"testing"
	"time"
)

func TestCookiesParsesSetCookieAndHidesValues(t *testing.T) {
	r := &Response{Headers: []Header{
		{Name: "content-type", Value: "text/plain"},
		{Name: "set-cookie", Value: "sid=abc123; Path=/; Domain=example.com; Max-Age=3600; Secure; HttpOnly; SameSite=Lax"},
		{Name: "set-cookie", Value: "theme=dark"},
	}}
	got := r.Cookies(false)
	if len(got) != 2 {
		t.Fatalf("got %d cookies", len(got))
	}
	c := got[0]
	if c.Name != "sid" || c.Value != Redacted || c.Path != "/" || c.Domain != "example.com" ||
		c.Expires != "max-age 3600 s" || c.SameSite != "Lax" || strings.Join(c.Flags, ",") != "Secure,HttpOnly" {
		t.Fatalf("cookie %+v", c)
	}
	if got[1].Expires != "session" {
		t.Fatalf("second cookie %+v", got[1])
	}
	if v := r.Cookies(true)[0].Value; v != "abc123" {
		t.Fatalf("reveal value %q", v)
	}
}

func TestWaterfallLaysPhasesOnOneTimeLine(t *testing.T) {
	ms := time.Millisecond
	got := Waterfall(Timing{DNS: 10 * ms, Connect: 10 * ms, TLS: 20 * ms, FirstByte: 80 * ms, Total: 100 * ms}, 10)
	want := []string{
		"dns       █········· 10 ms",
		"connect   ·█········ 10 ms",
		"tls       ··██······ 20 ms",
		"wait      ····████·· 40 ms",
		"download  ········██ 20 ms",
		"total                100 ms",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("waterfall:\n%s", strings.Join(got, "\n"))
	}
	if got := Waterfall(Timing{}, 10); len(got) != 1 || got[0] != "no timing data" {
		t.Fatalf("empty timing %q", got)
	}
}

func TestSizeBreakdownShowsWireAndDecodedBody(t *testing.T) {
	r := &Response{
		ReqHeaders: []Header{{Name: "a", Value: "b"}},
		Headers:    []Header{{Name: "content-encoding", Value: "gzip"}, {Name: "content-length", Value: "40"}},
		Size:       100,
	}
	got := r.SizeBreakdown(7)
	want := []SizeRow{{"request headers", 6}, {"request body", 7}, {"response headers", 44}, {"response body (gzip)", 40}, {"response body", 100}}
	if len(got) != len(want) {
		t.Fatalf("rows %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestPrettyMarkupIndentsXMLForFolds(t *testing.T) {
	got, ok := PrettyMarkup([]byte(`<?xml version="1.0"?><a x="1"><b>hi</b><c/></a>`))
	if !ok {
		t.Fatal("not markup")
	}
	want := "<?xml version=\"1.0\"?>\n<a x=\"1\">\n  <b>\n    hi\n  </b>\n  <c>\n  </c>\n</a>"
	if string(got) != want {
		t.Fatalf("got:\n%s", got)
	}
	if _, ok := PrettyMarkup([]byte(`{"a":1}`)); ok {
		t.Fatal("JSON read as markup")
	}
}

func TestHTMLTextDropsTagsAndScripts(t *testing.T) {
	got := HTMLText([]byte(`<html><head><title>T</title><style>p{}</style></head><body><h1>Hello &amp; bye</h1><script>alert(1)</script><p>one   two</p><br>three</body></html>`))
	if got != "Hello & bye\none two\nthree" {
		t.Fatalf("got %q", got)
	}
}

func TestDiffLinesMarksChanges(t *testing.T) {
	got := DiffLines([]string{"a", "b", "c", "d"}, []string{"a", "x", "c", "d", "e"})
	want := []string{"  a", "- b", "+ x", "  c", "  d", "+ e"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("diff %q", got)
	}
}

func TestWireLogHidesSecretsUnlessReveal(t *testing.T) {
	r := &Response{
		Proto: "HTTP/1.1", Status: 200, StatusText: "OK",
		ReqHeaders: []Header{{Name: "authorization", Value: "Bearer s3cr3t"}},
		Headers:    []Header{{Name: "content-type", Value: "text/plain"}},
		Body:       []byte("hi\n"),
	}
	got := strings.Join(r.WireLog("POST", "https://h/x?token=t0k", "body", false), "\n")
	want := "> POST https://h/x?token=" + Redacted + "\n> authorization: Bearer " + Redacted + "\n>\n> body\n\n< HTTP/1.1 200 OK\n< content-type: text/plain\n<\n< hi"
	if got != want {
		t.Fatalf("wire:\n%s", got)
	}
	if s := strings.Join(r.WireLog("GET", "https://h/x?token=t0k", "", true), "\n"); !strings.Contains(s, "s3cr3t") || !strings.Contains(s, "t0k") {
		t.Fatalf("reveal:\n%s", s)
	}
}
