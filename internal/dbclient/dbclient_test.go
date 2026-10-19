package dbclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestParseURL(t *testing.T) {
	cases := []struct {
		url, driver, host, db string
		port                  int
	}{{"postgres://u:p@[::1]:5433/app?sslmode=disable", "postgres", "::1", "app", 5433}, {"mysql://u:p@tcp(localhost:3307)/app?parseTime=true", "mysql", "localhost", "app", 3307}, {"mariadb://u@localhost/app", "mysql", "localhost", "app", 3306}, {"redis://:pw@localhost:6380/2", "redis", "localhost", "2", 6380}, {"rediss://localhost/0", "redis", "localhost", "0", 6379}}
	for _, c := range cases {
		t.Run(c.url, func(t *testing.T) {
			got, e := ParseURL(c.url)
			if e != nil {
				t.Fatal(e)
			}
			if got.Driver != c.driver || got.Host != c.host || got.Database != c.db || got.Port != c.port {
				t.Fatalf("%+v", got)
			}
		})
	}
}
func TestSplitAndClassify(t *testing.T) {
	q := "-- hi\nWITH x AS (SELECT ';' a) SELECT * FROM users; INSERT INTO users VALUES ('a;b');"
	ss := Split(q)
	if len(ss) != 2 {
		t.Fatalf("%#v", ss)
	}
	if k, _ := Classify(ss[0]); k != KindRead {
		t.Fatalf("%+v", k)
	}
	if k, _ := Classify(ss[1]); k != KindWrite {
		t.Fatalf("%+v", k)
	}
	ss = Split("DELIMITER $$\nCREATE PROCEDURE p() BEGIN SELECT 1; SELECT 2; END$$\nDELIMITER ;\nSELECT 3;")
	if len(ss) != 2 {
		t.Fatalf("delimiter: %#v", ss)
	}
	if k, _ := Classify("EXPLAIN ANALYZE SELECT 1"); k != KindWrite {
		t.Fatal("analyze must write")
	}
}

func TestClassifyDialectBypassProbes(t *testing.T) {
	cases := []struct {
		name, dialect, sql string
	}{
		{"postgres backslash is literal", "postgres", `SELECT 'a\'; DELETE FROM t; --'`},
		{"postgres hash is operator", "postgres", `SELECT 1 # 2; DELETE FROM t`},
		{"mysql dash comment requires whitespace", "mysql", `SELECT 1--1; DELETE FROM t`},
		{"mysql executable comment", "mysql", `SELECT 1; /*! DROP TABLE t */`},
		{"mysql outfile comment", "mysql", `SELECT 1 /*! INTO OUTFILE '/tmp/x' */`},
		{"mysql executable multi statement", "mysql", `SELECT /*!50000 1; DELETE FROM t */`},
		{"mysql outfile", "mysql", `SELECT ... INTO OUTFILE '/tmp/x'`},
		{"mysql dumpfile", "mysql", `SELECT ... INTO DUMPFILE '/tmp/x'`},
		{"default is conservative", "", `SELECT 1 # 2; DELETE FROM t`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := Classify(tc.sql, tc.dialect); got != KindWrite {
				t.Fatalf("Classify(%q, %q) = %s, want write", tc.sql, tc.dialect, got)
			}
		})
	}
}

func TestClassifySideEffectDenyList(t *testing.T) {
	cases := []string{
		"SELECT pg_reload_conf()", "SELECT pg_rotate_logfile()", "SELECT pg_switch_wal()",
		"SELECT pg_create_restore_point('x')", "SELECT dblink_connect('x')",
		"SELECT pg_try_advisory_lock(1)", "SELECT pg_advisory_lock_shared(1)",
		"SELECT nextval('s')", "SELECT set_config('x','y',false)", "SELECT pg_sleep(1)",
		"SELECT lo_create(1)", "SELECT pg_read_file('/x')", "SELECT pg_read_binary_file('/x')",
		"SELECT pg_ls_dir('/')", "COPY t TO PROGRAM 'x'", "COPY t FROM PROGRAM 'x'",
	}
	for _, sql := range cases {
		t.Run(sql, func(t *testing.T) {
			if got, _ := Classify(sql, "postgres"); got != KindWrite {
				t.Fatalf("Classify(%q) = %s, want write", sql, got)
			}
		})
	}
}

