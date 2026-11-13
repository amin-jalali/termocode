package httpclient

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

func TestCookiesFlagsExpiryAndSameSite(t *testing.T) {
	r := &Response{Headers: []Header{
		{Name: "set-cookie", Value: "a=1; Max-Age=60; Secure; HttpOnly; SameSite=Lax"},
		{Name: "set-cookie", Value: "b=2; Max-Age=-1; SameSite=Strict; Partitioned; Secure"},
		{Name: "set-cookie", Value: "c=3; Expires=Wed, 21 Oct 2037 07:28:00 GMT; SameSite=None; Secure"},
		{Name: "set-cookie", Value: "d=4"},
		{Name: "set-cookie", Value: "=nameless"},
		{Name: "set-cookie", Value: "not a cookie at all"},
		{Name: "content-type", Value: "text/plain"},
	}}
	rows := r.Cookies(false)
	if len(rows) != 4 {
		t.Fatalf("rows = %+v", rows)
	}
	want := []struct{ name, exp, same, flags string }{
		{"a", "max-age 60 s", "Lax", "Secure,HttpOnly"},
		{"b", "expired", "Strict", "Secure,Partitioned"},
		{"c", "Wed, 21 Oct 2037 07:28:00 UTC", "None", "Secure"},
		{"d", "session", "", ""},
	}
	for i, w := range want {
		g := rows[i]
		if g.Name != w.name || g.Expires != w.exp || g.SameSite != w.same || strings.Join(g.Flags, ",") != w.flags || g.Value != Redacted {
			t.Errorf("row %d = %+v, want %+v", i, g, w)
		}
	}
	if got := r.Cookies(true)[0].Value; got != "1" {
		t.Errorf("revealed value = %q", got)
	}
}

func TestWaterfallPhasesAndWidths(t *testing.T) {
	if got := Waterfall(Timing{}, 20); len(got) != 1 || got[0] != "no timing data" {
		t.Errorf("empty timing = %v", got)
	}
	got := Waterfall(Timing{DNS: 10 * time.Millisecond, Connect: 20 * time.Millisecond, TLS: 30 * time.Millisecond, FirstByte: 100 * time.Millisecond, Total: 200 * time.Millisecond}, 3)
	if len(got) != 6 {
		t.Fatalf("lines = %v", got)
	}
	for _, l := range got[:5] {
		if !strings.Contains(l, "█") {
			t.Errorf("every phase that happened has a bar: %q", l)
		}
	}
	// A total that is shorter than the phases is lifted to their end.
	if got := Waterfall(Timing{DNS: 50 * time.Millisecond, Total: 10 * time.Millisecond}, 20); len(got) != 2 || !strings.HasPrefix(got[0], "dns") {
		t.Errorf("short total = %v", got)
	}
	// A tiny phase at the very end still gets one cell inside the width.
	if got := Waterfall(Timing{DNS: 999 * time.Millisecond, Total: 1000 * time.Millisecond}, 10); len(got) != 3 || strings.Count(got[1], "█") != 1 {
		t.Errorf("end cell = %v", got)
	}
}

