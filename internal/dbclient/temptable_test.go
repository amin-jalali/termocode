package dbclient

import "testing"

// TempTableName finds the temp table a statement creates, per dialect.
func TestTempTableName(t *testing.T) {
	cases := []struct {
		driver, sql, want string
	}{
		{"postgres", "CREATE TEMP TABLE scratch (id int)", "scratch"},
		{"postgres", "create temporary table if not exists \"Scratch\" as select 1", "\"Scratch\""},
		{"postgres", "CREATE LOCAL TEMPORARY TABLE t1 (a int)", "t1"},
		{"postgres", "CREATE UNLOGGED TABLE t1 (a int)", ""},
		{"postgres", "-- note\nCREATE TEMP TABLE t2 AS SELECT 1", "t2"},
		{"mysql", "CREATE TEMPORARY TABLE `tmp_orders` SELECT * FROM orders", "`tmp_orders`"},
		{"mariadb", "create temporary table if not exists x (a int)", "x"},
		{"mysql", "CREATE TABLE orders (id int)", ""},
		{"sqlserver", "CREATE TABLE #work (id int)", "#work"},
		{"sqlserver", "SELECT id INTO #ids FROM users", "#ids"},
		{"sqlserver", "CREATE TABLE work (id int)", ""},
		{"oracle", "CREATE GLOBAL TEMPORARY TABLE gtt (a number)", "gtt"},
		{"oracle", "CREATE PRIVATE TEMPORARY TABLE ora$ptt_x (a number)", "ora$ptt_x"},
		{"sqlite", "CREATE TEMP TABLE s (a)", "s"},
		{"postgres", "SELECT 'CREATE TEMP TABLE x'", ""},
		{"postgres", "", ""},
	}
	for _, c := range cases {
		got, ok := TempTableName(c.sql, c.driver)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s %q = %q %v, want %q", c.driver, c.sql, got, ok, c.want)
		}
	}
}
