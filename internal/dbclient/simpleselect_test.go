package dbclient

import "testing"

func TestSimpleSelectFindsTheOneTable(t *testing.T) {
	ok := []struct {
		sql, dialect, schema, table string
	}{
		{"SELECT * FROM users", "postgres", "", "users"},
		{"select id, email from Users where plan = 'a, b' order by id limit 10;", "postgres", "", "users"},
		{`SELECT "Id", "e" FROM "Shop Data"."Order Items" WHERE x = 1`, "postgres", "Shop Data", "Order Items"},
		{"SELECT * FROM app.Users u WHERE u.id > 3", "mysql", "app", "Users"},
		{"SELECT u.* FROM `my db`.`t` AS u", "mysql", "my db", "t"},
		{"-- @conn x\nSELECT t.id FROM t /* c */ WHERE id IN (1, 2)", "sqlite", "", "t"},
		{"SELECT * FROM t FOR UPDATE", "postgres", "", "t"},
		{"SELECT * FROM t OFFSET 5", "postgres", "", "t"},
	}
	for _, c := range ok {
		got, why := SimpleSelect(c.sql, c.dialect)
		if why != "" || got.Schema != c.schema || got.Table != c.table {
			t.Errorf("%q = %+v (%s), want %s.%s", c.sql, got, why, c.schema, c.table)
		}
	}
	bad := []string{
		"SELECT id AS email FROM users",
		"SELECT id email FROM users",
		"SELECT lower(email) FROM users",
		"SELECT count(*) FROM users",
		"SELECT DISTINCT plan FROM users",
		"SELECT * FROM users JOIN orgs ON true",
		"SELECT * FROM users, orgs",
		"SELECT * FROM users u LEFT JOIN orgs o ON o.id = u.org",
		"SELECT plan FROM users GROUP BY plan",
		"SELECT * FROM a UNION SELECT * FROM b",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"SELECT * FROM (SELECT * FROM users) s",
		"SELECT 1",
		"SELECT o.id FROM users u",
		"SELECT * FROM a.b.c",
		"UPDATE users SET a = 1",
		"SELECT * FROM users; DELETE FROM users",
		"SELECT $$x$$ FROM t",
	}
	for _, sql := range bad {
		if got, why := SimpleSelect(sql, "postgres"); why == "" {
			t.Errorf("%q was taken as a plain select: %+v", sql, got)
		}
	}
}
