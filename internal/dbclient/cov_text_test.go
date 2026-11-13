package dbclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnOptionsFieldRoundTrip(t *testing.T) {
	var o ConnOptions
	for id, v := range map[string]string{
		"initSQL": "SET x", "schema": "app", "timeZone": "UTC", "isolation": "serializable", "txMode": "READ ONLY",
		"idleMinutes": "5", "socket": "/tmp/s", "databases": "a, b", "schemas": "x,y", "objects": "t1", "readOnlyTables": "audit",
	} {
		if err := o.SetField(id, v); err != nil {
			t.Errorf("SetField(%s): %v", id, err)
		}
	}
	for id, want := range map[string]string{
		"initSQL": "SET x", "schema": "app", "timeZone": "UTC", "isolation": "serializable", "txMode": "read only",
		"idleMinutes": "5", "socket": "/tmp/s", "databases": "a, b", "schemas": "x, y", "objects": "t1", "readOnlyTables": "audit", "nope": "",
	} {
		if got := o.Field(id); got != want {
			t.Errorf("Field(%s) = %q, want %q", id, got, want)
		}
	}
	if err := o.SetField("idleMinutes", ""); err != nil || o.Field("idleMinutes") != "" {
		t.Errorf("clearing idle minutes = %v", err)
	}
	for _, bad := range []string{"soon", "-3"} {
		if err := o.SetField("idleMinutes", bad); err == nil {
			t.Errorf("idle %q: want error", bad)
		}
	}
	if err := o.SetField("nope", "x"); err == nil {
		t.Error("an unknown option is refused")
	}
}

func TestSQLIdentsSkipLiteralsAndComments(t *testing.T) {
	got := sqlIdents("SELECT \"Q1\", [b2], `c3` FROM t$x WHERE a = 'skip ''me''' /* skip2 */ -- skip3\n AND d")
	for _, want := range []string{"select", "q1", "b2", "c3", "t$x", "where", "a", "and", "d"} {
		if !got[want] {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	for _, bad := range []string{"skip", "me", "skip2", "skip3"} {
		if got[bad] {
			t.Errorf("%q is inside a literal or comment", bad)
		}
	}
	for _, open := range []string{"a /* open", "a \"open", "a [open", "a `open"} {
		if !sqlIdents(open)["a"] {
			t.Errorf("%q: what came before the open part stays", open)
		}
	}
}

func TestJSONImportRowsAndFiles(t *testing.T) {
	rows, err := readJSONRows([]byte(`[{"b":1,"a":"x","b":2},{"a":null}]`))
	if err != nil || len(rows) != 2 || strings.Join(rows[0].keys, ",") != "b,a" || rows[0].vals["b"] != "2" {
		t.Errorf("array = %+v, %v", rows, err)
	}
	rows, err = readJSONRows([]byte("{\"a\":1}\n\n{\"a\":2}\n"))
	if err != nil || len(rows) != 2 {
		t.Errorf("ndjson = %+v, %v", rows, err)
	}
	for name, in := range map[string]string{
		"empty": "  ", "row not object": `[1]`, "cut row": `[{"a":`, "bad line": "{\"a\":1}\n{nope", "cut object": "{\"a\":1",
		"array of junk": `[{"a":1},`,
	} {
		if _, err := readJSONRows([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := readJSONRows([]byte(`[{"a":1},2]`)); err == nil || !strings.Contains(err.Error(), "row 2") {
		t.Errorf("the failing row is named: %v", err)
	}
	if _, err := readJSONRows([]byte("{\"a\":1}\n[1]")); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("the failing line is named: %v", err)
	}
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "in.json"), filepath.Join(dir, "out.csv")
	if err := os.WriteFile(src, []byte(`[{"a":1,"b":"x,y"},{"a":2,"c":true}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := JSONToCSV(src, dst)
	if err != nil || n != 2 {
		t.Fatalf("JSONToCSV = %d, %v", n, err)
	}
	out, _ := os.ReadFile(dst)
	if !strings.HasPrefix(string(out), "a,b,c\n") || !strings.Contains(string(out), `"x,y"`) {
		t.Errorf("csv = %q", out)
	}
	if st, _ := os.Stat(dst); st.Mode().Perm() != 0o600 {
		t.Errorf("csv mode = %v", st.Mode().Perm())
	}
	if _, err := JSONToCSV(filepath.Join(dir, "none.json"), dst); err == nil {
		t.Error("a missing file: want error")
	}
	if err := os.WriteFile(src, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := JSONToCSV(src, dst); err == nil || !strings.Contains(err.Error(), "no rows") {
		t.Errorf("no rows = %v", err)
	}
	if err := os.WriteFile(src, []byte(`[{"a":1}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := JSONToCSV(src, filepath.Join(dir, "no", "dir", "out.csv")); err == nil {
		t.Error("an unwritable destination: want error")
	}
}

func TestJoinSelectRefusals(t *testing.T) {
	for sql, why := range map[string]string{
		"":                                             "not a plain SELECT",
		"UPDATE t SET a = 1":                           "not a plain SELECT",
		"SELECT DISTINCT a FROM t JOIN u ON 1=1":       "DISTINCT",
		"SELECT a + 1 FROM t JOIN u ON 1=1":            "renamed or computed",
		"SELECT a AS b FROM t JOIN u ON 1=1":           "renamed or computed",
		"SELECT t. FROM t JOIN u ON 1=1":               "not a plain SELECT",
		"SELECT a FROM":                                "not a plain SELECT",
		"SELECT a FROM (SELECT 1) x JOIN u ON 1=1":     "not a plain SELECT",
		"SELECT a FROM t JOIN":                         "not a plain SELECT",
		"SELECT a FROM t NATURAL JOIN u":               "one table",
		"SELECT a FROM t JOIN u ON 1=1 GROUP BY a":     "not a plain SELECT",
		"SELECT a FROM t JOIN u ON (1=1;":              "not a plain SELECT",
		"SELECT a FROM t JOIN u ON 1=1 UNION SELECT 1": "not a plain SELECT",
	} {
		_, got := JoinSelect(sql, "")
		if got == "" || !strings.Contains(got, why) {
			t.Errorf("JoinSelect(%q) = %q, want %q", sql, got, why)
		}
	}
	jt, why := JoinSelect("SELECT a.x, b.* , c FROM t1 a JOIN t2 AS b ON a.id = b.id, t3 c LIMIT 5;", "postgres")
	if why != "" || len(jt.Tables) != 3 || len(jt.Quals) != 1 || len(jt.Bare) != 1 {
		t.Errorf("a plain join = %+v, %q", jt, why)
	}
	if got := jt.TableOf("a"); got != 0 {
		t.Errorf("TableOf(a) = %d", got)
	}
	if got := jt.TableOf("zzz"); got != -1 {
		t.Errorf("TableOf(zzz) = %d", got)
	}
}
