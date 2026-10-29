package app

import (
	"path/filepath"
	"testing"
)

// covRemote runs a remote palette action to its end and shows the result.
func covRemote(t *testing.T, m *Model, id string) gitRemoteDoneMsg {
	t.Helper()
	done := covMsg[gitRemoteDoneMsg](t, m.dispatchPaletteAction(id))
	m.gitRemoteDone(done)
	return done
}

// covBare is a bare repo to push to.
func covBare(t *testing.T) string {
	t.Helper()
	bare := t.TempDir()
	covGit(t, bare, "init", "-q", "--bare", "-b", "main")
	return bare
}

// Push with no remote fails; the first push sets the upstream; a second
// press while one runs only says so; fetch, pull and sync then work.
func TestCovGitPushPullSync(t *testing.T) {
	m, dir := covGitRepo(t)
	if d := covRemote(t, m, "git-push"); d.err == nil || !toastHas(m, "Push failed") {
		t.Fatalf("push with no remote: %+v", d)
	}
	bare := covBare(t)
	covGit(t, dir, "remote", "add", "origin", bare)
	if d := covRemote(t, m, "git-push"); d.err != nil || !toastHas(m, "Pushed to origin") {
		t.Fatalf("first push: %v", d.err)
	}
	if covGit(t, dir, "rev-parse", "--abbrev-ref", "main@{upstream}") != "origin/main" {
		t.Fatal("first push did not set the upstream")
	}

	// A second press while one runs starts nothing.
	cmd := m.dispatchPaletteAction("git-fetch")
	if m.dispatchPaletteAction("git-pull"); !toastHas(m, "Git operation already running") {
		t.Error("no busy note")
	}
	m.gitRemoteDone(covMsg[gitRemoteDoneMsg](t, cmd))
	if m.gitRemoteBusy != "" || !toastHas(m, "Fetched from origin") {
		t.Fatal("fetch did not finish")
	}

	// Someone else pushed: pull brings it in.
	other := t.TempDir()
	covGit(t, other, "clone", "-q", bare, ".")
	covWrite(t, other, "z.txt", "z\n")
	covCommit(t, other, "theirs")
	covGit(t, other, "push", "-q")
	if d := covRemote(t, m, "git-pull"); d.err != nil || !d.reload {
		t.Fatalf("pull: %+v", d)
	}
	if covGit(t, dir, "log", "-1", "--format=%s") != "theirs" {
		t.Fatal("pull did not bring the commit")
	}

	// Sync = pull then push.
	covWrite(t, dir, "y.txt", "y\n")
	covCommit(t, dir, "mine")
	if d := covRemote(t, m, "git-sync"); d.err != nil || !toastHas(m, "Synced with origin") {
		t.Fatalf("sync: %+v", d)
	}
	if covGit(t, bare, "log", "-1", "--format=%s", "main") != "mine" {
		t.Fatal("sync did not push")
	}

	// The remote refuses the push (a server hook): sync says which half failed.
	covHook(t, bare, "pre-receive")
	covWrite(t, dir, "y.txt", "y2\n")
	covCommit(t, dir, "refused")
	if d := covRemote(t, m, "git-sync"); d.err == nil || d.failTitle != "Sync failed (push)" || !toastHas(m, "Sync failed (push)") {
		t.Fatalf("rejected sync: %+v", d)
	}
	// The remote is gone: the pull half fails.
	covGit(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing"))
	if d := covRemote(t, m, "git-sync"); d.failTitle != "Sync failed (pull)" {
		t.Fatalf("sync with a lost remote: %+v", d)
	}
}

// A branch with no upstream: Sync does the first push; on a detached
// HEAD there is no branch to push.
func TestCovGitSyncFirstPushAndDetached(t *testing.T) {
	m, dir := covGitRepo(t)
	covGit(t, dir, "remote", "add", "origin", covBare(t))
	if d := covRemote(t, m, "git-sync"); d.err != nil || d.okText != "Pushed to origin" {
		t.Fatalf("first sync: %+v", d)
	}
	covGit(t, dir, "checkout", "-q", "--detach")
	covRefresh(m)
	if d := covRemote(t, m, "git-push"); d.err == nil || d.err.Error() != "no current branch" {
		t.Fatalf("detached push: %v", d.err)
	}
}
