package dbclient

import (
	"strings"
	"testing"
)

// at is the byte offset of the n-th (1-based) match of sub in s, plus off.
func intentAt(t *testing.T, s, sub string, n, off int) int {
	t.Helper()
	i := -1
	for k := 0; k < n; k++ {
		j := strings.Index(s[i+1:], sub)
		if j < 0 {
			t.Fatalf("%q not found %d times in %q", sub, n, s)
		}
		i += 1 + j
	}
	return i + off
}

func intentIDs(list []SQLIntention) map[string]string {
	out := map[string]string{}
	for _, in := range list {
		out[in.ID] = in.Title
	}
	return out
}

// Each intention rewrites only what it must; the rest of the text stays.
func TestApplySQLIntention(t *testing.T) {
	cols := map[string][]string{
		"users":  {"id", "name"},
		"orders": {"id", "user_id", "total"},
		"people": {"id", "Email"},
	}
	cases := []struct {
		name, driver, sql, at string
		nth, off              int
		id, title, want       string
	}{
		{"alias", "", "SELECT orders.id FROM orders WHERE orders.total > 5", "orders WHERE", 1, 2,
			IntentAlias, "Introduce alias o for orders", "SELECT o.id FROM orders o WHERE o.total > 5"},
		{"alias two words", "", "SELECT * FROM order_items", "order_items", 1, 0,
			IntentAlias, "Introduce alias oi for order_items", "SELECT * FROM order_items oi"},
		{"qualify", "", "SELECT id, name FROM users u JOIN orders o ON o.user_id = u.id WHERE total > 10 ORDER BY name", "SELECT", 1, 0,
			IntentQualify, "Qualify 3 column names", "SELECT id, u.name FROM users u JOIN orders o ON o.user_id = u.id WHERE o.total > 10 ORDER BY u.name"},
		{"expand star", "", "SELECT * FROM users", "*", 1, 0,
			IntentExpandStar, "Expand * into 2 columns", "SELECT id, name FROM users"},
		{"expand alias star", "", "SELECT u.* FROM users u", "*", 1, 0,
			IntentExpandStar, "Expand u.* into 2 columns", "SELECT u.id, u.name FROM users u"},
		{"expand quotes mixed case", "postgres", "SELECT * FROM people", "*", 1, 0,
			IntentExpandStar, "Expand * into 2 columns", `SELECT id, "Email" FROM people`},
		{"wrap", "", "SELECT * FROM orders o WHERE o.id = 1", "orders", 1, 1,
			IntentSubquery, "Wrap orders in a subquery", "SELECT * FROM (SELECT * FROM orders) o WHERE o.id = 1"},
		{"extract cte", "", "SELECT x.n FROM (SELECT count(*) AS n FROM t) x", "count", 1, 0,
			IntentExtractCTE, "Extract subquery as CTE x", "WITH x AS (SELECT count(*) AS n FROM t)\nSELECT x.n FROM x"},
		{"extract cte after the list", "", "WITH a AS (SELECT 1 AS n)\nSELECT * FROM (SELECT n FROM a) s", "n FROM a", 1, 0,
			IntentExtractCTE, "Extract subquery as CTE s", "WITH a AS (SELECT 1 AS n),\ns AS (SELECT n FROM a)\nSELECT * FROM s"},
		{"inline cte", "", "WITH x AS (SELECT 1 AS n)\nSELECT n FROM x", "x", 1, 0,
			IntentInlineCTE, "Inline CTE x", "SELECT n FROM (SELECT 1 AS n) x"},
		{"inline first of two", "", "WITH a AS (SELECT 1 AS n), b AS (SELECT n FROM a)\nSELECT n FROM b", "a AS", 1, 0,
			IntentInlineCTE, "Inline CTE a", "WITH b AS (SELECT n FROM (SELECT 1 AS n) a)\nSELECT n FROM b"},
		{"param string everywhere", "", "SELECT * FROM t WHERE status = 'open' OR note = 'open'", "'open'", 1, 1,
			IntentParam, "Replace 'open' with parameter :status (2 places)", "SELECT * FROM t WHERE status = :status OR note = :status"},
		{"param number here only", "", "SELECT * FROM t WHERE id = 42 LIMIT 42", "42", 1, 0,
			IntentParam, "Replace 42 with parameter :id", "SELECT * FROM t WHERE id = :id LIMIT 42"},
		{"distinct to group", "", "SELECT DISTINCT a, b AS bee FROM t ORDER BY a", "a,", 1, 0,
			IntentDistinctGroup, "Replace DISTINCT with GROUP BY", "SELECT a, b AS bee FROM t GROUP BY a, b ORDER BY a"},
		{"distinct to group lines", "", "SELECT DISTINCT a\nFROM t\nORDER BY a", "a", 1, 0,
			IntentDistinctGroup, "Replace DISTINCT with GROUP BY", "SELECT a\nFROM t\nGROUP BY a\nORDER BY a"},
		{"distinct to group at end", "", "SELECT DISTINCT a FROM t -- note", "a", 1, 0,
			IntentDistinctGroup, "Replace DISTINCT with GROUP BY", "SELECT a FROM t GROUP BY a -- note"},
		{"group to distinct", "", "SELECT a, b FROM t GROUP BY b, a", "a", 1, 0,
			IntentGroupDistinct, "Replace GROUP BY with DISTINCT", "SELECT DISTINCT a, b FROM t"},
		{"group by ordinals", "", "SELECT a, b FROM t GROUP BY 1, 2 ORDER BY a", "a", 1, 0,
			IntentGroupDistinct, "Replace GROUP BY with DISTINCT", "SELECT DISTINCT a, b FROM t ORDER BY a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur := intentAt(t, tc.sql, tc.at, tc.nth, tc.off)
			list := intentIDs(SQLIntentions(tc.sql, tc.driver, cur, cols))
			if list[tc.id] != tc.title {
				t.Fatalf("intentions = %v, want %s %q", list, tc.id, tc.title)
			}
			got, err := ApplySQLIntention(tc.sql, tc.driver, tc.id, cur, cols)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// Rewrites that would change what the statement means, or that need
// facts we do not have, are not offered.
func TestSQLIntentionsNotOffered(t *testing.T) {
	cases := []struct {
		name, sql, at string
		cols          map[string][]string
		id            string
	}{
		{"expand without columns", "SELECT * FROM users", "*", nil, IntentExpandStar},
		{"qualify without columns", "SELECT id FROM users", "id", nil, IntentQualify},
		{"qualify ambiguous only", "SELECT id FROM users u JOIN orders o ON o.uid = u.id", "id",
			map[string][]string{"users": {"id"}, "orders": {"id", "uid"}}, IntentQualify},
		{"alias already there", "SELECT * FROM orders o", "orders", nil, IntentAlias},
		{"group with aggregate", "SELECT a, count(*) FROM t GROUP BY a", "a", nil, IntentGroupDistinct},
		{"group with having", "SELECT a FROM t GROUP BY a HAVING a > 1", "a", nil, IntentGroupDistinct},
		{"group not the select list", "SELECT a, b FROM t GROUP BY a, b, c", "a", nil, IntentGroupDistinct},
		{"distinct on", "SELECT DISTINCT ON (a) a, b FROM t", "b", nil, IntentDistinctGroup},
		{"distinct star", "SELECT DISTINCT * FROM t", "*", nil, IntentDistinctGroup},
		{"inline recursive", "WITH RECURSIVE x AS (SELECT 1) SELECT * FROM x", "x", nil, IntentInlineCTE},
		{"inline used twice", "WITH x AS (SELECT 1 AS n) SELECT * FROM x JOIN x y ON true", "x", nil, IntentInlineCTE},
		{"param off a literal", "SELECT * FROM t WHERE id = 42", "id", nil, IntentParam},
		{"param decimal", "SELECT * FROM t WHERE p = 1.5", "1", nil, IntentParam},
		{"extract cte under insert", "INSERT INTO a SELECT * FROM (SELECT 1) s", "1", nil, IntentExtractCTE},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur := intentAt(t, tc.sql, tc.at, 1, 0)
			if title, ok := intentIDs(SQLIntentions(tc.sql, "", cur, tc.cols))[tc.id]; ok {
				t.Fatalf("%s offered: %q", tc.id, title)
			}
			if _, err := ApplySQLIntention(tc.sql, "", tc.id, cur, tc.cols); err == nil {
				t.Fatalf("%s applied", tc.id)
			}
		})
	}
	if got := SQLIntentions("SELECT 'open", "", 3, nil); got != nil {
		t.Fatalf("open quote: %v", got)
	}
	if _, err := ApplySQLIntention("SELECT 1", "", "nope", 0, nil); err == nil {
		t.Fatal("unknown id applied")
	}
}
