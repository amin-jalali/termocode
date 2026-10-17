package httpclient

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHistoryAppendTrimMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "http")
	s := &Store{Path: filepath.Join(dir, "history.jsonl"), Max: 3}
	for i := 0; i < 5; i++ {
		e := Entry{Time: time.Unix(int64(1000+i), 0), Method: "GET", URL: "http://x/" + string(rune('a'+i)), Status: 200}
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].URL != "http://x/e" || list[2].URL != "http://x/c" {
		t.Fatalf("newest first, trimmed to 3: %+v", list)
	}
	if list[0].ID == "" || list[0].ID == list[1].ID {
		t.Fatal("entries need distinct ids")
	}
	st, _ := os.Stat(s.Path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("history mode %v", st.Mode())
	}
	dst, _ := os.Stat(dir)
	if dst.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", dst.Mode())
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".tmp-*"))
	if len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
	if err := s.Delete(list[1].ID); err != nil {
		t.Fatal(err)
	}
	list, _ = s.List()
	if len(list) != 2 || list[1].URL != "http://x/c" {
		t.Fatalf("after delete: %+v", list)
	}
	if e, ok := s.Get(list[0].ID); !ok || e.URL != "http://x/e" {
		t.Fatal("Get failed")
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if list, _ = s.List(); len(list) != 0 {
		t.Fatalf("clear left %d", len(list))
	}
}

func TestHistoryEntryHidesSecrets(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "h.jsonl"), Max: 10, Redact: func(x string) string {
		return strings.ReplaceAll(x, "env-secret-value", "[REDACTED]")
	}}
	body := `{"t":"{{token}}"}`
	tmpl := Request{Method: "POST", URL: "{{host}}/login?token=lit-token-1", Headers: []Header{{"Authorization", "Bearer {{token}}"}, {"X-Api-Key", "literal-key-9"}}, Body: &body}
	resp := &Response{Status: 200, StatusText: "OK", Headers: []Header{{"set-cookie", "s=cookie-secret-7; Path=/"}}, Body: []byte(`{"echo":"env-secret-value"}`), ContentType: "application/json"}
	e := NewEntry(tmpl, "api/users.http", "dev", "ai", resp)
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.Path)
	for _, leak := range []string{"lit-token-1", "literal-key-9", "cookie-secret-7", "env-secret-value"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("history leaks %q:\n%s", leak, raw)
		}
	}
	if !strings.Contains(string(raw), "Bearer {{token}}") || !strings.Contains(string(raw), `"by":"ai"`) {
		t.Fatalf("history keeps the template and the author:\n%s", raw)
	}
}

func TestHistoryBodyCap(t *testing.T) {
	big := strings.Repeat("x", 300<<10)
	e := NewEntry(Request{Method: "GET", URL: "http://x"}, "", "", "user", &Response{Status: 200, Body: []byte(big), ContentType: "text/plain"})
	if len(e.Body) != 256<<10 || !e.BodyCut {
		t.Fatalf("body %d cut %v", len(e.Body), e.BodyCut)
	}
	e = NewEntry(Request{Method: "GET", URL: "http://x"}, "", "", "user", &Response{Status: 200, Body: []byte("\x89PNG\x00"), ContentType: "image/png", IsBinary: true})
	if e.Body != "" || !e.Binary {
		t.Fatal("binary bodies are not stored")
	}
}

func TestHistoryEntryLine(t *testing.T) {
	now := time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)
	e := Entry{Time: now.Add(-2 * time.Minute), Method: "POST", URL: "{{host}}/users?x=1", Status: 201, ElapsedMS: 84}
	if got := e.Line(now); got != "POST /users · 201 · 84 ms · 2 min ago" {
		t.Fatalf("line %q", got)
	}
	e.Err = "timed out after 30 s"
	if got := e.Line(now.Add(2 * time.Hour)); got != "POST /users · ERR · 84 ms · 2 h ago" {
		t.Fatalf("line %q", got)
	}
}

func TestEntryRequestRoundTrip(t *testing.T) {
	body := "{}"
	tmpl := Request{Method: "POST", URL: "{{host}}/u", Headers: []Header{{"Accept", "a"}}, Body: &body, Name: "n"}
	e := NewEntry(tmpl, "", "", "user", &Response{Status: 200})
	got := e.Request()
	if got.Method != "POST" || got.URL != "{{host}}/u" || got.BodyText() != "{}" || !reflect.DeepEqual(got.Headers, tmpl.Headers) {
		t.Fatalf("request %+v", got)
	}
}
