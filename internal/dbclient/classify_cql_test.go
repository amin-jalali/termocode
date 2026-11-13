package dbclient

import "testing"

// DBX-67: a CQL statement is judged by CQL's own rules: only one SELECT
// or DESCRIBE is a read; DDL and everything else is not.
func TestClassifyCQL(t *testing.T) {
	cases := []struct {
		stmt string
		want Kind
	}{
		{"SELECT * FROM shop.orders", KindRead},
		{"select id from shop.orders where id = 1 allow filtering;", KindRead},
		{"DESCRIBE KEYSPACES", KindRead},
		{"DESC TABLE shop.orders", KindRead},
		{"INSERT INTO shop.orders (id) VALUES (1)", KindWrite},
		{"UPDATE shop.orders SET n = 1 WHERE id = 1", KindWrite},
		{"DELETE FROM shop.orders WHERE id = 1", KindWrite},
		{"TRUNCATE shop.orders", KindDDL},
		{"BEGIN BATCH INSERT INTO t (a) VALUES (1); APPLY BATCH", KindWrite},
		{"CREATE TABLE shop.t (id int PRIMARY KEY)", KindDDL},
		{"ALTER TABLE shop.t ADD x int", KindDDL},
		{"DROP KEYSPACE shop", KindDDL},
		{"SELECT 1; DROP TABLE shop.t", KindDDL},
		{"", KindUnknown},
	}
	for _, c := range cases {
		if got, _ := Classify(c.stmt, "cassandra"); got != c.want {
			t.Errorf("%q: %s, want %s", c.stmt, got, c.want)
		}
	}
}
