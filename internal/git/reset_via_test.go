package git

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

// resetFakeRunner is a container repo that answers the reset commands
// and records every argv it ran.
type resetFakeRunner struct {
	mu      sync.Mutex
	argvs   []string
	dirs    []string
	dirty   string // `git status --porcelain` output
	noFirst bool   // HEAD~1 is missing (the first commit)
	failOn  string
	stashed bool
}

func (f *resetFakeRunner) Capture(_ context.Context, dir string, argv ...string) (string, string, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := strings.Join(argv, " ")
	f.argvs = append(f.argvs, line)
	f.dirs = append(f.dirs, dir)
	if f.failOn != "" && strings.Contains(line, f.failOn) {
		return "", "fatal: the container says no\nmore", 1, nil
	}
	switch {
	case line == "git rev-parse --verify -q stash@{0}":
		if !f.stashed {
			return "", "", 1, nil
		}
		return "5555555555\n", "", 0, nil
	case line == "git rev-parse --verify -q HEAD~1":
		if f.noFirst {
			return "", "", 1, nil
		}
		return "0000000000000000\n", "", 0, nil
	case strings.HasPrefix(line, "git rev-parse --verify -q"):
		return "abcdef1234567890\n", "", 0, nil
	case line == "git status --porcelain":
		return f.dirty, "", 0, nil
	case strings.HasPrefix(line, "git stash push"):
		f.stashed = true
		return "", "", 0, nil
	case strings.HasPrefix(line, "git rev-list --count"):
		return "3\n", "", 0, nil
	case strings.HasPrefix(line, "git log -1"):
		return "box commit\n", "", 0, nil
	}
	return "", "", 0, nil
}

func (f *resetFakeRunner) lines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.argvs)
}

// Spec: the soft, mixed and keep resets, the commit count and the last
// commit's subject of a container workspace run git through the Runner
// in the container folder.
func TestResetViaRunsInTheRunner(t *testing.T) {
	f := &resetFakeRunner{}
	r := StashAt("/work/app", f)
	for _, mode := range []string{ResetSoft, ResetMixed, ResetKeep} {
		if err := r.Reset(mode, "1234abc"); err != nil {
			t.Fatalf("Reset %s: %v", mode, err)
		}
	}
	if err := r.Reset(ResetHard, "1234abc"); err == nil {
		t.Error("Reset hard did not go through ResetHardSafe")
	}
	if n := r.CountCommits("1234abc..HEAD"); n != 3 {
		t.Errorf("CountCommits = %d", n)
	}
	if s := r.Subject(); s != "box commit" {
		t.Errorf("Subject = %q", s)
	}
	want := []string{
		"git reset -q --soft 1234abc --",
		"git reset -q --mixed 1234abc --",
		"git reset -q --keep 1234abc --",
		"git rev-list --count 1234abc..HEAD",
		"git log -1 --format=%s",
	}
	if got := f.lines(); !slices.Equal(got, want) {
		t.Errorf("argv\n got %q\nwant %q", got, want)
	}
	for _, d := range f.dirs {
		if d != "/work/app" {
			t.Errorf("ran in %q, want /work/app", d)
		}
	}
}

// Spec: a hard reset in the container saves HEAD as a backup ref and
// the changes (new files too) as a stash first, all in the container.
func TestResetHardSafeViaSavesFirst(t *testing.T) {
	defer clock.Set(func() time.Time { return time.Date(2026, 11, 7, 10, 20, 30, 0, time.UTC) })()
	f := &resetFakeRunner{dirty: " M box.go\n"}
	b, err := StashAt("/work/app", f).ResetHardSafe("1234abc")
	if err != nil {
		t.Fatal(err)
	}
	if b.Ref != "refs/termocode/backup/20261107-102030-000" || b.Old != "abcdef1" || !b.Stashed {
		t.Errorf("backup %+v", b)
	}
	want := []string{
		"git rev-parse --verify -q HEAD",
		"git update-ref refs/termocode/backup/20261107-102030-000 abcdef1234567890",
		"git status --porcelain",
		"git rev-parse --verify -q stash@{0}",
		"git stash push -m termocode: before reset --hard to 1234abc --include-untracked",
		"git rev-parse --verify -q stash@{0}",
		"git reset -q --hard 1234abc --",
	}
	if got := f.lines(); !slices.Equal(got, want) {
		t.Errorf("argv\n got %q\nwant %q", got, want)
	}
}

// Spec: when the changes cannot be saved in the container, nothing is
// reset; a clean tree is reset with no stash.
func TestResetHardSafeViaStopsWhenSaveFails(t *testing.T) {
	f := &resetFakeRunner{dirty: "?? new.go\n", failOn: "stash push"}
	_, err := StashAt("/work/app", f).ResetHardSafe("1234abc")
	if err == nil || !strings.Contains(err.Error(), "nothing was reset") {
		t.Errorf("err %v", err)
	}
	for _, l := range f.lines() {
		if strings.HasPrefix(l, "git reset") {
			t.Errorf("reset ran after the save failed: %q", l)
		}
	}

	clean := &resetFakeRunner{}
	b, err := StashAt("/work/app", clean).ResetHardSafe("1234abc")
	if err != nil || b.Stashed {
		t.Errorf("clean: %+v, %v", b, err)
	}
	if !slices.Contains(clean.lines(), "git reset -q --hard 1234abc --") {
		t.Errorf("no hard reset in %q", clean.lines())
	}
}

// Spec: Uncommit in the container is a soft reset to HEAD~1; the first
// commit says it has no commit before it and runs no reset.
func TestUncommitVia(t *testing.T) {
	f := &resetFakeRunner{}
	if err := StashAt("/work/app", f).Uncommit(); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.lines(), "git reset -q --soft HEAD~1 --") {
		t.Errorf("no soft reset in %q", f.lines())
	}
	first := &resetFakeRunner{noFirst: true}
	if err := StashAt("/work/app", first).Uncommit(); err == nil || !strings.Contains(err.Error(), "first commit") {
		t.Errorf("first commit: err %v", err)
	}
	for _, l := range first.lines() {
		if strings.HasPrefix(l, "git reset") {
			t.Errorf("reset ran on the first commit: %q", l)
		}
	}
}

// Spec: a git error in the container names git's first stderr line.
func TestResetViaErrorNamesGit(t *testing.T) {
	f := &resetFakeRunner{failOn: "reset -q --keep"}
	err := StashAt("/work/app", f).Reset(ResetKeep, "1234abc")
	if err == nil || err.Error() != "fatal: the container says no" {
		t.Errorf("err %v", err)
	}
}

// Spec: with no Runner the reset commands are the local ones.
func TestResetAtLocal(t *testing.T) {
	r := StashAt(stashRepo(t).dir, nil)
	if s := r.Subject(); s == "" {
		t.Error("no subject for the local last commit")
	}
	if n := r.CountCommits("HEAD..HEAD"); n != 0 {
		t.Errorf("CountCommits = %d", n)
	}
}
