package dbclient

import (
	"strings"
	"testing"
)

func inspectSchema() InspectSchema {
	return InspectSchema{
		Tables:   map[string]map[string]bool{"public": {"users": true, "orders": true, "order_items": true}},
		Complete: true,
		Columns:  map[string][]string{"users": {"id", "email", "plan"}, "orders": {"id", "user_id", "total"}},
	}
}

// issueAt returns the issue whose range covers the text want in sql.
func issueAt(t *testing.T, sql string, issues []Issue, want string) Issue {
	t.Helper()
	at := strings.Index(sql, want)
	for _, is := range issues {
		if is.Start == at && is.End == at+len(want) {
			return is
		}
	}
	t.Fatalf("no issue on %q in %q: %+v", want, sql, issues)
	return Issue{}
}

func TestInspectClean(t *testing.T) {
	for _, sql := range []string{
		"select u.id, u.email from users u join orders o on o.user_id = u.id where u.plan = 'pro'",
		"SELECT * FROM public.users AS u WHERE u.id = $1",
		"with recent as (select * from orders) select r.total from recent r",
		"select extract(year from now()), substring('abc' from 2)",
		"insert into users (id, email) values (1, 'a;b''c')",
		"update users set plan = 'x' where id in (select user_id from orders)",
		"select * from generate_series(1, 3) g",
		"select 'it''s' -- from nowhere\n/* from ghost */ from users",
		"select $$ from ghost $$ from users",
		"select u.* from users u",
		"select count(*) from orders, users",
		"delete from order_items where id = 1",
	} {
		if got := Inspect(sql, inspectSchema()); len(got) != 0 {
			t.Errorf("Inspect(%q) = %+v, want none", sql, got)
		}
	}
}

func TestInspectUnknownTable(t *testing.T) {
	sql := "select * from usrs u join ordres o on o.user_id = u.id"
	got := Inspect(sql, inspectSchema())
	if len(got) != 2 {
		t.Fatalf("issues = %+v", got)
	}
	is := issueAt(t, sql, got, "usrs")
	if is.Kind != IssueTable || is.Fix != "users" || !strings.Contains(is.Msg, "usrs") || !strings.Contains(is.Msg, "users") {
		t.Fatalf("usrs issue = %+v", is)
	}
	if is := issueAt(t, sql, got, "ordres"); is.Fix != "orders" {
		t.Fatalf("ordres fix = %q", is.Fix)
	}
	// Far from every name: no fix offered.
	if is := Inspect("select * from zzzzzzzz", inspectSchema()); len(is) != 1 || is[0].Fix != "" {
		t.Fatalf("far name = %+v", is)
	}
	// Qualified with a loaded schema.
	sql = "select * from public.usrs"
	issueAt(t, sql, Inspect(sql, inspectSchema()), "usrs")
}

// Tables are only flagged when the tree has loaded every table list
// (Complete) or the schema that qualifies the name.
func TestInspectIncompleteSchemaIsQuiet(t *testing.T) {
	sc := inspectSchema()
	sc.Complete = false
	if got := Inspect("select * from usrs", sc); len(got) != 0 {
		t.Fatalf("incomplete schema flagged %+v", got)
	}
	if got := Inspect("select * from other.usrs", inspectSchema()); len(got) != 0 {
		t.Fatalf("unloaded schema flagged %+v", got)
	}
	if got := Inspect("select * from usrs", InspectSchema{}); len(got) != 0 {
		t.Fatalf("empty schema flagged %+v", got)
	}
}

func TestInspectUnknownColumn(t *testing.T) {
	sql := "select u.emial, o.totl, x.anything from users u join orders o on o.user_id = u.id"
	got := Inspect(sql, inspectSchema())
	if len(got) != 2 {
		t.Fatalf("issues = %+v (x is not a known alias: not checked)", got)
	}
	if is := issueAt(t, sql, got, "emial"); is.Kind != IssueColumn || is.Fix != "email" {
		t.Fatalf("emial = %+v", is)
	}
	if is := issueAt(t, sql, got, "totl"); is.Fix != "total" {
		t.Fatalf("totl = %+v", is)
	}
	// Table name as qualifier; columns of a table nobody loaded are not checked.
	sql = "select users.nope, order_items.whatever from users, order_items"
	got = Inspect(sql, inspectSchema())
	if len(got) != 1 {
		t.Fatalf("issues = %+v", got)
	}
	issueAt(t, sql, got, "nope")
}

func TestInspectSyntax(t *testing.T) {
	cases := []struct{ sql, at string }{
		{"select 'abc from users", "'abc from users"},
		{`select "abc from users`, `"abc from users`},
		{"select 1 /* note", "/* note"},
		{"select (1 + 2 from users", "("},
		{"select 1 + 2) from users", ")"},
		{"select $$ body", "$$ body"},
	}
	for _, c := range cases {
		got := Inspect(c.sql, inspectSchema())
		if len(got) != 1 || got[0].Kind != IssueSyntax {
			t.Errorf("Inspect(%q) = %+v, want one syntax issue", c.sql, got)
			continue
		}
		issueAt(t, c.sql, got, c.at)
	}
}

// Several statements: aliases do not leak from one to the next.
func TestInspectStatementsScope(t *testing.T) {
	sql := "select u.id from users u;\nselect u.nope from orders o"
	if got := Inspect(sql, inspectSchema()); len(got) != 0 {
		t.Fatalf("alias leaked: %+v", got)
	}
}

func FuzzInspect(f *testing.F) {
	f.Add("select u.id from users u where u.x = 'a''b' and (1)")
	f.Add("select $tag$ x $tag$, \"q\"\"q\" from `t`")
	f.Add("/* x */ -- y\n)(")
	f.Fuzz(func(t *testing.T, sql string) {
		for _, is := range Inspect(sql, inspectSchema()) {
			if is.Start < 0 || is.End < is.Start || is.End > len(sql) || is.Msg == "" {
				t.Fatalf("bad issue %+v for %q", is, sql)
			}
		}
	})
}
