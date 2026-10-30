package git

import (
	"fmt"
	"strings"
	"testing"
)

// statusV2Big is `git status --porcelain=v2 --branch -z` output with
// 2 200 changed files (modified, deleted, staged, renamed, untracked).
func statusV2Big() string {
	var b strings.Builder
	b.WriteString("# branch.oid 0123456789abcdef0123456789abcdef01234567\x00# branch.head main\x00# branch.ab +1 -2\x00")
	h := "0123456789abcdef0123456789abcdef01234567"
	for i := 0; i < 2000; i++ {
		switch i % 4 {
		case 0:
			fmt.Fprintf(&b, "1 .M N... 100644 100644 100644 %s %s pkg%03d/file%03d.go\x00", h, h, i/10, i)
		case 1:
			fmt.Fprintf(&b, "1 .D N... 100644 100644 000000 %s %s pkg%03d/file%03d.go\x00", h, h, i/10, i)
		case 2:
			fmt.Fprintf(&b, "1 M. N... 100644 100644 100644 %s %s pkg%03d/file%03d.go\x00", h, h, i/10, i)
		case 3:
			fmt.Fprintf(&b, "2 R. N... 100644 100644 100644 %s %s R100 pkg%03d/new%03d.go\x00pkg%03d/old%03d.go\x00", h, h, i/10, i, i/10, i)
		}
	}
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "? pkg%03d/new.go\x00", i)
	}
	return b.String()
}

func BenchmarkParseStatusV2(b *testing.B) {
	out := statusV2Big()
	b.ReportAllocs()
	for b.Loop() {
		if bs := parseStatusV2(out); len(bs.Files) != 2200 {
			b.Fatalf("%d files", len(bs.Files))
		}
	}
}

// Spec: every v2 record kind gives its v1 code and its full path (spaces
// kept, the old path of a rename skipped).
func TestParseStatusV2Records(t *testing.T) {
	h := "0123456789abcdef0123456789abcdef01234567"
	out := "# branch.head main\x00" +
		"1 .M N... 100644 100644 100644 " + h + " " + h + " a b.go\x00" +
		"1 MD N... 100644 100644 000000 " + h + " " + h + " c.go\x00" +
		"2 R. N... 100644 100644 100644 " + h + " " + h + " R90 new name.go\x00old name.go\x00" +
		"u UU N... 100644 100644 100644 100644 " + h + " " + h + " " + h + " d.go\x00" +
		"1 A. N... 000000 100644 100644 " + h + " " + h + " e.go\x00" +
		"? f g.txt\x00"
	got := parseStatusV2(out).Files
	want := []FileStatus{{" M", "a b.go"}, {"MD", "c.go"}, {"R ", "new name.go"}, {"UU", "d.go"}, {"A ", "e.go"}, {"??", "f g.txt"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("files\n got %q\nwant %q", got, want)
	}
	if n := len(parseStatusV2(statusV2Big()).Files); n != 2200 {
		t.Fatalf("%d files, want 2200", n)
	}
}
