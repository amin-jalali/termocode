package dbclient

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDataSearchQueriesPerDialect(t *testing.T) {
	tables := []DataSearchTable{{Schema: "app", Table: "users", Columns: []string{"name", "e\"mail"}}}
	cases := []struct {
		driver, want string
	}{
		{"postgres", `SELECT * FROM "app"."users" WHERE LOWER(CAST("name" AS TEXT)) LIKE LOWER($1) ESCAPE '!' OR LOWER(CAST("e""mail" AS TEXT)) LIKE LOWER($2) ESCAPE '!' LIMIT 50 OFFSET 0`},
		{"mysql", "SELECT * FROM `app`.`users` WHERE LOWER(CAST(`name` AS CHAR)) LIKE LOWER(?) ESCAPE '!' OR LOWER(CAST(`e\"mail` AS CHAR)) LIKE LOWER(?) ESCAPE '!' LIMIT 50 OFFSET 0"},
	}
	for _, c := range cases {
		qs, err := DataSearchQueries(c.driver, tables, DataSearchOptions{Value: "50%_off!"})
		if err != nil || len(qs) != 1 {
			t.Fatalf("%s: %v %v", c.driver, qs, err)
		}
		if qs[0].SQL != c.want {
			t.Fatalf("%s sql:\n got %s\nwant %s", c.driver, qs[0].SQL, c.want)
		}
		if len(qs[0].Args) != 2 || qs[0].Args[0] != "%50!%!_off!!%" || qs[0].Args[1] != qs[0].Args[0] {
			t.Fatalf("%s args: %#v", c.driver, qs[0].Args)
		}
		if k, _ := Classify(qs[0].SQL, c.driver); k != KindRead {
			t.Fatalf("%s: search query must be read-only, got %s", c.driver, k)
		}
	}
}

func TestDataSearchQueriesExactAndSQLite(t *testing.T) {
	qs, err := DataSearchQueries("sqlite", []DataSearchTable{{Table: "t", Columns: []string{"a"}}},
		DataSearchOptions{Value: "x", Exact: true, RowsPerTable: 5})
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT * FROM "t" WHERE CAST("a" AS TEXT) = ? LIMIT 5 OFFSET 0`
	if qs[0].SQL != want || qs[0].Args[0] != "x" {
		t.Fatalf("got %s %#v", qs[0].SQL, qs[0].Args)
	}
}

func TestDataSearchQueriesBounds(t *testing.T) {
	if _, err := DataSearchQueries("postgres", nil, DataSearchOptions{Value: "  "}); err == nil {
		t.Fatal("empty value must fail")
	}
	if _, err := DataSearchQueries("redis", nil, DataSearchOptions{Value: "a"}); err == nil {
		t.Fatal("redis has no SQL search")
	}
	if _, err := DataSearchQueries("postgres", nil, DataSearchOptions{Value: strings.Repeat("a", 1001)}); err == nil {
		t.Fatal("too long value must fail")
	}
	var many []DataSearchTable
	for i := 0; i < 10; i++ {
		many = append(many, DataSearchTable{Table: "t", Columns: []string{"c"}})
	}
	many = append(many, DataSearchTable{Table: "empty"})
	qs, err := DataSearchQueries("postgres", many, DataSearchOptions{Value: "a", MaxTables: 3, RowsPerTable: 99999})
	if err != nil || len(qs) != 3 {
		t.Fatalf("max tables: %d %v", len(qs), err)
	}
	if !strings.Contains(qs[0].SQL, " LIMIT 1000 ") {
		t.Fatalf("rows per table must be capped: %s", qs[0].SQL)
	}
	qs, _ = DataSearchQueries("postgres", many[10:], DataSearchOptions{Value: "a"})
	if len(qs) != 0 {
		t.Fatalf("table without columns must be skipped: %v", qs)
	}
}

func TestRunDataSearchCollectsHitsAndErrors(t *testing.T) {
	qs := []DataSearchQuery{{Table: "a"}, {Table: "b"}, {Table: "c"}}
	rows := map[string]int{"a": 2, "b": 0}
	rep := RunDataSearch(context.Background(), qs, 10, func(_ context.Context, q DataSearchQuery) (int, error) {
		if q.Table == "c" {
			return 0, errors.New("permission denied")
		}
		return rows[q.Table], nil
	})
	if rep.Searched != 3 || len(rep.Hits) != 1 || rep.Hits[0].Table != "a" || rep.Hits[0].Rows != 2 {
		t.Fatalf("report: %+v", rep)
	}
	if len(rep.Errors) != 1 || rep.Errors[0].Table != "c" || rep.Canceled || rep.Truncated {
		t.Fatalf("errors: %+v", rep)
	}
}

func TestRunDataSearchStopsAtMaxHits(t *testing.T) {
	qs := []DataSearchQuery{{Table: "a"}, {Table: "b"}, {Table: "c"}}
	calls := 0
	rep := RunDataSearch(context.Background(), qs, 3, func(context.Context, DataSearchQuery) (int, error) {
		calls++
		return 2, nil
	})
	if calls != 2 || !rep.Truncated || len(rep.Hits) != 2 {
		t.Fatalf("calls=%d report=%+v", calls, rep)
	}
}

func TestRunDataSearchCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	qs := []DataSearchQuery{{Table: "a"}, {Table: "b"}}
	calls := 0
	rep := RunDataSearch(ctx, qs, 10, func(context.Context, DataSearchQuery) (int, error) {
		calls++
		cancel()
		return 0, context.Canceled
	})
	if calls != 1 || !rep.Canceled || len(rep.Errors) != 0 {
		t.Fatalf("calls=%d report=%+v", calls, rep)
	}
}
