package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// genSession is a metaSession that also hands out itself as the
// read-only session the foreign key samples run in.
type genSession struct{ *metaSession }

func (s genSession) ReadOnlySession() (dbclient.Session, error) { return s, nil }
func (s genSession) Close() error                              { return nil }

// DBX-20: the script fills a table by its columns, takes foreign key
// values that exist in the referenced table (read-only sample) and leaves
// the serial key to the database.
func TestDBBuildTestDataUsesColumnsAndForeignKeys(t *testing.T) {
	ms := &metaSession{
		ir: dbclient.IntrospectionResult{
			Keys:        []string{"id"},
			Columns:     []dbclient.Column{{Name: "id", Type: "integer", PK: true}, {Name: "email", Type: "text"}, {Name: "team_id", Type: "integer"}},
			ForeignKeys: []dbclient.SchemaForeignKey{{Name: "fk_team", Column: "team_id", RefTable: "teams", RefColumn: "id"}},
		},
		rows: map[string][][]string{
			"information_schema.columns": {{"id", "integer", "NO", "nextval('users_id_seq'::regclass)"}, {"email", "text", "NO", "<null>"}, {"team_id", "integer", "NO", "<null>"}},
			`SELECT DISTINCT "id" FROM "public"."teams"`: {{"4"}, {"8"}},
		},
	}
	tbl := dbTable{conn: "c", driver: "postgres", schema: "public", name: "users"}
	script, err := dbBuildTestData(context.Background(), genSession{ms}, tbl, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "-- id is left to the database") || !strings.Contains(script, `INSERT INTO "public"."users" ("email", "team_id") VALUES`) {
		t.Fatalf("script:\n%s", script)
	}
	if n := strings.Count(script, "'4')") + strings.Count(script, "'8')"); n != 5 {
		t.Fatalf("%d rows use an existing team, want 5:\n%s", n, script)
	}
	sampled := false
	for _, q := range ms.runs {
		if strings.Contains(q, `SELECT DISTINCT "id" FROM "public"."teams" WHERE "id" IS NOT NULL LIMIT 200`) {
			sampled = true
		}
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(q)), "INSERT") {
			t.Fatalf("the generator ran a write: %q", q)
		}
	}
	if !sampled {
		t.Fatalf("no sample of the referenced column: %q", ms.runs)
	}
}

// DBX-20: the palette entry exists, opens input (…) and the AI may not
// run it.
func TestDBGenerateDataPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range databasesPaletteItems() {
		if it.ID == "db-generate-data" {
			found = it.Title == "Databases: Generate Test Data…"
		}
	}
	if !found {
		t.Fatal("no Databases: Generate Test Data… palette item")
	}
	if _, ok := aiActionPolicies["db-generate-data"]; !ok {
		t.Fatal("db-generate-data has no AI policy")
	}
}
