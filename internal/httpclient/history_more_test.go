package httpclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A history entry opens again as the response it stored: redirects and
// URLs redacted, kind and JSON flag rebuilt, the request URL used when
// the final one is missing.
func TestHistoryEntryReopensItsResponse(t *testing.T) {
	resp := &Response{Status: 200, StatusText: "OK", URL: "https://api.test/u?token=abc123secret", ContentType: "application/json", IsJSON: true,
		Body: []byte(`{"a":1}`), Size: 7, Elapsed: 84 * time.Millisecond,
		Hops: []Hop{{Status: 302, URL: "https://api.test/login?token=abc123secret"}}}
	e := NewEntry(Request{Method: "GET", URL: "https://api.test/u"}, "", "dev", "user", resp)
	got := e.Response()
	if got.Status != 200 || !got.IsJSON || string(got.Body) != `{"a":1}` || got.Elapsed != 84*time.Millisecond || len(got.Hops) != 1 {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got.URL, "abc123secret") || strings.Contains(got.Hops[0].URL, "abc123secret") {
		t.Fatalf("secret in history: %q %q", got.URL, got.Hops[0].URL)
	}
	if got.Summary() != "200 OK · 84 ms · 7 B · json · 1 redirect" {
		t.Errorf("summary %q", got.Summary())
	}
	failed := NewEntry(Request{Method: "GET", URL: "https://api.test/x"}, "", "", "user", &Response{Err: "timed out after 30 s"})
	if r := failed.Response(); r.URL != "https://api.test/x" || r.StatusLine() != "ERR" {
		t.Errorf("failed send: %+v", r)
	}
}

// The picker line uses the request name when there is one, and the age
// reads naturally from seconds to days.
func TestHistoryLineNamesAndAges(t *testing.T) {
	now := time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)
	e := Entry{Time: now.Add(-3 * time.Second), Method: "GET", URL: "https://api.test/users", Name: "list users", Status: 200, ElapsedMS: 5}
	if got := e.Line(now); got != "GET list users · 200 · 5 ms · just now" {
		t.Errorf("line %q", got)
	}
	for d, want := range map[time.Duration]string{30 * time.Second: "30 s ago", 72 * time.Hour: "3 d ago"} {
		if got := Ago(d); got != want {
			t.Errorf("Ago(%v) = %q, want %q", d, got, want)
		}
	}
	for url, want := range map[string]string{"https://api.test": "GET /", "https://api.test/a/b?x=1": "GET /a/b", "": "GET /"} {
		if got := DeriveName("GET", url); got != want {
			t.Errorf("DeriveName(%q) = %q, want %q", url, got, want)
		}
	}
}

// The store: default size, redaction of hop URLs, a missing id, and a
// broken file are errors the page can show.
func TestHistoryStoreEdges(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "h.jsonl"), Redact: func(v string) string { return strings.ReplaceAll(v, "hunter2", "[redacted]") }}
	if s.max() != DefaultHistoryMax {
		t.Errorf("default max %d", s.max())
	}
	e, err := s.AppendEntry(Entry{Method: "GET", URL: "http://x/?p=hunter2", Hops: []Hop{{Status: 301, URL: "http://y/?p=hunter2"}}})
	if err != nil || e.Time.IsZero() {
		t.Fatalf("%+v %v", e, err)
	}
	raw, _ := os.ReadFile(s.Path)
	if strings.Contains(string(raw), "hunter2") {
		t.Fatalf("secret on disk: %s", raw)
	}
	if err := os.WriteFile(s.Path, append(raw, []byte("\n\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if list, err := s.List(); err != nil || len(list) != 1 {
		t.Fatalf("blank lines: %v %v", list, err)
	}
	if _, ok := s.Get("nope"); ok {
		t.Error("Get of a missing id")
	}
	dir := &Store{Path: t.TempDir()}
	if _, err := dir.List(); err == nil {
		t.Error("a folder read as history")
	}
	if err := dir.Append(Entry{}); err == nil {
		t.Error("append to a folder")
	}
	if err := dir.Delete("x"); err == nil {
		t.Error("delete in a folder")
	}
}

// Response facts: the kind word per content type, long durations, a
// cut body, a header that is absent, odd content types.
func TestResponseKindsAndSummary(t *testing.T) {
	for ct, want := range map[string]string{"text/html": "html", "application/xml": "xml", "text/plain": "text", "application/x-thing": ""} {
		if got := (&Response{ContentType: ct}).Kind(); got != want {
			t.Errorf("Kind(%s) = %q", ct, got)
		}
	}
	if got := (&Response{IsBinary: true}).Kind(); got != "binary" {
		t.Errorf("binary kind %q", got)
	}
	r := &Response{Status: 299, Elapsed: 12300 * time.Millisecond, Size: 1 << 20, Truncated: true, MaxBody: 1 << 20}
	if got := r.Summary(); got != "299 · 12.3 s · 1.0 MB · truncated at 1.0 MB" {
		t.Errorf("summary %q", got)
	}
	if r.Header("x-none") != "" {
		t.Error("absent header")
	}
	if mt, cs := parseContentType("text/html; charset"); mt != "text/html" || cs != "" {
		t.Errorf("broken params: %q %q", mt, cs)
	}
	if isBinary("image/svg+xml", []byte("<svg/>")) {
		t.Error("svg is text")
	}
	if _, ok := PrettyJSON([]byte("{not json")); ok {
		t.Error("PrettyJSON of broken JSON")
	}
}

// .http file helpers: a request is found by name and listed by label.
func TestFileFindAndNames(t *testing.T) {
	f := &File{Requests: []Request{{Name: "login", Method: "POST", URL: "https://api.test/login"}, {Method: "GET", URL: "https://api.test/me"}}}
	if r := f.Find("login"); r == nil || r.Method != "POST" {
		t.Errorf("Find = %+v", r)
	}
	if f.Find("nope") != nil {
		t.Error("Find of a missing name")
	}
	if got := strings.Join(f.Names(), ","); got != "login,GET /me" {
		t.Errorf("Names = %q", got)
	}
}
