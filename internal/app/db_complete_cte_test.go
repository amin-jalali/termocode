package app_test

import "testing"

// DBX-3: columns of CTEs (WITH name AS (...)) and of subqueries in FROM
// (… ) alias) come from their select lists; CTE names are offered as
// objects.
func TestDBOmnifuncCTEAndSubqueryColumns(t *testing.T) {
	d, _ := dbDriver(t, "-- @conn local\n")
	if w := omniItems(t, d, completeSchema, "WITH recent AS (SELECT id, total AS amount, o.user_id FROM orders o) SELECT * FROM recent r WHERE r."); !hasAll(w, "id", "amount", "user_id") || hasAll(w, "total") {
		t.Fatalf("CTE columns via alias: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "WITH recent AS (SELECT id, total AS amount FROM orders) SELECT recent.am"); !hasAll(w, "amount") || hasAll(w, "id") {
		t.Fatalf("CTE columns via name: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "WITH c(x, y) AS (SELECT 1, 2) SELECT * FROM c WHERE c."); !hasAll(w, "x", "y") {
		t.Fatalf("CTE column list: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "WITH a AS (SELECT 1 AS one), b AS (SELECT email FROM users) SELECT * FROM a, b WHERE b."); !hasAll(w, "email") || hasAll(w, "one") {
		t.Fatalf("second CTE: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "SELECT * FROM (SELECT u.email, count(*) AS n, plan p, coalesce(a, b) FROM users u GROUP BY u.email) s WHERE s."); !hasAll(w, "email", "n", "p") || hasAll(w, "plan", "id") {
		t.Fatalf("subquery columns: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "SELECT * FROM users u JOIN (SELECT user_id, sum(total) total_sum FROM orders GROUP BY user_id) AS t ON t."); !hasAll(w, "user_id", "total_sum") {
		t.Fatalf("subquery with AS: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "WITH recent AS (SELECT 1 AS a) SELECT * FROM rec"); !hasAll(w, "recent") {
		t.Fatalf("CTE name not offered: %v", w)
	}
	// A real table's columns still win when no CTE has that name.
	if w := omniItems(t, d, completeSchema, "WITH x AS (SELECT 1 AS a) SELECT * FROM users u WHERE u."); !hasAll(w, "email") {
		t.Fatalf("table columns: %v", w)
	}
}
