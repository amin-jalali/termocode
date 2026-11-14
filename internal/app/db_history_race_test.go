package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// A history write runs off the UI goroutine while the next run (on the
// UI goroutine) asks for the store again: reading the size setting must
// not race the write that is still going (NEW-5, nightly race).
func TestDBHistoryStoreWhileAnAppendRuns(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := &Model{w: 120, h: 40}
	run := &dbRun{conn: "c", sql: "SELECT 1", by: "run", result: dbclient.Result{Columns: []dbclient.Column{{Name: "a"}}}}
	cmd := m.databaseAppendHistory(run)
	done := make(chan struct{})
	go func() { defer close(done); cmd() }()
	for i := 0; i < 50; i++ {
		_ = m.databaseAppendHistory(run)
	}
	<-done
	entries, err := m.dbConsole().historyStore().List()
	if err != nil || len(entries) != 1 {
		t.Fatalf("history = %d entries, %v; want the one written", len(entries), err)
	}
}