func TestMarkupPrettyAndText(t *testing.T) {
	out, ok := PrettyMarkup([]byte(`<?xml version="1.0"?><!DOCTYPE a><a x:y="1 &amp; 2"><!-- note --><b>t</b></a>`))
	s := string(out)
	for _, want := range []string{"<?xml", "<!DOCTYPE a>", "<!-- note -->", "  <b>", `x:y="1 &amp; 2"`} {
		if !ok || !strings.Contains(s, want) {
			t.Errorf("pretty = %q lacks %q (ok %v)", s, want, ok)
		}
	}
	if _, ok := PrettyMarkup([]byte("plain")); ok {
		t.Error("text is not markup")
	}
	if _, ok := PrettyMarkup([]byte("   ")); ok {
		t.Error("blank is not markup")
	}
	bad := []byte("<a href='x")
	if got, ok := PrettyMarkup(bad); ok || string(got) != string(bad) {
		t.Errorf("broken markup is shown as it is: %q %v", got, ok)
	}
	if !(&Response{ContentType: "text/html"}).IsMarkup() || !(&Response{ContentType: "application/xml"}).IsMarkup() ||
		(&Response{ContentType: "application/json", IsJSON: true}).IsMarkup() {
		t.Error("IsMarkup")
	}
	if got := markupName(xmlName("http://x/ns", "tag")); got != "tag" {
		t.Errorf("a namespace URL is dropped, got %q", got)
	}
	if got := markupName(xmlName("ns", "tag")); got != "ns:tag" {
		t.Errorf("a prefix is kept, got %q", got)
	}
	txt := HTMLText([]byte("<html><head><title>T</title><style>p{}</style></head><body>a<br>b &amp; c<script>x()</script><p>last"))
	if strings.Contains(txt, "x()") || strings.Contains(txt, "p{}") || !strings.Contains(txt, "b & c") {
		t.Errorf("HTMLText = %q", txt)
	}
	if got := HTMLText([]byte("text <unclosed")); !strings.Contains(got, "text") {
		t.Errorf("an open tag at the end = %q", got)
	}
	if got := HTMLText([]byte("<script>never closed")); !strings.Contains(got, "never closed") {
		t.Errorf("a script with no end keeps the rest = %q", got)
	}
}

func TestDiffLinesFallbackAndTails(t *testing.T) {
	got := DiffLines([]string{"a", "b", "c", "x"}, []string{"a", "q", "c", "x"})
	if strings.Join(got, "|") != "  a|- b|+ q|  c|  x" {
		t.Errorf("diff = %v", got)
	}
	if got := DiffLines([]string{"a", "b"}, []string{"a"}); strings.Join(got, "|") != "  a|- b" {
		t.Errorf("older has more = %v", got)
	}
	if got := DiffLines([]string{"a"}, []string{"a", "b"}); strings.Join(got, "|") != "  a|+ b" {
		t.Errorf("newer has more = %v", got)
	}
	// Two long, different inputs fall back to remove-all, add-all.
	a := make([]string, 2100)
	b := make([]string, 2100)
	for i := range a {
		a[i], b[i] = "a"+string(rune('a'+i%7))+strings.Repeat("x", i%5), "b"+string(rune('a'+i%5))
	}
	a[0], b[0] = "first-a", "first-b"
	got = DiffLines(a, b)
	if len(got) != len(a)+len(b) || got[0] != "- first-a" {
		t.Errorf("fallback: %d lines, first %q", len(got), got[0])
	}
}

func TestWireLogEdges(t *testing.T) {
	r := &Response{Err: "refused", ReqHeaders: []Header{{Name: "Authorization", Value: "Bearer s3cret"}}}
	got := r.WireLog("POST", "https://u:p@x/y?token=abc", "line1\nline2\n", false)
	text := strings.Join(got, "\n")
	if strings.Contains(text, "s3cret") || strings.Contains(text, "abc") || !strings.Contains(text, "> line2") || !strings.HasSuffix(text, "! refused") {
		t.Errorf("failed send log:\n%s", text)
	}
	bin := &Response{Status: 200, IsBinary: true, Size: 2048, Headers: []Header{{Name: "content-type", Value: "image/png"}}}
	if text := strings.Join(bin.WireLog("GET", "https://x/", "", true), "\n"); !strings.Contains(text, "HTTP/1.1 200") || !strings.Contains(text, "(binary body") {
		t.Errorf("binary log:\n%s", text)
	}
	crlf := &Response{Status: 200, Proto: "HTTP/2.0", Body: []byte("a\r\nb\r\n")}
	if text := strings.Join(crlf.WireLog("GET", "https://x/", "", true), "\n"); !strings.Contains(text, "< HTTP/2.0 200") || !strings.Contains(text, "< a\n< b") {
		t.Errorf("text log:\n%s", text)
	}
}

func xmlName(space, local string) xml.Name { return xml.Name{Space: space, Local: local} }
