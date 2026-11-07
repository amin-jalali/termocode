package worktrees

import (
	"errors"
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/git"
)

func depEntry(path string, md Meta) Entry {
	return Entry{Worktree: git.Worktree{Path: path, Branch: "b" + path[1:]}, Kind: KindAgent, Meta: md}
}

// CMP-80: an agent records which worktree its work waits for; a self
// edge and a loop are refused, a second add changes nothing.
func TestAddBlocker(t *testing.T) {
	mm := map[string]Meta{}
	if err := AddBlocker(mm, "/b", "/a"); err != nil {
		t.Fatal(err)
	}
	if err := AddBlocker(mm, "/b", "/a"); err != nil || len(mm["/b"].BlockedBy) != 1 {
		t.Fatalf("second add: %v %v", err, mm["/b"].BlockedBy)
	}
	if err := AddBlocker(mm, "/c", "/b"); err != nil {
		t.Fatal(err)
	}
	if err := AddBlocker(mm, "/a", "/c"); !errors.Is(err, ErrBlockLoop) {
		t.Fatalf("loop a<-c<-b<-a: %v", err)
	}
	if err := AddBlocker(mm, "/a", "/a"); !errors.Is(err, ErrSelfBlock) {
		t.Fatalf("self: %v", err)
	}
	if len(mm["/a"].BlockedBy) != 0 {
		t.Fatalf("a refused add changed /a: %v", mm["/a"].BlockedBy)
	}
	for i := 0; i < MaxBlockers; i++ {
		_ = AddBlocker(mm, "/many", "/x"+string(rune('a'+i)))
	}
	if err := AddBlocker(mm, "/many", "/zz"); err == nil {
		t.Fatal("want an error past MaxBlockers")
	}
}

func TestRemoveBlocker(t *testing.T) {
	mm := map[string]Meta{}
	_ = AddBlocker(mm, "/b", "/a")
	_ = AddBlocker(mm, "/b", "/c")
	if !RemoveBlocker(mm, "/b", "/a") || reflect.DeepEqual(mm["/b"].BlockedBy, []string{"/a", "/c"}) {
		t.Fatalf("remove: %v", mm["/b"].BlockedBy)
	}
	if RemoveBlocker(mm, "/b", "/a") {
		t.Fatal("removing twice reports a change")
	}
	if !reflect.DeepEqual(mm["/b"].BlockedBy, []string{"/c"}) {
		t.Fatalf("left: %v", mm["/b"].BlockedBy)
	}
}

// A blocker is settled when its agent said done, its PR merged, its
// issue closed, or the worktree is gone or archived.
func TestSettled(t *testing.T) {
	for _, c := range []struct {
		e    Entry
		want bool
	}{
		{depEntry("/a", Meta{}), false},
		{depEntry("/a", Meta{Checkpoint: CheckWorking}), false},
		{depEntry("/a", Meta{Checkpoint: CheckDone}), true},
		{depEntry("/a", Meta{PR: 3, Link: LinkInfo{State: "merged"}}), true},
		{depEntry("/a", Meta{Issue: 3, Link: LinkInfo{State: "closed"}}), true},
		{depEntry("/a", Meta{State: StateArchived}), true},
		{Entry{Worktree: git.Worktree{Path: "/a"}, Gone: true}, true},
	} {
		if got := Settled(c.e); got != c.want {
			t.Errorf("Settled(%+v) = %v, want %v", c.e.Meta, got, c.want)
		}
	}
}

func TestOpenBlockersAndWaitsLine(t *testing.T) {
	a := depEntry("/a", Meta{Checkpoint: CheckDone})
	b := depEntry("/b", Meta{Name: "api"})
	c := depEntry("/c", Meta{BlockedBy: []string{"/a", "/b", "/forgotten"}})
	d := depEntry("/d", Meta{BlockedBy: []string{"/a"}})
	all := []Entry{a, b, c, d}
	open := OpenBlockers(all, c)
	if len(open) != 1 || open[0].Path != "/b" {
		t.Fatalf("open = %+v", open)
	}
	if got := WaitsLine(all, c); got != "waits for api" {
		t.Errorf("c: %q", got)
	}
	if got := WaitsLine(all, d); got != "ready: its blockers are done" {
		t.Errorf("d: %q", got)
	}
	if got := WaitsLine(all, b); got != "" {
		t.Errorf("b has no blockers: %q", got)
	}
}

// Waves order the graph: a worktree comes one wave after its latest
// blocker; worktrees outside the graph are left out.
func TestWaves(t *testing.T) {
	a := depEntry("/a", Meta{})
	b := depEntry("/b", Meta{BlockedBy: []string{"/a"}})
	c := depEntry("/c", Meta{BlockedBy: []string{"/a", "/b"}})
	d := depEntry("/d", Meta{BlockedBy: []string{"/a"}})
	lone := depEntry("/lone", Meta{})
	waves := Waves([]Entry{c, lone, b, a, d})
	var got [][]string
	for _, w := range waves {
		var ps []string
		for _, e := range w {
			ps = append(ps, e.Path)
		}
		got = append(got, ps)
	}
	want := [][]string{{"/a"}, {"/b", "/d"}, {"/c"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("waves = %v, want %v", got, want)
	}
	if Waves([]Entry{lone}) != nil {
		t.Fatal("no edges: no waves")
	}
}

// A loop written by hand into worktrees.json does not hang Waves: its
// members land in a last wave.
func TestWavesWithAHandMadeLoop(t *testing.T) {
	a := depEntry("/a", Meta{BlockedBy: []string{"/b"}})
	b := depEntry("/b", Meta{BlockedBy: []string{"/a"}})
	waves := Waves([]Entry{a, b})
	if len(waves) != 1 || len(waves[0]) != 2 {
		t.Fatalf("waves = %+v", waves)
	}
}
