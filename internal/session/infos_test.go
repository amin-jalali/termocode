//go:build unix

package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Spec (MOB-8): ReadInfos reads the sessions' Info files without dialing
// a socket: a session whose server is gone is skipped, and the other
// .json files of the folder (caps, web status) are not sessions.
func TestReadInfosSkipsDeadAndOtherFiles(t *testing.T) {
	dir := t.TempDir()
	put := func(name string, in Info) {
		t.Helper()
		b, _ := json.Marshal(in)
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Unix(1_700_000_000, 0).UTC()
	put("bbbb.json", Info{Workspace: "/home/u/src/web", PID: 11, Started: at, Attached: true})
	put("aaaa.json", Info{Workspace: "/home/u/src/api", PID: 12, Started: at})
	put("cccc.json", Info{Workspace: "/home/u/src/gone", PID: 13, Started: at})
	put("aaaa.caps.json", Info{Workspace: "/home/u/src/caps", PID: 11})
	put("dddd.json", Info{PID: 11})
	if err := os.WriteFile(filepath.Join(dir, "eeee.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	alive := func(pid int) bool { return pid != 13 }
	got := ReadInfos(dir, alive)
	if len(got) != 2 || got[0].Workspace != "/home/u/src/api" || got[1].Workspace != "/home/u/src/web" || !got[1].Attached {
		t.Fatalf("ReadInfos = %+v", got)
	}
	if got := ReadInfos(filepath.Join(dir, "none"), alive); got != nil {
		t.Errorf("missing dir: %+v", got)
	}
}
