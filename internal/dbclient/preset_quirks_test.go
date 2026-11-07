package dbclient

import (
	"reflect"
	"strings"
	"testing"
)

func TestPGPresetQueriesQuirks(t *testing.T) {
	base := PostgresDialect{}.CatalogQueries()
	rs, _ := PGPresetByID("redshift")
	q := rs.Queries(base)
	if strings.Contains(q["objects"], "pg_matviews") || q["sequences"] != "" {
		t.Fatalf("redshift queries keep matviews/sequences: %q / %q", q["objects"], q["sequences"])
	}
	if !strings.Contains(base["objects"], "pg_matviews") {
		t.Fatal("Queries changed the base map")
	}
	ts, _ := PGPresetByID("timescaledb")
	if q := ts.Queries(base); !strings.Contains(q["hypertables"], "timescaledb_information.hypertables") {
		t.Fatalf("timescale has no hypertables query: %q", q["hypertables"])
	}
	neon, _ := PGPresetByID("neon")
	if q := neon.Queries(base); !reflect.DeepEqual(q, base) {
		t.Fatal("a preset without quirks changed the queries")
	}
}

func TestMarkHypertables(t *testing.T) {
	objs := []SchemaObject{{Name: "metrics", Kind: "BASE TABLE"}, {Name: "users", Kind: "BASE TABLE"}, {Name: "v", Kind: "VIEW"}}
	MarkHypertables(objs, []string{"metrics", "v"})
	if objs[0].Kind != "HYPERTABLE" || objs[1].Kind != "BASE TABLE" || objs[2].Kind != "VIEW" {
		t.Fatalf("objects = %+v", objs)
	}
}

func TestPresetExplainForms(t *testing.T) {
	if got := PresetExplainForms("redshift", "postgres", "SELECT 1;", true, false); !reflect.DeepEqual(got, []string{"EXPLAIN SELECT 1"}) {
		t.Fatalf("redshift = %v", got)
	}
	if got := PresetExplainForms("cockroachdb", "postgres", "SELECT 1", true, false); !reflect.DeepEqual(got, []string{"EXPLAIN ANALYZE SELECT 1"}) {
		t.Fatalf("cockroach analyze = %v", got)
	}
	if got := PresetExplainForms("cockroachdb", "postgres", "SELECT 1", false, false); !reflect.DeepEqual(got, []string{"EXPLAIN SELECT 1"}) {
		t.Fatalf("cockroach = %v", got)
	}
	if got, want := PresetExplainForms("neon", "postgres", "SELECT 1", true, false), ExplainForms("postgres", "SELECT 1", true, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("neon = %v, want %v", got, want)
	}
	if !PGPresetNoAnalyze("redshift") || PGPresetNoAnalyze("cockroachdb") || PGPresetNoAnalyze("") {
		t.Fatal("PGPresetNoAnalyze is wrong")
	}
}
