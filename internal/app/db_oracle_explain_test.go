package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
)

// DBX-63: Oracle has no EXPLAIN ANALYZE. X on an Oracle statement asks no
// analyze card and runs nothing: it says so and shows the EXPLAIN PLAN
// plan from the read-only session.
func TestExplainOracleHasNoAnalyzeAndShowsThePlan(t *testing.T) {
	s := &planSession{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	conns := `{"connections":[{"id":"ora","driver":"oracle","host":"db","port":1521,"user":"app","database":"FREEPDB1"}]}`
	d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true, Files: map[string]string{"query.sql": "-- @conn ora\nUPDATE t SET x = 1;\n", ".termocode/databases.json": conns}})
	d.Action("open:query.sql")
	d.Key("esc", "down")
	explainKey(d, "X")
	if !d.WaitForText("Oracle has no EXPLAIN ANALYZE", 2*time.Second) {
		t.Fatalf("no note:\n%s", planFrame(d))
	}
	if strings.Contains(planFrame(d), "EXPLAIN ANALYZE runs the statement") {
		t.Fatalf("analyze card on Oracle:\n%s", planFrame(d))
	}
	d.Settle(300 * time.Millisecond)
	if !s.has("ro run "+dbclient.OraclePlanForm("UPDATE t SET x = 1")) || s.has("rw") {
		t.Fatalf("runs: %q", s.events())
	}
}
