package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bisectRepo: 8 commits c1..c8; from c5 on, bug.txt says "bug".
func bisectRepo(t *testing.T) (*gitTestRepo, []string) {
	t.Helper()
	r := newGitTestRepo(t)
	var hs []string
	for i := 1; i <= 8; i++ {
		v := "ok"
		if i >= 5 {
			v = "bug"
		}
		r.write(t, "bug.txt", v+"\n")
		r.write(t, fmt.Sprintf("f%d.txt", i), "x\n")
		r.git(t, "add", "-A")
		r.git(t, "commit", "-q", "-m", fmt.Sprintf("c%d", i))
		hs = append(hs, strings.TrimSpace(r.git(t, "rev-parse", "HEAD")))
	}
	return r, hs
}

func bugNow(t *testing.T, r *gitTestRepo) bool {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(r.dir, "bug.txt"))
	return strings.TrimSpace(string(b)) == "bug"
}

// GITX-19 (M12): start with a bad and a good commit, answer good / bad
// until git names the first bad commit, then reset to where we were.
func TestBisectSteps(t *testing.T) {
	r, hs := bisectRepo(t)
	if BisectStatus(r.dir).Active {
		t.Fatal("active before start")
	}
	st, err := BisectStart(r.dir, "HEAD", hs[0])
	if err != nil || !st.Active || st.Current == "" || st.Done {
		t.Fatalf("start %+v %v", st, err)
	}
	for i := 0; i < 10 && !st.Done; i++ {
		v := "good"
		if bugNow(t, r) {
			v = "bad"
		}
		if st, err = BisectMark(r.dir, v); err != nil {
			t.Fatal(err)
		}
	}
	if !st.Done || st.FirstBad != hs[4] || st.FirstBadSubject != "c5" {
		t.Fatalf("result %+v", st)
	}
	if !BisectStatus(r.dir).Active {
		t.Error("not active before reset")
	}
	if err := BisectReset(r.dir); err != nil {
		t.Fatal(err)
	}
	if BisectStatus(r.dir).Active || strings.TrimSpace(r.git(t, "rev-parse", "HEAD")) != hs[7] {
		t.Error("reset did not go back")
	}
	if _, err := BisectMark(r.dir, "maybe"); err == nil {
		t.Error("a verdict that is not good / bad / skip")
	}
	if _, err := BisectStart(r.dir, "--all", hs[0]); err == nil {
		t.Error("an option as commit")
	}
}

// Skip moves to another commit; the status after a restart of termocode
// still knows the bisect and the commit under test.
func TestBisectSkipAndStatus(t *testing.T) {
	r, hs := bisectRepo(t)
	st, err := BisectStart(r.dir, hs[7], hs[0])
	if err != nil {
		t.Fatal(err)
	}
	first := st.Current
	st, err = BisectMark(r.dir, "skip")
	if err != nil || st.Current == first || !st.Active {
		t.Errorf("skip %+v %v", st, err)
	}
	now := BisectStatus(r.dir)
	if !now.Active || now.Current != st.Current {
		t.Errorf("status %+v want current %s", now, st.Current)
	}
	_ = BisectReset(r.dir)
}

// GITX-19 (M13): bisect run with a command finds the first bad commit
// by itself and leaves the bisect open for a look; the reset ends it.
func TestBisectRun(t *testing.T) {
	r, hs := bisectRepo(t)
	if _, err := BisectStart(r.dir, "HEAD", hs[0]); err != nil {
		t.Fatal(err)
	}
	st, err := BisectRun(r.dir, r.dir, "grep -q ok bug.txt")
	if err != nil || !st.Done || st.FirstBad != hs[4] {
		t.Fatalf("run %+v %v", st, err)
	}
	if err := BisectReset(r.dir); err != nil {
		t.Fatal(err)
	}
}

// GITX-24: the refs to compare (local and remote branches, tags), the
// merge-base, and the files that differ between the working tree and a
// ref.
func TestCompareHelpers(t *testing.T) {
	r, hs := bisectRepo(t)
	r.git(t, "branch", "old", hs[2])
	r.git(t, "tag", "v1", hs[1])
	refs, err := RefNames(r.dir)
	if err != nil || strings.Join(refs, ",") != "main,old,v1" {
		t.Errorf("refs %v %v", refs, err)
	}
	r.git(t, "checkout", "-q", "-b", "topic", hs[3])
	r.write(t, "t.txt", "t\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "t")
	if mb, err := MergeBase(r.dir, "topic", "main"); err != nil || mb != hs[3] {
		t.Errorf("merge-base %q %v", mb, err)
	}
	r.write(t, "f1.txt", "changed\n")
	r.write(t, "new.txt", "untracked files are not compared\n")
	files, err := WorkTreeDiffFiles(r.dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(files, ","); got != "bug.txt,f1.txt,f5.txt,f6.txt,f7.txt,f8.txt,t.txt" {
		t.Errorf("files %s", got)
	}
	if _, err := MergeBase(r.dir, "--all", "main"); err == nil {
		t.Error("an option as ref")
	}
}
