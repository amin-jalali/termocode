package suggest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanCommentsFindsMarkersOnlyInComments(t *testing.T) {
	src := `package a
// TODO: cache the parsed file
x := "TODO in a string is not a task"
var todoCount = 1 // FIXME(amin) off by one
# HACK - python style
/* XXX keep in sync with b.c */
<!-- OPTIMIZE: lazy load -->
// see the TODO list (not a task: the marker is not first)
//TODO
`
	got := ScanComments("a.go", []byte(src))
	want := []struct {
		kind Kind
		line int
		text string
	}{
		{KindTodo, 2, "cache the parsed file"},
		{KindFixme, 4, "off by one"},
		{KindHack, 5, "python style"},
		{KindXXX, 6, "keep in sync with b.c"},
		{KindPerf, 7, "lazy load"},
		{KindTodo, 9, "(no text)"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d tasks, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Kind != w.kind || g.Line != w.line || g.Text != w.text || g.File != "a.go" {
			t.Errorf("task %d = %+v, want %v line %d %q", i, g, w.kind, w.line, w.text)
		}
	}
}

func TestScanGoFindsSlowLoops(t *testing.T) {
	src := `package a

import ("regexp"; "fmt")

func f(xs [][][]int, names []string) string {
	s := ""
	for _, a := range xs {
		for _, b := range a {
			for range b {
			}
		}
	}
	for _, n := range names {
		re := regexp.MustCompile(n)
		_ = re
		defer fmt.Println(n)
		s += fmt.Sprintf("%s,", n)
		s += "x"
	}
	n := 0
	for range names {
		n += 1
		go func() { defer fmt.Println() }()
	}
	return s
}
`
	got := ScanGo("a.go", []byte(src))
	lines := map[int]string{}
	for _, tk := range got {
		if tk.Kind != KindSlow {
			t.Errorf("kind %v", tk.Kind)
		}
		lines[tk.Line] = tk.Text
	}
	for line, want := range map[int]string{9: "nested 3 deep", 14: "regexp compiled", 16: "defer inside a loop", 17: "strings.Builder", 18: "strings.Builder"} {
		if !strings.Contains(lines[line], want) {
			t.Errorf("line %d = %q, want %q", line, lines[line], want)
		}
	}
	if len(got) != 5 {
		t.Errorf("got %d tasks, want 5 (n += 1 and the defer in a func literal are fine): %+v", len(got), got)
	}
	if ScanGo("b.go", []byte("package (")) != nil {
		t.Error("a file that does not parse gives tasks")
	}
}

func TestScanSortsSkipsAndLimits(t *testing.T) {
	root := t.TempDir()
	write(t, root, "b.py", "# TODO later\n")
	write(t, root, "a/c.go", "package a\n// FIXME now\n")
	write(t, root, "node_modules/x.js", "// FIXME not ours\n")
	write(t, root, ".hidden/y.go", "// FIXME hidden\n")
	write(t, root, "app.min.js", "// FIXME minified\n")
	write(t, root, "bin.dat", "\x00// FIXME binary\n")
	res, err := Scan(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range res.Tasks {
		got = append(got, string(tk.Kind)+" "+tk.Where())
	}
	if strings.Join(got, "|") != "FIXME a/c.go:2|TODO b.py:1" {
		t.Fatalf("tasks %q", got)
	}
	if res.Truncated {
		t.Error("truncated without a limit")
	}
	res, _ = Scan(root, Options{MaxTasks: 1})
	if len(res.Tasks) != 1 || !res.Truncated || res.Tasks[0].Kind != KindFixme {
		t.Errorf("limit: %+v", res)
	}
	if _, err := Scan(filepath.Join(root, "missing"), Options{}); err == nil {
		t.Error("a missing root gives no error")
	}
}
