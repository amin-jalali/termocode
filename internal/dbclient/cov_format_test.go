package dbclient

import (
	"strings"
	"testing"
)

// Formatting never changes what the statement says: formatting the
// result again gives the same text, and what comes out has the same
// words as what went in.
func TestFormatQueryOnManyForms(t *testing.T) {
	stmts := []string{
		"select 1e-5, 1E+7, 2.5, .5, 'it''s', E'a\\'b', $$dollar$$, $t$x$t$ from t",
		"select a -- tail\r\nfrom t /* block */ where x between 1 and 2 and y = 3",
		"select * from a natural left join b natural join c cross join d full outer join e on 1=1",
		"select distinct a from t union all select b from u union select c from v except select d from w",
		"insert into t (a, b) values (1, 2), (3, 4) on conflict do nothing returning id",
		"delete from t where id in (select id from u)",
		"update t set a = 1, b = 2 where c = 3",
		"with c as (select 1) select * from c",
		"select a[1], b[2:3], (c).d, e::int, f->'g', h->>'i' from t",
		"select count(*) over (partition by a order by b rows between unbounded preceding and current row) from t",
		"select case when a then 1 else 2 end from t group by a having count(*) > 1 order by 1 desc limit 5 offset 2",
		"select a from t where b is distinct from c",
		"select a from t where x = @p and y = :q and z = $1 and w = ?",
		"select @@version, @var, #temp from t",
		"select `a`, \"b\", [c] from `t`",
		"select a\n\n\nfrom t\n\n-- trailing comment",
		"select 1;\nselect 2;",
		"select * from t where a = - 1 and b = + 2 and c = 3 - -4",
		"select a.* , b.*, * from a, b",
		"select ( select 1 ) , ( 2 ) from t",
	}
	for _, dialect := range []string{"", "postgres", "mysql", "sqlite", "sqlserver"} {
		for _, sql := range stmts {
			out, err := FormatQuery(sql, dialect)
			if err != nil {
				t.Errorf("%s %q: %v", dialect, sql, err)
				continue
			}
			again, err := FormatQuery(out, dialect)
			if err != nil || again != out {
				t.Errorf("%s %q: formatting is not stable\nfirst:\n%s\nsecond:\n%s", dialect, sql, out, again)
			}
			if words(out) != words(sql) {
				t.Errorf("%s %q changed its words:\n%s", dialect, sql, out)
			}
		}
	}
	for _, bad := range []string{"select 'open", "select /* open", "select \"open", "select $$open"} {
		if out, err := FormatQuery(bad, ""); err == nil && !strings.Contains(out, "open") {
			t.Errorf("%q = %q, %v", bad, out, err)
		}
	}
	if out, err := FormatQuery("", ""); err != nil || strings.TrimSpace(out) != "" {
		t.Errorf("empty = %q, %v", out, err)
	}
}

// words is the SQL with all spacing and case removed: formatting may
// move spaces and change the case of keywords, never anything else.
func words(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.NewReplacer(" ", "", "\n", "", "\t", "", "\r", "").Replace(s)), ""))
}

func TestInspectOnManyForms(t *testing.T) {
	sc := InspectSchema{}
	stmts := []string{
		"select a from t", "select * from t, u where t.id = u.id", "select $1, :a, @b, ?, @, : from t",
		"select a.b.c from lateral (select 1) x", "select 1 from only t", "select x.y from s.t x",
		"update t set a = b", "insert into t (a) values (1)", "delete from t using u where t.a = u.a",
		"with c as (select 1) select * from c, d", "select f(1), g.h(2) from t", "select 'a' || \"b\" from t",
		"", ";", "select", "select 'open", "select /* c */ 1 -- d", "select 1; select 2 from nope",
	}
	for _, sql := range stmts {
		_ = Inspect(sql, sc)
	}
	known := InspectSchema{Complete: true, Tables: map[string]map[string]bool{"": {"users": true, "orders": true}}, Columns: map[string][]string{"users": {"id", "name"}}}
	issues := Inspect("select name, nope from users u join ordrs o on 1 = 1", known)
	var kinds []IssueKind
	for _, is := range issues {
		kinds = append(kinds, is.Kind)
		if is.Start < 0 || is.End < is.Start || is.Msg == "" {
			t.Errorf("an issue needs a range and a message: %+v", is)
		}
	}
	if len(issues) == 0 {
		t.Error("a table that does not exist must be reported")
	}
	if got := Inspect("select name from users", known); len(got) != 0 {
		t.Errorf("a correct query has no issues: %+v", got)
	}
	_ = kinds
}
