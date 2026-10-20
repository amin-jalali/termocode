package dbclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSQLiteSessionUsesDialectAndKeepsJSONOrder(t *testing.T) {
	fake, err := os.ReadFile(filepath.Join("dbtest", "sqlite3.sh"))
	if err != nil {
		t.Fatal(err)
	}
	bin := writeFakeSQLite(t, string(fake))
	old := lookupSQLite
	lookupSQLite = func() (string, error) { return bin, nil }
	t.Cleanup(func() { lookupSQLite = old })

	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := s.Run(context.Background(), `SELECT 1 AS z`, RunOptions{})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(r.Columns) != 2 || r.Columns[0].Name != "z" || r.Columns[1].Name != "a" {
		t.Fatalf("JSON key order lost: %+v", r.Columns)
	}
}

func TestSQLiteFakeCancelRespawnsAndRollsBackManualTransaction(t *testing.T) {
	fake, err := os.ReadFile(filepath.Join("dbtest", "sqlite3.sh"))
	if err != nil {
		t.Fatal(err)
	}
	bin := writeFakeSQLite(t, string(fake))
	old := lookupSQLite
	lookupSQLite = func() (string, error) { return bin, nil }
	t.Cleanup(func() { lookupSQLite = old })
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r := s.Run(context.Background(), "BEGIN", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	done := make(chan Result, 1)
	go func() { done <- s.Run(context.Background(), "SELECT SLOW", RunOptions{}) }()
	time.Sleep(50 * time.Millisecond)
	s.Cancel()
	var r Result
	select {
	case r = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Cancel did not interrupt the fake query")
	}
	if r.Err == nil {
		t.Fatal("cancelled fake query returned without an error")
	}
	if len(r.Notices) != 1 || !strings.Contains(r.Notices[0], "cancelled · the session was restarted") || !strings.Contains(r.Notices[0], "transaction was rolled back") {
		t.Fatalf("restart notices = %#v", r.Notices)
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("respawned session unusable: %v", err)
	}
}

func fakeHost(t *testing.T, goos, release string) {
	t.Helper()
	oldOS, oldRel := hostOS, readOSRelease
	hostOS = func() (string, string) { return goos, "amd64" }
	readOSRelease = func() (string, error) { return release, nil }
	t.Cleanup(func() { hostOS, readOSRelease = oldOS, oldRel })
}

func TestSQLiteInstallHintIgnoresHostOSRelease(t *testing.T) {
	fakeHost(t, "linux", "ID=fedora\n")
	if got := SQLiteInstallHint("linux", "amd64"); !strings.Contains(got, "dnf install sqlite") {
		t.Fatalf("hint = %q", got)
	}
	fakeHost(t, "linux", "ID=ubuntu\n")
	if got := SQLiteInstallHint("linux", "amd64"); !strings.Contains(got, "apt install sqlite3") {
		t.Fatalf("hint = %q", got)
	}
}

func TestSQLiteJSONBlobForms(t *testing.T) {
	// Forms seen from different sqlite3 builds for X'41FF' and X'00'.
	for _, text := range []string{"Auffffffff", "u0000", "A\xff", "A\ufffd"} {
		if !sqliteJSONBlob(text) {
			t.Errorf("%q not detected as binary", text)
		}
	}
	for _, text := range []string{"hello", "caf\u00e9", "u1234"} {
		if sqliteJSONBlob(text) {
			t.Errorf("%q wrongly detected as binary", text)
		}
	}
}

func TestSQLiteDetectionAndInstallHints(t *testing.T) {
	fakeHost(t, "linux", "ID=ubuntu\nID_LIKE=debian\n")
	for _, tc := range []struct{ goos, goarch, want string }{
		{"linux", "amd64", "sudo apt install sqlite3"},
		{"darwin", "arm64", "brew install sqlite"},
		{"android", "arm64", "pkg install sqlite"},
	} {
		if got := SQLiteInstallHint(tc.goos, tc.goarch); !strings.Contains(got, tc.want) {
			t.Errorf("hint(%s) = %q, want %q", tc.goos, got, tc.want)
		}
	}
	if _, err := ParseSQLiteVersion("3.32.3 2020-06-18"); err == nil {
		t.Fatal("accepted sqlite without JSON support")
	}
	if _, err := ParseSQLiteVersion("3.33.0 2020-08-14"); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteReadOnlyRequiresSafeModeVersion(t *testing.T) {
	bin := writeFakeSQLite(t, "#!/bin/sh\nif [ \"$1\" = \"-version\" ]; then echo '3.36.0'; exit 0; fi\nexit 0\n")
	old := lookupSQLite
	lookupSQLite = func() (string, error) { return bin, nil }
	t.Cleanup(func() { lookupSQLite = old })
	if _, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:", ReadOnly: true}); err == nil || !strings.Contains(err.Error(), "Update sqlite3 to 3.37 or newer to let the AI read this database") {
		t.Fatalf("old read-only CLI error = %v", err)
	}
	if _, err := ParseSQLiteVersion("3.36.0"); err != nil {
		t.Fatalf("3.36 user session version should remain supported: %v", err)
	}
}

func TestSQLiteUnsafeStatementsClassifyAsDangerous(t *testing.T) {
	for _, sql := range []string{`SELECT edit('x','x')`, `SELECT writefile('x','x')`, `SELECT readfile('x')`, `SELECT fsdir('/etc/passwd')`, `SELECT fts3_tokenizer('simple')`, `SELECT load_extension('x')`, `SELECT zipfile('x')`, `SELECT lsmode('x')`, `SELECT sqlar_compress('x')`, `SELECT shell_escape('x')`, `VACUUM main INTO 'x'`, `ATTACH 'x' AS y`, `CREATE TEMP TABLE t(x)`, `.shell touch x`} {
		if kind, _ := Classify(sql, "sqlite"); kind == KindRead || kind == KindUnknown {
			t.Errorf("Classify(%q) = %s", sql, kind)
		}
	}
	for _, sql := range []string{`SELECT edit FROM t`, `SELECT readfile FROM t`} {
		if kind, _ := Classify(sql, "sqlite"); kind != KindRead {
			t.Errorf("bare identifier Classify(%q) = %s, want read", sql, kind)
		}
	}
}

func TestSQLiteErrorRemovesGrowingLineNumber(t *testing.T) {
	if got := cleanSQLiteError("Parse error near line 100000: near 'x': syntax error"); strings.Contains(got, "line 100000") {
		t.Fatalf("line number was not removed: %q", got)
	}
}

func TestSQLiteDialect(t *testing.T) {
	d := SQLiteDialect{}
	if got := d.QuoteIdent(`a"b`); got != `"a""b"` {
		t.Fatalf("QuoteIdent = %q", got)
	}
	if got := d.Explain("SELECT 1", false); got != "EXPLAIN QUERY PLAN SELECT 1" {
		t.Fatalf("Explain = %q", got)
	}
}

func TestSQLiteSessionNoCLIMessage(t *testing.T) {
	fakeHost(t, "linux", "ID=ubuntu\n")
	old := lookupSQLite
	lookupSQLite = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { lookupSQLite = old })
	_, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: "db.sqlite"})
	if err == nil || !strings.Contains(err.Error(), "install sqlite3") {
		t.Fatalf("missing CLI error = %v", err)
	}
}

