package git

import (
	"os/exec"
	"strings"
	"testing"
)

// tagRepo is main with two commits and a bare remote "origin" that has
// main.
func tagRepo(t *testing.T) (*gitTestRepo, string) {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "a\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "first")
	r.write(t, "a.txt", "b\n")
	r.git(t, "commit", "-q", "-am", "second")
	bare := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	r.git(t, "remote", "add", "origin", bare)
	r.git(t, "push", "-q", "-u", "origin", "main")
	return r, bare
}

func bareGit(t *testing.T, bare string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = bare
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// GITX-11 (G2, G6): a lightweight tag and an annotated one on any
// commit; the details name the tagger, message and commit.
func TestCreateTagAndDetail(t *testing.T) {
	r, _ := tagRepo(t)
	first := strings.TrimSpace(r.git(t, "rev-parse", "--short", "HEAD~1"))
	if err := CreateTag(r.dir, "v1", "HEAD~1", ""); err != nil {
		t.Fatal(err)
	}
	if err := CreateTag(r.dir, "v2", "HEAD", "Release two\n\nNotes"); err != nil {
		t.Fatal(err)
	}
	if err := CreateTag(r.dir, "v1", "HEAD", ""); err == nil {
		t.Error("made a tag that exists")
	}
	if err := CreateTag(r.dir, "bad name", "HEAD", ""); err == nil {
		t.Error("made a tag with a bad name")
	}
	d1, err := TagDetail(r.dir, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if d1.Annotated || d1.Commit != first || d1.Subject != "first" {
		t.Errorf("v1 %+v", d1)
	}
	d2, err := TagDetail(r.dir, "v2")
	if err != nil {
		t.Fatal(err)
	}
	if !d2.Annotated || d2.Message != "Release two\n\nNotes" || d2.Tagger == "" || d2.Date == "" || d2.Subject != "second" {
		t.Errorf("v2 %+v", d2)
	}
}

// GITX-11 (G3): a deleted tag can come back from the hash TagTip named.
func TestDeleteTag(t *testing.T) {
	r, _ := tagRepo(t)
	r.git(t, "tag", "v1")
	tip := TagTip(r.dir, "v1")
	if len(tip) < 7 {
		t.Fatalf("tip %q", tip)
	}
	if err := DeleteTag(r.dir, "v1"); err != nil {
		t.Fatal(err)
	}
	if out := r.git(t, "tag"); strings.Contains(out, "v1") {
		t.Fatal("tag still there")
	}
	r.git(t, "tag", "v1", tip)
	if err := DeleteTag(r.dir, "nope"); err == nil {
		t.Error("no error for a missing tag")
	}
}

// GITX-11 (G4, G5, H12): push one tag, all tags, and delete a tag on the
// remote.
func TestPushAndDeleteRemoteTag(t *testing.T) {
	r, bare := tagRepo(t)
	r.git(t, "tag", "v1")
	r.git(t, "tag", "v2")
	if err := PushTag(r.dir, "origin", "v1"); err != nil {
		t.Fatal(err)
	}
	if got := bareGit(t, bare, "tag"); got != "v1" {
		t.Fatalf("remote tags %q", got)
	}
	if err := PushAllTags(r.dir, "origin"); err != nil {
		t.Fatal(err)
	}
	if got := bareGit(t, bare, "tag"); got != "v1\nv2" {
		t.Fatalf("remote tags %q", got)
	}
	if err := DeleteRemoteTag(r.dir, "origin", "v1"); err != nil {
		t.Fatal(err)
	}
	if got := bareGit(t, bare, "tag"); got != "v2" {
		t.Errorf("remote tags after delete %q", got)
	}
	if out := r.git(t, "tag"); !strings.Contains(out, "v1") {
		t.Error("the local tag went too")
	}
}

// GITX-13: list, add, rename, set the URL of and remove remotes.
func TestRemotesManage(t *testing.T) {
	r, bare := tagRepo(t)
	rs, err := ListRemotes(r.dir)
	if err != nil || len(rs) != 1 || rs[0].Name != "origin" || rs[0].FetchURL != bare || rs[0].PushURL != bare {
		t.Fatalf("remotes %+v %v", rs, err)
	}
	other := t.TempDir()
	if err := AddRemote(r.dir, "fork", other); err != nil {
		t.Fatal(err)
	}
	if err := AddRemote(r.dir, "fork", other); err == nil {
		t.Error("added a remote that exists")
	}
	if err := RenameRemote(r.dir, "fork", "mine"); err != nil {
		t.Fatal(err)
	}
	if err := SetRemoteURL(r.dir, "mine", bare); err != nil {
		t.Fatal(err)
	}
	rs, _ = ListRemotes(r.dir)
	if len(rs) != 2 || rs[0].Name != "mine" || rs[0].FetchURL != bare {
		t.Fatalf("after rename %+v", rs)
	}
	if err := RemoveRemote(r.dir, "mine"); err != nil {
		t.Fatal(err)
	}
	if rs, _ = ListRemotes(r.dir); len(rs) != 1 {
		t.Errorf("after remove %+v", rs)
	}
}

// GITX-13: set and unset the upstream of a branch.
func TestUpstreamSetUnset(t *testing.T) {
	r, _ := tagRepo(t)
	if got := UpstreamCandidates(r.dir); strings.Join(got, ",") != "origin/main" {
		t.Fatalf("candidates %q", got)
	}
	if err := UnsetUpstream(r.dir, "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Command("git", "-C", r.dir, "rev-parse", "--abbrev-ref", "main@{upstream}").Output(); err == nil {
		t.Fatal("upstream still set")
	}
	if err := SetUpstream(r.dir, "main", "origin/main"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "rev-parse", "--abbrev-ref", "main@{upstream}")); got != "origin/main" {
		t.Errorf("upstream %q", got)
	}
}

// GITX-14: a force push with lease replaces the remote branch only while
// it is still at the commit we saw.
func TestForcePushWithLease(t *testing.T) {
	r, bare := tagRepo(t)
	seen := RemoteTip(r.dir, "origin", "main")
	if len(seen) != 40 {
		t.Fatalf("seen %q", seen)
	}
	r.git(t, "commit", "-q", "--amend", "-m", "second, reworded")
	if err := PushForceWithLease(r.dir, "origin", "main", seen); err != nil {
		t.Fatal(err)
	}
	if got := bareGit(t, bare, "log", "-1", "--format=%s", "main"); got != "second, reworded" {
		t.Fatalf("remote main %q", got)
	}
	// Someone else pushes; our lease (the old commit) no longer holds.
	r.git(t, "commit", "-q", "--amend", "-m", "third try")
	if err := PushForceWithLease(r.dir, "origin", "main", seen); err == nil {
		t.Error("forced over a commit we did not see")
	}
	if got := bareGit(t, bare, "log", "-1", "--format=%s", "main"); got != "second, reworded" {
		t.Errorf("remote main changed: %q", got)
	}
}
