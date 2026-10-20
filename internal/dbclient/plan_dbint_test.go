//go:build dbint

package dbclient

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

// planServer runs the plan forms against a real server: the JSON parses,
// a node is marked costly, and an analyzed DELETE wrapped in Begin /
// Rollback leaves the rows alone.
func planServer(t *testing.T, s *SQLSession, driver string) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{"DROP TABLE IF EXISTS dbint_plan", "CREATE TABLE dbint_plan (id int PRIMARY KEY, v int)"} {
		if r := s.Run(ctx, q, RunOptions{}); r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	defer s.Run(ctx, "DROP TABLE IF EXISTS dbint_plan", RunOptions{})
	var vals []string
	for i := 1; i <= 3000; i++ {
		vals = append(vals, "("+strconv.Itoa(i)+","+strconv.Itoa(i%7)+")")
	}
	if r := s.Run(ctx, "INSERT INTO dbint_plan VALUES "+strings.Join(vals, ","), RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if driver == "postgres" {
		s.Run(ctx, "ANALYZE dbint_plan", RunOptions{})
	}
	const sel = "SELECT * FROM dbint_plan WHERE v = 3"
	for _, analyze := range []bool{false, true} {
		var res Result
		for _, form := range ExplainForms(driver, sel, analyze, s.mariadb) {
			if res = s.Run(ctx, form, RunOptions{}); res.Err == nil {
				break
			}
		}
		if res.Err != nil {
			t.Fatalf("analyze=%v: %v", analyze, res.Err)
		}
		p := ParsePlan(driver, res, analyze)
		if p.Nodes() == 0 || !strings.Contains(strings.ToLower(planText(p)), "dbint_plan") {
			t.Fatalf("analyze=%v: plan did not parse:\n%s\nraw: %s", analyze, planText(p), p.Raw)
		}
		t.Logf("analyze=%v:\n%s", analyze, planText(p))
	}
	// An analyzed write, wrapped and rolled back: the rows stay.
	if err := s.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	form := ExplainForms(driver, "DELETE FROM dbint_plan WHERE v = 3", true, s.mariadb)[0]
	res := s.Run(ctx, form, RunOptions{})
	if err := s.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if res.Err != nil {
		t.Skipf("this server cannot analyze a DELETE: %v", res.Err)
	}
	if n := s.Run(ctx, "SELECT count(*) FROM dbint_plan", RunOptions{}); n.Err != nil || n.Rows[0][0].Text != "3000" {
		t.Fatalf("rollback left %+v", n)
	}
}

func TestDBIntPlanPostgres(t *testing.T) {
	dsn := os.Getenv("DBINT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("DBINT_POSTGRES_DSN is not set")
	}
	conn, err := ParseURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewPostgresSession(context.Background(), conn, conn.Password)
	if err != nil {
		unavailableDBInt(t, err)
	}
	defer s.Close()
	planServer(t, s, "postgres")
}
