package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// scriptRunner answers git commands of a container repo from a table of
// "joined argv prefix" → stdout; a command without an entry exits 1.
type scriptRunner struct {
	mu   sync.Mutex
	out  map[string]string
	seen []string
}

func (s *scriptRunner) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line := strings.Join(argv, " ")
	s.seen = append(s.seen, line)
	for p, o := range s.out {
		if strings.HasPrefix(line, p) {
			return o, "", 0, nil
		}
	}
	return "", "fatal: no", 1, nil
}

func TestReviewDiffsAndDefaultBase(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "a.txt", "one\nTWO\nthree\n")
	d, cut, err := WorkDiff(r.dir, 1<<20)
	if err != nil || cut || !strings.Contains(d, "+TWO") {
		t.Fatalf("WorkDiff = %q, %v, %v", d, cut, err)
	}
	if b, err := DefaultBase(r.dir); err != nil || b != "main" {
		t.Fatalf("DefaultBase = %q, %v", b, err)
	}
	r.git(t, "checkout", "-q", "-b", "topic")
	r.git(t, "commit", "-qam", "topic change")
	d, _, err = BranchDiff(r.dir, "main", 1<<20)
	if err != nil || !strings.Contains(d, "+TWO") {
		t.Fatalf("BranchDiff = %q, %v", d, err)
	}
	empty := newGitTestRepo(t)
	if _, err := DefaultBase(empty.dir); err == nil {
		t.Error("DefaultBase without main or master: want error")
	}
	// origin/HEAD wins when set.
	r.git(t, "update-ref", "refs/remotes/origin/trunk", "HEAD")
	r.git(t, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	if b, _ := DefaultBase(r.dir); b != "origin/trunk" {
		t.Errorf("DefaultBase with origin/HEAD = %q", b)
	}
}

func TestTagsAndSubjectsSince(t *testing.T) {
	r := committedRepo(t)
	if LatestTag(r.dir) != "" {
		t.Error("LatestTag without a tag should be empty")
	}
	r.git(t, "tag", "v1")
	r.write(t, "c.txt", "c\n")
	r.git(t, "add", "c.txt")
	r.git(t, "commit", "-qm", "add c")
	if got := LatestTag(r.dir); got != "v1" {
		t.Errorf("LatestTag = %q", got)
	}
	got, err := SubjectsSince(r.dir, "v1", 10)
	if err != nil || len(got) != 1 || got[0] != "add c" {
		t.Errorf("SubjectsSince(v1) = %v, %v", got, err)
	}
	all, err := SubjectsSince(r.dir, "", 10)
	if err != nil || len(all) != 2 {
		t.Errorf("SubjectsSince(all) = %v, %v", all, err)
	}
	if _, err := SubjectsSince(r.dir, "nope", 10); err == nil {
		t.Error("unknown from: want error")
	}
	if _, err := SubjectsSince(t.TempDir(), "", 10); err == nil {
		t.Error("outside a repo: want error")
	}
}

