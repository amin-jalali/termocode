package aimemory

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func lesson(date, title, text string, paths ...string) Lesson {
	return Lesson{ID: NewID(title, text), Date: date, Title: title, Text: text, Paths: paths}
}

func TestParseMarshalRoundTrip(t *testing.T) {
	ls := []Lesson{
		lesson("2026-10-24", "Run go test before claiming a fix", "The app tests take ten minutes; use -run.", "internal/app/ai_chat.go", "go.mod"),
		lesson("2026-10-25", "Golden frames", "Accept them with -update only after a look."),
	}
	b := Marshal(ls)
	if !strings.Contains(string(b), "## 2026-10-24 · Run go test before claiming a fix {#"+ls[0].ID+"}\n") ||
		!strings.Contains(string(b), "paths: internal/app/ai_chat.go, go.mod\n") {
		t.Fatalf("file:\n%s", b)
	}
	if got := Parse(b); !reflect.DeepEqual(got, ls) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, ls)
	}
	// A hand-written section without an id gets one from its text; a
	// stray heading ends a section; CRLF is read.
	hand := "# notes\r\n\r\n## 2026-10-20 · Hand made\r\nline one\r\nline two\r\n\r\n# other\r\njunk\r\n"
	got := Parse([]byte(hand))
	if len(got) != 1 || got[0].ID != NewID("Hand made", "line one\nline two") || got[0].Text != "line one\nline two" {
		t.Fatalf("hand-made section: %+v", got)
	}
}

func TestForgetKeepsOthers(t *testing.T) {
	a, b, c := lesson("2026-10-01", "A", "a"), lesson("2026-10-02", "B", "b"), lesson("2026-10-03", "C", "c")
	got := Forget([]Lesson{a, b, c}, b.ID)
	if !reflect.DeepEqual(got, []Lesson{a, c}) {
		t.Fatalf("forget: %+v", got)
	}
	if got := Forget([]Lesson{a}, "nope"); !reflect.DeepEqual(got, []Lesson{a}) {
		t.Fatalf("forget unknown: %+v", got)
	}
}

func TestAppendCaps(t *testing.T) {
	var ls []Lesson
	for i := range MaxLessons {
		ls = append(ls, lesson("2026-10-25", fmt.Sprintf("Lesson %d", i), "short"))
	}
	ls = Append(ls, Lesson{Date: "2026-10-26", Title: "Lesson new", Text: "short"})
	if len(ls) != MaxLessons || ls[0].Title != "Lesson 1" || ls[len(ls)-1].Title != "Lesson new" || ls[len(ls)-1].ID == "" {
		t.Fatalf("201st lesson: %d lessons, first %q last %+v", len(ls), ls[0].Title, ls[len(ls)-1])
	}
	// The same lesson again is not added twice.
	if again := Append(ls, Lesson{Date: "2026-10-27", Title: "Lesson new", Text: "short"}); len(again) != len(ls) {
		t.Fatalf("duplicate added: %d", len(again))
	}
	// 64 KB: long lessons push the oldest out.
	var big []Lesson
	for i := range 150 {
		big = Append(big, Lesson{Date: "2026-10-25", Title: fmt.Sprintf("Big %d", i), Text: strings.Repeat("x", 600)})
	}
	if n := len(Marshal(big)); n > MaxBytes {
		t.Fatalf("file %d bytes > %d", n, MaxBytes)
	}
	if big[len(big)-1].Title != "Big 149" || big[0].Title == "Big 0" {
		t.Fatalf("64 KB cap kept %q … %q", big[0].Title, big[len(big)-1].Title)
	}
}

func TestSelectByPathThenWordsThenNewest(t *testing.T) {
	ls := []Lesson{
		lesson("2026-10-01", "Old path lesson", "about the chat", "internal/app/ai_chat.go"),   // 0
		lesson("2026-10-02", "Toast timing", "The toast slides twice when animation restarts"), // 1
		lesson("2026-10-03", "Unrelated one", "nothing here"),                                  // 2
		lesson("2026-10-04", "Unrelated two", "nothing there"),                                 // 3
		lesson("2026-10-05", "Unrelated three", "still nothing"),                               // 4
	}
	h := Hints{Text: "why does the toast animation slide out?", Paths: []string{"/home/u/repo/internal/app/ai_chat.go"}}
	got := titles(Select(ls, h, 2<<10, 10))
	want := []string{"Old path lesson", "Toast timing", "Unrelated three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("select: %v, want %v", got, want)
	}
	// Nothing matches: the three newest.
	if got := titles(Select(ls, Hints{Text: "hello"}, 2<<10, 10)); !reflect.DeepEqual(got, []string{"Unrelated three", "Unrelated two", "Unrelated one"}) {
		t.Fatalf("newest: %v", got)
	}
	// One shared word is not enough; the size and count caps hold.
	if got := titles(Select(ls, Hints{Text: "toast"}, 2<<10, 1)); !reflect.DeepEqual(got, []string{"Unrelated three"}) {
		t.Fatalf("maxN: %v", got)
	}
	long := []Lesson{lesson("2026-10-01", "A", strings.Repeat("a ", 600)), lesson("2026-10-02", "B", strings.Repeat("b ", 600))}
	if got := Select(long, Hints{}, 2<<10, 10); len(got) != 1 {
		t.Fatalf("maxBytes: %d lessons", len(got))
	}
}

func titles(ls []Lesson) []string {
	var out []string
	for _, l := range ls {
		out = append(out, l.Title)
	}
	return out
}

