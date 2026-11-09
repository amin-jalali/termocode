package app

import (
	"strings"
	"testing"
)

// Explain Analyze from the AI: a read is run with ANALYZE in the AI's
// read-only session, at once (no question, as db_query runs a read); a
// statement that is not a read is refused, since ANALYZE would run it.
func TestAIDBExplainAnalyzeOnlyForReads(t *testing.T) {
	e := newAIDBEnv(t)
	ask, _, err := e.gate(t, "db_explain", map[string]any{"conn": "local", "sql": "SELECT * FROM users", "analyze": true})
	if err != nil || ask {
		t.Fatalf("analyze of a read: ask=%v err=%v", ask, err)
	}
	if _, err := e.run(t, "db_explain", map[string]any{"conn": "local", "sql": "SELECT * FROM users", "analyze": true}); err != nil {
		t.Fatal(err)
	}
	ran := strings.Join(e.log, ";")
	if !strings.Contains(ran, "/ro: EXPLAIN (ANALYZE, FORMAT TEXT) SELECT * FROM users") {
		t.Fatalf("ran %q", ran)
	}
	if _, _, err := e.gate(t, "db_explain", map[string]any{"conn": "local", "sql": "DELETE FROM users WHERE id = 1", "analyze": true}); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("analyze of a write: %v", err)
	}
}