func TestCommitOfAndRemoteBranchLists(t *testing.T) {
	r := committedRepo(t)
	h := headOf(t, r)
	if got, err := CommitOf(r.dir, "main"); err != nil || got != h {
		t.Errorf("CommitOf(main) = %q, %v", got, err)
	}
	if _, err := CommitOf(r.dir, "nope"); err == nil || !strings.Contains(err.Error(), "no commit") {
		t.Errorf("CommitOf(nope) = %v", err)
	}
	if _, err := CommitOf(r.dir, "--upload-pack=x"); err == nil {
		t.Error("an option as ref must be refused")
	}
	if RemoteTrackingBranches(r.dir) != nil {
		t.Error("no remotes: want nil")
	}
	r.git(t, "update-ref", "refs/remotes/origin/dev", h)
	r.git(t, "update-ref", "refs/remotes/origin/main", h)
	r.git(t, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	for _, got := range [][]string{RemoteTrackingBranches(r.dir), mustList(t, r.dir)} {
		if strings.Join(got, ",") != "origin/dev,origin/main" {
			t.Errorf("remote branches = %v", got)
		}
	}
	if RemoteTrackingBranches(t.TempDir()) != nil {
		t.Error("outside a repo: want nil")
	}
	if _, err := RemoteBranches(t.TempDir()); err == nil {
		t.Error("RemoteBranches outside a repo: want error")
	}
}

func mustList(t *testing.T, dir string) []string {
	t.Helper()
	l, err := RemoteBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestAgoAndShortHash(t *testing.T) {
	if Ago(0) == "" {
		t.Error("Ago(0) should read as an old date")
	}
	if got := (ReflogEntry{Hash: "0123456789"}).Short(); got != "0123456" {
		t.Errorf("Short = %q", got)
	}
	if shortHash("abc") != "abc" {
		t.Error("a short hash stays as it is")
	}
}

func TestUndoPlanNoop(t *testing.T) {
	if !(UndoPlan{Target: "a", From: "a"}).Noop() || (UndoPlan{Target: "a", From: "b"}).Noop() {
		t.Error("Noop compares Target with From")
	}
	if !(UndoPlan{Switch: "x", Branch: "x"}).Noop() || (UndoPlan{Switch: "x", Branch: "y"}).Noop() {
		t.Error("Noop with Switch compares it with Branch")
	}
	if (&BusyError{Op: "rebase"}).Error() != "a rebase is in progress" {
		t.Error("BusyError text")
	}
}

func TestPlanMoveToAndRedoGuards(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	p, err := PlanMoveTo(r.dir, ReflogEntry{Hash: h[0]})
	if err != nil || p.Target != h[0] || p.TargetSubject != "c1" || p.From != h[2] {
		t.Fatalf("PlanMoveTo = %+v, %v", p, err)
	}
	if _, err := PlanMoveTo(t.TempDir(), ReflogEntry{Hash: h[0]}); err != ErrNothingToUndo {
		t.Errorf("PlanMoveTo outside a repo = %v", err)
	}
	// Redo refuses when HEAD is not where the undo left it.
	if err := RunRedo(r.dir, p, h[0]); err != ErrUndoStale {
		t.Errorf("RunRedo stale = %v", err)
	}
	// An undo that switched branches cannot be redone from a detached HEAD.
	r.git(t, "checkout", "-q", "-b", "x")
	if err := RunRedo(r.dir, UndoPlan{Switch: "x", Branch: ""}, h[2]); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Errorf("RunRedo detached = %v", err)
	}
	// A rebase in progress blocks both.
	r.git(t, "update-ref", "ORIG_HEAD", h[0])
	if err := os.MkdirAll(filepath.Join(r.dir, ".git", "rebase-merge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanMoveTo(r.dir, ReflogEntry{Hash: h[0]}); err == nil {
		t.Error("PlanMoveTo during a rebase: want BusyError")
	}
	if err := RunRedo(r.dir, p, h[2]); err == nil {
		t.Error("RunRedo during a rebase: want BusyError")
	}
}

func TestHeadCommitTime(t *testing.T) {
	r := committedRepo(t)
	if HeadCommitTime(r.dir).IsZero() {
		t.Error("HeadCommitTime of a repo is not zero")
	}
	if !HeadCommitTime(t.TempDir()).IsZero() {
		t.Error("HeadCommitTime outside a repo is zero")
	}
}

func TestSubmodulesCachedAndHere(t *testing.T) {
	dir := t.TempDir()
	if SubmodulesCached(dir, "k") != nil {
		t.Error("no .gitmodules: nil")
	}
	subs := []Submodule{{Path: "lib"}, {Path: "other"}}
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib", ".git"), []byte("gitdir: x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := subsHere(dir, subs); got != "10" {
		t.Errorf("subsHere = %q", got)
	}
	// .gitmodules present but not a repo: Submodules fails, the answer is nil.
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if SubmodulesCached(dir, "k") != nil {
		t.Error("not a repo: nil")
	}
	r := committedRepo(t)
	if err := os.WriteFile(filepath.Join(r.dir, ".gitmodules"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	a := SubmodulesCached(r.dir, "k")
	if a == nil || len(a) != 0 {
		t.Fatalf("empty list expected, got %#v", a)
	}
	b := SubmodulesCached(r.dir, "k") // served from the cache
	if b == nil || len(b) != 0 {
		t.Errorf("cached = %#v", b)
	}
}

func TestViaWrappersRunInTheRunner(t *testing.T) {
	x := &scriptRunner{out: map[string]string{
		"git --no-optional-locks status --porcelain=v2": "# branch.oid abc\x00# branch.head main\x00",
		"git log":        "",
		"git stash list": "stash@{0}|WIP on x\n",
		"git stash show --no-color -p --include-untracked": "diff one",
	}}
	bs, err := StatusBranchVia(x, "/c")
	if err != nil || bs.Branch.Name != "main" {
		t.Errorf("StatusBranchVia = %+v, %v", bs, err)
	}
	if _, err := StatusBranchVia(&scriptRunner{}, "/c"); err == nil {
		t.Error("a failing status is an error")
	}
	if _, _, err := CommitsVia(x, "/c", 0, 5); err != nil {
		t.Errorf("CommitsVia = %v", err)
	}
	s := StashAt("/c", x)
	if l, err := s.List(); err != nil || len(l) != 1 {
		t.Errorf("List = %v, %v", l, err)
	}
	if d, err := s.Diff("stash@{0}"); err != nil || d != "diff one" {
		t.Errorf("Diff = %q, %v", d, err)
	}
	for name, fn := range map[string]func() error{
		"Apply":  func() error { return s.Apply("stash@{0}") },
		"Drop":   func() error { return s.Drop("stash@{0}") },
		"Pop":    func() error { return s.Pop("") },
		"Branch": func() error { return s.Branch("b", "stash@{0}") },
	} {
		if err := fn(); err == nil {
			t.Errorf("%s: the runner fails, want error", name)
		}
	}
	x.out["git stash"] = ""
	for name, fn := range map[string]func() error{
		"Apply":  func() error { return s.Apply("stash@{0}") },
		"Drop":   func() error { return s.Drop("stash@{0}") },
		"Pop":    func() error { return s.Pop("") },
		"PopRef": func() error { return s.Pop("stash@{1}") },
		"Branch": func() error { return s.Branch("b", "stash@{0}") },
	} {
		if err := fn(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Diff falls back to the form without untracked files.
	y := &scriptRunner{out: map[string]string{"git stash show --no-color -p stash": "plain"}}
	if d, err := StashAt("/c", y).Diff("stash@{0}"); err != nil || d != "plain" {
		t.Errorf("Diff fallback = %q, %v", d, err)
	}
}
