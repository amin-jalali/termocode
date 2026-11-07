package dbclient

import (
	"regexp"
	"strings"
	"testing"
)

func genUsersCols() []GenColumn {
	return []GenColumn{
		{Name: "id", Type: "integer", NotNull: true, PK: true, Default: "nextval('users_id_seq'::regclass)"},
		{Name: "email", Type: "character varying", NotNull: true},
		{Name: "team_id", Type: "integer", NotNull: true},
		{Name: "active", Type: "boolean"},
		{Name: "code", Type: "varchar(5)"},
		{Name: "created_at", Type: "timestamp with time zone"},
	}
}

func TestGenerateTestDataShapesRowsByTypeAndFK(t *testing.T) {
	opt := GenOptions{Rows: 250, Seed: 7, FKColumns: map[string]bool{"team_id": true}, FKValues: map[string][]string{"team_id": {"3", "9"}}}
	got := GenerateTestData(PostgresDialect{}, `"public"."users"`, genUsersCols(), opt)
	if !strings.HasPrefix(got, "-- Test data for \"public\".\"users\": 250 rows") || !strings.Contains(got, "-- id is left to the database") {
		t.Fatalf("header:\n%s", got[:200])
	}
	if n := strings.Count(got, `INSERT INTO "public"."users" ("email", "team_id", "active", "code", "created_at") VALUES`); n != 3 {
		t.Fatalf("%d INSERT batches, want 3", n)
	}
	rowRe := regexp.MustCompile(`(?m)^  \('([^']*)', '(\d+)', (true|false), '([^']*)', '(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)'\)[,;]$`)
	rows := rowRe.FindAllStringSubmatch(got, -1)
	if len(rows) != 250 {
		t.Fatalf("%d well-formed rows, want 250:\n%s", len(rows), got[:600])
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r[1]] || !strings.HasSuffix(r[1], "@example.test") {
			t.Fatalf("email %q not unique or not a test address", r[1])
		}
		seen[r[1]] = true
		if r[2] != "3" && r[2] != "9" {
			t.Fatalf("team_id %q is not an existing team", r[2])
		}
		if len(r[4]) > 5 {
			t.Fatalf("code %q longer than varchar(5)", r[4])
		}
	}
	if again := GenerateTestData(PostgresDialect{}, `"public"."users"`, genUsersCols(), opt); again != got {
		t.Fatal("the same seed made a different script")
	}
}

func TestGenerateTestDataNotesAndDialects(t *testing.T) {
	cols := []GenColumn{
		{Name: "id", Type: "int", PK: true, NotNull: true},
		{Name: "owner_id", Type: "int", NotNull: true},
		{Name: "kind", Type: "enum('a','b''c')"},
		{Name: "flag", Type: "tinyint(1)"},
	}
	got := GenerateTestData(MySQLDialect{}, "`t`", cols, GenOptions{Rows: 4, Seed: 1, FKColumns: map[string]bool{"owner_id": true}})
	if !strings.Contains(got, "-- id is left to the database") || !strings.Contains(got, "-- owner_id: the referenced table has no rows; add some there first") {
		t.Fatalf("notes:\n%s", got)
	}
	if !strings.Contains(got, "INSERT INTO `t` (`owner_id`, `kind`, `flag`) VALUES") {
		t.Fatalf("mysql quoting:\n%s", got)
	}
	rowRe := regexp.MustCompile(`(?m)^  \(NULL, '(a|b''c)', (0|1)\)[,;]$`)
	if n := len(rowRe.FindAllString(got, -1)); n != 4 {
		t.Fatalf("%d rows match, want 4:\n%s", n, got)
	}
	only := []GenColumn{{Name: "id", Type: "INTEGER", PK: true}}
	if s := GenerateTestData(SQLiteDialect{}, `"t"`, only, GenOptions{Rows: 2}); strings.Count(s, `INSERT INTO "t" DEFAULT VALUES;`) != 2 {
		t.Fatalf("all-default rows:\n%s", s)
	}
	if s := GenerateTestData(MySQLDialect{}, "`t`", only, GenOptions{Rows: 1}); !strings.Contains(s, "INSERT INTO `t` () VALUES ();") {
		t.Fatalf("mysql all-default rows:\n%s", s)
	}
	if s := GenerateTestData(SQLiteDialect{}, `"t"`, only, GenOptions{Rows: GenMaxRows + 5}); strings.Count(s, "INSERT") != GenMaxRows {
		t.Fatal("row count is not capped")
	}
}

func TestGenerateTestDataQuotesFKValues(t *testing.T) {
	cols := []GenColumn{{Name: "ref", Type: "text"}}
	opt := GenOptions{Rows: 1, FKColumns: map[string]bool{"ref": true}, FKValues: map[string][]string{"ref": {`o'k\x`}}}
	if s := GenerateTestData(PostgresDialect{}, `"t"`, cols, opt); !strings.Contains(s, `('o''k\x');`) {
		t.Fatalf("pg literal:\n%s", s)
	}
	if s := GenerateTestData(MySQLDialect{}, "`t`", cols, opt); !strings.Contains(s, `('o''k\\x');`) {
		t.Fatalf("mysql literal:\n%s", s)
	}
}
