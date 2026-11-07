package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBTypePickerListsLibSQL(t *testing.T) {
	found := false
	for _, it := range dbTypePickerItems(false) {
		if it.ID == "libsql" {
			found = true
			if !strings.Contains(it.Title, "libSQL") {
				t.Errorf("title = %q", it.Title)
			}
		}
	}
	if !found {
		t.Fatal("the add picker has no libSQL row")
	}
}

func TestDBAddLibSQLAsksURLThenToken(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	m.databaseStartAdd("libsql")
	if m.promptKind != promptKindDBAdd || !strings.HasPrefix(m.prompt.Value(), "libsql://") {
		t.Fatalf("no libSQL URL prompt: kind=%v value=%q", m.promptKind, m.prompt.Value())
	}
	m.onDBPrompt(promptKindDBAdd, "postgres://u@h:5432/x")
	if !strings.Contains(covSvcToast(m), "libsql://") {
		t.Fatalf("a non-libSQL URL was taken: %q", covSvcToast(m))
	}
	m.databaseStartAdd("libsql")
	m.onDBPrompt(promptKindDBAdd, "libsql://shop-team.turso.io")
	if c := s.pendingConn; c.Driver != "libsql" || c.ID == "" {
		t.Fatalf("pending = %+v", c)
	}
	if m.promptKind != promptKindDBPassword || !strings.Contains(m.prompt.View(), "token") {
		t.Fatalf("no token prompt: kind=%v view=%q", m.promptKind, m.prompt.View())
	}
}

func TestDBLibSQLUsesSQLiteDialect(t *testing.T) {
	if _, ok := dbclient.DialectFor("libsql").(dbclient.SQLiteDialect); !ok {
		t.Fatal("libsql has no SQLite dialect")
	}
}
