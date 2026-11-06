package git

import (
	"os"
	"path/filepath"
	"testing"
)

// GetWorktreeStatus counts changed and new files and reads ahead / behind
// against the upstream in one git call.
func TestGetWorktreeStatus(t *testing.T) {
	r := committedRepo(t)
	st, err := GetWorktreeStatus(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Dirty != 0 || st.Upstream || st.Ahead != 0 || st.Behind != 0 {
		t.Errorf("clean: %+v", st)
	}
	// An upstream two commits back and one commit the upstream has but
	// this branch lacks.
	r.git(t, "branch", "up")
	r.write(t, "u.txt", "u\n")
	r.git(t, "checkout", "-q", "up")
	r.git(t, "add", "u.txt")
	r.git(t, "commit", "-q", "-m", "upstream only")
	r.git(t, "checkout", "-q", "main")
	for _, n := range []string{"c1", "c2"} {
		r.write(t, n+".txt", n)
		r.git(t, "add", n+".txt")
		r.git(t, "commit", "-q", "-m", n)
	}
	r.git(t, "branch", "--set-upstream-to=up")
	r.write(t, "a.txt", "changed\n")
	r.write(t, "new file.txt", "new\n")
	if err := os.Remove(filepath.Join(r.dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
	st, err = GetWorktreeStatus(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Dirty != 3 || !st.Upstream || st.Ahead != 2 || st.Behind != 1 {
		t.Errorf("got %+v, want 3 dirty, 2 ahead, 1 behind", st)
	}
	if _, err := GetWorktreeStatus(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing folder had a status")
	}
}

func TestParseWorktreeStatus(t *testing.T) {
	out := "# branch.oid abc\n# branch.head main\n# branch.upstream origin/main\n# branch.ab +4 -7\n" +
		"1 .M N... 100644 100644 100644 a a a.txt\n2 R. N... 100644 100644 100644 a a R100 new\told\n" +
		"u UU N... 1 1 1 1 a a a c.txt\n? n.txt\n"
	st := parseWorktreeStatus(out)
	if st.Dirty != 4 || st.Ahead != 4 || st.Behind != 7 || !st.Upstream {
		t.Errorf("got %+v", st)
	}
	if st := parseWorktreeStatus("# branch.head main\n"); st.Upstream || st.Dirty != 0 {
		t.Errorf("no upstream: %+v", st)
	}
}
