package agentrace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (AGX-4, CMP-18): a race has 2–8 lanes and a task; each lane's id names its
// worktree and branch (race-<n>-<k>-<agent>: lower case, digits, dashes),
// so the same agent may run twice and two races never share a lane.
func TestNewRace(t *testing.T) {
	r, err := New(3, "  Make the toast queue\nthread-safe!  ", "go test ./...", []LaneSpec{
		{Agent: "claude", Title: "Claude Code"}, {Agent: "codex", Title: "Codex"}, {Agent: "claude", Title: "Claude Code"}}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "race-3" || r.Name != "race/make-the-toast-queue-thread-safe" || r.Prompt != "Make the toast queue thread-safe!" {
		t.Errorf("race %q %q %q", r.ID, r.Name, r.Prompt)
	}
	if r.Verify != "go test ./..." || r.StartedMs != 1000 {
		t.Errorf("verify %q started %d", r.Verify, r.StartedMs)
	}
	var ids []string
	for _, l := range r.Lanes {
		ids = append(ids, l.ID)
		if l.State != Starting {
			t.Errorf("lane %s starts %s", l.ID, l.State)
		}
	}
	if got := strings.Join(ids, " "); got != "race-3-1-claude race-3-2-codex race-3-3-claude" {
		t.Errorf("lane ids %s", got)
	}
	for _, bad := range [][]LaneSpec{nil, {{Agent: "claude"}}, {{Agent: "a"}, {Agent: "b"}, {Agent: "c"}, {Agent: "d"}, {Agent: "e"}, {Agent: "f"}, {Agent: "g"}, {Agent: "h"}, {Agent: "i"}}} {
		if _, err := New(1, "task", "", bad, 0); err == nil {
			t.Errorf("%d lanes: no error", len(bad))
		}
	}
	if _, err := New(1, "   ", "", []LaneSpec{{Agent: "a"}, {Agent: "b"}}, 0); err == nil {
		t.Error("an empty task: no error")
	}
	if _, err := New(1, "task", "", []LaneSpec{{Agent: "../x"}, {Agent: "b"}}, 0); err == nil {
		t.Error("an agent id that is not a word: no error")
	}
	if got := Slug(strings.Repeat("word ", 20)); len(got) > 32 || strings.HasSuffix(got, "-") {
		t.Errorf("long slug %q", got)
	}
	if got := Slug("äöü ###"); got != "task" {
		t.Errorf("slug with no letters %q", got)
	}
}

// Spec (AGX-4): the next race number is one more than the highest kept.
func TestNextNumber(t *testing.T) {
	if n := NextNumber(nil); n != 1 {
		t.Errorf("first %d", n)
	}
	if n := NextNumber([]Race{{Number: 4}, {Number: 2}}); n != 5 {
		t.Errorf("next %d", n)
	}
}

// Spec (AGX-4): the race records are one JSON file in the git dir,
// written atomically, readable by the user only; a missing file is no
// races; a broken file is an error (never a silent empty list that the
// next save would write over); at most MaxKept races, newest kept.
func TestStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "termocode")
	got, err := Load(dir)
	if err != nil || len(got) != 0 {
		t.Fatalf("no file: %v %v", got, err)
	}
	var rs []Race
	for i := 1; i <= MaxKept+3; i++ {
		r, err := New(i, "task", "", []LaneSpec{{Agent: "a"}, {Agent: "b"}}, int64(i))
		if err != nil {
			t.Fatal(err)
		}
		rs = append(rs, r)
	}
	rs[len(rs)-1].Lanes[0].Files = []FileStat{{Status: "M", Path: "x.go", Add: 3, Del: 1}}
	if err := Save(dir, rs); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	back, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != MaxKept || back[0].Number != 4 || back[len(back)-1].Lanes[0].Files[0].Add != 3 {
		t.Errorf("kept %d, first %d", len(back), back[0].Number)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("a broken file loaded")
	}
}

// Spec (AGX-5): a lane's totals are the sum of its files; the verify
// result is the command's exit code (0 passed; anything else failed).
func TestTotalsAndVerify(t *testing.T) {
	l := Lane{Files: []FileStat{{Add: 31, Del: 7}, {Add: 10, Del: 2}, {Binary: true}}}
	if f, a, d := l.Totals(); f != 3 || a != 41 || d != 9 {
		t.Errorf("totals %d +%d -%d", f, a, d)
	}
	if v := VerifyResult(0); v.State != VerifyPass || v.Exit != 0 {
		t.Errorf("exit 0: %+v", v)
	}
	if v := VerifyResult(2); v.State != VerifyFail || v.Exit != 2 {
		t.Errorf("exit 2: %+v", v)
	}
	r := Race{Lanes: []Lane{{ID: "a", State: Done}, {ID: "b", State: Working}, {ID: "c", State: Stopped}, {ID: "d", State: Merged}}}
	if got := laneIDs(r.Finished()); got != "a c" {
		t.Errorf("finished %s", got)
	}
	if got := laneIDs(r.Open()); got != "a b c" {
		t.Errorf("open %s", got)
	}
	if r.Lane("b") == nil || r.Lane("x") != nil {
		t.Error("Lane lookup")
	}
}

func laneIDs(ls []*Lane) string {
	var ids []string
	for _, l := range ls {
		ids = append(ids, l.ID)
	}
	return strings.Join(ids, " ")
}
