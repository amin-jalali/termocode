package git

import "testing"

// BlameRange blames a line range in one git call: each line gets the
// short sha, author, date and subject; lines not committed yet are
// "uncommitted"; a range past the end is clamped by git.
func TestBlameRange(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "x.txt", "alpha\nbeta\n")
	r.git(t, "add", "x.txt")
	r.git(t, "commit", "-q", "-m", "first two")
	r.write(t, "x.txt", "alpha\nbeta\ngamma\n")
	r.git(t, "commit", "-q", "-am", "add gamma")
	r.write(t, "x.txt", "alpha\nBETA\ngamma\n")

	lines, err := BlameRange(r.dir, "x.txt", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines: %#v", len(lines), lines)
	}
	if l := lines[0]; l.Line != 1 || l.Author != "Test" || l.Subject != "first two" || len(l.Hash) != 7 || len(l.Date) != 10 {
		t.Errorf("line 1: %#v", l)
	}
	if l := lines[1]; l.Line != 2 || l.Hash != "uncommitted" {
		t.Errorf("line 2: %#v", l)
	}
	if l := lines[2]; l.Line != 3 || l.Subject != "add gamma" || l.Hash == lines[0].Hash {
		t.Errorf("line 3: %#v", l)
	}

	one, err := BlameRange(r.dir, "x.txt", 3, 3)
	if err != nil || len(one) != 1 || one[0].Line != 3 || one[0].Subject != "add gamma" {
		t.Errorf("range 3,3: %#v %v", one, err)
	}
	if past, err := BlameRange(r.dir, "x.txt", 2, 50); err != nil || len(past) != 2 {
		t.Errorf("range 2,50: %#v %v", past, err)
	}
	if _, err := BlameRange(r.dir, "x.txt", 9, 0); err == nil {
		t.Error("start past the end: want an error")
	}
}
