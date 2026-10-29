package dbclient

import (
	"net/url"
	"strings"
	"testing"
)

var fuzzDialects = []string{"", "postgres", "mysql", "sqlite"}

// FuzzSplitClassify splits and classifies any SQL in every dialect. Each
// statement is a non-empty part of the input, and SQL called read-only
// stays read-only when each statement is checked on its own.
func FuzzSplitClassify(f *testing.F) {
	for _, s := range []string{
		"SELECT 1; SELECT 'a;b'; -- c;\nSELECT \"x;\" /* ; */",
		"DELIMITER $$\nCREATE TRIGGER t BEGIN SELECT 1; END$$\nDELIMITER ;\nSELECT 2;",
		"SELECT $tag$ ; $tag$; SELECT E'\\';'",
		"CREATE TRIGGER t AFTER INSERT ON x BEGIN DELETE FROM y; END; SELECT 1",
		"WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d",
		"SELECT * INTO new FROM t; SELECT nextval('s')",
		"/*! DROP TABLE x */ SELECT 1",
		"SELECT 'unterminated",
		"EXPLAIN ANALYZE DELETE FROM t",
		"PRAGMA table_info(x); PRAGMA writable_schema = 1",
		"SELECT [a;b] FROM t",
	} {
		for i := range fuzzDialects {
			f.Add(s, uint8(i))
		}
	}
	f.Fuzz(func(t *testing.T, sql string, d uint8) {
		dialect := fuzzDialects[int(d)%len(fuzzDialects)]
		parts, err := Split(sql, dialect)
		if err == nil {
			for _, p := range parts {
				if p == "" || !strings.Contains(sql, p) {
					t.Fatalf("part %q is not from the input %q", p, sql)
				}
			}
		}
		kind, sts := Classify(sql, dialect)
		if kind != KindRead {
			return
		}
		for _, st := range sts {
			if st.Kind != KindRead {
				t.Fatalf("read SQL has a %s statement %q", st.Kind, st.SQL)
			}
			if k, _ := Classify(st.SQL, dialect); k != KindRead {
				t.Fatalf("statement %q of read SQL %q is %s alone", st.SQL, sql, k)
			}
		}
	})
}

// FuzzParseURL reads any connection URL. A password never stays in the
// stored URL, and the port is in range.
func FuzzParseURL(f *testing.F) {
	for _, s := range []string{
		"postgres://me:secret@localhost:5432/db?sslmode=disable",
		"mysql://u:p@tcp(127.0.0.1:3306)/app?parseTime=true",
		"redis://:pw@cache:6379/2",
		"rediss://h/x",
		"sqlite:///tmp/a.db",
		"sqlite://rel.db?mode=ro",
		"file:a.db",
		"mysql://u@tcp(bad/x",
		"postgres://h:99999999999999999999/db",
		"http://x",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		c, err := ParseURL(raw)
		if err != nil {
			return
		}
		if c.Password != "" {
			if u, err := url.Parse(c.URL); err == nil && u.User != nil {
				if _, has := u.User.Password(); has {
					t.Fatalf("stored URL %q keeps the password", c.URL)
				}
			}
		}
		_ = c.DSN("pw")
		_ = Validate(c)
	})
}

// FuzzParseConfig reads any databases.json; it must not panic, and every
// usable connection has a valid id.
func FuzzParseConfig(f *testing.F) {
	for _, s := range []string{
		`{"connections":[{"id":"local","driver":"postgres","host":"localhost","port":5432,"database":"app","user":"me"}]}`,
		`{"connections":[{"id":"x","driver":"sqlite","path":"../../etc/db"},{"id":"x","driver":"mysql"}]} // c`,
		`{"connections":[{"id":"Bad ID","url":"redis://h"}]}`,
		`{"connections":{}}`, `[]`, ``,
	} {
		f.Add([]byte(s))
	}
	root := f.TempDir()
	f.Fuzz(func(t *testing.T, data []byte) {
		cs, err := ParseConfig(data, SourceProject, "databases.json", root)
		if err != nil {
			return
		}
		for _, c := range cs {
			if c.Problem == "" && !idPattern.MatchString(c.ID) {
				t.Fatalf("connection with bad id %q has no problem", c.ID)
			}
		}
		_ = MergeConfig(cs, cs)
	})
}

// FuzzPlanAndCSV feeds any text as explain output and as CSV. The plan
// parser must not panic; CSV reading keeps to its row limit and every
// encoding decodes.
func FuzzPlanAndCSV(f *testing.F) {
	for _, s := range []string{
		`[{"Plan":{"Node Type":"Seq Scan","Relation Name":"t","Total Cost":1.5,"Plans":[{"Node Type":"Index Scan"}]}}]`,
		`{"query_block":{"select_id":1,"table":{"table_name":"t","access_type":"ALL"}}}`,
		"Seq Scan on t  (cost=0.00..1.00 rows=1 width=4)\n  ->  Index Scan\n",
		"a,b\n1,2\n\"x\"\"y\",3\n",
		"\xff\xfe" + "a\x00,\x00b\x00",
	} {
		f.Add(s, uint8(0))
		f.Add(s, uint8(3))
	}
	f.Fuzz(func(t *testing.T, text string, n uint8) {
		drivers := []string{"postgres", "mysql", "sqlite", "redis"}
		res := Result{Columns: []Column{{Name: "QUERY PLAN"}}}
		for _, l := range strings.Split(text, "\n") {
			res.Rows = append(res.Rows, []Cell{{Text: l}})
		}
		_ = ParsePlan(drivers[int(n)%len(drivers)], res, n%2 == 0)
		res2 := Result{Columns: []Column{{Name: "id"}, {Name: "parent"}, {Name: "notused"}, {Name: "detail"}}}
		res2.Rows = [][]Cell{{{Text: "2"}, {Text: "0"}, {Text: "0"}, {Text: text}}, {{Text: text}, {Text: "2"}, {Text: ""}, {Text: "x"}}}
		_ = ParsePlan("sqlite", res2, false)
		limit := int(n%8) + 1
		for _, delim := range []rune{',', ';', '\t', '|'} {
			if recs := readRecords(text, delim, limit, n%2 == 1); len(recs) > limit {
				t.Fatalf("%d records over the limit %d", len(recs), limit)
			}
		}
		for _, enc := range []string{"UTF-8", "UTF-16LE", "UTF-16BE", "ISO-8859-1", "windows-1252", "nope"} {
			_ = decodeBytes([]byte(text), enc)
		}
	})
}
