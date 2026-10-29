package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

// A folder that is not a git repo: every read the sidebar does must fail
// with an error (not a panic, not an empty "clean" answer).
func TestReadsOutsideARepoFail(t *testing.T) {
	dir := t.TempDir()
	if _, err := Status(dir); err == nil {
		t.Error("Status outside a repo: want error")
	}
	if _, err := GetBranch(dir); err == nil {
		t.Error("GetBranch outside a repo: want error")
	}
	if _, err := Log(dir, 0); err == nil {
		t.Error("Log outside a repo: want error")
	}
	if _, err := GraphLog(dir, 0); err == nil {
		t.Error("GraphLog outside a repo: want error")
	}
	if _, err := FileHistory(dir, "a.txt", 0); err == nil {
		t.Error("FileHistory outside a repo: want error")
	}
	if _, err := Tags(dir); err == nil {
		t.Error("Tags outside a repo: want error")
	}
	if _, err := StashList(dir); err == nil {
		t.Error("StashList outside a repo: want error")
	}
	if _, err := Branches(dir); err == nil {
		t.Error("Branches outside a repo: want error")
	}
	if _, err := BlameLineAt(dir, "a.txt", 1); err == nil {
		t.Error("BlameLineAt outside a repo: want error")
	}
	if _, err := ShowCommitDetail(dir, "HEAD"); err == nil {
		t.Error("ShowCommitDetail outside a repo: want error")
	}
	if HasUpstream(dir) {
		t.Error("HasUpstream outside a repo = true")
	}
	err := Commit(dir, "msg")
	var ce *CmdError
	if !errors.As(err, &ce) || !strings.Contains(ce.Error(), "not a git repository") {
		t.Errorf("Commit outside a repo = %v, want a CmdError saying not a git repository", err)
	}
	if ce != nil && !strings.Contains(ce.Output, "fatal:") {
		t.Errorf("CmdError.Output = %q, want git's full text", ce.Output)
	}
}

// Without git on PATH nothing works, and the user gets an error, not a hang
// or a crash.
func TestGitMissingFromPath(t *testing.T) {
	r := committedRepo(t)
	t.Setenv("PATH", t.TempDir())
	if IsRepo(r.dir) {
		t.Error("IsRepo without git = true")
	}
	if _, err := Status(r.dir); err == nil {
		t.Error("Status without git: want error")
	}
	err := Push(r.dir)
	if err == nil {
		t.Fatal("Push without git: want error")
	}
	if errors.As(err, new(*CmdError)) {
		t.Errorf("Push without git = %T, want the plain exec error (git wrote nothing)", err)
	}
}

// An empty repo (no commit yet) still has a branch name and an empty log.
func TestEmptyRepoBranchAndLog(t *testing.T) {
	r := newGitTestRepo(t)
	b, err := GetBranch(r.dir)
	if err != nil || b.Name != "main" || b.Ahead != 0 || b.Behind != 0 {
		t.Fatalf("GetBranch(empty) = %+v, %v; want main 0/0", b, err)
	}
	if _, err := Log(r.dir, 5); err == nil {
		t.Error("Log on a repo with no commits: want git's error")
	}
	if got, err := Tags(r.dir); err != nil || len(got) != 0 {
		t.Errorf("Tags(empty) = %v, %v", got, err)
	}
}

// A detached HEAD shows as "(detached <sha>)".
func TestGetBranchDetachedHead(t *testing.T) {
	r := committedRepo(t)
	sha := strings.TrimSpace(r.git(t, "rev-parse", "--short", "HEAD"))
	r.git(t, "checkout", "-q", "--detach")
	b, err := GetBranch(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "(detached " + sha + ")"; b.Name != want {
		t.Errorf("GetBranch detached = %q, want %q", b.Name, want)
	}
}

// File names with spaces and non-ASCII letters come back raw, so they match
// the file on disk.
func TestStatusUnicodeAndSpaceNames(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "my notes.txt", "x\n")
	r.write(t, "日本 語.md", "y\n")
	files, err := Status(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Path] = true
		if _, err := os.Stat(filepath.Join(r.dir, f.Path)); err != nil {
			t.Errorf("status path %q is not a file on disk: %v", f.Path, err)
		}
	}
	if !got["my notes.txt"] || !got["日本 語.md"] {
		t.Errorf("Status paths = %v", got)
	}
}

