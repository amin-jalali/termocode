package httpclient

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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

func histLines(t *testing.T, p string) int {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

// PERF-8: a send adds one line to the history file instead of rewriting
// it; the file is rewritten (trimmed to Max) only once it grows a
// quarter over the cap.
func TestHistoryAppendAddsALine(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "http", "history.jsonl"), Max: 8}
	if err := s.Append(Entry{Method: "GET", URL: "http://x/0"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 8; i++ {
		if err := s.Append(Entry{Method: "GET", URL: "http://x/" + string(rune('0'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.Stat(s.Path)
	if !os.SameFile(before, after) {
		t.Fatal("append under the cap rewrote the file (new inode)")
	}
	if after.Mode().Perm() != 0o600 {
		t.Fatalf("history mode %v", after.Mode())
	}
	if n := histLines(t, s.Path); n != 8 {
		t.Fatalf("%d lines, want 8", n)
	}
}

func TestHistoryCompactsOverTheCap(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl"), Max: 8}
	for i := 0; i < 50; i++ {
		if err := s.Append(Entry{Method: "GET", URL: "http://x/" + strconv.Itoa(i)}); err != nil {
			t.Fatal(err)
		}
		if n := histLines(t, s.Path); n > 8+historySlack(8) {
			t.Fatalf("after %d appends the file holds %d lines", i+1, n)
		}
		list, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if want := min(i+1, 8); len(list) != want || list[0].URL != "http://x/"+strconv.Itoa(i) {
			t.Fatalf("after %d appends: %d entries, newest %q", i+1, len(list), list[0].URL)
		}
	}
	// Another Store on the same file (the app makes one per call) sees
	// the same list.
	other := &Store{Path: s.Path, Max: 8}
	a, _ := s.List()
	b, _ := other.List()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two stores disagree")
	}
}

// The cached list follows writes made outside the store.
func TestHistoryListSeesOutsideWrites(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl"), Max: 10}
	for i := 0; i < 3; i++ {
		_ = s.Append(Entry{ID: "id" + strconv.Itoa(i), Method: "GET", URL: "http://x/" + strconv.Itoa(i)})
	}
	if l, _ := s.List(); len(l) != 3 {
		t.Fatalf("%d", len(l))
	}
	// Same size, other content, other mtime.
	raw, _ := os.ReadFile(s.Path)
	edited := strings.Replace(string(raw), "http://x/1", "http://y/1", 1)
	if err := os.WriteFile(s.Path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(s.Path, future, future)
	if l, _ := s.List(); len(l) != 3 || l[1].URL != "http://y/1" {
		t.Fatalf("stale list after an outside edit: %+v", l)
	}
	// Removed from outside.
	_ = os.Remove(s.Path)
	if l, _ := s.List(); len(l) != 0 {
		t.Fatalf("stale list after the file was removed: %d", len(l))
	}
	_ = s.Append(Entry{Method: "GET", URL: "http://z/"})
	if l, _ := s.List(); len(l) != 1 || l[0].URL != "http://z/" {
		t.Fatalf("after remove + append: %+v", l)
	}
}

// A torn last line (a crash mid-write) costs only that line.
func TestHistoryAppendAfterTornLine(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl"), Max: 10}
	_ = s.Append(Entry{Method: "GET", URL: "http://x/a"})
	f, _ := os.OpenFile(s.Path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"id":"torn","meth`)
	_ = f.Close()
	_ = s.Append(Entry{Method: "GET", URL: "http://x/b"})
	l, _ := s.List()
	if len(l) != 2 || l[0].URL != "http://x/b" || l[1].URL != "http://x/a" {
		t.Fatalf("%+v", l)
	}
}

// The list a caller gets is its own copy.
func TestHistoryListIsACopy(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl"), Max: 10}
	_ = s.Append(Entry{Method: "GET", URL: "http://x/a"})
	l, _ := s.List()
	l[0].URL = "changed"
	if l2, _ := s.List(); l2[0].URL != "http://x/a" {
		t.Fatal("List hands out the cache")
	}
}

func BenchmarkHistoryAppend1000(b *testing.B) {
	s := &Store{Path: filepath.Join(b.TempDir(), "history.jsonl"), Max: 1000}
	for i := 0; i < 1000; i++ {
		_ = s.Append(Entry{Method: "GET", URL: "http://x/", Body: strings.Repeat("x", 900)})
	}
	e := Entry{Method: "GET", URL: "http://x/", Body: strings.Repeat("y", 900)}
	b.ReportAllocs()
	for b.Loop() {
		_ = s.Append(e)
	}
}

func BenchmarkHistoryList1000(b *testing.B) {
	s := &Store{Path: filepath.Join(b.TempDir(), "history.jsonl"), Max: 1000}
	for i := 0; i < 1000; i++ {
		_ = s.Append(Entry{Method: "GET", URL: "http://x/", Body: strings.Repeat("x", 900)})
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = s.List()
	}
}

// The first List of a session (nothing cached): read and decode.
func BenchmarkHistoryListCold1000(b *testing.B) {
	s := &Store{Path: filepath.Join(b.TempDir(), "history.jsonl"), Max: 1000}
	for i := 0; i < 1000; i++ {
		_ = s.Append(Entry{Method: "GET", URL: "http://x/", Body: strings.Repeat("x", 900), Headers: []Header{{"Accept", "a"}}, RespHeaders: []Header{{"content-type", "application/json"}, {"date", "now"}}})
	}
	b.ReportAllocs()
	for b.Loop() {
		histMu.Lock()
		delete(histFiles, s.Path)
		histMu.Unlock()
		_, _ = s.List()
	}
}

// A long file decodes on several cores: order kept, bad lines skipped.
func TestHistoryDecodeManyKeepsOrder(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.jsonl")
	var sb strings.Builder
	for i := 0; i < 1500; i++ {
		if i%97 == 0 {
			sb.WriteString("{not json\n")
		}
		fmt.Fprintf(&sb, `{"id":"%d","method":"GET","url":"http://x/%d"}`+"\n", i, i)
	}
	if err := os.WriteFile(p, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := (&Store{Path: p, Max: 2000}).List()
	if err != nil || len(l) != 1500 {
		t.Fatalf("%d entries, %v", len(l), err)
	}
	for i, e := range l {
		if want := strconv.Itoa(1499 - i); e.ID != want {
			t.Fatalf("entry %d: id %s, want %s", i, e.ID, want)
		}
	}
}
