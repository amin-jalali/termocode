package git

import (
	"strings"
	"testing"
)

// GITX-10: the STASHES panel section reads the stash list on every
// poll, so StashListCached runs git only when the stash reflog changed.
func TestStashListCached(t *testing.T) {
	r := stashRepo(t)
	if got := StashListCached(r.dir); len(got) != 0 {
		t.Fatalf("no stash yet: %+v", got)
	}
	if err := StashPush(r.dir, StashOptions{Message: "first one"}); err != nil {
		t.Fatal(err)
	}
	got := StashListCached(r.dir)
	if len(got) != 1 || got[0].Ref != "stash@{0}" || !strings.HasSuffix(got[0].Message, "first one") {
		t.Fatalf("after one stash: %+v", got)
	}
	// Same reflog: the same slice comes back (no git process).
	if again := StashListCached(r.dir); len(again) != 1 || &again[0] != &got[0] {
		t.Errorf("not reused: %+v", again)
	}
	r.write(t, "b.txt", "b3\n")
	if err := StashPush(r.dir, StashOptions{Message: "second", Untracked: true}); err != nil {
		t.Fatal(err)
	}
	got = StashListCached(r.dir)
	if len(got) != 2 || got[1].Ref != "stash@{1}" {
		t.Fatalf("after two stashes: %+v", got)
	}
	if err := StashDrop(r.dir, "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if err := StashDrop(r.dir, "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if got := StashListCached(r.dir); len(got) != 0 {
		t.Errorf("after dropping all: %+v", got)
	}
	if got := StashListCached(t.TempDir()); got != nil {
		t.Errorf("not a repo: %+v", got)
	}
}
