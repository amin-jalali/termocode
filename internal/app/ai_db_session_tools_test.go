package app

import (
	"strings"
	"testing"
)

// db_session controls the user's console session as the GUI does: the
// web's db.tx cards (commit, roll back, the commit mode) and the TUI's
// roll-back-to-savepoint card ask every time with no "Always allow";
// status, a new savepoint and cancel run at once (no question in the GUI).
func TestAIDBSessionGateMatchesTheGUI(t *testing.T) {
	e := newAIDBEnv(t)
	tl := e.tool(t, "db_session")
	for op, ask := range map[string]bool{"status": false, "cancel": false, "savepoint": false,
		"commit": true, "rollback": true, "tx_mode": true, "rollback_savepoint": true} {
		if got, class := tl.Gate(map[string]any{"op": op}); got != ask || class != "" {
			t.Errorf("op %s: ask=%v class=%q, want ask=%v and no Always allow", op, got, class, ask)
		}
	}
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"op": "commit"}, "conn"},
		{map[string]any{"op": "tx_mode", "conn": "local", "mode": "sometimes"}, "mode"},
		{map[string]any{"op": "savepoint", "conn": "local", "name": "1 bad"}, "name"},
		{map[string]any{"op": "dance", "conn": "local"}, "op"},
	} {
		if _, _, err := e.gate(t, "db_session", tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err %v, want %q", tc.args, err, tc.want)
		}
	}
}

// The ops work on the user's own session of the Databases view: commit
// and roll back end its open transaction, savepoints are set on it, the
// commit mode switches, and cancel stops the console's running query.
func TestAIDBSessionControlsTheUsersTransaction(t *testing.T) {
	e := newAIDBEnv(t)
	s := e.m.databases
	user := &aiDBSession{name: "user"}
	s.sessions["local"] = user
	out, err := e.run(t, "db_session", map[string]any{"op": "status", "conn": "local"})
	if err != nil || !strings.Contains(out, "auto-commit") || !strings.Contains(out, "no open transaction") {
		t.Fatalf("status: %q %v", out, err)
	}
	if _, err := e.run(t, "db_session", map[string]any{"op": "commit", "conn": "local"}); err == nil || !strings.Contains(err.Error(), "no open transaction") {
		t.Fatalf("commit with nothing open: %v", err)
	}
	if _, err := e.run(t, "db_session", map[string]any{"op": "tx_mode", "conn": "local", "mode": "manual"}); err != nil {
		t.Fatal(err)
	}
	if !s.manualMode("local") {
		t.Fatal("not in manual mode")
	}
	tx := s.tx("local")
	tx.open, tx.writes = true, 3
	card := e.tool(t, "db_session").Human(map[string]any{"op": "commit", "conn": "local"})
	if !strings.Contains(card, "Commit the open transaction on local") {
		t.Errorf("commit card: %s", card)
	}
	out, err = e.run(t, "db_session", map[string]any{"op": "status", "conn": "local"})
	if err != nil || !strings.Contains(out, "manual commit") || !strings.Contains(out, "3 uncommitted changes") {
		t.Fatalf("status in a transaction: %q %v", out, err)
	}
	if out, err := e.run(t, "db_session", map[string]any{"op": "savepoint", "conn": "local", "name": "before_fix"}); err != nil || !strings.Contains(out, "before_fix") {
		t.Fatalf("savepoint: %q %v", out, err)
	}
	if !strings.Contains(strings.Join(user.ran, ";"), "SAVEPOINT before_fix") {
		t.Fatalf("savepoint not sent on the user's session: %v", user.ran)
	}
	if _, err := e.run(t, "db_session", map[string]any{"op": "rollback_savepoint", "conn": "local", "name": "before_fix"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(user.ran, ";"), "ROLLBACK TO SAVEPOINT before_fix") {
		t.Fatalf("rollback to savepoint not sent: %v", user.ran)
	}
	if _, err := e.run(t, "db_session", map[string]any{"op": "rollback_savepoint", "conn": "local", "name": "never_set"}); err == nil {
		t.Fatal("a savepoint that was never set was rolled back to")
	}
	if out, err := e.run(t, "db_session", map[string]any{"op": "rollback", "conn": "local"}); err != nil || !strings.Contains(out, "Roll") {
		t.Fatalf("rollback: %q %v", out, err)
	}
	cancelled := false
	s.console = &dbConsoleState{cancel: func() { cancelled = true }, running: &dbRun{conn: "local"}}
	if out, err := e.run(t, "db_session", map[string]any{"op": "cancel", "conn": "local"}); err != nil || !cancelled || !strings.Contains(out, "stopped") {
		t.Fatalf("cancel: %q %v cancelled=%v", out, err, cancelled)
	}
	s.console = &dbConsoleState{}
	if out, err := e.run(t, "db_session", map[string]any{"op": "cancel", "conn": "local"}); err != nil || !strings.Contains(out, "Nothing runs") {
		t.Fatalf("cancel with nothing running: %q %v", out, err)
	}
}
