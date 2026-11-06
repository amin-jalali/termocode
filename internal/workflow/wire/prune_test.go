package wire

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Spec (WFX-18, plan §4.3): history pruning at start removes the run
// files (and the run's folder: write ledger, logs, audit) of runs that
// ended before the cut; running and newer runs stay.
func TestRunFilesPrune(t *testing.T) {
	root := t.TempDir()
	rs := RunFiles{Root: root}
	write := func(id, state, ended string) {
		t.Helper()
		body := `{"version":1,"id":"` + id + `","workflow":"bugs","state":"` + state + `","started_at":"2026-10-01T09:00:00Z"`
		if ended != "" {
			body += `,"ended_at":"` + ended + `"`
		}
		if err := rs.WriteRunFile(id, []byte(body+`}`)); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "runs", id), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "runs", id, "ledger.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("old", "done", "2026-10-01T09:05:00Z")
	write("live", "running", "")
	write("new", "failed", "2026-11-05T09:05:00Z")
	n, err := rs.Prune(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if err != nil || n != 1 {
		t.Fatalf("Prune = %d, %v", n, err)
	}
	for id, keep := range map[string]bool{"old": false, "live": true, "new": true} {
		_, errF := os.Stat(filepath.Join(root, "runs", id+".json"))
		_, errD := os.Stat(filepath.Join(root, "runs", id))
		if (errF == nil) != keep || (errD == nil) != keep {
			t.Errorf("%s: file %v dir %v, keep %v", id, errF, errD, keep)
		}
	}
}
