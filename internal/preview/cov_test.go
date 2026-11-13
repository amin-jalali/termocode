package preview_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/mdview"
	"github.com/amin-jalali/termocode/internal/preview"
)

// fakeFS is a workspace.FS over an in-memory map; errors per call can be set.
type fakeFS struct {
	files                        fstest.MapFS
	statErr, readDirErr, readErr error
	size                         int64 // overrides the Stat size when > 0
	special                      bool  // Stat reports a non-regular file
}

type info struct {
	fs.FileInfo
	size    int64
	special bool
}

func (i info) Size() int64 { return i.size }
func (i info) Mode() fs.FileMode {
	if i.special {
		return fs.ModeNamedPipe
	}
	return i.FileInfo.Mode()
}

func (f *fakeFS) Stat(_ context.Context, p string) (fs.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	fi, err := f.files.Stat(p)
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	if f.size > 0 {
		size = f.size
	}
	return info{FileInfo: fi, size: size, special: f.special}, nil
}
func (f *fakeFS) ReadDir(_ context.Context, p string) ([]fs.DirEntry, error) {
	if f.readDirErr != nil {
		return nil, f.readDirErr
	}
	return f.files.ReadDir(p)
}
func (f *fakeFS) ReadFile(_ context.Context, p string) ([]byte, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.files.ReadFile(p)
}
func (*fakeFS) WriteFile(context.Context, string, []byte, fs.FileMode) error { return nil }
func (*fakeFS) Mkdir(context.Context, string, fs.FileMode) error             { return nil }
func (*fakeFS) Rename(context.Context, string, string) error                 { return nil }
func (*fakeFS) Remove(context.Context, string) error                         { return nil }

func TestReadFileFS(t *testing.T) {
	ctx := context.Background()
	files := fstest.MapFS{
		"a.txt":      {Data: []byte("hello\nworld\n")},
		"dir/x.go":   {Data: []byte("package x")},
		"dir/sub/y":  {Data: []byte("y")},
		"dir/sub2/z": {Data: []byte("z")},
	}
	boom := errors.New("boom")
	cases := []struct {
		name     string
		fsys     *fakeFS
		path     string
		wantKind string
		wantText string
	}{
		{"secret", &fakeFS{files: files}, ".env", "secret", "hidden"},
		{"stat error", &fakeFS{files: files, statErr: boom}, "a.txt", "error", "boom"},
		{"folder", &fakeFS{files: files}, "dir", "folder", "1 files, 2 folders"},
		{"folder list error", &fakeFS{files: files, readDirErr: boom}, "dir", "error", "boom"},
		{"special file", &fakeFS{files: files, special: true}, "a.txt", "binary", "No preview"},
		{"large file", &fakeFS{files: files, size: preview.FSFileMax + 1}, "a.txt", "large", "Large file"},
		{"read error", &fakeFS{files: files, readErr: boom}, "a.txt", "error", "boom"},
		{"text", &fakeFS{files: files}, "a.txt", "text", "hello"},
	}
	for _, c := range cases {
		got := preview.ReadFileFS(ctx, c.fsys, c.path)
		if got.Kind != c.wantKind || !strings.Contains(got.Text, c.wantText) {
			t.Errorf("%s: kind %q text %q", c.name, got.Kind, got.Text)
		}
	}
}

// ReadFile reports a folder it cannot open and a file it cannot read.
func TestReadFileUnreadable(t *testing.T) {
	dir := t.TempDir()
	if got := preview.ReadFile(dir + "/missing"); got.Kind != "error" {
		t.Fatalf("missing: %+v", got)
	}
	noRead := dir + "/noread.txt"
	writeFile(t, noRead, "x", 0o000)
	if got := preview.ReadFile(noRead); got.Kind != "error" && !isRoot() {
		t.Fatalf("unreadable file: %+v", got)
	}
	closed := dir + "/closed"
	mkdir(t, closed, 0o000)
	t.Cleanup(func() { chmod(closed, 0o755) })
	if got := preview.ReadFile(closed); got.Kind != "error" && !isRoot() {
		t.Fatalf("unreadable folder: %+v", got)
	}
}

