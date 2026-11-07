package projtasks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = "# Tasks\n\n" +
	"Some text.\n" +
	"- [ ] **MOB-52** `project.tasks` — M\n" +
	"- [x] done one\n" +
	"  - [ ] nested child\n" +
	"* [X] star done\n" +
	"1. [ ] numbered\n" +
	"- [ ]   \n" +
	"- [?] not a box\n" +
	"- [ ]no space after box\n" +
	"- plain bullet\n" +
	"## Later ##\n" +
	"```md\n- [ ] inside a fence\n```\n" +
	"+ [ ] after the fence\n"

// Spec (MOB-52): Parse keeps each checklist line with its file, line,
// nearest heading, text, done state and depth; empty items, other boxes,
// plain bullets and fenced code are skipped.
func TestParse(t *testing.T) {
	items := Parse("TASKS.md", []byte(sample))
	want := []Item{
		{File: "TASKS.md", Line: 4, Group: "Tasks", Text: "**MOB-52** `project.tasks` — M"},
		{File: "TASKS.md", Line: 5, Group: "Tasks", Text: "done one", Done: true},
		{File: "TASKS.md", Line: 6, Group: "Tasks", Text: "nested child", Depth: 1},
		{File: "TASKS.md", Line: 7, Group: "Tasks", Text: "star done", Done: true},
		{File: "TASKS.md", Line: 8, Group: "Tasks", Text: "numbered"},
		{File: "TASKS.md", Line: 17, Group: "Later", Text: "after the fence"},
	}
	if len(items) != len(want) {
		t.Fatalf("items %+v", items)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, items[i], want[i])
		}
	}
	if id := items[0].ID(); id != "TASKS.md:4" {
		t.Errorf("id %q", id)
	}
}

// A long text and a long heading are cut on a rune edge with "…".
func TestParseClips(t *testing.T) {
	long := strings.Repeat("é", MaxText)
	items := Parse("T.md", []byte("# "+strings.Repeat("h", 500)+"\n- [ ] "+long+"\n"))
	if len(items) != 1 {
		t.Fatal(items)
	}
	if it := items[0]; len(it.Text) > MaxText || !strings.HasSuffix(it.Text, "…") || len(it.Group) > MaxGroup {
		t.Errorf("not clipped: %d %d", len(it.Text), len(it.Group))
	}
}

// At most MaxItems are kept.
func TestParseCap(t *testing.T) {
	data := strings.Repeat("- [ ] x\n", MaxItems+50)
	if n := len(Parse("T.md", []byte(data))); n != MaxItems {
		t.Errorf("items %d", n)
	}
}

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Spec (MOB-52): Read takes the known files in order (TASKS.md, TODO.md,
// docs/…); a project with none says ErrNone.
func TestRead(t *testing.T) {
	root := t.TempDir()
	if _, err := Read(root); !errors.Is(err, ErrNone) {
		t.Errorf("empty project: %v", err)
	}
	write(t, root, "docs/TODO.md", "- [ ] from docs\n")
	write(t, root, "TASKS.md", "- [x] from tasks\n")
	items, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].File != "TASKS.md" || !items[0].Done || items[1].File != "docs/TODO.md" {
		t.Errorf("items %+v", items)
	}
}

// Never-test (MOB-52): a symlinked checklist file or a symlinked docs
// folder is not read, so nothing outside the project leaks to the phone.
func TestReadSkipsSymlinks(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	write(t, out, "TASKS.md", "- [ ] secret outside\n")
	if err := os.Symlink(filepath.Join(out, "TASKS.md"), filepath.Join(root, "TASKS.md")); err != nil {
		t.Skip("no symlinks:", err)
	}
	if err := os.Symlink(out, filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}
	items, err := Read(root)
	if !errors.Is(err, ErrNone) || len(items) != 0 {
		t.Errorf("read through a symlink: %+v %v", items, err)
	}
}

// A file bigger than MaxFileBytes is read up to the cap, not refused.
func TestReadBigFile(t *testing.T) {
	root := t.TempDir()
	write(t, root, "TODO.md", "- [ ] first\n"+strings.Repeat("x", MaxFileBytes)+"\n- [ ] past the cap\n")
	items, err := Read(root)
	if err != nil || len(items) != 1 || items[0].Text != "first" {
		t.Errorf("items %+v err %v", items, err)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(sample))
	f.Add([]byte("```\n- [ ] x\n~~~\n- [x] y"))
	f.Fuzz(func(t *testing.T, data []byte) {
		items := Parse("F.md", data)
		if len(items) > MaxItems {
			t.Fatal("over the cap")
		}
		for _, it := range items {
			if it.Text == "" || len(it.Text) > MaxText || len(it.Group) > MaxGroup || it.Line < 1 || it.Depth < 0 {
				t.Fatalf("bad item %+v", it)
			}
		}
	})
}
