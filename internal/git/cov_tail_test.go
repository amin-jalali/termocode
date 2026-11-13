package git

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestV1CodeAndShortOID(t *testing.T) {
	for in, want := range map[string]string{
		".M": " M", ".D": " D", "M.": "M ", "A.": "A ", "D.": "D ", "R.": "R ",
		"MM": "MM", "UU": "UU", "T.": "T ", ".T": " T",
	} {
		if got := v1Code(in); got != want {
			t.Errorf("v1Code(%q) = %q, want %q", in, got, want)
		}
	}
	if shortOID("0123456789") != "0123456" || shortOID("abc") != "abc" {
		t.Error("shortOID")
	}
}

func TestReflogKindOfEachAction(t *testing.T) {
	for msg, want := range map[string]ReflogKind{
		"commit (amend): x":     ReflogAmend,
		"commit (initial): x":   ReflogInitial,
		"commit (merge): x":     ReflogMerge,
		"merge feat: ff":        ReflogMerge,
		"commit: x":             ReflogCommit,
		"checkout: moving":      ReflogCheckout,
		"reset: moving to HEAD": ReflogReset,
		"rebase (start): x":     ReflogRebase,
		"cherry-pick: x":        ReflogCherryPick,
		"revert: x":             ReflogRevert,
		"pull: Fast-forward":    ReflogPull,
		"pull --rebase (start)": ReflogPull,
		"something else: x":     ReflogOther,
	} {
		if got := reflogKind(msg); got != want {
			t.Errorf("reflogKind(%q) = %v, want %v", msg, got, want)
		}
	}
}

func TestWorktreeRefsListsRemotesAndTags(t *testing.T) {
	r := committedRepo(t)
	r.git(t, "tag", "v1")
	r.git(t, "branch", "dev")
	h := headOf(t, r)
	r.git(t, "update-ref", "refs/remotes/origin/feat", h)
	r.git(t, "update-ref", "refs/remotes/origin/main", h)
	r.git(t, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	refs, cur, err := WorktreeRefs(r.dir)
	if err != nil || cur != "main" {
		t.Fatalf("WorktreeRefs = %v, %q, %v", refs, cur, err)
	}
	if refs[0] != "main" {
		t.Errorf("the current branch is first, got %v", refs)
	}
	joined := strings.Join(refs, ",")
	for _, want := range []string{"dev", "origin/feat", "origin/main", "v1"} {
		if !strings.Contains(","+joined+",", ","+want+",") {
			t.Errorf("%q missing from %v", want, refs)
		}
	}
	if strings.Contains(joined, "origin/HEAD") {
		t.Errorf("the remote HEAD alias is not a start point: %v", refs)
	}
}

func TestForgetWorktreeForms(t *testing.T) {
	r := committedRepo(t)
	if err := ForgetWorktree(r.dir, r.dir); err == nil {
		t.Error("a folder that is there is refused")
	}
	// A path git never listed is fine.
	if err := ForgetWorktree(r.dir, filepath.Join(t.TempDir(), "never")); err != nil {
		t.Errorf("an unknown path = %v", err)
	}
	if err := ForgetWorktree(t.TempDir(), filepath.Join(t.TempDir(), "x")); err == nil {
		t.Error("outside a repo: git's error")
	}
}
