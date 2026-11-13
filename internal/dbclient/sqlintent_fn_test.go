package dbclient

import (
	"strings"
	"testing"
)

// Extract subquery as table function (PostgreSQL, DBX-42): the FROM
// subquery at the cursor becomes a LANGUAGE sql function; its whole
// numbers become parameters, its rows SETOF the table or a typed TABLE.
func TestApplySQLIntentionTableFunction(t *testing.T) {
	cols := map[string][]string{"orders": {"id", "user_id", "total"}}
	types := map[string]map[string]string{"orders": {"id": "bigint", "user_id": "bigint", "total": "numeric(10,2)"}}
	cases := []struct {
		name, sql, at, title, want string
	}{
		{"setof the table", "SELECT x.id FROM (SELECT * FROM orders WHERE total > 100) x", "total",
			"Extract subquery as table function get_x",
			"CREATE FUNCTION get_x(p_total integer)\nRETURNS SETOF orders\nLANGUAGE sql STABLE\nAS $$\nSELECT * FROM orders WHERE total > p_total\n$$;\n\nSELECT x.id FROM get_x(100) x"},
		{"typed table", "SELECT r.amount FROM (SELECT o.id, o.total AS amount FROM orders o LIMIT 5) r", "LIMIT",
			"Extract subquery as table function get_r",
			"CREATE FUNCTION get_r(p_limit integer)\nRETURNS TABLE (id bigint, amount numeric(10,2))\nLANGUAGE sql STABLE\nAS $$\nSELECT o.id, o.total AS amount FROM orders o LIMIT p_limit\n$$;\n\nSELECT r.amount FROM get_r(5) r"},
		{"no numbers", "SELECT * FROM (SELECT * FROM orders) o2", "orders",
			"Extract subquery as table function get_o2",
			"CREATE FUNCTION get_o2()\nRETURNS SETOF orders\nLANGUAGE sql STABLE\nAS $$\nSELECT * FROM orders\n$$;\n\nSELECT * FROM get_o2() o2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur := strings.Index(tc.sql, tc.at)
			o := SQLIntentOpts{Types: types}
			list := intentIDs(SQLIntentionsWith(tc.sql, "postgres", cur, cols, o))
			if list[IntentTableFunc] != tc.title {
				t.Fatalf("intentions = %v, want %q", list, tc.title)
			}
			got, err := ApplySQLIntentionWith(tc.sql, "postgres", IntentTableFunc, cur, cols, o)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestSQLIntentionTableFunctionNotOffered(t *testing.T) {
	types := map[string]map[string]string{"orders": {"id": "bigint", "total": "numeric"}}
	cases := []struct {
		name, driver, sql, at string
		types                 map[string]map[string]string
	}{
		{"not postgres", "mysql", "SELECT * FROM (SELECT * FROM orders) x", "orders", types},
		{"types unknown", "postgres", "SELECT * FROM (SELECT id, total FROM orders) x", "total", nil},
		{"an expression column", "postgres", "SELECT * FROM (SELECT total * 2 AS t2 FROM orders) x", "total", types},
		{"star over a join", "postgres", "SELECT * FROM (SELECT * FROM orders o JOIN users u ON true) x", "orders", types},
		{"outer name", "postgres", "SELECT * FROM users u, LATERAL (SELECT * FROM orders o WHERE o.user_id = u.id) x", "orders", types},
		{"not in a subquery", "postgres", "SELECT * FROM orders", "orders", types},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur := strings.Index(tc.sql, tc.at)
			o := SQLIntentOpts{Types: tc.types}
			if title, ok := intentIDs(SQLIntentionsWith(tc.sql, tc.driver, cur, nil, o))[IntentTableFunc]; ok {
				t.Fatalf("offered: %q", title)
			}
			if _, err := ApplySQLIntentionWith(tc.sql, tc.driver, IntentTableFunc, cur, nil, o); err == nil {
				t.Fatal("applied")
			}
		})
	}
}