func TestSQLiteRealCLI(t *testing.T) {
	_, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not installed")
	}
	file := filepath.Join(t.TempDir(), "fixture.db")
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: file})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, sql := range []string{
		`CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id), payload BLOB)`,
		`CREATE INDEX child_parent ON child(parent_id)`,
		`INSERT INTO parent VALUES (1, 'one')`,
		`INSERT INTO child VALUES (1, 1, X'0102')`,
	} {
		r := s.Run(context.Background(), sql, RunOptions{})
		if r.Err != nil {
			t.Fatalf("Run(%q): %v", sql, r.Err)
		}
		if strings.HasPrefix(sql, "INSERT") && r.Affected != 1 {
			t.Fatalf("affected rows for %q = %d", sql, r.Affected)
		}
		if strings.HasPrefix(sql, "CREATE") && r.Affected != 0 {
			t.Fatalf("DDL affected rows for %q = %d, want 0", sql, r.Affected)
		}
	}
	root, err := s.Introspect(context.Background(), IntrospectionRequest{})
	if err != nil || len(root.Databases) != 1 || root.Databases[0] != "main" {
		t.Fatalf("database introspection: %+v, %v", root, err)
	}
	meta, err := s.Introspect(context.Background(), IntrospectionRequest{Object: "child"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta.DDL, "CREATE TABLE child") || !strings.Contains(meta.DDL, "CREATE INDEX child_parent") || len(meta.Columns) != 3 || len(meta.Indexes) != 1 || len(meta.ForeignKeys) != 1 {
		t.Fatalf("incomplete introspection: %+v", meta)
	}
	zero := s.Run(context.Background(), "SELECT id FROM child WHERE id = -1", RunOptions{})
	if zero.Err != nil || len(zero.Rows) != 0 || len(zero.Columns) != 0 {
		t.Fatalf("zero-row result: %+v", zero)
	}
	if err := s.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := s.Run(context.Background(), `INSERT INTO parent VALUES (2, 'two')`, RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if err := s.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := s.Run(context.Background(), `INSERT INTO parent VALUES (2, 'two')`, RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if err := s.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	count := s.Run(context.Background(), `SELECT COUNT(*) AS n FROM parent`, RunOptions{})
	if count.Err != nil || count.Rows[0][0].Text != "2" {
		t.Fatalf("rollback count: %+v", count)
	}

	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	if sqlite, ok := ro.(*SQLiteSession); !ok || !sqlite.readonly {
		t.Fatal("ReadOnlySession did not start read-only")
	}
	defer ro.Close()
	write := ro.Run(context.Background(), `INSERT INTO parent VALUES (3, 'three')`, RunOptions{})
	if write.Err == nil || !strings.Contains(strings.ToLower(write.Err.Error()), "readonly") {
		t.Fatalf("read-only write error = %v", write.Err)
	}
	for _, probe := range []string{
		`SELECT edit('x','touch /tmp/termocode-sqlite-probe; true')`,
		`SELECT writefile('/tmp/termocode-sqlite-probe','x')`,
		`VACUUM INTO '/tmp/termocode-copy.db'`,
		`SELECT readfile('/etc/passwd')`,
		`CREATE TEMP TABLE probe (x)`,
		`SELECT fts3_tokenizer('simple')`,
		`SELECT load_extension('x')`,
		`ATTACH DATABASE '/tmp/termocode-attach.db' AS extra`,
		`.shell touch /tmp/termocode-shell-probe`,
		`SELECT data FROM fsdir('/etc/passwd')`,
		`SELECT * FROM fsdir('/etc/passwd')`,
		`SELECT * FROM readfile('/etc/passwd')`,
		`SELECT * FROM zipfile('/tmp/x')`,
		`SELECT shell_escape('x')`,
		`SELECT lsmode('/etc')`,
		`PRAGMA writable_schema=ON`,
		`VACUUM main INTO '/tmp/termocode-vacuum.db'`,
	} {
		if r := ro.Run(context.Background(), probe, RunOptions{}); r.Err == nil {
			t.Errorf("read-only safe session accepted %q", probe)
		} else if strings.Contains(probe, "VACUUM") && !strings.Contains(r.Err.Error(), "VACUUM INTO") {
			t.Errorf("VACUUM INTO error is unclear: %v", r.Err)
		}
	}
}

func TestSQLiteUserSessionAllowsIdentifiersAndTemporaryObjects(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is not installed")
	}
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, sql := range []string{
		`CREATE TEMP TABLE t (edit TEXT, readfile TEXT)`,
		`INSERT INTO t VALUES ('ok', 'ok')`,
		`ATTACH DATABASE ':memory:' AS extra`,
	} {
		if r := s.Run(context.Background(), sql, RunOptions{}); r.Err != nil {
			t.Errorf("Run(%q): %v", sql, r.Err)
		}
	}
	r := s.Run(context.Background(), `SELECT edit, readfile FROM t`, RunOptions{})
	if r.Err != nil || r.Rows[0][0].Text != "ok" {
		t.Fatalf("identifier query: %+v", r)
	}
}

func TestSQLiteRealCLICancelLeavesSessionUsable(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is not installed")
	}
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "cancel.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	go func() {
		done <- s.Run(ctx, `WITH RECURSIVE n(x) AS (VALUES(0) UNION ALL SELECT x+1 FROM n WHERE x<100000000) SELECT sum(x) FROM n`, RunOptions{})
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case r := <-done:
		if r.Err == nil {
			t.Fatal("cancelled query returned without error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not return promptly")
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("session unusable after cancel: %v", err)
	}
}

