package wire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow/run"
)

var _ run.RunFileWriter = RunFiles{}

// Spec (WFX wiring, plan §4.3): run files are written atomically under
// <root>/runs/<id>.json (private), listed newest first per workflow and
// read back; a bad id never leaves the folder.
func TestRunFilesStore(t *testing.T) {
	root := t.TempDir()
	rs := RunFiles{Root: root}
	for _, id := range []string{"20261106-090000", "20261107-090000"} {
		if err := rs.WriteRunFile(id, []byte(`{"version":1,"id":"`+id+`","workflow":"bugs","state":"done"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := rs.WriteRunFile("x", []byte(`{"version":1,"id":"x","workflow":"other","state":"done"}`)); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(root, "runs", "20261106-090000.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("run file %v %v", fi, err)
	}
	got, err := rs.List("bugs", 0)
	if err != nil || len(got) != 2 || got[0].ID != "20261107-090000" {
		t.Fatalf("List = %+v, %v", got, err)
	}
	if all, _ := rs.List("", 1); len(all) != 1 {
		t.Fatalf("limit: %d", len(all))
	}
	rf, err := rs.Load("20261106-090000")
	if err != nil || rf.Workflow != "bugs" {
		t.Fatalf("Load = %+v, %v", rf, err)
	}
	for _, bad := range []string{"../x", "", ".hidden", "a/b"} {
		if err := rs.WriteRunFile(bad, []byte("{}")); err == nil {
			t.Errorf("id %q accepted", bad)
		}
	}
	ents, _ := os.ReadDir(filepath.Join(root, "runs"))
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}
