package git

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

// reflogLine is one raw record as Reflog asks git for it.
func reflogLine(hash string, when int64, msg, subject string) string {
	return strings.Join([]string{hash, "HEAD@{" + strconv.FormatInt(when, 10) + "}", msg, subject}, "\x1f") + "\n"
}

// G-UND: each reflog line becomes an entry in plain words, newest first;
// Prev is the HEAD before it (the next older line), "" for the oldest.
func TestParseReflogKinds(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	at := now.Unix() - 120
	raw := reflogLine("h9", at, "revert: Revert \"c2\"", "Revert \"c2\"") +
		reflogLine("h8", at, "cherry-pick: c5", "c5") +
		reflogLine("h7", at, "pull: Fast-forward", "c4") +
		reflogLine("h6", at, "merge feat: Fast-forward", "c4") +
		reflogLine("h5", at, "reset: moving to HEAD~1", "c2") +
		reflogLine("h4", at, "checkout: moving from main to feat/x", "c3") +
		reflogLine("h3", at, "commit (amend): c3b", "c3b") +
		reflogLine("h2", at, "commit: fix x", "fix x") +
		reflogLine("h1", at, "commit (initial): c1", "c1")
	got := parseReflog(raw)
	want := []struct {
		kind    ReflogKind
		summary string
		prev    string
	}{
		{ReflogRevert, `revert 'Revert "c2"'`, "h8"},
		{ReflogCherryPick, "cherry-pick 'c5'", "h7"},
		{ReflogPull, "pull", "h6"},
		{ReflogMerge, "merge feat", "h5"},
		{ReflogReset, "reset to HEAD~1", "h4"},
		{ReflogCheckout, "checkout main → feat/x", "h3"},
		{ReflogAmend, "amend 'c3b'", "h2"},
		{ReflogCommit, "commit 'fix x'", "h1"},
		{ReflogInitial, "first commit 'c1'", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	for i, w := range want {
		e := got[i]
		if e.Kind != w.kind || e.Summary() != w.summary || e.Prev != w.prev {
			t.Errorf("entry %d = %q %q prev %q; want %q %q %q", i, e.Kind, e.Summary(), e.Prev, w.kind, w.summary, w.prev)
		}
		if e.Ago() != "2 min ago" {
			t.Errorf("entry %d ago %q", i, e.Ago())
		}
	}
	if co := got[5]; co.From != "main" || co.To != "feat/x" {
		t.Errorf("checkout from/to %q %q", co.From, co.To)
	}
}

// G-UND: Reflog reads a real repo's HEAD reflog, newest first, with full
// hashes; an empty repo has none.
func TestReflogRealRepo(t *testing.T) {
	r, h := rebaseRepo(t, 2)
	r.git(t, "commit", "-q", "--amend", "-m", "c2b")
	amended := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	got, err := Reflog(r.dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("entries %+v", got)
	}
	if got[0].Kind != ReflogAmend || got[0].Hash != amended || got[0].Prev != h[1] || got[0].Subject != "c2b" {
		t.Errorf("newest %+v", got[0])
	}
	if got[2].Kind != ReflogInitial || got[2].Prev != "" {
		t.Errorf("oldest %+v", got[2])
	}
	if one, _ := Reflog(r.dir, 1); len(one) != 1 || one[0].Prev != h[1] {
		t.Errorf("limit 1: %+v", one)
	}
	empty := newGitTestRepo(t)
	if got, err := Reflog(empty.dir, 10); err != nil || len(got) != 0 {
		t.Errorf("empty repo: %+v %v", got, err)
	}
}