func TestSQLiteSplitRejectsIncompleteStatements(t *testing.T) {
	for _, sql := range []string{`SELECT 'abc`, `SELECT 1 /* x`, `SELECT [unclosed`, `CREATE TRIGGER t AFTER INSERT ON x BEGIN SELECT 1`, `CREATE TRIGGER t AFTER INSERT ON x BEGIN SELECT 1;`} {
		if _, err := Split(sql, "sqlite"); err == nil || !strings.Contains(err.Error(), "incomplete") {
			t.Errorf("Split(%q) error = %v", sql, err)
		}
	}
}

func TestSQLiteCancelledContextDoesNotRunStatement(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is not installed")
	}
	path := filepath.Join(t.TempDir(), "cancel-before.db")
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r := s.Run(context.Background(), `CREATE TABLE t (x)`, RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := s.Run(ctx, `INSERT INTO t VALUES (1)`, RunOptions{})
	if !errors.Is(r.Err, context.Canceled) {
		t.Fatalf("cancelled Run error = %v", r.Err)
	}
	count := s.Run(context.Background(), `SELECT count(*) AS n FROM t`, RunOptions{})
	if count.Err != nil || count.Rows[0][0].Text != "0" {
		t.Fatalf("cancelled statement ran: %+v", count)
	}
}

