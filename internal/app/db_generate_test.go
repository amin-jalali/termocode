package app

import (
	"strings"
	"testing"
)

// DBX-43: c on a table in the tree makes code from its columns: SQL
// templates, column lists and a Go / TypeScript / Python type.
func dbGenFixture() (dbTable, dbTableMeta) {
	t := dbTable{conn: "local", driver: "postgres", schema: "public", name: "user_accounts"}
	meta := dbTableMeta{cols: []dbColInfo{
		{name: "id", typ: "bigint", notNull: true, pk: true},
		{name: "email", typ: "text", notNull: true},
		{name: "created_at", typ: "timestamp with time zone"},
		{name: "score", typ: "numeric"},
		{name: "active", typ: "boolean", notNull: true},
	}}
	return t, meta
}

func TestDBGenerateSQLTemplates(t *testing.T) {
	tbl, meta := dbGenFixture()
	cases := map[string]string{
		"select": "SELECT \"id\", \"email\", \"created_at\", \"score\", \"active\"\nFROM \"public\".\"user_accounts\";",
		"insert": "INSERT INTO \"public\".\"user_accounts\" (\"id\", \"email\", \"created_at\", \"score\", \"active\")\nVALUES ($1, $2, $3, $4, $5);",
		"update": "UPDATE \"public\".\"user_accounts\"\nSET \"email\" = $1, \"created_at\" = $2, \"score\" = $3, \"active\" = $4\nWHERE \"id\" = $5;",
		"delete": "DELETE FROM \"public\".\"user_accounts\"\nWHERE \"id\" = $1;",
		"drop":   "DROP TABLE \"public\".\"user_accounts\";",
		"names":  "id, email, created_at, score, active",
		"qnames": "\"public\".\"user_accounts\".\"id\", \"public\".\"user_accounts\".\"email\", \"public\".\"user_accounts\".\"created_at\", \"public\".\"user_accounts\".\"score\", \"public\".\"user_accounts\".\"active\"",
	}
	for kind, want := range cases {
		got, ok := dbGenerate(kind, tbl, meta)
		if !ok || got != want {
			t.Errorf("%s:\n got %q\nwant %q", kind, got, want)
		}
	}
}

func TestDBGenerateSQLiteAndNoKey(t *testing.T) {
	tbl := dbTable{driver: "sqlite", name: "notes"}
	meta := dbTableMeta{cols: []dbColInfo{{name: "body", typ: "TEXT"}}}
	if got, _ := dbGenerate("insert", tbl, meta); got != "INSERT INTO \"notes\" (\"body\")\nVALUES (?);" {
		t.Errorf("insert: %q", got)
	}
	// Without a primary key the WHERE is left for the user to write.
	if got, _ := dbGenerate("update", tbl, meta); got != "UPDATE \"notes\"\nSET \"body\" = ?\nWHERE <condition>;" {
		t.Errorf("update: %q", got)
	}
	if got, _ := dbGenerate("delete", tbl, meta); got != "DELETE FROM \"notes\"\nWHERE <condition>;" {
		t.Errorf("delete: %q", got)
	}
	view := dbTable{driver: "sqlite", name: "v", view: true}
	if got, _ := dbGenerate("drop", view, meta); got != "DROP VIEW \"v\";" {
		t.Errorf("drop view: %q", got)
	}
	if _, ok := dbGenerate("insert", tbl, dbTableMeta{}); ok {
		t.Error("no columns should give nothing")
	}
	if _, ok := dbGenerate("nope", tbl, meta); ok {
		t.Error("unknown kind")
	}
}

func TestDBGenerateTypes(t *testing.T) {
	tbl, meta := dbGenFixture()
	goWant := "type UserAccounts struct {\n" +
		"\tID        int64      `db:\"id\" json:\"id\"`\n" +
		"\tEmail     string     `db:\"email\" json:\"email\"`\n" +
		"\tCreatedAt *time.Time `db:\"created_at\" json:\"created_at\"`\n" +
		"\tScore     *float64   `db:\"score\" json:\"score\"`\n" +
		"\tActive    bool       `db:\"active\" json:\"active\"`\n" +
		"}"
	if got, _ := dbGenerate("go", tbl, meta); got != goWant {
		t.Errorf("go:\n%s\nwant\n%s", got, goWant)
	}
	tsWant := "export interface UserAccounts {\n  id: number;\n  email: string;\n  created_at: string | null;\n  score: number | null;\n  active: boolean;\n}"
	if got, _ := dbGenerate("ts", tbl, meta); got != tsWant {
		t.Errorf("ts:\n%s", got)
	}
	pyWant := "@dataclass\nclass UserAccounts:\n    id: int\n    email: str\n    active: bool\n    created_at: Optional[datetime] = None\n    score: Optional[float] = None"
	if got, _ := dbGenerate("python", tbl, meta); got != pyWant {
		t.Errorf("python:\n%s", got)
	}
}

func TestDBGenerateOddNames(t *testing.T) {
	tbl := dbTable{driver: "postgres", name: "2 odd-table"}
	meta := dbTableMeta{cols: []dbColInfo{{name: "a b", typ: "int", notNull: true}, {name: "from", typ: "text", notNull: true}}}
	got, _ := dbGenerate("go", tbl, meta)
	if !strings.HasPrefix(got, "type T2OddTable struct {") || !strings.Contains(got, "AB ") || !strings.Contains(got, "From ") {
		t.Errorf("go: %s", got)
	}
	py, _ := dbGenerate("python", tbl, meta)
	if !strings.Contains(py, "    a_b: int") || !strings.Contains(py, "    from_: str") {
		t.Errorf("python: %s", py)
	}
	ts, _ := dbGenerate("ts", tbl, meta)
	if !strings.Contains(ts, `  "a b": number;`) {
		t.Errorf("ts: %s", ts)
	}
}

func TestDBGenerateItemsForViews(t *testing.T) {
	for _, it := range dbGenerateItems(true) {
		switch it.ID {
		case "insert", "update", "delete":
			t.Errorf("a view offers %s", it.ID)
		}
	}
	if n := len(dbGenerateItems(false)); n < 10 {
		t.Errorf("table items = %d", n)
	}
}