func TestRedactDialectBypassProbes(t *testing.T) {
	cases := []struct{ input, secret string }{
		{"PGPASSWORD=s3cret", "s3cret"}, {"DB_PASSWORD=s3cret", "s3cret"},
		{"MYSQL_PWD=s3cret", "s3cret"}, {"xpassword=s3cret", "s3cret"},
		{`{"password":"s3cret"}`, "s3cret"}, {`{"password": "s3cret"}`, "s3cret"},
		{"password = s3cret", "s3cret"}, {"redis://:s3cret@h:6379/0", "s3cret"},
		{"postgres://u:s3c/ret@h/db", "s3c/ret"},
		{"postgresql+psycopg://u:s3cret@h/db", "s3cret"},
		{"u:p@ss@tcp(h:3306)/db", "p@ss"}, {"u:p ss@tcp(h:3306)/db", "p ss"},
		{"CONFIG SET requirepass x", "x"}, {"AUTH2 u x", "x"}, {"password=''x''", "x"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := Redact(tc.input); strings.Contains(got, tc.secret) {
				t.Fatalf("Redact(%q) leaked %q: %q", tc.input, tc.secret, got)
			}
		})
	}
}

func TestClassifySecurityProbes(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want Kind
	}{
		{"multi statement drop", "SELECT 1; DROP TABLE t", KindDDL},
		{"multi statement delete", "SHOW TABLES; DELETE FROM t", KindWrite},
		{"hash in quote", "SELECT '#' ; DELETE FROM t", KindWrite},
		{"block markers in quotes", "SELECT '/*'; DELETE FROM t; SELECT '*/'", KindWrite},
		{"explain analyse", "EXPLAIN ANALYSE DELETE FROM t", KindWrite},
		{"explain analyze write", "EXPLAIN ANALYZE UPDATE t SET a=1", KindWrite},
		{"select into", "WITH x AS (SELECT 1) SELECT * INTO newt FROM x", KindWrite},
		{"write cte", "WITH x AS (DELETE FROM t RETURNING *) SELECT * FROM x", KindWrite},
		{"for update", "SELECT * FROM t FOR UPDATE", KindWrite},
		{"for share", "SELECT * FROM t FOR SHARE", KindWrite},
		{"mysql lock", "SELECT * FROM t LOCK IN SHARE MODE", KindWrite},
		{"terminate backend", "SELECT pg_terminate_backend(1)", KindWrite},
		{"setval", "SELECT setval('s', 1)", KindWrite},
		{"dblink exec", "SELECT dblink_exec('c', 'DELETE FROM t')", KindWrite},
		{"lo import", "SELECT lo_import('/tmp/x')", KindWrite},
		{"pragma write", "PRAGMA journal_mode=DELETE", KindWrite},
		{"unknown", "BOGUS THING", KindWrite},
		{"read-only pragma", "PRAGMA table_info(t)", KindRead},
		{"pragma write function form", "PRAGMA user_version(1)", KindWrite},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, statements := Classify(tc.sql)
			if got != tc.want {
				t.Fatalf("Classify(%q) = %s, want %s; statements=%+v", tc.sql, got, tc.want, statements)
			}
		})
	}
}

func TestSplitSecurityProbes(t *testing.T) {
	cases := []struct {
		sql  string
		want int
	}{
		{"SELECT 1 -- x; y\n; SELECT 2", 2},
		{"SELECT 1 /* a; b */; SELECT 2", 2},
		{"SELECT a$b$c FROM t; SELECT 2", 2},
		{"SELECT ';'; SELECT '' ; SELECT 'a\\';b'", 3},
		{"SELECT $$a;b$$; SELECT $tag$x;y$tag$;", 2},
	}
	for _, tc := range cases {
		t.Run(tc.sql, func(t *testing.T) {
			got := Split(tc.sql)
			if len(got) != tc.want {
				t.Fatalf("Split(%q) = %#v, want %d statements", tc.sql, got, tc.want)
			}
		})
	}
}

