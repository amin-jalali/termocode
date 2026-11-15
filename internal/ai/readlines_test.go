package ai

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// lines is n lines "l1" … "ln", each ending in a newline.
func lines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "l%d\n", i)
	}
	return b.String()
}

func readIn(t *testing.T, body string, args map[string]any) (string, error) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.txt"), body)
	if args == nil {
		args = map[string]any{}
	}
	args["path"] = "x.txt"
	return runTool(t, Workspace{Root: dir}, "read_file", args)
}

// read_file numbers each line ("N|") and says how many lines the file
// has, so the model can ask for a part and name lines.
func TestReadFileNumbersLines(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"lines", "a\nb\nc\n", "x.txt (3 lines)\n1|a\n2|b\n3|c\n"},
		{"no last newline", "a\nb", "x.txt (2 lines)\n1|a\n2|b\n"},
		{"crlf", "a\r\nb\r\n", "x.txt (2 lines)\n1|a\n2|b\n"},
		{"empty", "", "x.txt (empty)\n"},
		{"blank lines", "a\n\n\nb\n", "x.txt (4 lines)\n1|a\n2|\n3|\n4|b\n"},
	} {
		got, err := readIn(t, c.body, nil)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q %v, want %q", c.name, got, err, c.want)
		}
	}
}

// offset (first line, from 1) and limit (how many lines) read a part;
// the header names the part, the end says how to read on.
func TestReadFileOffsetLimit(t *testing.T) {
	got, err := readIn(t, lines(10), map[string]any{"offset": 4, "limit": 3})
	want := "x.txt: lines 4-6 of 10\n4|l4\n5|l5\n6|l6\n… 4 more lines: read_file with offset=7\n"
	if err != nil || got != want {
		t.Fatalf("got %q %v, want %q", got, err, want)
	}
	if got, _ := readIn(t, lines(10), map[string]any{"offset": 8}); got != "x.txt: lines 8-10 of 10\n8|l8\n9|l9\n10|l10\n" {
		t.Errorf("to the end: %q", got)
	}
	if _, err := readIn(t, lines(10), map[string]any{"offset": 11}); err == nil || !strings.Contains(err.Error(), "10 lines") {
		t.Errorf("past the end: %v", err)
	}
}

// With no limit a long file is read in a window of 2000 lines.
func TestReadFileDefaultWindow(t *testing.T) {
	got, err := readIn(t, lines(3000), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "x.txt: lines 1-2000 of 3000\n1|l1\n") ||
		!strings.HasSuffix(got, "2000|l2000\n… 1000 more lines: read_file with offset=2001\n") {
		t.Fatalf("%q … %q", got[:40], got[len(got)-80:])
	}
}

// A window bigger than the byte cap stops early on a whole line and
// says where to go on.
func TestReadFileByteCap(t *testing.T) {
	long := strings.Repeat("y", 1000)
	var b strings.Builder
	for range 500 {
		b.WriteString(long + "\n")
	}
	got, err := readIn(t, b.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > maxReadBytes+200 || !strings.Contains(got, "more lines: read_file with offset=") || !strings.HasPrefix(got, "x.txt: lines 1-") {
		t.Fatalf("len %d: %q … %q", len(got), got[:40], got[len(got)-80:])
	}
}

// A container workspace reads in the same form.
func TestReadFileNumbersLinesOnHost(t *testing.T) {
	w, _, _ := hostWorkspace(t)
	got, err := runHostTool(t, w, "read_file", map[string]any{"path": "main.go", "offset": 2})
	if err != nil || got != "main.go: lines 2-2 of 2\n2|// needle\n" {
		t.Fatalf("got %q %v", got, err)
	}
}

// The schema has offset and limit.
func TestReadFileSchemaHasOffsetLimit(t *testing.T) {
	tl := wsTool(t, Workspace{Root: t.TempDir()}, "read_file")
	for _, k := range []string{"offset", "limit"} {
		if _, ok := tl.Params[k]; !ok {
			t.Errorf("no %s param", k)
		}
	}
}

// edit_file takes the plain text. When the model copies the "N|"
// numbers of read_file into old_text (and new_text), they are taken
// off; when the text is still not there, the error says not to copy
// them.
func TestEditFileStripsLineNumbers(t *testing.T) {
	got, err := ApplyEdit("a\nb\nc\n", "2|b\n3|c", "2|B\n3|C", false)
	if err != nil || got != "a\nB\nC\n" {
		t.Fatalf("prefixed both: %q %v", got, err)
	}
	got, err = ApplyEdit("a\nb\nc\n", "2|b\n3|c", "B\nC", false)
	if err != nil || got != "a\nB\nC\n" {
		t.Fatalf("prefixed old only: %q %v", got, err)
	}
	if _, err := ApplyEdit("a\nb\nc\n", "2|x", "y", false); err == nil || !strings.Contains(err.Error(), "line numbers") {
		t.Fatalf("not found: %v", err)
	}
	if _, err := ApplyEdit("a\nb\nc\n", "2|b\n3|c", "2|B\nC", false); err == nil || !strings.Contains(err.Error(), "line numbers") {
		t.Fatalf("mixed new_text: %v", err)
	}
	// A file whose text really has "2|b" is edited as it is.
	if got, err := ApplyEdit("x\n2|b\n", "2|b", "2|c", false); err != nil || got != "x\n2|c\n" {
		t.Fatalf("real text: %q %v", got, err)
	}
}
