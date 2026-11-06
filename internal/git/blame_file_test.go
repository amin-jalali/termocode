package git

import (
	"strings"
	"testing"
)

// blameRepo has f.txt changed in three commits: "one" wrote a/b/c,
// "two" changed line 2, "three" changed line 1.
func blameRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	for _, step := range []struct{ body, msg string }{
		{"a\nb\nc\n", "one"},
		{"a\nB\nc\n", "two"},
		{"A\nB\nc\n", "three"},
	} {
		r.write(t, "f.txt", step.body)
		r.git(t, "add", "f.txt")
		r.git(t, "commit", "-q", "-m", step.msg)
	}
	return r
}

// GITX-3/4: BlameFile blames every line and keeps the full hash and the
// commit before the change (for "blame before this change").
func TestBlameFileWholeFile(t *testing.T) {
	r := blameRepo(t)
	ls, err := BlameFile(r.dir, "f.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 3 {
		t.Fatalf("lines %d", len(ls))
	}
	var subj []string
	for _, l := range ls {
		subj = append(subj, l.Subject)
	}
	if got := strings.Join(subj, ","); got != "three,two,one" {
		t.Errorf("subjects %s", got)
	}
	two := strings.TrimSpace(r.git(t, "rev-parse", "HEAD~1"))
	one := strings.TrimSpace(r.git(t, "rev-parse", "HEAD~2"))
	if ls[1].Full != two || ls[1].Hash != two[:7] {
		t.Errorf("full %q hash %q want %s", ls[1].Full, ls[1].Hash, two)
	}
	if ls[1].PrevHash != one || ls[1].PrevPath != "f.txt" || ls[1].OrigLine != 2 {
		t.Errorf("prev %q %q line %d", ls[1].PrevHash, ls[1].PrevPath, ls[1].OrigLine)
	}
	if ls[2].PrevHash != "" {
		t.Errorf("the first commit has a previous: %q", ls[2].PrevHash)
	}
}

// GITX-4 (D6): blame at an older revision shows that revision's lines.
func TestBlameFileAtRevision(t *testing.T) {
	r := blameRepo(t)
	ls, err := BlameFile(r.dir, "f.txt", BlameOptions{Rev: "HEAD~2"})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range ls {
		if l.Subject != "one" {
			t.Errorf("line %d from %q", l.Line, l.Subject)
		}
	}
}

// GITX-4 (D7): -w ignores a whitespace-only change.
func TestBlameFileIgnoreWhitespace(t *testing.T) {
	r := blameRepo(t)
	r.write(t, "f.txt", "A\nB\n  c\n")
	r.git(t, "commit", "-q", "-am", "indent")
	ls, err := BlameFile(r.dir, "f.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ls[2].Subject != "indent" {
		t.Errorf("plain blame %q", ls[2].Subject)
	}
	ls, err = BlameFile(r.dir, "f.txt", BlameOptions{IgnoreWhitespace: true})
	if err != nil {
		t.Fatal(err)
	}
	if ls[2].Subject != "one" {
		t.Errorf("-w blame %q", ls[2].Subject)
	}
}

// GITX-4 (D16): commits named in .git-blame-ignore-revs are skipped.
func TestBlameFileIgnoreRevsFile(t *testing.T) {
	r := blameRepo(t)
	three := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	r.write(t, ".git-blame-ignore-revs", "# formatting\n"+three+"\n")
	ls, err := BlameFile(r.dir, "f.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ls[0].Subject == "three" {
		t.Errorf("ignored rev still blamed: %+v", ls[0])
	}
}

// GITX-3: a changed, unsaved-to-git line is "uncommitted".
func TestBlameFileUncommitted(t *testing.T) {
	r := blameRepo(t)
	r.write(t, "f.txt", "A\nB\nnew\n")
	ls, err := BlameFile(r.dir, "f.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ls[2].Hash != "uncommitted" || ls[2].Full != "" {
		t.Errorf("line 3 %+v", ls[2])
	}
}

// GITX-5 (D12): LineHistory lists the commits that changed lines
// start..end, newest first.
func TestLineHistory(t *testing.T) {
	r := blameRepo(t)
	cs, err := LineHistory(r.dir, "f.txt", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	var subj []string
	for _, c := range cs {
		subj = append(subj, c.Subject)
	}
	if got := strings.Join(subj, ","); got != "two,one" {
		t.Errorf("history %s", got)
	}
	if cs[0].Author == "" || cs[0].When == 0 || len(cs[0].Hash) < 7 {
		t.Errorf("head %+v", cs[0])
	}
	if _, err := LineHistory(r.dir, "f.txt", 9, 12); err == nil {
		t.Error("no error for lines past the end")
	}
}
