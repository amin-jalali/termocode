package git

import (
	"strings"
	"testing"
)

// remoteRepo is a repo whose main is pushed to a bare "origin" (main
// tracks origin/main), and the bare remote's dir.
func remoteRepo(t *testing.T) (*gitTestRepo, string) {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "a\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "first")
	bare := t.TempDir()
	r.git(t, "init", "-q", "--bare", "-b", "main", bare)
	r.git(t, "remote", "add", "origin", bare)
	r.git(t, "push", "-q", "-u", "origin", "main")
	return r, bare
}

func commitFile(t *testing.T, r *gitTestRepo, name, msg string) {
	t.Helper()
	r.write(t, name, msg+"\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", msg)
}

// otherPush makes a commit on origin/main from a second clone, so the
// first repo is behind after a fetch.
func otherPush(t *testing.T, bare, msg string) {
	t.Helper()
	o := &gitTestRepo{dir: t.TempDir()}
	o.git(t, "clone", "-q", bare, o.dir)
	o.git(t, "config", "user.email", "o@example.com")
	o.git(t, "config", "user.name", "O")
	o.git(t, "config", "commit.gpgsign", "false")
	commitFile(t, o, "o-"+msg+".txt", msg)
	o.git(t, "push", "-q", "origin", "HEAD:main")
}

// GITX-12 (H10, H19): Outgoing lists the commits a push sends, Incoming
// the ones a pull brings (as of the last fetch); OutgoingFiles counts
// the files they change.
func TestOutgoingIncoming(t *testing.T) {
	r, bare := remoteRepo(t)
	if up, err := Upstream(r.dir); err != nil || up != "origin/main" {
		t.Fatalf("upstream %q %v", up, err)
	}
	if out, err := Outgoing(r.dir, "", 10); err != nil || len(out) != 0 {
		t.Fatalf("outgoing at start: %v %v", out, err)
	}
	commitFile(t, r, "b.txt", "two")
	commitFile(t, r, "c.txt", "three")
	out, err := Outgoing(r.dir, "", 10)
	if err != nil || logSubjects(out) != "three|two" {
		t.Fatalf("outgoing %q %v", logSubjects(out), err)
	}
	if n, err := OutgoingFiles(r.dir, ""); err != nil || n != 2 {
		t.Errorf("files %d %v", n, err)
	}
	otherPush(t, bare, "theirs")
	if in, _ := Incoming(r.dir, 10); len(in) != 0 {
		t.Errorf("incoming before a fetch: %v", in)
	}
	if err := FetchAll(r.dir); err != nil {
		t.Fatal(err)
	}
	in, err := Incoming(r.dir, 10)
	if err != nil || logSubjects(in) != "theirs" {
		t.Errorf("incoming %q %v", logSubjects(in), err)
	}
}

// A branch with no upstream: Outgoing is every commit no remote has, and
// Incoming says there is no upstream.
func TestOutgoingNoUpstream(t *testing.T) {
	r, _ := remoteRepo(t)
	r.git(t, "checkout", "-q", "-b", "topic")
	commitFile(t, r, "t.txt", "topic work")
	if _, err := Upstream(r.dir); err != ErrNoUpstream {
		t.Errorf("upstream err %v", err)
	}
	out, err := Outgoing(r.dir, "", 10)
	if err != nil || logSubjects(out) != "topic work" {
		t.Errorf("outgoing %q %v", logSubjects(out), err)
	}
	if _, err := Incoming(r.dir, 10); err != ErrNoUpstream {
		t.Errorf("incoming err %v", err)
	}
	// against a named remote branch
	if out, err := Outgoing(r.dir, "origin/main", 10); err != nil || logSubjects(out) != "topic work" {
		t.Errorf("against origin/main %q %v", logSubjects(out), err)
	}
}

// GITX-12 (H11): PushTo sends HEAD to any remote under any branch name;
// option-like names are refused.
func TestPushTo(t *testing.T) {
	r, bare := remoteRepo(t)
	commitFile(t, r, "b.txt", "two")
	if err := PushTo(r.dir, "origin", "review/two"); err != nil {
		t.Fatal(err)
	}
	b := &gitTestRepo{dir: bare}
	if got := strings.TrimSpace(b.git(t, "log", "-1", "--format=%s", "review/two")); got != "two" {
		t.Errorf("remote branch has %q", got)
	}
	for _, c := range [][2]string{{"--mirror", "x"}, {"origin", "--force"}, {"origin", "a..b"}, {"", "x"}, {"origin", ""}} {
		if err := PushTo(r.dir, c[0], c[1]); err == nil {
			t.Errorf("PushTo(%q, %q) was run", c[0], c[1])
		}
	}
}

// GITX-12 (H8, H9): PullFrom merges or rebases, from the upstream or a
// named remote branch.
func TestPullFrom(t *testing.T) {
	r, bare := remoteRepo(t)
	commitFile(t, r, "mine.txt", "mine")
	otherPush(t, bare, "theirs")
	if err := PullFrom(r.dir, "", "", true); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "--format=%s", "-3")); got != "mine\ntheirs\nfirst" {
		t.Errorf("rebase pull gave\n%s", got)
	}
	if n := strings.TrimSpace(r.git(t, "rev-list", "--merges", "--count", "HEAD")); n != "0" {
		t.Errorf("rebase made %s merges", n)
	}
	// a merge pull from a named branch
	o := &gitTestRepo{dir: t.TempDir()}
	o.git(t, "clone", "-q", bare, o.dir)
	o.git(t, "config", "user.email", "o@example.com")
	o.git(t, "config", "user.name", "O")
	o.git(t, "checkout", "-q", "-b", "feature")
	commitFile(t, o, "f.txt", "feature work")
	o.git(t, "push", "-q", "origin", "feature")
	if err := PullFrom(r.dir, "origin", "feature", false); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "-1", "--format=%s")); !strings.Contains(got, "feature") {
		t.Errorf("merge pull: HEAD is %q", got)
	}
	if err := PullFrom(r.dir, "origin", "--all", false); err == nil {
		t.Error("an option as branch was pulled")
	}
}

func TestRemoteNames(t *testing.T) {
	r, bare := remoteRepo(t)
	r.git(t, "remote", "add", "backup", bare)
	if got, err := RemoteNames(r.dir); err != nil || strings.Join(got, ",") != "backup,origin" {
		t.Errorf("remotes %v %v", got, err)
	}
}