func TestSQLiteUnexpectedProcessExitRestartsAndClearsTransaction(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is not installed")
	}
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.stateMu.Lock()
	p := s.proc
	s.stateMu.Unlock()
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	r := s.Run(context.Background(), `SELECT 1`, RunOptions{})
	if r.Err == nil || len(r.Notices) != 1 || !strings.Contains(r.Notices[0], "session was restarted") || !strings.Contains(r.Notices[0], "transaction was rolled back") {
		t.Fatalf("unexpected exit result: %+v", r)
	}
	if s.transaction {
		t.Fatal("transaction flag remained set after process restart")
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("restarted session unusable: %v", err)
	}
}

func TestSQLiteAffectedOnlyForWritesAndBlobNote(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is not installed")
	}
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r := s.Run(context.Background(), "CREATE TABLE t (x)", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if r := s.Run(context.Background(), "INSERT INTO t VALUES (1)", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if r := s.Run(context.Background(), "SELECT * FROM t", RunOptions{}); r.Err != nil || r.Affected != 0 {
		t.Fatalf("read result: %+v", r)
	}
	r := s.Run(context.Background(), `SELECT X'41FF' AS b`, RunOptions{})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(r.Rows) != 1 || len(r.Rows[0]) != 1 || r.Rows[0][0].Text == "" || r.Rows[0][0].Kind != "binary" || len(r.Notices) == 0 {
		t.Fatalf("blob result: %+v", r)
	}
}

func TestSQLiteMaxRowsDrainsAndTruncates(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is not installed")
	}
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	r := s.Run(context.Background(), `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000) SELECT x FROM n`, RunOptions{MaxRows: 10})
	runtime.GC()
	runtime.ReadMemStats(&after)
	if after.HeapAlloc > before.HeapAlloc+12<<20 {
		t.Fatalf("MaxRows retained too much memory: %d bytes", after.HeapAlloc-before.HeapAlloc)
	}
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if !r.Truncated || len(r.Rows) != 10 {
		t.Fatalf("rows=%d truncated=%v", len(r.Rows), r.Truncated)
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("session not drained: %v", err)
	}
}

func TestSQLiteLateCancelLoopKeepsFollowingQueriesWorking(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is not installed")
	}
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 300; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan Result, 1)
		go func() { done <- s.Run(ctx, "SELECT 1", RunOptions{}) }()
		cancel()
		r := <-done
		if r.Err != nil && !errors.Is(r.Err, context.Canceled) {
			t.Fatalf("iteration %d: %v", i, r.Err)
		}
		if err := s.Ping(context.Background()); err != nil {
			t.Fatalf("iteration %d following query: %v", i, err)
		}
	}
}

