package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

func httpRowKind(p httpPage, text string) string {
	for _, r := range p.rows {
		if l, _ := r["l"].(int); l >= 1 && l <= len(p.lines) && p.lines[l-1] == text {
			k, _ := r["k"].(string)
			return k
		}
	}
	return ""
}

// API-10: the Cookies, Timing and Wire views; secrets hidden until x.
func TestHTTPInspectViews(t *testing.T) {
	unicodeIcons(t)
	run := testRun()
	text := strings.Join(buildHTTPPage(run, httpViewCookies, false, false).lines, "\n")
	if !strings.Contains(text, "COOKIES  1 set") || !strings.Contains(text, "s: [REDACTED]") || !strings.Contains(text, "path / · session") {
		t.Fatalf("cookies view:\n%s", text)
	}
	if strings.Contains(text, "sekrit") {
		t.Fatalf("cookies view leaks a value:\n%s", text)
	}
	if !strings.Contains(strings.Join(buildHTTPPage(run, httpViewCookies, true, false).lines, "\n"), "s: sekrit-cookie") {
		t.Fatal("x does not reveal the cookie value")
	}

	text = strings.Join(buildHTTPPage(run, httpViewTiming, false, false).lines, "\n")
	for _, want := range []string{"TIMING", "connect ", "wait ", "download ", "41 ms", "SIZE", "request headers", "response body", "96 B"} {
		if !strings.Contains(text, want) {
			t.Errorf("timing view lacks %q:\n%s", want, text)
		}
	}

	text = strings.Join(buildHTTPPage(run, httpViewWire, false, false).lines, "\n")
	if !strings.Contains(text, "> POST http://localhost:8080/users?token=[REDACTED]") || !strings.Contains(text, "> authorization: Bearer [REDACTED]") ||
		!strings.Contains(text, "< HTTP/1.1 500 Internal Server Error") {
		t.Fatalf("wire view:\n%s", text)
	}
	if strings.Contains(text, "sekrit") {
		t.Fatalf("wire view leaks a secret:\n%s", text)
	}
	if !strings.Contains(strings.Join(buildHTTPPage(run, httpViewWire, true, false).lines, "\n"), "Bearer sekrit-token-123") {
		t.Fatal("x does not reveal the wire log")
	}
}

// API-10: Diff compares with the answer the same request got before.
func TestHTTPDiffViewComparesWithPreviousAnswer(t *testing.T) {
	s := newHTTPState()
	mk := func(body string) *httpRun {
		r := jsonRun(body)
		r.file, r.line = "/ws/a.http", 3
		return r
	}
	first := mk(`{"a":1,"b":2}`)
	s.remember(first)
	if first.prev != nil {
		t.Fatal("first answer has a previous one")
	}
	p := buildHTTPPage(first, httpViewDiff, false, false)
	if !strings.Contains(strings.Join(p.lines, "\n"), "No earlier answer") {
		t.Fatalf("diff without a previous answer:\n%s", strings.Join(p.lines, "\n"))
	}
	other := jsonRun(`{"z":0}`)
	other.file, other.line = "/ws/a.http", 9
	s.remember(other)
	second := mk(`{"a":5,"b":2}`)
	s.remember(second)
	if second.prev != first.resp {
		t.Fatal("previous answer is not the same request's last answer")
	}
	p = buildHTTPPage(second, httpViewDiff, false, false)
	if k := httpRowKind(p, `-   "a": 1,`); k != "del" {
		t.Fatalf("old line kind %q:\n%s", k, strings.Join(p.lines, "\n"))
	}
	if k := httpRowKind(p, `+   "a": 5,`); k != "add" {
		t.Fatalf("new line kind %q:\n%s", k, strings.Join(p.lines, "\n"))
	}
	if !strings.Contains(strings.Join(p.lines, "\n"), `    "b": 2`) {
		t.Fatalf("same lines missing:\n%s", strings.Join(p.lines, "\n"))
	}
}

// API-10: XML is indented (indent folds work) with xml syntax; Text
// shows an HTML page as plain text.
func TestHTTPMarkupBodyAndTextView(t *testing.T) {
	run := &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: "http://x/"}},
		resp: &httpclient.Response{Status: 200, StatusText: "OK", ContentType: "application/xml", Body: []byte(`<a><b>1</b></a>`), Size: 15,
			Timing: httpclient.Timing{Total: time.Millisecond}}}
	p := buildHTTPPage(run, httpViewBody, false, false)
	if got := strings.Join(p.lines[3:], "\n"); got != "<a>\n  <b>\n    1\n  </b>\n</a>" || p.head["syntax"] != "xml" {
		t.Fatalf("xml body %q syntax %v", got, p.head["syntax"])
	}
	if text := strings.Join(buildHTTPPage(run, httpViewText, false, false).lines, "\n"); !strings.Contains(text, "this answer is xml") {
		t.Fatalf("text view on xml:\n%s", text)
	}
	run.resp = &httpclient.Response{Status: 200, ContentType: "text/html", Body: []byte(`<html><body><h1>Hi</h1><script>x()</script><p>there</p></body></html>`)}
	if got := strings.Join(buildHTTPPage(run, httpViewText, false, false).lines[3:], "\n"); got != "Hi\nthere" {
		t.Fatalf("text view %q", got)
	}
}
