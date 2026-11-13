package dbclient

import (
	"strings"
	"testing"
)

// Whatever statement the cursor is in, every intention that is offered
// can be applied, and applying it changes the text.
func TestEveryOfferedIntentionApplies(t *testing.T) {
	cols := map[string][]string{
		"users":    {"id", "name", "Mail"},
		"orders":   {"id", "user_id", "total"},
		"items":    {"id", "order_id", "sku"},
		"t":        {"a", "b"},
		"public.t": {"a", "b"},
	}
	stmts := []string{
		"SELECT * FROM users",
		"SELECT u.*, o.total FROM users u JOIN orders o ON o.user_id = u.id",
		"SELECT * FROM users u, orders o WHERE o.user_id = u.id",
		"SELECT name, total FROM users JOIN orders ON orders.user_id = users.id WHERE total > 5 ORDER BY name",
		"SELECT * FROM public.t WHERE a = 1",
		`SELECT * FROM "users" AS u LEFT JOIN orders USING (id)`,
		"SELECT a FROM (SELECT a, b FROM t WHERE b = 'x') s WHERE s.a > 3",
		"SELECT * FROM (SELECT a FROM t) AS s JOIN (SELECT b FROM t) AS r ON r.b = s.a",
		"WITH RECURSIVE r AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM r WHERE n < 5) SELECT n FROM r",
		"WITH a AS (SELECT a FROM t), b AS (SELECT a FROM a) SELECT * FROM b JOIN a ON a.a = b.a",
		"SELECT DISTINCT a, b FROM t WHERE a = 'x' AND b = 'x'",
		"SELECT a, count(*) FROM t GROUP BY a HAVING count(*) > 2 ORDER BY a",
		"SELECT a, b FROM t GROUP BY a, b",
		"SELECT * FROM t WHERE a = 1.5 OR a = 2 OR b = :p OR b = $1 OR b = ?",
		"SELECT * FROM t WHERE a IN (1, 2, 3) AND b LIKE '%x%' -- why\n",
		"SELECT /* c */ * FROM t /* d */ WHERE a = 1",
		"SELECT * FROM t WHERE a = 'it''s' AND b = E'x'",
		"UPDATE t SET a = 1 WHERE b = 2",
		"SELECT",
		"",
		"SELECT * FROM (",
		"WITH x AS (SELECT 1 SELECT * FROM x",
		"SELECT * FROM t t1 JOIN t t2 ON t1.a = t2.a JOIN t t3 ON t3.a = t2.a",
		"SELECT t.a FROM t WHERE t.b = 4 UNION SELECT a FROM t",
	}
	applied := 0
	for _, driver := range []string{"", "postgres", "mysql", "sqlserver"} {
		for _, stmt := range stmts {
			for cur := 0; cur <= len(stmt); cur++ {
				for _, in := range SQLIntentions(stmt, driver, cur, cols) {
					got, err := ApplySQLIntention(stmt, driver, in.ID, cur, cols)
					if err != nil {
						t.Fatalf("%s %q @%d: %s offered but not applicable: %v", driver, stmt, cur, in.ID, err)
					}
					if got == stmt || strings.TrimSpace(got) == "" {
						t.Fatalf("%s %q @%d: %s changed nothing: %q", driver, stmt, cur, in.ID, got)
					}
					applied++
				}
			}
		}
	}
	if applied < 200 {
		t.Errorf("only %d intentions were applied: the statements no longer exercise them", applied)
	}
	if _, err := ApplySQLIntention("SELECT 1", "", "no-such-id", 0, cols); err == nil {
		t.Error("an unknown intention: want error")
	}
	if got := SQLIntentions("SELECT 'unterminated", "", 3, cols); len(got) != 0 {
		t.Errorf("a statement that does not lex offers nothing: %v", got)
	}
}
