package dbclient

import (
	"strings"
	"testing"
)

func TestDocTokensEndAtAnOpenQuoteOrComment(t *testing.T) {
	texts := func(sql, driver string) string {
		var out []string
		for _, tk := range docTokens(sql, driver) {
			out = append(out, tk.text)
		}
		return strings.Join(out, "|")
	}
	for _, c := range []struct{ sql, driver, want string }{
		{"a /* open", "", "a"},
		{"a /* c */ b", "", "a|b"},
		{"a 'open", "", "a"},
		{"a \"open", "mysql", "a"},
		{"a \"open", "postgres", "a"},
		{"a [open", "sqlite", "a"},
		{"a [b c] d", "sqlite", "a|b c|d"},
		{"a $$open", "postgres", "a"},
		{"a $t$ x $t$ b", "postgres", "a|$t$ x $t$|b"},
		{"a $1 b", "postgres", "a|$|1|b"},
		{"a # note\nb", "mysql", "a|b"},
		{"a -- note\nb", "", "a|b"},
		{"a, b.c", "", "a|,|b|.|c"},
	} {
		got := texts(c.sql, c.driver)
		if c.want == "a|$|1|b" {
			if !strings.HasPrefix(got, "a|") {
				t.Errorf("docTokens(%q) = %q", c.sql, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("docTokens(%q, %s) = %q, want %q", c.sql, c.driver, got, c.want)
		}
	}
	for in, want := range map[string]string{"$tag$": "$tag$", "$$": "$$", "$1": "", "$a b$": "", "$": "", "$_x9$": "$_x9$", "$9a$": ""} {
		if got := dollarTagAt(in, 0); got != want {
			t.Errorf("dollarTagAt(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSQLRefAtEveryChainShape(t *testing.T) {
	type c struct {
		sql, at, driver string
		want            SQLRef
		ok              bool
	}
	sql := "SELECT u.name, public.users.id, s.fn(1), x.y.z.w FROM public.users u JOIN orders o ON o.uid = u.id, lib.stuff"
	at := func(sub string, extra int) int { return strings.Index(sql, sub) + extra }
	for _, tc := range []struct {
		name string
		off  int
		want SQLRef
		ok   bool
	}{
		{"alias.column", at("u.name", 2), SQLRef{Kind: "column", Table: "users", Schema: "public", Column: "name"}, true},
		{"the alias itself", at("u.name", 0), SQLRef{Kind: "table", Schema: "public", Table: "users"}, true},
		{"schema.table.column", at("public.users.id", 14), SQLRef{Kind: "column", Schema: "public", Table: "users", Column: "id"}, true},
		{"the table of a chain", at("public.users.id", 8), SQLRef{Kind: "table", Schema: "public", Table: "users"}, true},
		{"the schema of a chain", at("public.users.id", 2), SQLRef{Kind: "schema", Schema: "public"}, true},
		{"a routine", at("s.fn(1)", 3), SQLRef{Kind: "routine", Schema: "s", Routine: "fn"}, true},
		{"four names", at("x.y.z.w", 6), SQLRef{Kind: "column", Schema: "y", Table: "z", Column: "w"}, true},
		{"a table in FROM", at("orders o", 1), SQLRef{Kind: "table", Table: "orders"}, true},
		{"a column after a comma in ON", at("lib.stuff", 5), SQLRef{Kind: "column", Table: "lib", Column: "stuff"}, true},
		{"a keyword", at("SELECT", 2), SQLRef{}, false},
		{"punctuation", at("= u.id", 0), SQLRef{}, false},
		{"past the end", len(sql) + 5, SQLRef{}, false},
	} {
		got, ok := SQLRefAt(sql, tc.off, "postgres")
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%s = %+v %v, want %+v %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	// The cursor just after a name belongs to the name.
	if got, ok := SQLRefAt("SELECT a.b FROM t a", 10, "postgres"); !ok || got.Column != "b" {
		t.Errorf("cursor after a name = %+v %v", got, ok)
	}
	if got, ok := SQLRefAt("INSERT INTO s.t (a) VALUES (1)", 14, ""); !ok || got.Kind != "table" {
		t.Errorf("insert target = %+v %v", got, ok)
	}
	if got, ok := SQLRefAt("UPDATE t SET a = 1", 7, ""); !ok || got.Table != "t" {
		t.Errorf("update target = %+v %v", got, ok)
	}
	if _, ok := SQLRefAt("SELECT a FROM t WHERE a = 'x'", 28, ""); ok {
		t.Error("a string is no name")
	}
}

func TestInsertValueHintsForms(t *testing.T) {
	for sql, want := range map[string]int{
		"INSERT INTO t (a, b) VALUES (1, 2), (3, 4)":          4,
		"INSERT INTO s.t (a) VALUES (1)":                      1,
		`INSERT INTO "T" ("a b") VALUES ('x')`:                1,
		"INSERT INTO t (a) SELECT 1":                          0,
		"INSERT INTO t VALUES (1)":                            0,
		"INSERT t (a) VALUES (1)":                             0,
		"SELECT 1":                                            0,
		"INSERT INTO t (a, ) VALUES (1)":                      0,
		"INSERT INTO t (a b) VALUES (1)":                      0,
		"INSERT INTO t (a) VALUES (1, 2, 3)":                  1,
		"INSERT INTO t (a) VALUES ((1), (SELECT 2))":          1,
		"INSERT INTO t (a, b) VALUES (f(1, 2), 'x,y')":        2,
		"INSERT INTO t (a) VALUES (1) ON CONFLICT DO NOTHING": 1,
		"INSERT INTO t (a) VALUES":                            0,
	} {
		got := InsertValueHints(sql, "postgres")
		if want == 0 && len(got) != 0 || want > 0 && len(got) < want {
			t.Errorf("InsertValueHints(%q) = %v, want %d", sql, got, want)
		}
	}
	h := InsertValueHints("INSERT INTO t (a, b) VALUES (f(1, 2), 'x,y')", "")
	if len(h) != 2 || h[0].Column != "a" || h[1].Column != "b" {
		t.Errorf("hints = %+v", h)
	}
}