func writeFakeSQLite(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sqlite3")
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSQLiteTwinRefusesHelperNamesAnywhere(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is not installed")
	}
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("TOPSECRET"), 0600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "twin.db")
	w, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: db})
	if err != nil {
		t.Fatal(err)
	}
	if r := w.Run(context.Background(), `CREATE TABLE t (a)`, RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	w.Close()
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: db, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, sql := range []string{
		`SELECT data FROM fsdir WHERE path='` + secret + `'`,
		`SELECT data FROM "fsdir" WHERE path='` + secret + `'`,
		`SELECT data FROM main.fsdir WHERE path='` + secret + `'`,
		`SELECT f.data FROM fsdir AS f WHERE f.path='` + secret + `'`,
		`SELECT data FROM [fsdir] WHERE path='` + secret + `'`,
		"SELECT data FROM `fsdir` WHERE path='" + secret + `'`,
		`SELECT data FROM main."FsDir" WHERE path='` + secret + `'`,
		`SELECT * FROM zipfile('x')`,
		`SELECT * FROM sqlar`,
		`SELECT shell_add_schema_name(1,2)`,
		`PRAGMA temp_store_directory = '/tmp'`,
		`PRAGMA main.temp_store_directory`,
	} {
		r := s.Run(context.Background(), sql, RunOptions{})
		if r.Err == nil || !strings.Contains(r.Err.Error(), "not allowed") {
			t.Errorf("Run(%q) err = %v", sql, r.Err)
		}
		for _, row := range r.Rows {
			for _, c := range row {
				if strings.Contains(c.Text, "TOPSECRET") {
					t.Errorf("Run(%q) leaked the file", sql)
				}
			}
		}
	}
	// Strings and comments are not identifiers.
	if r := s.Run(context.Background(), `SELECT 'fsdir' AS x /* readfile */`, RunOptions{}); r.Err != nil {
		t.Errorf("string literal refused: %v", r.Err)
	}
}

func TestSQLiteCancelAfterSendSaysWriteMayHaveFinished(t *testing.T) {
	bin := writeFakeSQLite(t, `#!/bin/sh
if [ "$1" = "-version" ]; then
	echo '3.45.1 2024-01-30'
	exit 0
fi
while IFS= read -r line; do
	case "$line" in
	INSERT*|SELECT\ SLOW*) sleep 10 ;;
	.print\ __TC_END_*) printf '%s\n' "${line#*.print }" ;;
	*) : ;;
	esac
done
`)
	old := lookupSQLite
	lookupSQLite = func() (string, error) { return bin, nil }
	t.Cleanup(func() { lookupSQLite = old })
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for sql, want := range map[string]string{
		`INSERT INTO t VALUES (1)`: "cancelled · the statement may already have finished",
		`SELECT SLOW`:              "context canceled",
	} {
		done := make(chan Result, 1)
		go func() { done <- s.Run(context.Background(), sql, RunOptions{}) }()
		time.Sleep(50 * time.Millisecond)
		s.Cancel()
		select {
		case r := <-done:
			if r.Err == nil || r.Err.Error() != want || !errors.Is(r.Err, context.Canceled) {
				t.Errorf("%q err = %v, want %q", sql, r.Err, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%q: cancel did not return", sql)
		}
	}
}

func TestSQLiteSplitTriggerBodies(t *testing.T) {
	ok := []string{
		`CREATE TRIGGER tr AFTER INSERT ON t BEGIN SELECT 1; END;`,
		`CREATE TRIGGER tr AFTER INSERT ON t BEGIN SELECT CASE WHEN 1 THEN 2 END; END;`,
		`CREATE TRIGGER tr AFTER INSERT ON t BEGIN UPDATE t SET a = 'END'; END;`,
		`CREATE TRIGGER tr AFTER INSERT ON t BEGIN UPDATE t SET a = 1; /* END; */ -- END;
 END;`,
		`CREATE TRIGGER tr AFTER INSERT ON t BEGIN UPDATE t SET "END" = 1, [END] = 2; END;`,
		`CREATE TRIGGER tr AFTER INSERT ON t BEGIN SELECT CASE WHEN 1 THEN CASE WHEN 2 THEN 3 END END; END;`,
	}
	for _, sql := range ok {
		got, err := Split(sql, "sqlite")
		if err != nil || len(got) != 1 {
			t.Errorf("Split(%q) = %v, %v", sql, got, err)
		}
	}
	for _, sql := range []string{
		`CREATE TRIGGER tr AFTER INSERT ON t BEGIN SELECT 1`,
		`CREATE TRIGGER tr AFTER INSERT ON t BEGIN SELECT 1;`,
		`CREATE TRIGGER tr AFTER INSERT ON t BEGIN SELECT CASE WHEN 1 THEN 2 END;`,
		`CREATE TRIGGER tr AFTER INSERT ON t BEGIN UPDATE t SET a = 'END';`,
	} {
		if _, err := Split(sql, "sqlite"); err == nil || !strings.Contains(err.Error(), "incomplete") {
			t.Errorf("Split(%q) error = %v", sql, err)
		}
	}
}
