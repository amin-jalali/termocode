package dbclient

import (
	"strings"
	"testing"
)

func TestFormatQuery(t *testing.T) {
	cases := []struct {
		name, dialect, in, want string
	}{
		{"select list and where", "postgres",
			"select id, name from users where a = 1 and b = 'x  from' order by id",
			"SELECT\n  id,\n  name\nFROM users\nWHERE a = 1\n  AND b = 'x  from'\nORDER BY id"},
		{"one item stays inline", "postgres",
			"select * from t",
			"SELECT *\nFROM t"},
		{"functions and casts", "postgres",
			"select count(*), a::int, t.* from t where x in (1,2) and y=-1",
			"SELECT\n  COUNT(*),\n  a::int,\n  t.*\nFROM t\nWHERE x IN (1, 2)\n  AND y = -1"},
		{"subquery", "postgres",
			"select * from (select id from t) s where id > 0",
			"SELECT *\nFROM (\n  SELECT id\n  FROM t\n) s\nWHERE id > 0"},
		{"joins", "postgres",
			"select a.id from a left join b on a.id = b.aid and b.x = 1 join c using (id)",
			"SELECT a.id\nFROM a\nLEFT JOIN b ON a.id = b.aid\n  AND b.x = 1\nJOIN c USING (id)"},
		{"insert", "postgres",
			"insert into t (a, b) values (1, 'two') returning id",
			"INSERT INTO t (a, b)\nVALUES (1, 'two')\nRETURNING id"},
		{"update", "postgres",
			"update t set a = 1, b = 2 where id = $1",
			"UPDATE t\nSET\n  a = 1,\n  b = 2\nWHERE id = $1"},
		{"ddl keeps types", "postgres",
			"create table t (id int primary key, name text not null)",
			"CREATE TABLE t (id int PRIMARY KEY, name text NOT NULL)"},
		{"comments kept", "postgres",
			"-- top note\nselect a, -- first\n b /* why */ from t",
			"-- top note\nSELECT\n  a, -- first\n  b /* why */\nFROM t"},
		{"statements", "postgres",
			"select 1; select 2;",
			"SELECT 1;\n\nSELECT 2;"},
		{"dollar quotes", "postgres",
			"select $$ select from $$ as x",
			"SELECT $$ select from $$ AS x"},
		{"window keeps one line", "postgres",
			"select rank() over (partition by a order by b) from t",
			"SELECT RANK() OVER (PARTITION BY a ORDER BY b)\nFROM t"},
		{"cte", "postgres",
			"with x as (select 1 as n) select n from x",
			"WITH x AS (\n  SELECT 1 AS n\n)\nSELECT n\nFROM x"},
		{"string prefixes", "postgres",
			"select E'a\\'b', N'x' from t",
			"SELECT\n  E'a\\'b',\n  N'x'\nFROM t"},
		{"mysql backticks and hash comment", "mysql",
			"select `from` from t # note\nwhere @v = ?",
			"SELECT `from`\nFROM t # note\nWHERE @v = ?"},
		{"sqlite brackets", "sqlite",
			"select [order] from t where :name = 1",
			"SELECT [order]\nFROM t\nWHERE :name = 1"},
		{"no comment from two minus signs", "postgres",
			"select (- -1)",
			"SELECT (- -1)"},
	}
	for _, c := range cases {
		got, err := FormatQuery(c.in, c.dialect)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s:\ngot\n%s\nwant\n%s", c.name, got, c.want)
			continue
		}
		again, err := FormatQuery(got, c.dialect)
		if err != nil || again != got {
			t.Errorf("%s: not stable:\n%s", c.name, again)
		}
	}
}

func TestFormatQueryRefusesWhatItCannotRead(t *testing.T) {
	for _, in := range []string{
		"select 'open",
		"select /* open",
		"select $$ open",
		"DELIMITER //\ncreate procedure p() begin select 1; end//",
	} {
		if _, err := FormatQuery(in, "mysql"); err == nil && !strings.HasPrefix(in, "select $$") {
			t.Errorf("%q: want an error", in)
		}
	}
	if _, err := FormatQuery("select $$ open", "postgres"); err == nil {
		t.Error("open dollar quote: want an error")
	}
}

// The formatter only moves whitespace and upper-cases keywords: the
// tokens stay the same.
func TestFormatQueryKeepsTokens(t *testing.T) {
	for _, in := range []string{
		"select a.b, c||d, e->>'k', f #> '{a}' from t where g <> 1 and h >= 2 or i != 3",
		"select x from t where y = 'it''s' -- c\n;",
		"insert into t values (1), (2) on conflict (id) do update set a = excluded.a",
	} {
		out, err := FormatQuery(in, "postgres")
		if err != nil {
			t.Fatal(err)
		}
		if a, b := sqlTokenTexts(in, "postgres"), sqlTokenTexts(out, "postgres"); strings.Join(a, "\x00") != strings.Join(b, "\x00") {
			t.Errorf("%q -> %q\n%q\n%q", in, out, a, b)
		}
	}
}

func FuzzFormatQuery(f *testing.F) {
	f.Add("select a, b from t where x = 1 and y in (select z from u)", "postgres")
	f.Add("select `a` from t # c\n", "mysql")
	f.Fuzz(func(t *testing.T, in, dialect string) {
		out, err := FormatQuery(in, dialect)
		if err != nil {
			return
		}
		if a, b := sqlTokenTexts(in, dialect), sqlTokenTexts(out, dialect); strings.Join(a, "\x00") != strings.Join(b, "\x00") {
			t.Fatalf("%q -> %q", in, out)
		}
	})
}

// sqlTokenTexts lists the non-space tokens, keywords upper-cased.
func sqlTokenTexts(sql, dialect string) []string {
	toks, err := sqlLex(sql, dialectArg([]string{dialect}))
	if err != nil {
		return nil
	}
	var out []string
	for _, tk := range toks {
		s := tk.text
		if tk.kind == tokWord {
			s = strings.ToUpper(s)
		}
		out = append(out, s)
	}
	return out
}
