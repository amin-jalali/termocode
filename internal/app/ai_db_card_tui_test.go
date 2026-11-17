//go:build unix

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Owner 2026-11-15: the chat's database list had no actions. In the
// desktop chat the db_connections card is drawn under its tool line: one
// row per connection with its status, a tap on the row opens Databases
// on it, and buttons run what the Databases view runs (Connect, Open,
// List tables, New query, Test, Fix… when info is missing, + Chat).

const tuiCardProject = `{"connections":[` +
	`{"id":"local","driver":"postgres","host":"localhost","port":5432,"user":"app","database":"app","password":"${env:TC_CARD_PW}"},` +
	`{"id":"prod","driver":"postgres","host":"db.example","port":5432,"user":"app","database":"app","environment":"production"}]}`

const tuiCardUser = `{"connections":[` +
	`{"id":"mss","driver":"sqlserver","host":"mss.example","port":1433,"database":"shop","password":"x"},` +
	`{"id":"rentcar-db-1","driver":"postgres","host":"rentcar-db-1","port":5432,"user":"rentcar","database":"rentcar",` +
	`"ssh":{"host":"65.109.212.14","user":"deploy","port":2222,"auth":"agent"},"container":{"name":"rentcar-db-1","context":"Baaytona"}}]}`

// dbCardModel is a chat with one db_connections call and its card.
func dbCardModel(t *testing.T) Model {
	t.Helper()
	m := aiTestModel(t)
	t.Setenv("TC_CARD_PW", "pw")
	root := t.TempDir()
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "db")
	for _, f := range []struct{ dir, name, body string }{{filepath.Join(root, ".termocode"), "databases.json", tuiCardProject}, {cfg, "connections.json", tuiCardUser}} {
		if err := os.MkdirAll(f.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.dir, f.name), []byte(f.body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dbNoSSHForTest(t)
	dbResetConnErrs(t)
	m.databases = newDBState()
	m.databases.load(root)
	m.databases.sessions["local"] = &aiDBSession{name: "user"}
	call := func(_ context.Context, fn aiIDEFunc) (string, error) { out, _, err := fn(&m); return out, err }
	tl, _ := ai.FindTool(aiDBTools(root, call), "db_connections")
	out, err := tl.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b := tl.View(nil, out)
	if b == nil {
		t.Fatalf("no card:\n%s", out)
	}
	b.ID, b.Source = "call_1", "db_connections"
	s := m.aiS()
	s.conv.Messages = append(s.conv.Messages,
		ai.StoredMessage{Role: ai.StoredUser, Text: "Which remote databases do we have connected?"},
		ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ List database connections → 4 connections", Block: b})
	return m
}

// dbCardButtons are the card's button ids by row.
func dbCardButtons(m Model) []string {
	var ids []string
	for _, l := range m.buildAITranscript(80) {
		for _, b := range l.buttons {
			if strings.HasPrefix(b.id, dbCardButton) {
				ids = append(ids, b.id)
			}
		}
	}
	return ids
}

func TestAIDBCardTUIGolden(t *testing.T) {
	m := dbCardModel(t)
	var rows []string
	for _, l := range m.buildAITranscript(80) {
		rows = append(rows, strings.TrimRight(ansi.Strip(l.text+l.styled), " "))
	}
	got := strings.Join(rows, "\n") + "\n"
	golden := filepath.Join("testdata", "ai_db_card_80.golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("no golden (UPDATE_GOLDEN=1 writes it): %v\n%s", err, got)
	}
	if string(want) != got {
		t.Errorf("the card changed.\n--- want\n%s\n--- got\n%s", want, got)
	}
	for _, w := range []string{"Databases · 4 connections", "connected", "missing password", "missing user", "nothing saved"} {
		if !strings.Contains(got, w) {
			t.Errorf("card lacks %q", w)
		}
	}
}

// Each row has its buttons; Fix… only on a row with missing info;
// Connect not on a connected one.
func TestAIDBCardTUIButtons(t *testing.T) {
	m := dbCardModel(t)
	ids := strings.Join(dbCardButtons(m), " ")
	for _, w := range []string{"dbcard:open:local", "dbcard:tables:local", "dbcard:query:local", "dbcard:test:local", "dbcard:chat:local",
		"dbcard:fix_password:prod", "dbcard:connect:prod", "dbcard:fix_user:mss", "dbcard:connect:rentcar-db-1", "dbcard:fix_password:rentcar-db-1"} {
		if !strings.Contains(ids, w) {
			t.Errorf("no button %s in %s", w, ids)
		}
	}
	for _, no := range []string{"dbcard:connect:local", "dbcard:fix_password:local", "dbcard:fix_user:rentcar-db-1", "dbcard:fix_user:prod"} {
		if strings.Contains(ids+" ", no+" ") {
			t.Errorf("unexpected button %s", no)
		}
	}
}

// Tapping a row, Open, Connect, List tables, Test, Fix… and + Chat run
// what the Databases view runs.
func TestAIDBCardTUIActions(t *testing.T) {
	sel := func(m *Model) string {
		rows := m.databases.rows()
		if c := m.databases.cursor; c >= 0 && c < len(rows) {
			return rows[c].path
		}
		return ""
	}
	t.Run("tap opens Databases on the row", func(t *testing.T) {
		m := dbCardModel(t)
		m.onAIButton("dbcard:open:prod")
		if !m.databasesVisible() || m.activity.Active() != activity.ViewDatabases || sel(&m) != "prod" {
			t.Fatalf("visible %v, row %q", m.databasesVisible(), sel(&m))
		}
	})
	t.Run("connect", func(t *testing.T) {
		m := dbCardModel(t)
		if cmd := m.onAIButton("dbcard:connect:prod"); cmd == nil {
			t.Fatal("no connect command")
		}
		if !m.databases.nodes["prod"].loading || sel(&m) != "prod" || !m.databasesVisible() {
			t.Fatalf("not connecting: %+v row %q", m.databases.nodes["prod"], sel(&m))
		}
	})
	t.Run("list tables connects a closed one", func(t *testing.T) {
		m := dbCardModel(t)
		m.onAIButton("dbcard:tables:prod")
		if !m.databases.nodes["prod"].loading {
			t.Fatal("tables did not connect")
		}
	})
	t.Run("list tables unfolds an open one", func(t *testing.T) {
		m := dbCardModel(t)
		m.databases.folded["local"] = true
		m.onAIButton("dbcard:tables:local")
		if m.databases.folded["local"] || sel(&m) != "local" {
			t.Fatalf("folded %v row %q", m.databases.folded["local"], sel(&m))
		}
	})
	t.Run("test runs the view's test", func(t *testing.T) {
		m := dbCardModel(t)
		cmd := m.onAIButton("dbcard:test:prod")
		if cmd == nil {
			t.Fatal("no test command")
		}
		if m.databases.nodes["prod"].loading {
			t.Error("a test opened the connection")
		}
	})
	t.Run("fix password asks with the view's prompt", func(t *testing.T) {
		m := dbCardModel(t)
		m.onAIButton("dbcard:fix_password:prod")
		if !m.promptOpen || m.promptKind != promptKindDBPassword || m.databases.pendingSecretID != "prod" {
			t.Fatalf("prompt %v kind %v pending %q", m.promptOpen, m.promptKind, m.databases.pendingSecretID)
		}
	})
	t.Run("fix user opens the edit form at the user", func(t *testing.T) {
		m := dbCardModel(t)
		m.onAIButton("dbcard:fix_user:mss")
		if !m.promptOpen || m.promptKind != promptKindDBEditField || m.databases.manage == nil || m.databases.manage.step != 3 {
			t.Fatalf("prompt %v kind %v manage %+v", m.promptOpen, m.promptKind, m.databases.manage)
		}
	})
	t.Run("+ chat adds the connection to the message", func(t *testing.T) {
		m := dbCardModel(t)
		m.onAIButton("dbcard:chat:rentcar-db-1")
		at := m.aiS().attachments
		if len(at) != 1 || !strings.Contains(at[0].Label, "rentcar-db-1") || !strings.Contains(at[0].Text, `db_schema {"conn":"rentcar-db-1"}`) {
			t.Fatalf("attachments = %+v", at)
		}
	})
	t.Run("a connection that is gone", func(t *testing.T) {
		m := dbCardModel(t)
		m.databases.conns = []dbclient.Conn{}
		if cmd := m.onAIButton("dbcard:connect:prod"); cmd == nil {
			t.Fatal("no word that it is gone")
		}
	})
}
