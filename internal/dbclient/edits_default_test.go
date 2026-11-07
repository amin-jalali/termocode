package dbclient

import (
	"regexp"
	"strings"
	"testing"
)

// DBX-55: a cell can be set back to the column's DEFAULT. The keyword is
// written into the SQL (it is not a value), never bound as the text "DEFAULT".
func TestEditStatementsSetDefault(t *testing.T) {
	key := []EditValue{{Column: "id", Value: Cell{Text: "7"}}}
	upd := RowEdit{Kind: EditUpdate, Key: key, Values: []EditValue{
		{Column: "plan", Default: true, Old: Cell{Text: "pro"}, Check: true},
		{Column: "name", Value: Cell{Text: "Ann"}},
	}}
	ins := RowEdit{Kind: EditInsert, Values: []EditValue{
		{Column: "id", Default: true},
		{Column: "name", Value: Cell{Text: "Bo"}},
	}}
	cases := []struct {
		d       Dialect
		upd     string
		ins     string
		preview string
	}{
		{PostgresDialect{}, `UPDATE "t" SET "plan" = DEFAULT, "name" = $1 WHERE "id" = $2 AND "plan" = $3`,
			`INSERT INTO "t" ("id", "name") VALUES (DEFAULT, $1)`, `UPDATE "t" SET "plan" = DEFAULT, "name" = 'Ann' WHERE "id" = '7' AND "plan" = 'pro';`},
		{MySQLDialect{}, "UPDATE `t` SET `plan` = DEFAULT, `name` = ? WHERE `id` = ? AND `plan` = ?",
			"INSERT INTO `t` (`id`, `name`) VALUES (DEFAULT, ?)", "UPDATE `t` SET `plan` = DEFAULT, `name` = 'Ann' WHERE `id` = '7' AND `plan` = 'pro';"},
	}
	for _, c := range cases {
		stmts, err := EditStatements(c.d, "", "t", []RowEdit{upd, ins})
		if err != nil {
			t.Fatal(err)
		}
		if stmts[0].SQL != c.upd || stmts[1].SQL != c.ins {
			t.Fatalf("%T:\n got %q\n     %q", c.d, stmts[0].SQL, stmts[1].SQL)
		}
		if stmts[0].Preview != c.preview {
			t.Fatalf("%T preview %q", c.d, stmts[0].Preview)
		}
		if len(stmts[0].Args) != 3 || len(stmts[1].Args) != 1 {
			t.Fatalf("%T args %v / %v", c.d, stmts[0].Args, stmts[1].Args)
		}
		for _, st := range stmts {
			for _, a := range st.Args {
				if a == "DEFAULT" {
					t.Fatalf("DEFAULT was bound as a value: %v", st.Args)
				}
			}
		}
	}
}

// SQLite has no DEFAULT keyword in VALUES or SET: an INSERT leaves the
// column out (so it gets its default), an UPDATE says it cannot.
func TestEditStatementsSetDefaultSQLite(t *testing.T) {
	ins := RowEdit{Kind: EditInsert, Values: []EditValue{{Column: "id", Default: true}, {Column: "name", Value: Cell{Text: "Bo"}}}}
	stmts, err := EditStatements(SQLiteDialect{}, "", "t", []RowEdit{ins})
	if err != nil {
		t.Fatal(err)
	}
	if want := `INSERT INTO "t" ("name") VALUES (?)`; stmts[0].SQL != want {
		t.Fatalf("got %q", stmts[0].SQL)
	}
	all := RowEdit{Kind: EditInsert, Values: []EditValue{{Column: "id", Default: true}}}
	stmts, err = EditStatements(SQLiteDialect{}, "", "t", []RowEdit{all})
	if err != nil {
		t.Fatal(err)
	}
	if want := `INSERT INTO "t" DEFAULT VALUES`; stmts[0].SQL != want {
		t.Fatalf("got %q", stmts[0].SQL)
	}
	upd := RowEdit{Kind: EditUpdate, Key: []EditValue{{Column: "id", Value: Cell{Text: "1"}}}, Values: []EditValue{{Column: "plan", Default: true}}}
	if _, err := EditStatements(SQLiteDialect{}, "", "t", []RowEdit{upd}); err == nil || !strings.Contains(err.Error(), "DEFAULT") {
		t.Fatalf("want a clear SQLite error, got %v", err)
	}
}

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// DBX-55: "Generate UUID" fills a cell with a fresh random (version 4) UUID.
func TestNewCellUUID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		c := NewCellUUID()
		if c.Null || !uuidV4.MatchString(c.Text) {
			t.Fatalf("not a v4 UUID: %+v", c)
		}
		if seen[c.Text] {
			t.Fatalf("UUID repeated: %s", c.Text)
		}
		seen[c.Text] = true
	}
}
