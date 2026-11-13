package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutgoingRangeForms(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	if _, err := Outgoing(r.dir, "-x", 0); err == nil {
		t.Error("an option as base must be refused")
	}
	if _, err := OutgoingFiles(r.dir, "-x"); err == nil {
		t.Error("an option as base must be refused")
	}
	// No upstream and no remotes: every commit is outgoing (default limit).
	got, err := Outgoing(r.dir, "", 0)
	if err != nil || len(got) != 3 {
		t.Errorf("Outgoing all = %v, %v", got, err)
	}
	// An unknown base reads as a new remote branch: same answer.
	if got, _ := Outgoing(r.dir, "origin/new", 2); len(got) != 2 {
		t.Errorf("Outgoing with a limit = %v", got)
	}
	// Against a known base only the newer commits are outgoing.
	if got, _ := Outgoing(r.dir, h[1], 0); len(got) != 1 || got[0].Subject != "c3" {
		t.Errorf("Outgoing(%s) = %v", h[1][:7], got)
	}
	// An empty repository has nothing outgoing, and that is no error.
	if got, err := Outgoing(newGitTestRepo(t).dir, "", 0); err != nil || len(got) != 0 {
		t.Errorf("empty repo = %v, %v", got, err)
	}
}

func TestPullAndPushNameChecks(t *testing.T) {
	r := committedRepo(t)
	if err := PullFrom(r.dir, "-x", "main", true); err == nil {
		t.Error("an option as remote must be refused")
	}
	if err := PullFrom(r.dir, "origin", "-x", false); err == nil {
		t.Error("an option as branch must be refused")
	}
	if err := PullFrom(r.dir, "", "", false); err == nil {
		t.Error("no upstream: git's error")
	}
	if err := PushForceWithLease(r.dir, "origin", "main", ""); err == nil || !strings.Contains(err.Error(), "fetch first") {
		t.Errorf("no expected tip = %v", err)
	}
}

func TestDeletedByNamesTheSide(t *testing.T) {
	for _, c := range []struct {
		s    ConflictStages
		want string
	}{
		{ConflictStages{}, "both"},
		{ConflictStages{Theirs: ConflictSide{Present: true}}, "ours"},
		{ConflictStages{Ours: ConflictSide{Present: true}}, "theirs"},
		{ConflictStages{Ours: ConflictSide{Present: true}, Theirs: ConflictSide{Present: true}}, ""},
	} {
		if got := c.s.DeletedBy(); got != c.want {
			t.Errorf("DeletedBy(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestRecentMessagesAuthorsDefaults(t *testing.T) {
	r, _ := rebaseRepo(t, 3)
	r.write(t, "z.txt", "z\n")
	r.git(t, "add", "z.txt")
	r.git(t, "commit", "-qm", "c3") // the same subject again: listed once
	got, err := RecentMessages(r.dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, m := range got {
		seen[m]++
	}
	for m, n := range seen {
		if n != 1 {
			t.Errorf("message %q listed %d times", m, n)
		}
	}
	if two, _ := RecentMessages(r.dir, 2); len(two) != 2 {
		t.Errorf("RecentMessages(2) = %v", two)
	}
	if a := Authors(r.dir, 0); len(a) != 1 || !strings.Contains(a[0], "test@example.com") {
		t.Errorf("Authors = %v", a)
	}
	if Authors(t.TempDir(), 5) != nil {
		t.Error("no repo: no authors")
	}
}

func TestTagAndRemoteTipsOfMissingRefs(t *testing.T) {
	r := committedRepo(t)
	if err := CreateTag(r.dir, "v1", "", "release one"); err != nil {
		t.Fatal(err)
	}
	if err := CreateTag(r.dir, "v1", "", ""); err == nil {
		t.Error("an existing tag is refused")
	}
	if TagTip(r.dir, "v1") == "" || TagTip(r.dir, "nope") != "" {
		t.Error("TagTip")
	}
	if _, err := TagDetail(r.dir, "nope"); err == nil {
		t.Error("TagDetail of an unknown tag: want error")
	}
	if RemoteTip(r.dir, "origin", "main") != "" {
		t.Error("RemoteTip without the remote is empty")
	}
	if UpstreamCandidates(r.dir) != nil {
		t.Error("no remotes: no candidates")
	}
}

func TestWebBaseOddRemotes(t *testing.T) {
	for in, want := range map[string]string{
		"https://":                      "",
		"https://host":                  "",
		"git@host:":                     "",
		"C:\\repos\\x":                  "",
		"a:b":                           "",
		"/srv/git/x.git":                "",
		"git@github.com:me/x.git":       "https://github.com/me/x",
		"https://u:tok@gitlab.com/g/p/": "https://gitlab.com/g/p",
	} {
		got, ok := WebBase(in)
		if got != want || ok != (want != "") {
			t.Errorf("WebBase(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestRemoteWebPicksTheRemote(t *testing.T) {
	r := committedRepo(t)
	if _, err := RemoteWeb(r.dir); err == nil || !strings.Contains(err.Error(), "no remote") {
		t.Errorf("no remote = %v", err)
	}
	r.git(t, "remote", "add", "up", "/srv/local/x.git")
	if _, err := RemoteWeb(r.dir); err == nil || !strings.Contains(err.Error(), "no web page") {
		t.Errorf("a local path = %v", err)
	}
	r.git(t, "remote", "set-url", "up", "git@github.com:me/x.git")
	if got, err := RemoteWeb(r.dir); err != nil || got != "https://github.com/me/x" {
		t.Errorf("the only remote = %q, %v", got, err)
	}
	r.git(t, "remote", "add", "second", "https://example.com/other/y.git")
	if _, err := RemoteWeb(r.dir); err == nil {
		t.Error("two remotes and no origin: want error")
	}
}

func TestBranchPickerGuards(t *testing.T) {
	r := committedRepo(t)
	if err := CheckoutTracking(r.dir, "-x/y"); err == nil {
		t.Error("an option must be refused")
	}
	if err := CheckoutTracking(r.dir, "nobar"); err == nil {
		t.Error("a name without a remote must be refused")
	}
	if IsBlockedByChanges(nil) {
		t.Error("nil is not a block")
	}
	if RecentBranches(r.dir, 0) != nil {
		t.Error("n <= 0 lists nothing")
	}
}

func TestAddWorktreeRefusals(t *testing.T) {
	r := committedRepo(t)
	if err := AddWorktree(r.dir, filepath.Join(t.TempDir(), "w"), "b1", "--x"); err == nil || !strings.Contains(err.Error(), "not a ref") {
		t.Errorf("an option as ref = %v", err)
	}
	if err := AddWorktree(r.dir, filepath.Join(t.TempDir(), "w"), "bad..name", ""); err == nil {
		t.Error("a bad branch name: want error")
	}
	if RemoteHosts(t.TempDir()) != nil {
		t.Error("no repo: no hosts")
	}
}

func TestMoveIndexAcrossOutsideARepo(t *testing.T) {
	a, b := t.TempDir(), filepath.Join(t.TempDir(), "sub")
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MoveIndexAcross(filepath.Join(a, "f"), filepath.Join(b, "f")); err != nil {
		t.Errorf("not in a repo: nothing to move, no error; got %v", err)
	}
	// Tracked nowhere: an untracked file is not in the index.
	r := committedRepo(t)
	r.write(t, "u.txt", "u\n")
	if err := os.MkdirAll(filepath.Join(r.dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MoveIndexAcross(filepath.Join(r.dir, "u.txt"), filepath.Join(r.dir, "d", "u.txt")); err != nil {
		t.Errorf("untracked: %v", err)
	}
}