// A renamed file is listed once, under its new name.
func TestStatusRenameListsNewName(t *testing.T) {
	r := committedRepo(t)
	r.git(t, "mv", "b.txt", "c d.txt")
	files, err := Status(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "c d.txt" || files[0].Code[0] != 'R' {
		t.Fatalf("Status after rename = %#v, want one R entry for \"c d.txt\"", files)
	}
}

func TestFileStatusShortCodes(t *testing.T) {
	if (FileStatus{}).Staged() || (FileStatus{}).Unstaged() {
		t.Error("empty code must be neither staged nor unstaged")
	}
	if (FileStatus{Code: "M"}).Unstaged() {
		t.Error("one-letter code must not read past its end")
	}
}

func TestErrorReasonPicksTheUsefulLine(t *testing.T) {
	cases := []struct{ out, want string }{
		{"To origin\nfatal: could not read Username", "could not read Username"},
		{"To /x.git\n ! [rejected]        main -> main (fetch first)\nerror: failed to push some refs\nhint: pull first",
			"[rejected] main -> main (fetch first)"},
		{"Auto-merging a.txt\nCONFLICT (content): Merge conflict in a.txt\nerror: could not apply",
			"CONFLICT (content): Merge conflict in a.txt"},
		{"warning: x\nerror: pathspec 'nope' did not match", "pathspec 'nope' did not match"},
		{"Something odd\nlast line\nhint: try this\n", "last line"},
		{"hint: only hints", "hint: only hints"},
	}
	for _, c := range cases {
		if got := errorReason(c.out); got != c.want {
			t.Errorf("errorReason(%q) = %q, want %q", c.out, got, c.want)
		}
	}
	base := errors.New("exit status 1")
	if got := cmdError(base, []byte("  \n")); got != base {
		t.Errorf("cmdError without output = %v, want the exec error itself", got)
	}
	ce := cmdError(base, []byte("fatal: boom\n"))
	if !errors.Is(ce, base) || ce.Error() != "boom" {
		t.Errorf("cmdError = %v (Is base: %v), want reason boom wrapping the exec error", ce, errors.Is(ce, base))
	}
}

// pushRepo returns a committed repo whose origin is a local bare repo.
func pushRepo(t *testing.T) (*gitTestRepo, string) {
	t.Helper()
	r := committedRepo(t)
	bare := filepath.Join(t.TempDir(), "origin.git")
	r.git(t, "init", "-q", "--bare", "--initial-branch=main", bare)
	r.git(t, "remote", "add", "origin", bare)
	return r, bare
}

// The first push sets the upstream; then push, fetch and pull work, and a
// rejected push says why in one line.
func TestPushPullFetchWithLocalRemote(t *testing.T) {
	r, bare := pushRepo(t)
	if HasUpstream(r.dir) {
		t.Fatal("HasUpstream before the first push = true")
	}
	err := Push(r.dir)
	var ce *CmdError
	if !errors.As(err, &ce) || !strings.Contains(ce.Error(), "no upstream") {
		t.Fatalf("Push without upstream = %v, want a CmdError about no upstream", err)
	}
	if err := PushSetUpstream(r.dir, "main"); err != nil {
		t.Fatalf("PushSetUpstream: %v", err)
	}
	if !HasUpstream(r.dir) {
		t.Fatal("HasUpstream after push -u = false")
	}
	r.write(t, "a.txt", "changed\n")
	r.git(t, "commit", "-qam", "second")
	if err := Push(r.dir); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if got := strings.TrimSpace(r.git(t, "--git-dir", bare, "log", "-1", "--format=%s", "main")); got != "second" {
		t.Errorf("remote main = %q, want second", got)
	}

	// Someone else pushes; our push is rejected until we pull.
	other := filepath.Join(t.TempDir(), "other")
	r.git(t, "clone", "-q", bare, other)
	o := &gitTestRepo{dir: other}
	o.write(t, "b.txt", "theirs\n")
	o.git(t, "commit", "-qam", "theirs")
	o.git(t, "push", "-q")

	if err := Fetch(r.dir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	b, _ := GetBranch(r.dir)
	if b.Behind != 1 || b.Ahead != 0 {
		t.Errorf("after fetch ahead/behind = %d/%d, want 0/1", b.Ahead, b.Behind)
	}
	r.write(t, "c.txt", "mine\n")
	r.git(t, "add", "c.txt")
	r.git(t, "commit", "-qm", "mine")
	err = Push(r.dir)
	if !errors.As(err, &ce) || !strings.Contains(ce.Error(), "rejected") {
		t.Fatalf("Push behind remote = %v, want a rejected CmdError", err)
	}
	r.git(t, "config", "pull.rebase", "true")
	if err := Pull(r.dir); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if got := readFile(t, filepath.Join(r.dir, "b.txt")); got != "theirs\n" {
		t.Errorf("b.txt after pull = %q", got)
	}
}

// Fetch and pull without any remote fail with git's reason.
func TestFetchPullWithoutRemote(t *testing.T) {
	r := committedRepo(t)
	if err := Pull(r.dir); err == nil {
		t.Error("Pull without remote: want error")
	}
	if err := PushSetUpstream(r.dir, "main"); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Errorf("PushSetUpstream without origin = %v, want an error naming origin", err)
	}
	r.git(t, "remote", "add", "origin", filepath.Join(t.TempDir(), "gone.git"))
	if err := Fetch(r.dir); err == nil {
		t.Error("Fetch from a missing remote: want error")
	}
}

func TestStashApplyAndDrop(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "a.txt", "wip\n")
	if err := Stash(r.dir, "my wip"); err != nil {
		t.Fatal(err)
	}
	list, err := StashList(r.dir)
	if err != nil || len(list) != 1 || list[0].Ref != "stash@{0}" || !strings.Contains(list[0].Message, "my wip") {
		t.Fatalf("StashList = %+v, %v", list, err)
	}
	if err := StashApply(r.dir, list[0].Ref); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(r.dir, "a.txt")); got != "wip\n" {
		t.Errorf("a.txt after apply = %q", got)
	}
	if list, _ := StashList(r.dir); len(list) != 1 {
		t.Errorf("apply must keep the stash, list = %+v", list)
	}
	if err := StashDrop(r.dir, "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if list, _ := StashList(r.dir); len(list) != 0 {
		t.Errorf("after drop list = %+v", list)
	}
	if err := StashDrop(r.dir, "stash@{0}"); err == nil {
		t.Error("drop of a missing stash: want error")
	}
	if err := StashApply(r.dir, "stash@{5}"); err == nil {
		t.Error("apply of a missing stash: want error")
	}
	if err := StashPop(r.dir); err == nil {
		t.Error("pop with no stash: want error")
	}
}

