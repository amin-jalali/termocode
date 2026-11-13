package dbclient

import (
	"strings"
	"testing"
)

// The selection-based rewrites (DBX-42): a selected expression becomes a
// column of a subquery or a CTE over the block's one table; a selected
// subquery becomes a CTE.
func TestApplySQLIntentionSelection(t *testing.T) {
	cols := map[string][]string{"orders": {"id", "user_id", "total", "rate"}, "items": {"id", "name", "price", "qty"}}
	cases := []struct {
		name, sql, sel, id, title, want string
	}{
		{"expression to subquery", "SELECT name, price * qty FROM items WHERE price * qty > 10 ORDER BY price * qty", "price * qty",
			IntentExtractExprSub, "Extract price * qty into subquery column expr",
			"SELECT name, expr FROM (SELECT items.*, price * qty AS expr FROM items) items WHERE expr > 10 ORDER BY expr"},
		{"expression to cte keeps the alias name", "SELECT i.price * i.qty AS total FROM items i WHERE i.price * i.qty > 10", "i.price * i.qty",
			IntentExtractExprCTE, "Extract i.price * i.qty into CTE items_total",
			"WITH items_total AS (SELECT i.*, i.price * i.qty AS total FROM items i)\nSELECT total FROM items_total i WHERE total > 10"},
		{"expression in a nested block with known columns", "SELECT * FROM users WHERE EXISTS (SELECT 1 FROM orders WHERE total * rate > 1)", "total * rate",
			IntentExtractExprSub, "Extract total * rate into subquery column expr",
			"SELECT * FROM users WHERE EXISTS (SELECT 1 FROM (SELECT orders.*, total * rate AS expr FROM orders) orders WHERE expr > 1)"},
		{"subquery to cte", "SELECT * FROM users WHERE id IN (SELECT user_id FROM orders WHERE total > 5)", "SELECT user_id FROM orders WHERE total > 5",
			IntentExtractSelCTE, "Extract the selected subquery as CTE sub",
			"WITH sub AS (SELECT user_id FROM orders WHERE total > 5)\nSELECT * FROM users WHERE id IN (SELECT * FROM sub)"},
		{"subquery with parens after a with list", "WITH a AS (SELECT 1 AS n)\nSELECT n, (SELECT max(total) FROM orders) FROM a", "(SELECT max(total) FROM orders)",
			IntentExtractSelCTE, "Extract the selected subquery as CTE sub",
			"WITH a AS (SELECT 1 AS n),\nsub AS (SELECT max(total) FROM orders)\nSELECT n, (SELECT * FROM sub) FROM a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := strings.Index(tc.sql, tc.sel)
			o := SQLIntentOpts{SelStart: at, SelEnd: at + len(tc.sel)}
			list := intentIDs(SQLIntentionsWith(tc.sql, "", at, cols, o))
			if list[tc.id] != tc.title {
				t.Fatalf("intentions = %v, want %s %q", list, tc.id, tc.title)
			}
			got, err := ApplySQLIntentionWith(tc.sql, "", tc.id, at, cols, o)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// What would change the meaning, or what we cannot read, is not offered.
func TestSQLIntentionSelectionNotOffered(t *testing.T) {
	cases := []struct {
		name, sql, sel string
		cols           map[string][]string
		id             string
	}{
		{"aggregate", "SELECT count(*) + 1 FROM t", "count(*) + 1", nil, IntentExtractExprSub},
		{"two tables", "SELECT a + b FROM t JOIN u ON true", "a + b", nil, IntentExtractExprSub},
		{"outer name", "SELECT * FROM users u WHERE EXISTS (SELECT 1 FROM orders o WHERE o.total * u.rate > 1)", "o.total * u.rate", nil, IntentExtractExprSub},
		{"nested without columns", "SELECT * FROM users WHERE EXISTS (SELECT 1 FROM orders WHERE total * rate > 1)", "total * rate", nil, IntentExtractExprCTE},
		{"half an expression", "SELECT (a + b) * c FROM t", "(a + b", nil, IntentExtractExprSub},
		{"part of a token", "SELECT price * qty FROM t", "rice * qty", nil, IntentExtractExprSub},
		{"not a subquery", "SELECT a FROM t WHERE a = 1", "a = 1", nil, IntentExtractSelCTE},
		{"a bare column", "SELECT a, b FROM t", "b", nil, IntentExtractExprSub},
		{"correlated subquery", "SELECT * FROM users u WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id)", "SELECT 1 FROM orders o WHERE o.user_id = u.id", nil, IntentExtractSelCTE},
		{"under insert", "INSERT INTO a SELECT x FROM t WHERE x IN (SELECT y FROM u)", "SELECT y FROM u", nil, IntentExtractSelCTE},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := strings.Index(tc.sql, tc.sel)
			o := SQLIntentOpts{SelStart: at, SelEnd: at + len(tc.sel)}
			if title, ok := intentIDs(SQLIntentionsWith(tc.sql, "", at, tc.cols, o))[tc.id]; ok {
				t.Fatalf("%s offered: %q", tc.id, title)
			}
			if _, err := ApplySQLIntentionWith(tc.sql, "", tc.id, at, tc.cols, o); err == nil {
				t.Fatalf("%s applied", tc.id)
			}
		})
	}
	// Without a selection none of them is offered.
	sql := "SELECT price * qty FROM items"
	for _, id := range []string{IntentExtractExprSub, IntentExtractExprCTE, IntentExtractSelCTE} {
		if _, ok := intentIDs(SQLIntentions(sql, "", 8, nil))[id]; ok {
			t.Fatalf("%s offered without a selection", id)
		}
	}
}