func TestRedactProbeInputs(t *testing.T) {
	cases := []struct{ input, secret string }{
		{"failed to connect to postgres://u:s3cret@h/db: refused", "s3cret"},
		{"driver: u:s3cret@tcp(h:3306)/db failed", "s3cret"},
		{"mysql://u:s3cret@tcp(h:3306)/db", "s3cret"},
		{"x password='s3 cret' y", "s3 cret"},
		{"x pass=s3cret y", "s3cret"},
		{"x pwd= y", ""},
		{"AUTH s3cret\nPING", "s3cret"},
		{"AUTH user s3cret\nPING", "s3cret"},
		{"connect failed after AUTH s3cret: refused", "s3cret"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := Redact(tc.input)
			if tc.secret != "" && strings.Contains(got, tc.secret) {
				t.Fatalf("Redact(%q) leaked secret: %q", tc.input, got)
			}
			if strings.Count(got, "\n") != strings.Count(tc.input, "\n") {
				t.Fatalf("Redact changed newlines: %q", got)
			}
		})
	}
}

func TestHistoryRedactsBeforeAppend(t *testing.T) {
	h := History{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	for _, sql := range []string{"AUTH user s3cret", "SELECT 1 /* password=s3cret */", "ALTER USER bob PASSWORD 's3 cret'", "CREATE USER bob IDENTIFIED BY 's3 cret'"} {
		if err := h.Append(HistoryEntry{SQL: sql}); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(h.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"s3cret", "s3 cret"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("history leaked %q: %s", secret, b)
		}
	}
}

func TestGridPastEndAndJSONInvalidNumber(t *testing.T) {
	r := Result{Columns: []Column{{Name: "n"}}, Rows: [][]Cell{{{Text: "not-a-number", Kind: "number"}}}}
	if got := Grid(r, 2, nil); len(got) != 0 {
		t.Fatalf("past-end page = %#v", got)
	}
	if got := ToJSON(r); got != `[{"n":"not-a-number"}]` {
		t.Fatalf("JSON = %s", got)
	}
}

func TestSortNumericAndNullPlacement(t *testing.T) {
	r := Result{Columns: []Column{{Name: "n"}}, Rows: [][]Cell{{{Text: "10", Kind: "number"}}, {{Text: "2", Kind: "number"}}, {{Null: true}}}}
	Sort(&r, 0, false)
	if !r.Rows[0][0].Null || r.Rows[1][0].Text != "2" || r.Rows[2][0].Text != "10" {
		t.Fatalf("ascending rows: %+v", r.Rows)
	}
	Sort(&r, 0, true)
	if !r.Rows[2][0].Null || r.Rows[0][0].Text != "10" || r.Rows[1][0].Text != "2" {
		t.Fatalf("descending rows: %+v", r.Rows)
	}
}

func TestValidateDriverAndUnterminatedReference(t *testing.T) {
	if err := Validate(Conn{ID: "x", Driver: "other", Host: "h", Port: 1}); err == nil {
		t.Fatal("accepted unsupported driver")
	}
	if !literalPassword("${oops") {
		t.Fatal("unterminated reference accepted")
	}
}

func TestMySQLDSN(t *testing.T) {
	c := Conn{Driver: "mysql", Host: "localhost", Port: 3306, User: "u", Database: "app", Params: map[string]string{"parseTime": "true"}}
	got := c.DSN("p")
	if !strings.HasPrefix(got, "u:p@tcp(localhost:3306)/app?") {
		t.Fatalf("DSN = %q, want go-sql-driver DSN form", got)
	}
	if _, err := mysql.ParseDSN(got); err != nil {
		t.Fatalf("go-sql-driver rejected DSN %q: %v", got, err)
	}
}

func TestMySQLDSNPasswordRoundTrip(t *testing.T) {
	password := "p@ss word"
	c := Conn{Driver: "mysql", Host: "localhost", Port: 3306, User: "u", Database: "app"}
	got := c.DSN(password)
	parsed, err := mysql.ParseDSN(got)
	if err != nil {
		t.Fatalf("ParseDSN(%q): %v", got, err)
	}
	if parsed.Passwd != password {
		t.Fatalf("password = %q, want round-trip %q", parsed.Passwd, password)
	}
}

func TestClassifyStatementMetadataAndDSNRedaction(t *testing.T) {
	kind, statements := Classify(`SELECT * FROM "users" WHERE id = 1`)
	if kind != KindRead || len(statements) != 1 || !statements[0].HasWhere || statements[0].Table != "users" {
		t.Fatalf("classification = %s %+v", kind, statements)
	}
	for _, c := range []Conn{{Driver: "postgres", URL: "postgres://u:embedded@host/db"}, {Driver: "redis", URL: "redis://:embedded@host/0"}} {
		if got := c.DSN(""); strings.Contains(got, "embedded") {
			t.Fatalf("DSN leaked embedded password: %q", got)
		}
	}
}
func TestFormats(t *testing.T) {
	r := Result{Columns: []Column{{Name: "id"}, {Name: "name"}}, Rows: [][]Cell{{{Text: "1"}, {Null: true}}, {{Text: "2"}, {Text: "a,b"}}}}
	if got := ToCSV(r); got != "id,name\r\n1,\r\n2,\"a,b\"\r\n" {
		t.Fatalf("csv %q", got)
	}
	if got := ToJSON(r); got != "[{\"id\":\"1\",\"name\":null},{\"id\":\"2\",\"name\":\"a,b\"}]" {
		t.Fatalf("json %s", got)
	}
	if len(Grid(r, 0, []int{4, 4})) == 0 || len(Transposed(r, 0)) == 0 {
		t.Fatal("empty rendering")
	}
}
func TestConfigRejectsLiteralAndPathEscape(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, ".termocode", "databases.json")
	if err := os.MkdirAll(filepath.Dir(project), 0700); err != nil {
		t.Fatal(err)
	}
	b := []byte(`{"connections":[{"id":"bad","driver":"sqlite","path":"../secret.db"},{"id":"pw","driver":"postgres","host":"localhost","password":"plain"}]}`)
	cs, err := ParseConfig(b, SourceProject, project, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Problem == "" || cs[1].Problem == "" {
		t.Fatalf("%+v", cs)
	}
}
func TestHistoryCapPermissionsAndClear(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.jsonl")
	s := History{Path: p, Max: 1}
	if err := s.Append(HistoryEntry{SQL: "SELECT 1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(HistoryEntry{SQL: string(make([]byte, 5000))}); err != nil {
		t.Fatal(err)
	}
	es, e := s.List()
	if e != nil || len(es) != 1 || len(es[0].SQL) > 4096 {
		t.Fatalf("%d %v", len(es), e)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	if e = s.Clear(); e != nil {
		t.Fatal(e)
	}
}
func TestRedactDSNAndError(t *testing.T) {
	for _, s := range []string{"postgres://u:secret@host/db", "password=secret", "AUTH secret"} {
		if got := Redact(s); got == s || contains(got, "secret") {
			t.Fatalf("%q => %q", s, got)
		}
	}
}
func contains(a, b string) bool { return len(a) >= len(b) && index(a, b) >= 0 }
func index(a, b string) int {
	for i := 0; i+len(b) <= len(a); i++ {
		if a[i:i+len(b)] == b {
			return i
		}
	}
	return -1
}

func TestConfigJSONCMergeAndValidate(t *testing.T) {
	const body = `{
 // project config
 "connections": [{"id":"local","driver":"sqlite","path":"app.db",},],
 }`
	project, err := ParseConfig([]byte(body), SourceProject, "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	user, err := ParseConfig([]byte(`{"connections":[{"id":"personal","driver":"redis","host":"localhost","port":6379}]}`), SourceUser, "user", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	merged := MergeConfig(project, user)
	if len(merged) != 2 || merged[0].ID != "local" || merged[1].ID != "personal" {
		t.Fatalf("%+v", merged)
	}
	if err := Validate(Conn{ID: "bad id", Driver: "redis", Host: "localhost", Port: 6379}); err == nil {
		t.Fatal("accepted invalid id")
	}
	if got := ConfigTemplate(); got == "" {
		t.Fatal("missing template")
	}
}

func TestAdditionalFormatsAndSortFilter(t *testing.T) {
	r := Result{Columns: []Column{{Name: "name"}, {Name: "n"}}, Rows: [][]Cell{{{Text: "beta"}, {Text: "2", Kind: "number"}}, {{Text: "alpha"}, {Text: "1", Kind: "number"}}}}
	if got := ToTSV(r); got != "name\tn\r\nbeta\t2\r\nalpha\t1\r\n" {
		t.Fatalf("tsv %q", got)
	}
	if got := ToSQLInserts(r, "users", "postgres"); !strings.Contains(got, `INSERT INTO "users"`) || !strings.Contains(got, "'beta'") {
		t.Fatalf("inserts %s", got)
	}
	if got := ToMarkdown(r); !strings.Contains(got, "|") || !strings.Contains(got, "alpha") {
		t.Fatalf("markdown %s", got)
	}
	Sort(&r, 0, false)
	if r.Rows[0][0].Text != "alpha" {
		t.Fatalf("sort: %+v", r.Rows)
	}
	filtered := Filter(r, "BETA")
	if len(filtered.Rows) != 1 || filtered.Rows[0][0].Text != "beta" {
		t.Fatalf("filter: %+v", filtered.Rows)
	}
}

func TestHistoryDeleteAndRedaction(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	h := History{Path: p}
	if e := h.Append(HistoryEntry{ID: "one", SQL: "SELECT 1"}); e != nil {
		t.Fatal(e)
	}
	if e := h.Delete("one"); e != nil {
		t.Fatal(e)
	}
	items, e := h.List()
	if e != nil || len(items) != 0 {
		t.Fatalf("%+v %v", items, e)
	}
	if got := Redact("mysql://u:secret@host/db?password=secret"); strings.Contains(got, "secret") {
		t.Fatalf("not redacted: %q", got)
	}
}

func TestSecurityGapsRound2(t *testing.T) {
	cases := []struct {
		name, sql, dialect string
		want               Kind
		stmts              int
	}{
		{"mariadb exec comment mysql", "SELECT 1; /*M! DROP TABLE t */", "mysql", KindWrite, 0},
		{"mariadb exec comment empty", "SELECT 1; /*M! DROP TABLE t */", "", KindWrite, 0},
		{"mysql no dollar quote", "SELECT 1 AS $a$; DELETE FROM t; SELECT $a$", "mysql", KindWrite, 3},
		{"postgres E after word char", "SELECT name'a\\'; DELETE FROM t; --'", "postgres", KindWrite, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, st := Classify(tc.sql, tc.dialect)
			if got != tc.want || severity(got) < severity(KindWrite) {
				t.Fatalf("Classify = %s, want %s; %+v", got, tc.want, st)
			}
			if tc.stmts > 0 && len(st) != tc.stmts {
				t.Fatalf("got %d statements, want %d: %+v", len(st), tc.stmts, st)
			}
		})
	}
}

func TestRedactColonForms(t *testing.T) {
	for _, in := range []string{"Password: s3cret", "password:s3cret", "PASSWORD : s3cret", "passphrase=s3cret", "Passphrase: s3cret", "db_token : s3cret"} {
		if got := Redact(in); strings.Contains(got, "s3cret") {
			t.Errorf("Redact(%q) leaked: %q", in, got)
		}
	}
}