func TestStashOutsideRepo(t *testing.T) {
	if err := Stash(t.TempDir(), ""); err == nil {
		t.Error("Stash outside a repo: want error")
	}
}

func TestCheckoutAndCreateBranchErrors(t *testing.T) {
	r := committedRepo(t)
	if err := Checkout(r.dir, "no-such-branch"); err == nil {
		t.Error("Checkout of a missing branch: want error")
	}
	if err := CreateBranch(r.dir, "main"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("CreateBranch of an existing name = %v, want already exists", err)
	}
	if err := CreateBranch(r.dir, "feat/x"); err != nil {
		t.Fatal(err)
	}
	if b, _ := GetBranch(r.dir); b.Name != "feat/x" {
		t.Errorf("branch after CreateBranch = %q", b.Name)
	}
}

// A commit while another git holds index.lock fails with git's reason.
func TestCommitWithIndexLock(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "a.txt", "new\n")
	r.git(t, "add", "a.txt")
	if err := os.WriteFile(filepath.Join(r.dir, ".git", "index.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := Commit(r.dir, "x")
	if err == nil || !strings.Contains(err.Error(), "index.lock") {
		t.Errorf("Commit with index.lock = %v, want an error naming index.lock", err)
	}
}

func TestShowCommitAndFileAtRevision(t *testing.T) {
	r := committedRepo(t)
	out, err := ShowCommit(r.dir, "HEAD")
	if err != nil || !strings.Contains(out, "+bee") || !strings.Contains(out, "init") {
		t.Errorf("ShowCommit = %q, %v", out, err)
	}
	if _, err := ShowCommit(r.dir, "nope"); err == nil {
		t.Error("ShowCommit of a bad rev: want error")
	}
	got, err := ShowFileAtRevision(r.dir, "HEAD", "b.txt")
	if err != nil || got != "bee\n" {
		t.Errorf("ShowFileAtRevision = %q, %v", got, err)
	}
	// A file added after HEAD has an empty "before" side, not an error.
	got, err = ShowFileAtRevision(r.dir, "HEAD", "new.txt")
	if err != nil || got != "" {
		t.Errorf("ShowFileAtRevision(missing) = %q, %v; want empty, nil", got, err)
	}
}

func TestDiffOfMissingFileFails(t *testing.T) {
	r := committedRepo(t)
	if _, err := Diff(r.dir, "gone.txt"); err == nil {
		t.Error("Diff of a file that is nowhere: want error")
	}
	if _, err := Diff(t.TempDir(), "gone.txt"); err == nil {
		t.Error("Diff outside a repo of a missing file: want error")
	}
}

// Stage hunk on a line with no change says so instead of staging anything.
func TestStageHunkNoChangeUnderCursor(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-qm", "init")
	r.write(t, "a.txt", "1\n2\n3\n4\n5\n6\n7\n8\n9\nTEN\n")
	err := StageHunk(r.dir, "a.txt", 1)
	if err == nil || !strings.Contains(err.Error(), "no change under the cursor") {
		t.Fatalf("StageHunk on line 1 = %v", err)
	}
	if err := StageHunk(t.TempDir(), "a.txt", 1); err == nil {
		t.Error("StageHunk outside a repo: want error")
	}
}

// A one-line deletion hunk ("+3,0") still matches the line where it sits.
func TestExtractHunkPatchPureDeletion(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -3 +2,0 @@\n-gone\n"
	p, ok := extractHunkPatch(diff, 2)
	if !ok || !strings.Contains(p, "-gone") || !strings.HasPrefix(p, "diff --git") {
		t.Errorf("extractHunkPatch = %q, %v", p, ok)
	}
	if _, ok := extractHunkPatch(diff, 9); ok {
		t.Error("line outside every hunk matched")
	}
}

func TestParseHunkNewRange(t *testing.T) {
	cases := []struct {
		h          string
		start, cnt int
	}{
		{"@@ -1,2 +3,4 @@ func x", 3, 4},
		{"@@ -1 +7 @@", 7, 1},
		{"@@ -1 +9", 9, 1},
		{"@@ no plus", 0, 0},
		{"@@ -1 +5x,2y @@", 5, 2},
	}
	for _, c := range cases {
		if s, n := parseHunkNewRange(c.h); s != c.start || n != c.cnt {
			t.Errorf("parseHunkNewRange(%q) = %d,%d; want %d,%d", c.h, s, n, c.start, c.cnt)
		}
	}
}

func TestShowCommitDetailCounts(t *testing.T) {
	r := committedRepo(t)
	d, err := ShowCommitDetail(r.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if d.Subject != "init" || d.Files != 2 || d.Add != 4 || d.Del != 0 {
		t.Errorf("ShowCommitDetail = %+v", d)
	}
}

func TestStashListSkipsOddLines(t *testing.T) {
	r := committedRepo(t)
	// A ref with no "|" in its line cannot be shown; a normal one is.
	r.write(t, "a.txt", "wip\n")
	r.git(t, "stash", "push", "-q", "-m", "a|b")
	list, err := StashList(r.dir)
	if err != nil || len(list) != 1 || !strings.HasSuffix(list[0].Message, "a|b") {
		t.Errorf("StashList with | in the message = %+v, %v", list, err)
	}
}

// Blame ages read like "3 days ago"; a time in the future reads "just now".
func TestBlameRelativeTime(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{-time.Hour, "just now"},
		{10 * time.Second, "just now"},
		{5 * time.Minute, "5 min ago"},
		{3 * time.Hour, "3 hr ago"},
		{3 * 24 * time.Hour, "3 days ago"},
		{90 * 24 * time.Hour, "3 months ago"},
		{800 * 24 * time.Hour, "2 years ago"},
	}
	for _, c := range cases {
		if got := relativeTimeFromEpoch(now.Add(-c.ago).Unix()); got != c.want {
			t.Errorf("relativeTimeFromEpoch(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
	if got := relativeTimeFromEpoch(0); got != "" {
		t.Errorf("relativeTimeFromEpoch(0) = %q", got)
	}
}
