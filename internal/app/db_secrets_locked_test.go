package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// lockedDBModel is a Databases view with one connection and a locked age
// store (an earlier session left it). saved names are put in the store
// first.
func lockedDBModel(t *testing.T, c dbclient.Conn, saved ...string) *Model {
	t.Helper()
	dir, _ := withSecretSetting(t, "age", false)
	s, err := secrets.Open(secrets.Options{Dir: dir, Backend: "age", ScryptWorkFactor: 14})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unlock(secretsTestPass); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ai.key.openai", secretsTestKey); err != nil {
		t.Fatal(err)
	}
	for _, name := range saved {
		if err := s.Set(name, "db-pass-123456"); err != nil {
			t.Fatal(err)
		}
	}
	s.Lock()
	if !storeLocked(t) {
		t.Fatal("store should start locked")
	}
	m := secretsTestModel(t)
	m.activity = activity.New()
	st := newDBState()
	st.root, st.conns = t.TempDir(), []dbclient.Conn{c}
	st.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.Name, path: c.ID, conn: c.ID}
	m.databases = st
	return m
}

// A connection with no saved password connects while the store is
// locked: a fresh setup has a locked encrypted store, and a SQLite file
// needs nothing from it.
func TestDatabaseOpenLockedStoreWithoutSavedPassword(t *testing.T) {
	c := dbclient.Conn{ID: "app", Name: "app", Driver: "sqlite", Path: "app.db"}
	m := lockedDBModel(t, c)
	if cmd := m.databaseOpen(c.ID, false); cmd == nil {
		t.Fatal("no connect command")
	}
	if m.secretsPromptOpen() {
		t.Fatal("asked for the passphrase though nothing is saved for this connection")
	}
	if p := m.databases.nodes[c.ID].problem; p != "" {
		t.Fatalf("row problem %q", p)
	}
	if pw, err := dbAIPassword(m.databases.root, c); err != nil || pw != "" {
		t.Fatalf("AI password = %q, %v; want empty and no error", pw, err)
	}
}

// A connection whose password is in the locked store asks for the
// passphrase first, then connects.
func TestDatabaseOpenLockedStoreAsksWhenPasswordSaved(t *testing.T) {
	for _, tc := range []struct {
		conn dbclient.Conn
		name string
	}{
		{dbclient.Conn{ID: "pg", Name: "pg", Driver: "postgres", Host: "localhost", Port: 5432}, "db.pg"},
		{dbclient.Conn{ID: "pg2", Name: "pg2", Driver: "postgres", Host: "localhost", Port: 5432, Password: "${secret:shared.pg}"}, "shared.pg"},
	} {
		m := lockedDBModel(t, tc.conn, tc.name)
		m.databaseOpen(tc.conn.ID, false)
		if !m.secretsPromptOpen() {
			t.Fatalf("%s: no passphrase prompt for a saved password", tc.conn.ID)
		}
		if p := m.databases.nodes[tc.conn.ID].problem; strings.Contains(p, "locked") {
			t.Fatalf("%s: row shows %q instead of asking", tc.conn.ID, p)
		}
	}
}
