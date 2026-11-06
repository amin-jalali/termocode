package git

import (
	"strings"
	"testing"
)

// peekRepo has f.txt with ten lines committed.
func peekRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	var b strings.Builder
	for i := 1; i <= 10; i++ {
		b.WriteString("l" + itoaTest(i) + "\n")
	}
	r.write(t, "f.txt", b.String())
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "first")
	return r
}

func itoaTest(n int) string {
	if n >= 10 {
		return string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return string(rune('0' + n))
}

// GITX-21: the peek finds the change under a line (the same hunk Stage
// and Revert use), says which of how many it is and lists every start
// for Next; a staged-only change is marked Staged; a clean line is an
// error.
func TestPeekHunk(t *testing.T) {
	r := peekRepo(t)
	r.write(t, "f.txt", "l1\nL2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\n")
	p, err := PeekHunk(r.dir, "f.txt", 2)
	if err != nil {
		t.Fatal(err)
	}
	// Starts are the first changed line of each hunk (where the mark is).
	if p.Staged || p.Index != 1 || p.Count != 2 || len(p.Starts) != 2 || p.Starts[0] != 2 || p.Starts[1] != 11 {
		t.Fatalf("peek = %+v", p)
	}
	body := strings.Join(p.Lines, "\n")
	if !strings.Contains(body, "-l2") || !strings.Contains(body, "+L2") || !strings.HasPrefix(p.Header, "@@ ") {
		t.Fatalf("body:\n%s\nheader %q", body, p.Header)
	}
	p2, err := PeekHunk(r.dir, "f.txt", 11)
	if err != nil || p2.Index != 2 || !strings.Contains(strings.Join(p2.Lines, "\n"), "+l11") {
		t.Fatalf("second hunk = %+v %v", p2, err)
	}
	// Stage the first: the work tree has only the second, the index the first.
	if err := StageHunk(r.dir, "f.txt", 2); err != nil {
		t.Fatal(err)
	}
	p3, err := PeekHunk(r.dir, "f.txt", 2)
	if err != nil || !p3.Staged || !strings.Contains(strings.Join(p3.Lines, "\n"), "+L2") {
		t.Fatalf("staged peek = %+v %v", p3, err)
	}
	r.git(t, "checkout", "--", "f.txt")
	r.git(t, "reset", "-q")
	r.git(t, "checkout", "--", "f.txt")
	if _, err := PeekHunk(r.dir, "f.txt", 5); err == nil {
		t.Fatal("a clean line has no change")
	}
}

// A pure deletion is found on the line its mark is drawn on.
func TestPeekHunkDeletion(t *testing.T) {
	r := peekRepo(t)
	r.write(t, "f.txt", "l1\nl2\nl3\nl4\nl6\nl7\nl8\nl9\nl10\n")
	p, err := PeekHunk(r.dir, "f.txt", 4)
	if err != nil || !strings.Contains(strings.Join(p.Lines, "\n"), "-l5") || p.Starts[0] != 4 {
		t.Fatalf("deletion = %+v %v", p, err)
	}
}
