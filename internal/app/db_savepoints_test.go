package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

type dbSavepointSess struct {
	dbclient.Session
	ran *[]string
}

func (s dbSavepointSess) Run(_ context.Context, sql string, _ dbclient.RunOptions) dbclient.Result {
	*s.ran = append(*s.ran, sql)
	return dbclient.Result{}
}

func dbSavepointModel(t *testing.T) (*Model, *[]string) {
	t.Helper()
	m := &Model{w: 120, h: 40}
	m.databases = newDBState()
	m.databases.loaded = true
	m.databases.conns = []dbclient.Conn{{ID: "pg", Name: "pg", Driver: "postgres"}}
	ran := []string{}
	m.databases.sessions["pg"] = dbSavepointSess{ran: &ran}
	m.databases.activeID = "pg"
	return m, &ran
}

// A savepoint needs an open transaction; the name must be a plain word.
func TestDBSavepointNeedsTxAndName(t *testing.T) {
	m, ran := dbSavepointModel(t)
	if _, ok := m.dispatchDatabasesPalette("db-savepoint"); !ok {
		t.Fatal("db-savepoint not dispatched")
	}
	if m.promptOpen {
		t.Fatal("asked for a name without an open transaction")
	}
	m.databases.tx("pg").open = true
	m.databaseSavepointTyped("pg", "a b; drop")
	if len(*ran) != 0 {
		t.Fatalf("a bad name was sent: %v", *ran)
	}
	msg := m.databaseSavepointTyped("pg", "before_fix")()
	m.onDBSavepoint(msg.(dbSavepointMsg))
	if strings.Join(*ran, "|") != "SAVEPOINT before_fix" {
		t.Fatalf("ran %v", *ran)
	}
	if got := m.databases.tx("pg").savepoints; len(got) != 1 || got[0] != "before_fix" {
		t.Fatalf("savepoints = %v", got)
	}
}

// Roll back to a savepoint asks first (Cancel first), then sends ROLLBACK
// TO SAVEPOINT and forgets the later savepoints.
func TestDBRollbackToSavepoint(t *testing.T) {
	m, ran := dbSavepointModel(t)
	tx := m.databases.tx("pg")
	tx.open, tx.savepoints = true, []string{"a", "b", "c"}
	m.dispatchDatabasesPalette("db-rollback-savepoint")
	if !m.pickerOpen || m.pickerKind != pickerKindDBSavepoint {
		t.Fatal("no savepoint picker")
	}
	if _, ok := m.databaseConsolePicker("b"); !ok {
		t.Fatal("pick not routed")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindDBSavepointRollback {
		t.Fatal("no card")
	}
	if !strings.Contains(m.confirm.View(), "Cancel") {
		t.Fatal("card has no Cancel")
	}
	m.confirmOpen = false
	cmd, _ := m.databaseConsoleConfirm("rollback")
	m.onDBSavepoint(cmd().(dbSavepointMsg))
	if strings.Join(*ran, "|") != "ROLLBACK TO SAVEPOINT b" {
		t.Fatalf("ran %v", *ran)
	}
	if got := strings.Join(tx.savepoints, ","); got != "a,b" {
		t.Fatalf("savepoints = %q", got)
	}
}
