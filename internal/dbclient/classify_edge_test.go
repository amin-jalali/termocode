package dbclient

import (
	"reflect"
	"testing"
)

// Classify decides the read-only gate and the "this changes data" card:
// whenever it cannot be sure, it must say write.
func TestClassifyUncertainInputIsAWrite(t *testing.T) {
	for _, tc := range []struct {
		sql, dialect string
		want         Kind
	}{
		{"SELECT 'open", "postgres", KindWrite},                 // incomplete: cannot split
		{"SELECT 1 --x\nDELETE FROM t", "", KindWrite},           // unknown dialect: --x may not be a comment
		{"SELECT 1 -- note", "", KindRead},                       // a real comment
		{"SELECT 1 /*! , (DELETE FROM t) */", "mysql", KindWrite}, // MySQL runs /*! … */
		{"WITH a AS (SELECT 1) DELETE FROM t", "postgres", KindWrite},
		{"WITH a AS (SELECT 1) SELECT * FROM a", "postgres", KindRead},
		{"WITH a AS (SELECT 1) CREATE TABLE b AS SELECT * FROM a", "postgres", KindDDL},
		{"WITH a AS (SELECT 1)", "postgres", KindWrite},
		{"PRAGMA", "sqlite", KindWrite},
		{"PRAGMA database_list", "sqlite", KindRead},
		{"PRAGMA page_count = 5", "sqlite", KindWrite},
		{"SELECT 1 # DROP TABLE t", "mysql", KindRead},
		{"SELECT /* /* ; */ 1", "postgres", KindWrite}, // nested comment left open
	} {
		if got, _ := Classify(tc.sql, tc.dialect); got != tc.want {
			t.Errorf("Classify(%q, %q) = %v, want %v", tc.sql, tc.dialect, got, tc.want)
		}
	}
}

// Quotes inside strings and names never end them early, so a ';' or
// FROM inside a literal does not split the statement or name the table.
func TestSplitAndTableIgnoreQuotedText(t *testing.T) {
	for _, tc := range []struct {
		sql, dialect string
		parts        int
		table        string
	}{
		{`SELECT 'it''s; FROM fake' FROM real_t`, "postgres", 1, "real_t"},
		{`SELECT "a""; x" FROM real_t`, "postgres", 1, "real_t"},
		{"SELECT `a``; x` FROM real_t", "mysql", 1, "real_t"},
		{`SELECT $q$ ; FROM fake $q$ FROM real_t`, "postgres", 1, "real_t"},
		{"SELECT 1 # ; FROM fake\nFROM real_t", "mysql", 1, "real_t"},
		{"SELECT /* /* ; */ */ x FROM real_t", "postgres", 1, "real_t"}, // PostgreSQL nests comments
		{"SELECT a$b FROM real_t", "postgres", 1, "real_t"},
	} {
		_, sts := Classify(tc.sql, tc.dialect)
		if len(sts) != tc.parts || sts[0].Table != tc.table {
			t.Errorf("Classify(%q) = %+v", tc.sql, sts)
		}
	}
}

func TestSplitMySQLDelimiterOnLastLine(t *testing.T) {
	got, err := Split("SELECT 1;\nDELIMITER //", "mysql")
	if err != nil || !reflect.DeepEqual(got, []string{"SELECT 1"}) {
		t.Fatalf("Split = %q %v", got, err)
	}
}

// A SQLite trigger body keeps its inner ';' — including in quoted text
// and names with doubled quotes.
func TestSplitSQLiteTriggerWithQuotes(t *testing.T) {
	sql := `CREATE TRIGGER tr AFTER INSERT ON "t""x" BEGIN UPDATE "t""x" SET a = 'it''s; ok' -- c;
 ; END; SELECT 1`
	got, err := Split(sql, "sqlite")
	if err != nil || len(got) != 2 || got[1] != "SELECT 1" {
		t.Fatalf("Split = %q %v", got, err)
	}
	if w := twinBlockedIdentifier(`SELECT * FROM "fs""dir" -- fsdir`); w != "" {
		t.Errorf("a quoted name with doubled quotes is not the helper: %q", w)
	}
	if w := twinBlockedIdentifier("SELECT 1 -- fsdir\n, 'fsdir'"); w != "" {
		t.Errorf("comments and strings are not identifiers: %q", w)
	}
}
