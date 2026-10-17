package git

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// historyRepo makes a repo with n linear commits on main (a tag every
// 100) in one git fast-import run, so a big history costs well under a
// second to set up.
func historyRepo(tb testing.TB, n int) string {
	tb.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		tb.Skip("git not on PATH")
	}
	dir := tb.TempDir()
	run := func(stdin string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			tb.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("", "init", "-q")
	run("", "symbolic-ref", "HEAD", "refs/heads/main")
	var b strings.Builder
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	for i := 1; i <= n; i++ {
		msg := fmt.Sprintf("feat(part%d): change number %d\n", i%7, i)
		body := fmt.Sprintf("line %d\n", i)
		fmt.Fprintf(&b, "commit refs/heads/main\nmark :%d\n", i)
		fmt.Fprintf(&b, "author Test <test@example.com> %d +0000\ncommitter Test <test@example.com> %d +0000\n", start+int64(i)*600, start+int64(i)*600)
		fmt.Fprintf(&b, "data %d\n%s", len(msg), msg)
		if i > 1 {
			fmt.Fprintf(&b, "from :%d\n", i-1)
		}
		fmt.Fprintf(&b, "M 644 inline f%d.txt\ndata %d\n%s\n", i%50, len(body), body)
		if i%100 == 0 {
			fmt.Fprintf(&b, "reset refs/tags/v%d\nfrom :%d\n\n", i/100, i)
		}
	}
	run(b.String(), "fast-import", "--quiet")
	return dir
}

// Budget (scm-graph design §4.6): one GRAPH page (200 commits) parses in
// ≤ 30 ms; measured on a 2 000-commit repo.
func BenchmarkCommits200(b *testing.B) {
	dir := historyRepo(b, 2000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cs, more, err := Commits(dir, 0, 200)
		if err != nil || len(cs) != 200 || !more {
			b.Fatalf("Commits: %d rows, more=%v, err=%v", len(cs), more, err)
		}
	}
}

// Perf guard: a GRAPH page from a 2 000-commit repo, and the next page
// (--skip), each well inside a frame budget. The limit is loose (the
// budget is 30 ms) so a busy CI box or -race does not flake; it catches
// a page that went quadratic or started walking the whole history.
func TestCommitsPageFast(t *testing.T) {
	dir := historyRepo(t, 2000)
	best := time.Hour
	for i := 0; i < 3; i++ {
		t0 := time.Now()
		cs, more, err := Commits(dir, 200, 200)
		if d := time.Since(t0); d < best {
			best = d
		}
		if err != nil || len(cs) != 200 || !more {
			t.Fatalf("page 2: %d rows, more=%v, err=%v", len(cs), more, err)
		}
	}
	if best > 300*time.Millisecond {
		t.Errorf("a 200-commit page took %v (budget 30 ms, guard 300 ms)", best)
	}
}
