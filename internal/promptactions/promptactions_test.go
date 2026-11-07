package promptactions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseKeepsGoodRowsAndNotesBadOnes(t *testing.T) {
	in := `{"version":1,"actions":[
	 {"name":"Review diff","group":"Review","prompt":"Review my changes."},
	 {"name":"Tests","prompt":"Write tests.","target":"terminal"},
	 {"name":"","prompt":"no name"},
	 {"name":"Empty","prompt":"   "},
	 {"name":"Bad target","prompt":"x","target":"moon"},
	 {"name":"review DIFF","group":"review","prompt":"a second one with the same key"}
	]}`
	list, notes, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d actions: %+v", len(list), list)
	}
	if list[0].Name != "Review diff" || list[0].TargetOrChat() != TargetChat {
		t.Errorf("first: %+v", list[0])
	}
	if list[1].Target != TargetTerminal {
		t.Errorf("second: %+v", list[1])
	}
	if len(notes) != 4 {
		t.Errorf("notes %q", notes)
	}
}

func TestParseRejectsNewerVersionAndBrokenJSON(t *testing.T) {
	if _, _, err := Parse([]byte(`{"version":99,"actions":[]}`)); err == nil {
		t.Error("newer version accepted")
	}
	if _, _, err := Parse([]byte(`{broken`)); err == nil {
		t.Error("broken JSON accepted")
	}
	if list, _, err := Parse([]byte("  ")); err != nil || len(list) != 0 {
		t.Errorf("empty file: %v %v", list, err)
	}
}

func TestParseRefSplitsGroupAndName(t *testing.T) {
	cases := []struct{ in, group, name string }{
		{"Review/Diff", "Review", "Diff"},
		{"  Diff  ", "", "Diff"},
		{"a/b/c", "a", "b/c"},
	}
	for _, c := range cases {
		g, n, err := ParseRef(c.in)
		if err != nil || g != c.group || n != c.name {
			t.Errorf("%q: %q %q %v", c.in, g, n, err)
		}
	}
	for _, bad := range []string{"", "Group/", strings.Repeat("x", MaxName+1)} {
		if _, _, err := ParseRef(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestMergeAddsAndReplacesByKey(t *testing.T) {
	have := []Action{{Name: "A", Prompt: "1"}, {Name: "B", Group: "G", Prompt: "2"}}
	add := []Action{{Name: "b", Group: "g", Prompt: "new"}, {Name: "C", Prompt: "3"}}
	out, added, replaced := Merge(have, add)
	if added != 1 || replaced != 1 || len(out) != 3 {
		t.Fatalf("added %d replaced %d: %+v", added, replaced, out)
	}
	for _, a := range out {
		if a.Key() == "g/b" && a.Prompt != "new" {
			t.Errorf("not replaced: %+v", a)
		}
	}
}

func TestSortedGroupsFirstThenNames(t *testing.T) {
	got := Sorted([]Action{{Name: "z"}, {Name: "b", Group: "Review"}, {Name: "a", Group: "Review"}, {Name: "a"}})
	var keys []string
	for _, a := range got {
		keys = append(keys, a.Key())
	}
	if strings.Join(keys, ",") != "a,z,review/a,review/b" {
		t.Errorf("order %v", keys)
	}
}

func TestSaveLoadRoundTripAndMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", FileName)
	if list, _, err := Load(p); err != nil || len(list) != 0 {
		t.Fatalf("missing file: %v %v", list, err)
	}
	in := []Action{{Name: "Fix", Group: "Go", Prompt: "Fix it.\nThen test.", Target: TargetTerminal}}
	if err := Save(p, in); err != nil {
		t.Fatal(err)
	}
	out, notes, err := Load(p)
	if err != nil || len(notes) != 0 || len(out) != 1 || out[0] != in[0] {
		t.Fatalf("round trip: %+v %v %v", out, notes, err)
	}
	// A broken file is never overwritten by Save's callers: Load says so.
	if err := os.WriteFile(p, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(p); err == nil {
		t.Error("broken file loaded")
	}
}

func TestLoadRefusesHugeFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(p, make([]byte, MaxFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(p); err == nil {
		t.Error("huge file loaded")
	}
}

func TestSaveRefusesTooMany(t *testing.T) {
	list := make([]Action, MaxActions+1)
	for i := range list {
		list[i] = Action{Name: strings.Repeat("n", 1+i%50) + string(rune('a'+i%26)), Prompt: "p"}
	}
	if err := Save(filepath.Join(t.TempDir(), FileName), list); err == nil {
		t.Error("too many saved")
	}
}
