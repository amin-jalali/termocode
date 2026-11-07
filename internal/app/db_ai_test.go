package app

import (
	"errors"
	"strings"
	"testing"
)

// DBX-24: AI fix / explain / generate on top of the safe db_* tools.

func TestDBAIPaletteEntries(t *testing.T) {
	want := map[string]bool{"db-ai-fix-error": false, "db-ai-explain-query": false, "db-ai-generate-sql": false}
	for _, it := range databasesPaletteItems() {
		if _, ok := want[it.ID]; ok {
			want[it.ID] = true
			if !strings.HasPrefix(it.Title, "Databases: AI ") {
				t.Errorf("%s title %q should start with \"Databases: AI \"", it.ID, it.Title)
			}
		}
	}
	for id, seen := range want {
		if !seen {
			t.Errorf("palette has no %s", id)
		}
	}
	if !strings.HasSuffix(dbAIPaletteTitle("db-ai-generate-sql"), "…") {
		t.Error("Generate SQL asks a question first: its title ends with …")
	}
}

func TestDBAIFixAskHasSQLErrorAndSafeTools(t *testing.T) {
	pw := "hun" + "ter2"
	att, q := dbAIFixAsk("postgres", "local-pg", "SELECT nme FROM users", errors.New("column \"nme\" does not exist; dsn postgres://u:"+pw+"@h/db"))
	if !strings.Contains(att.Text, "SELECT nme FROM users") || !strings.Contains(att.Text, "does not exist") {
		t.Fatalf("attachment misses the SQL or the error: %q", att.Text)
	}
	if strings.Contains(att.Text, pw) || strings.Contains(q, pw) {
		t.Fatal("the password reached the AI")
	}
	for _, w := range []string{"local-pg", "postgres", "db_schema", "```sql"} {
		if !strings.Contains(q, w) {
			t.Errorf("question misses %q: %q", w, q)
		}
	}
	if strings.Contains(strings.ToLower(q), "run it") && !strings.Contains(q, "Do not run") {
		t.Errorf("the question must not ask the AI to run a write: %q", q)
	}
}

func TestDBAIExplainAsk(t *testing.T) {
	att, q := dbAIExplainAsk("mysql", "shop", "SELECT * FROM orders WHERE id = 1")
	if att.Text != "SELECT * FROM orders WHERE id = 1" || att.Label == "" {
		t.Fatalf("attachment = %+v", att)
	}
	for _, w := range []string{"shop", "mysql", "db_explain"} {
		if !strings.Contains(q, w) {
			t.Errorf("question misses %q: %q", w, q)
		}
	}
}

func TestDBAIGenerateAsk(t *testing.T) {
	q := dbAIGenerateAsk("sqlite", "notes", "  the ten newest notes  ")
	for _, w := range []string{"the ten newest notes", "notes", "sqlite", "db_schema", "```sql", "Do not run"} {
		if !strings.Contains(q, w) {
			t.Errorf("question misses %q: %q", w, q)
		}
	}
	if dbAIGenerateAsk("sqlite", "notes", "   ") != "" {
		t.Error("an empty description asks nothing")
	}
}

func TestDBStatementAtLine(t *testing.T) {
	text := "SELECT 1;\n\nSELECT a\nFROM t\nWHERE b = 2;\nSELECT 3;"
	cases := map[int]string{1: "SELECT 1", 3: "SELECT a\nFROM t\nWHERE b = 2", 4: "SELECT a\nFROM t\nWHERE b = 2", 6: "SELECT 3"}
	for line, want := range cases {
		got := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(dbStatementAtLine(text, "postgres", line)), ";"))
		if got != want {
			t.Errorf("line %d: got %q, want %q", line, got, want)
		}
	}
	if dbStatementAtLine("   ", "postgres", 1) != "" {
		t.Error("no SQL: empty")
	}
}