func TestParseReply(t *testing.T) {
	good := "Here:\n```json\n{\"lessons\":[{\"title\":\"Use -race\",\"text\":\"CI runs tests with -race.\",\"paths\":[\"Makefile\",\"/etc/passwd\",\"../x\",\"a,b\"]}]}\n```\n"
	ls, err := ParseReply(good)
	if err != nil || len(ls) != 1 || ls[0].Title != "Use -race" || !reflect.DeepEqual(ls[0].Paths, []string{"Makefile"}) {
		t.Fatalf("good: %+v %v", ls, err)
	}
	if _, err := ParseReply("no json here"); err == nil {
		t.Fatal("junk: no error")
	}
	if _, err := ParseReply("```json\n{\"other\":1}\n```"); err == nil {
		t.Fatal("no lessons key: no error")
	}
	if ls, err := ParseReply(`{"lessons":[]}`); err != nil || len(ls) != 0 {
		t.Fatalf("empty list: %+v %v", ls, err)
	}
	four := `{"lessons":[{"title":"1","text":"a"},{"title":"2","text":"b"},{"title":"3","text":"c"},{"title":"4","text":"d"}]}`
	if ls, _ := ParseReply(four); !reflect.DeepEqual(titles(ls), []string{"1", "2", "3"}) {
		t.Fatalf("four: %v", titles(ls))
	}
	red := `{"lessons":[{"title":"DB password","text":"it is [REDACTED]"},{"title":"ok","text":"fine"}]}`
	if ls, _ := ParseReply(red); !reflect.DeepEqual(titles(ls), []string{"ok"}) {
		t.Fatalf("redacted: %v", titles(ls))
	}
	esc := "{\"lessons\":[{\"title\":\"x\\u001b[2Jy\\u202ez {#abcd}\",\"text\":\"line\\none\"}]}"
	ls, _ = ParseReply(esc)
	if len(ls) != 1 || strings.ContainsAny(ls[0].Title, "\x1b‮") || strings.Contains(ls[0].Title, "{#") || ls[0].Text != "line one" {
		t.Fatalf("escapes: %+v", ls)
	}
	long := fmt.Sprintf(`{"lessons":[{"title":%q,"text":%q}]}`, strings.Repeat("t", 200), strings.Repeat("x", 900))
	ls, _ = ParseReply(long)
	if len(ls) != 1 || len([]rune(ls[0].Title)) != MaxTitleRunes || len([]rune(ls[0].Text)) != MaxTextRunes {
		t.Fatalf("caps: %d / %d runes", len([]rune(ls[0].Title)), len([]rune(ls[0].Text)))
	}
}

func TestDir(t *testing.T) {
	if got := Dir("/w"); got != filepath.FromSlash("/w/.termocode/memory") {
		t.Fatalf("Dir %q", got)
	}
	if got := Path("/w"); got != filepath.FromSlash("/w/.termocode/memory/lessons.md") {
		t.Fatalf("Path %q", got)
	}
}

func TestLoadSaveAtomic(t *testing.T) {
	root := t.TempDir()
	if ls, err := Load(root); err != nil || ls != nil {
		t.Fatalf("missing file: %v %v", ls, err)
	}
	ls := []Lesson{lesson("2026-10-25", "A", "a")}
	if err := Save(root, ls); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil || !reflect.DeepEqual(got, ls) {
		t.Fatalf("load: %+v %v", got, err)
	}
	ents, _ := os.ReadDir(Dir(root))
	if len(ents) != 1 || ents[0].Name() != File {
		t.Fatalf("leftover temp files: %v", ents)
	}
}

// A repository cannot point the lessons at another file or folder.
func TestLoadSaveRefuseLinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret")
	_ = os.WriteFile(secret, []byte("## 2026-10-25 · leak {#aaaa}\nsecret\n"), 0o600)
	_ = os.MkdirAll(Dir(root), 0o755)
	if err := os.Symlink(secret, Path(root)); err != nil {
		t.Skip("no symlinks")
	}
	if ls, err := Load(root); err == nil || len(ls) != 0 {
		t.Fatalf("read through a link: %+v %v", ls, err)
	}
	root2 := t.TempDir()
	_ = os.Symlink(outside, filepath.Join(root2, ".termocode"))
	if err := Save(root2, []Lesson{lesson("2026-10-25", "A", "a")}); err == nil {
		t.Fatal("wrote through a linked .termocode")
	}
	if _, err := os.Stat(filepath.Join(outside, "memory")); err == nil {
		t.Fatal("made a folder outside the workspace")
	}
}

func TestEnsureIgnored(t *testing.T) {
	// No .gitignore: made with the one line.
	root := t.TempDir()
	if r, err := EnsureIgnored(root); err != nil || r != IgnoreCreated {
		t.Fatalf("create: %v %v", r, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".gitignore")); string(b) != ".termocode/memory/\n" {
		t.Fatalf("created %q", b)
	}
	// Existing lines stay byte-equal; the line is added once.
	root = t.TempDir()
	old := "# build\r\n/bin\nnode_modules/\n*.log" // CRLF and no final newline
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte(old), 0o644)
	for i := range 2 {
		r, err := EnsureIgnored(root)
		want := IgnoreAdded
		if i == 1 {
			want = IgnoreAlready
		}
		if err != nil || r != want {
			t.Fatalf("run %d: %v %v", i, r, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".gitignore")); string(b) != old+"\n.termocode/memory/\n" {
		t.Fatalf("appended %q", b)
	}
	for _, s := range []string{".termocode/\n", "/.termocode\n", "x\n/.termocode/memory/*\n"} {
		if !Covered([]byte(s)) {
			t.Errorf("%q should cover", s)
		}
	}
	for _, s := range []string{"", "termocode/\n", "# .termocode/\n", ".termocode/\n!.termocode/memory/\n", ".termocode/\n!.termocode/memory/lessons.md\n"} {
		if Covered([]byte(s)) {
			t.Errorf("%q should not cover", s)
		}
	}
}
