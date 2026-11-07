package git

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
)

// stashFakeRunner is a container that answers the stash commands: it
// keeps a stash list in memory and records every argv it ran.
type stashFakeRunner struct {
	mu      sync.Mutex
	argvs   [][]string
	dirs    []string
	stashes []string // hashes, stash@{0} first
	msgs    []string
	dirty   bool // there is something to stash
	failOn  string
}

func (f *stashFakeRunner) Capture(_ context.Context, dir string, argv ...string) (string, string, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.argvs = append(f.argvs, slices.Clone(argv))
	f.dirs = append(f.dirs, dir)
	if len(argv) < 2 || argv[0] != "git" {
		return "", "not git", 127, nil
	}
	line := strings.Join(argv[1:], " ")
	if f.failOn != "" && strings.Contains(line, f.failOn) {
		return "", "error: the container says no\nmore", 1, nil
	}
	switch {
	case strings.HasPrefix(line, "stash list"):
		var b strings.Builder
		for i, m := range f.msgs {
			b.WriteString("stash@{" + string(rune('0'+i)) + "}|" + m + "\n")
		}
		return b.String(), "", 0, nil
	case strings.HasPrefix(line, "rev-parse --verify -q stash@{0}"):
		if len(f.stashes) == 0 {
			return "", "", 1, nil
		}
		return f.stashes[0] + "\n", "", 0, nil
	case strings.HasPrefix(line, "rev-parse --verify -q"):
		return "abcdef1234567890\n", "", 0, nil
	case strings.HasPrefix(line, "stash push"):
		if f.dirty {
			f.stashes = append([]string{"9999999999"}, f.stashes...)
			f.msgs = append([]string{"On main: new"}, f.msgs...)
			f.dirty = false
		}
		return "", "", 0, nil
	case strings.HasPrefix(line, "stash show"):
		return "diff --git a/x b/x\n", "", 0, nil
	}
	return "", "", 0, nil
}

func (f *stashFakeRunner) lines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, a := range f.argvs {
		out = append(out, strings.Join(a, " "))
	}
	return out
}

// Spec: StashVia runs every stash command through the Runner in the
// container folder (a container workspace), never on this machine.
func TestStashViaRunsInTheRunner(t *testing.T) {
	f := &stashFakeRunner{stashes: []string{"1111111111"}, msgs: []string{"WIP on main: old"}}
	r := StashAt("/work/app", f)

	es, err := r.List()
	if err != nil || len(es) != 1 || es[0].Ref != "stash@{0}" || es[0].Message != "WIP on main: old" {
		t.Fatalf("List = %+v, %v", es, err)
	}
	if h := r.Hash("stash@{0}"); h != "1111111111" {
		t.Errorf("Hash = %q", h)
	}
	if err := r.Apply("stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if err := r.Pop("stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if err := r.Pop(""); err != nil {
		t.Fatal(err)
	}
	if err := r.Drop("stash@{1}"); err != nil {
		t.Fatal(err)
	}
	if d, err := r.Diff("stash@{0}"); err != nil || !strings.HasPrefix(d, "diff --git") {
		t.Errorf("Diff = %q, %v", d, err)
	}
	if err := r.Branch("topic", "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"git stash list --pretty=%gd|%s",
		"git rev-parse --verify -q stash@{0}",
		"git stash apply stash@{0}",
		"git stash apply stash@{0}",
		"git stash drop stash@{0}",
		"git stash pop",
		"git stash drop stash@{1}",
		"git stash show --no-color -p --include-untracked stash@{0}",
		"git stash branch topic stash@{0}",
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

// Spec: StashVia's push sends the stash options (message, new files,
// staged only, one file) and says so when there was nothing to stash.
func TestStashViaPushOptions(t *testing.T) {
	f := &stashFakeRunner{dirty: true}
	r := StashAt("/work/app", f)
	if err := r.Push(StashOptions{Message: "wip", Untracked: true, StagedOnly: true, Paths: []string{"a.go"}}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.lines(), "git stash push -m wip --include-untracked --staged -- a.go") {
		t.Errorf("no push with the options in %q", f.lines())
	}
	if err := r.Push(StashOptions{}); err == nil || !strings.Contains(err.Error(), "no local changes") {
		t.Errorf("clean tree: err %v, want no local changes", err)
	}
}

// Spec: a git error in the container names git's first stderr line.
func TestStashViaErrorNamesGit(t *testing.T) {
	f := &stashFakeRunner{failOn: "stash apply"}
	err := StashAt("/work/app", f).Apply("stash@{0}")
	if err == nil || err.Error() != "error: the container says no" {
		t.Errorf("err %v", err)
	}
}

// Spec: StashAt with no Runner is the local stash functions.
func TestStashAtLocal(t *testing.T) {
	r := StashAt(stashRepo(t).dir, nil)
	if err := r.Push(StashOptions{Message: "local wip"}); err != nil {
		t.Fatal(err)
	}
	es, err := r.List()
	if err != nil || len(es) != 1 || !strings.Contains(es[0].Message, "local wip") {
		t.Fatalf("List = %+v, %v", es, err)
	}
}
