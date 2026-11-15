package ai

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// numbered is n lines "line 1" … "line n".
func numbered(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// A cut command output keeps its start and, bigger, its end (test
// failures and build errors are at the end), on whole lines, and says
// how many lines are cut in the middle.
func TestCapHeadTailKeepsBothEnds(t *testing.T) {
	s := numbered(5000)
	out := capHeadTail(s, 4000)
	if len(out) > 4000+100 {
		t.Fatalf("len %d, want about 4000", len(out))
	}
	if !strings.HasPrefix(out, "line 1\nline 2\n") || !strings.HasSuffix(out, "line 4999\nline 5000\n") {
		t.Fatalf("ends lost: %q … %q", out[:40], out[len(out)-40:])
	}
	head, tail, ok := strings.Cut(out, "… ")
	if !ok {
		t.Fatalf("no marker: %q", out)
	}
	marker, tail, _ := strings.Cut(tail, "\n")
	if len(tail) <= 2*len(head) {
		t.Errorf("the end should get the bigger share: head %d, tail %d", len(head), len(tail))
	}
	kept := strings.Count(head, "\n") + strings.Count(tail, "\n")
	if want := fmt.Sprintf("%d lines cut …", 5000-kept); marker != want {
		t.Errorf("marker %q, want %q", marker, want)
	}
	for _, l := range strings.Split(strings.TrimSuffix(head+tail, "\n"), "\n") {
		if !strings.HasPrefix(l, "line ") {
			t.Errorf("a line was cut in half: %q", l)
		}
	}
	if got := capHeadTail("short\n", 4000); got != "short\n" {
		t.Errorf("short output changed: %q", got)
	}
}

// One huge line (no line breaks) is cut too, never inside a UTF-8 rune.
func TestCapHeadTailOneLongLine(t *testing.T) {
	s := strings.Repeat("é", 10000)
	out := capHeadTail(s, 4000)
	if len(out) > 4100 || !utf8.ValidString(out) || !strings.Contains(out, "cut …") {
		t.Fatalf("len %d valid %v: %q", len(out), utf8.ValidString(out), out[:60])
	}
}

// run_command keeps the end of a long output, where the error is.
func TestRunCommandKeepsTheEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	w := Workspace{Root: t.TempDir()}
	out, err := wsTool(t, w, "run_command").Run(context.Background(),
		map[string]any{"command": `i=1; while [ $i -le 5000 ]; do echo "ok $i"; i=$((i+1)); done; echo "FAIL: boom at the end"; exit 3`})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "ok 1\n") || !strings.Contains(out, "FAIL: boom at the end\n[exit status 3]") ||
		!strings.Contains(out, "lines cut …") || len(out) > maxOutBytes+200 {
		t.Fatalf("len %d: %q … %q", len(out), out[:30], out[len(out)-80:])
	}
}

// A command on another host is cut the same way.
func TestHostRunKeepsTheEnd(t *testing.T) {
	x := &hostExec{answer: func([]string) (string, string, int) { return numbered(5000), "FAIL at the end\n", 1 }}
	w := Workspace{Root: "/app", Exec: x}
	out, err := w.hostRun(context.Background(), 10*time.Second, "sh", "-c", "make")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "line 1\n") || !strings.Contains(out, "FAIL at the end\n[exit status 1]") || !strings.Contains(out, "lines cut …") {
		t.Fatalf("%q … %q", out[:30], out[len(out)-60:])
	}
}