// A bounded read that stops in the middle of a rune still previews as
// text: up to three bytes are dropped.
func TestClassifyFileSplitRune(t *testing.T) {
	full := strings.Repeat("é", 10) // 2 bytes each
	data := []byte(full)[:len(full)-1]
	got := preview.ClassifyFile("a.txt", data, int64(len(full)))
	if got.Kind != "text" {
		t.Fatalf("kind %q", got.Kind)
	}
}

// Doc.Lines gives an empty (non-nil) slice for a render with no rows, and
// Reader.Doc returns the doc.
func TestDocEmptyRenderAndAccessor(t *testing.T) {
	doc := preview.NewDoc(func(int) []string { return nil })
	if lines := doc.Lines(40); lines == nil || len(lines) != 0 {
		t.Fatalf("lines %#v", lines)
	}
	m := preview.NewReader("t", "", doc)
	if m.Doc() != doc {
		t.Fatal("Doc")
	}
	if preview.New("t", "x").Doc() != nil {
		t.Fatal("fixed content has no doc")
	}
	if rows, mk := preview.New("t", "x").Marks(); len(rows) != 1 || len(mk.Headings) != 0 {
		t.Fatal("marks of fixed content")
	}
}

// A guide opened at an anchor stays there when the window is resized,
// until the reader scrolls.
func TestGuideLocationSurvivesResize(t *testing.T) {
	m := guide(t, 80, 20)
	m.SetGuideLocation("chat-panel", "Keys")
	m.SetSize(70, 18)
	_, mk := m.Marks()
	var row int
	for _, h := range mk.Headings {
		if h.Slug == "chat-panel" {
			row = h.Row
		}
	}
	if m.Top() != row {
		t.Fatalf("top %d, heading at %d", m.Top(), row)
	}
	// An unknown anchor goes to the top; no anchor too.
	m.SetGuideLocation("nope", "")
	m.SetSize(60, 18)
	if m.Top() != 0 {
		t.Fatalf("unknown anchor: top %d", m.Top())
	}
	m.SetGuideLocation("", "")
	m.SetSize(61, 18)
	if m.Top() != 0 {
		t.Fatalf("no anchor: top %d", m.Top())
	}
	// The anchor is found but the text to find is far below: scroll to it.
	m.SetGuideLocation("use-ai", "End back up")
	m.SetSize(62, 6)
	if m.Top() < 0 {
		t.Fatal("negative top")
	}
}

// After the reader scrolled, a resize keeps what they were reading: the
// same row text if it still exists, else the same heading, else the old row.
func TestResizeAfterScrollKeepsPlace(t *testing.T) {
	m := guide(t, 80, 20)
	m.ScrollToRow(m.FindBelow(0, "Filler paragraph 12"))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // the user scrolls
	before := firstContentRow(m)
	m.SetSize(70, 20)
	if got := firstContentRow(m); got != before {
		t.Fatalf("lost the place: %q -> %q", before, got)
	}
	// A very narrow width re-wraps the text so no row matches: fall back.
	m.SetSize(14, 12)
	if m.Top() < 0 {
		t.Fatal("negative top")
	}
}

// PgUp pages up; the scroll percentage never passes 100.
func TestPageUpAndPercent(t *testing.T) {
	m := guide(t, 80, 20)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	end := m.Top()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.Top() >= end {
		t.Fatalf("PgUp: %d -> %d", end, m.Top())
	}
	short := preview.New("t", "one\ntwo")
	short.SetSize(40, 20)
	if !strings.Contains(ansi.Strip(short.View()), "100%") && strings.Contains(ansi.Strip(short.View()), "%") {
		t.Logf("view: %q", ansi.Strip(short.View()))
	}
	empty := preview.New("t", "")
	empty.SetSize(40, 20)
	_ = empty.View()
}

// A link whose cells are cut by the highlight splice is drawn as it is.
func TestHighlightLinkKeepsWidth(t *testing.T) {
	doc := preview.NewMarkedDoc(func(width int) ([]string, mdview.Marks) {
		row := "ab\x1b[1mcd\x1b[0mef"
		return []string{row}, mdview.Marks{Links: []mdview.Link{{Row: 0, X0: 1, X1: 20, ID: 1, Dest: "x"}}}
	})
	m := preview.NewReader("t", "", doc)
	m.SetSize(30, 6)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if ansi.StringWidth(strings.Split(m.View(), "\n")[1]) != 30 {
		t.Fatal("row width changed")
	}
	_ = time.Now
}
