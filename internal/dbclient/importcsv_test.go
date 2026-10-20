package dbclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSniffCSVDelimitersAndHeader(t *testing.T) {
	cases := []struct {
		name, text string
		delim      rune
		header     bool
		cols       []string
	}{
		{"comma", "id,email,plan\n1,a@x.io,free\n2,b@x.io,pro\n", ',', true, []string{"id", "email", "plan"}},
		{"semicolon", "id;email;plan\n1;a@x.io;free\n2;b@x.io;pro\n", ';', true, []string{"id", "email", "plan"}},
		{"tab", "id\temail\tplan\n1\ta@x.io\tfree\n", '\t', true, []string{"id", "email", "plan"}},
		{"pipe", "id|email|plan\n1|a@x.io|free\n2|b@x.io|pro\n", '|', true, []string{"id", "email", "plan"}},
		{"quoted delimiter", "name,note\n\"a;b;c\",\"x;y\"\n\"d;e;f\",\"z;w\"\n", ',', true, []string{"name", "note"}},
		{"no header", "1,a@x.io,free\n2,b@x.io,pro\n", ',', false, []string{"column 1", "column 2", "column 3"}},
		{"single column", "name\nada\ngrace\n", ',', true, []string{"name"}},
	}
	for _, c := range cases {
		sn, err := SniffCSV(writeFile(t, c.name+".csv", []byte(c.text)))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if sn.Delimiter != c.delim || sn.Header != c.header || strings.Join(sn.Columns, "|") != strings.Join(c.cols, "|") {
			t.Fatalf("%s: delimiter %q header %v columns %q", c.name, sn.Delimiter, sn.Header, sn.Columns)
		}
	}
}

func TestSniffCSVSampleIsTheFirstFiveDataRows(t *testing.T) {
	var b strings.Builder
	b.WriteString("n,v\n")
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "%d,x%d\n", i, i)
	}
	sn, err := SniffCSV(writeFile(t, "s.csv", []byte(b.String())))
	if err != nil {
		t.Fatal(err)
	}
	if len(sn.Sample) != 5 || sn.Sample[0][0] != "1" || sn.Sample[4][1] != "x5" {
		t.Fatalf("sample %v", sn.Sample)
	}
}

func TestSniffCSVEncodings(t *testing.T) {
	utf8bom := append([]byte{0xEF, 0xBB, 0xBF}, "name,city\nAda,Zürich\n"...)
	u16 := func(order string, text string) []byte {
		units := utf16.Encode([]rune(text))
		out := []byte{0xFF, 0xFE}
		if order == "be" {
			out = []byte{0xFE, 0xFF}
		}
		for _, u := range units {
			if order == "be" {
				out = append(out, byte(u>>8), byte(u))
			} else {
				out = append(out, byte(u), byte(u>>8))
			}
		}
		return out
	}
	latin1 := []byte("name,city\nAda,Z\xfcrich\n")
	for _, c := range []struct {
		name string
		data []byte
		enc  string
	}{
		{"utf8", []byte("name,city\nAda,Zürich\n"), "UTF-8"},
		{"utf8bom", utf8bom, "UTF-8"},
		{"utf16le", u16("le", "name,city\nAda,Zürich\n"), "UTF-16LE"},
		{"utf16be", u16("be", "name,city\nAda,Zürich\n"), "UTF-16BE"},
		{"latin1", latin1, "ISO-8859-1"},
	} {
		sn, err := SniffCSV(writeFile(t, c.name+".csv", c.data))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if sn.Encoding != c.enc || strings.Join(sn.Columns, ",") != "name,city" || len(sn.Sample) != 1 || sn.Sample[0][1] != "Zürich" {
			t.Fatalf("%s: encoding %q columns %q sample %q", c.name, sn.Encoding, sn.Columns, sn.Sample)
		}
	}
}

func TestSniffCSVEmptyFile(t *testing.T) {
	if _, err := SniffCSV(writeFile(t, "e.csv", nil)); err == nil {
		t.Fatal("an empty file was accepted")
	}
	if _, err := SniffCSV(filepath.Join(t.TempDir(), "missing.csv")); err == nil {
		t.Fatal("a missing file was accepted")
	}
}

func TestCountCSVRowsSkipsHeaderAndKeepsQuotedNewlines(t *testing.T) {
	p := writeFile(t, "c.csv", []byte("id,note\n1,\"a\nb\"\n2,c\n3,d\n"))
	sn, _ := SniffCSV(p)
	n, err := CountCSVRows(context.Background(), p, sn)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	sn.Header = false
	if n, _ := CountCSVRows(context.Background(), p, sn); n != 4 {
		t.Fatalf("without header n=%d", n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CountCSVRows(ctx, p, sn); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestAutoMapByNameThenByPosition(t *testing.T) {
	sn := CSVSniff{Header: true, Columns: []string{"EMAIL", "id", "extra"}}
	got := AutoMap([]string{"id", "email", "plan"}, sn)
	if fmt.Sprint(got) != "[1 0 -1]" {
		t.Fatalf("by name %v", got)
	}
	sn = CSVSniff{Header: false, Columns: []string{"column 1", "column 2"}}
	got = AutoMap([]string{"id", "email", "plan"}, sn)
	if fmt.Sprint(got) != "[0 1 -1]" {
		t.Fatalf("by position %v", got)
	}
}

// importRec is a fake session that records every call.
type importRec struct {
	Session
	events    []string
	args      [][]any
	failValue string // a statement that binds this value fails
	commitErr error
	onRun     func(n int)
	runs      int
}

func (r *importRec) Begin(context.Context) error { r.events = append(r.events, "BEGIN"); return nil }
func (r *importRec) Commit(context.Context) error {
	r.events = append(r.events, "COMMIT")
	return r.commitErr
}
func (r *importRec) Rollback(context.Context) error {
	r.events = append(r.events, "ROLLBACK")
	return nil
}
func (r *importRec) Run(ctx context.Context, sql string, o RunOptions) Result {
	r.runs++
	if err := ctx.Err(); err != nil {
		return Result{Err: err}
	}
	r.events = append(r.events, sql)
	r.args = append(r.args, o.Args)
	if r.onRun != nil {
		r.onRun(r.runs)
	}
	if strings.HasPrefix(sql, "INSERT") {
		for _, a := range o.Args {
			if a == r.failValue && r.failValue != "" {
				return Result{Err: errors.New(`ERROR: invalid input syntax for type integer: "` + r.failValue + `"`)}
			}
		}
		return Result{Affected: int64(len(o.Args))}
	}
	return Result{}
}

func importSpec(t *testing.T, csv string, batch int, inTx bool) ImportSpec {
	t.Helper()
	p := writeFile(t, "in.csv", []byte(csv))
	sn, err := SniffCSV(p)
	if err != nil {
		t.Fatal(err)
	}
	return ImportSpec{Path: p, Sniff: sn, Dialect: PostgresDialect{}, Schema: "public", Table: `us"ers`, Targets: []string{"id", "email"}, Sources: []int{0, 1}, BatchRows: batch, InTx: inTx}
}

func TestImportCSVBatchesInOneTransactionWithBoundValues(t *testing.T) {
	spec := importSpec(t, "id,email\n1,a@x\n2,\n3,c'); DROP TABLE users;--\n4,d@x\n5,e@x\n", 2, false)
	rec := &importRec{}
	var progress []int64
	res := ImportCSV(context.Background(), rec, spec, func(n int64) { progress = append(progress, n) })
	if res.Err != nil || res.Rows != 5 || res.Cancelled || res.FailedRow != 0 {
		t.Fatalf("%+v", res)
	}
	want := []string{
		"BEGIN",
		"SAVEPOINT termocode_import_batch",
		`INSERT INTO "public"."us""ers" ("id", "email") VALUES ($1, $2), ($3, $4)`,
		"RELEASE SAVEPOINT termocode_import_batch",
		"SAVEPOINT termocode_import_batch",
		`INSERT INTO "public"."us""ers" ("id", "email") VALUES ($1, $2), ($3, $4)`,
		"RELEASE SAVEPOINT termocode_import_batch",
		"SAVEPOINT termocode_import_batch",
		`INSERT INTO "public"."us""ers" ("id", "email") VALUES ($1, $2)`,
		"RELEASE SAVEPOINT termocode_import_batch",
		"COMMIT",
	}
	if strings.Join(rec.events, "\n") != strings.Join(want, "\n") {
		t.Fatalf("events:\n%s", strings.Join(rec.events, "\n"))
	}
	inserts := 0
	for i, e := range rec.events {
		if strings.HasPrefix(e, "INSERT") {
			if strings.Contains(e, "DROP") || strings.Contains(e, "a@x") {
				t.Fatalf("a value is in the SQL text: %s", e)
			}
			_ = i
			inserts++
		}
	}
	// Empty cells are NULL; the quote-and-semicolon cell is one bound string.
	var flat []any
	for _, a := range rec.args {
		flat = append(flat, a...)
	}
	if fmt.Sprintf("%#v", flat[:6]) != fmt.Sprintf("%#v", []any{"1", "a@x", "2", nil, "3", "c'); DROP TABLE users;--"}) {
		t.Fatalf("args %#v", flat)
	}
	if fmt.Sprint(progress) != "[2 4 5]" {
		t.Fatalf("progress %v", progress)
	}
}

func TestImportCSVBadRowRollsEverythingBackAndNamesTheRow(t *testing.T) {
	spec := importSpec(t, "id,email\n1,a\n2,b\n3,c\nBAD,d\n5,e\n", 3, false)
	rec := &importRec{failValue: "BAD"}
	res := ImportCSV(context.Background(), rec, spec, nil)
	if res.Err == nil || res.FailedRow != 4 || res.FailedLine != 5 || res.Rows != 0 {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.Err.Error(), "invalid input syntax") {
		t.Fatalf("err %v", res.Err)
	}
	last := rec.events[len(rec.events)-1]
	if last != "ROLLBACK" {
		t.Fatalf("last event %q:\n%s", last, strings.Join(rec.events, "\n"))
	}
	for _, e := range rec.events {
		if e == "COMMIT" {
			t.Fatal("committed a failed import")
		}
	}
	// The batch was rolled back to its savepoint before the rows were tried one by one.
	joined := strings.Join(rec.events, "\n")
	if !strings.Contains(joined, "ROLLBACK TO SAVEPOINT termocode_import_batch") {
		t.Fatalf("no savepoint rollback:\n%s", joined)
	}
}

func TestImportCSVWrongFieldCountIsARowError(t *testing.T) {
	spec := importSpec(t, "id,email\n1,a\n2\n3,c\n", 10, false)
	rec := &importRec{}
	res := ImportCSV(context.Background(), rec, spec, nil)
	if res.Err == nil || res.FailedRow != 2 || res.FailedLine != 3 || res.Rows != 0 {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.Err.Error(), "1 field") {
		t.Fatalf("err %v", res.Err)
	}
	for _, e := range rec.events {
		if e == "COMMIT" {
			t.Fatal("committed")
		}
	}
}

func TestImportCSVCancelRollsBack(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("id,email\n")
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&sb, "%d,x\n", i)
	}
	spec := importSpec(t, sb.String(), 10, false)
	ctx, cancel := context.WithCancel(context.Background())
	rec := &importRec{}
	res := ImportCSV(ctx, rec, spec, func(n int64) {
		if n >= 20 {
			cancel()
		}
	})
	if !res.Cancelled || res.Rows != 0 || res.Err != nil {
		t.Fatalf("%+v", res)
	}
	if rec.events[len(rec.events)-1] != "ROLLBACK" {
		t.Fatalf("events:\n%s", strings.Join(rec.events, "\n"))
	}
	inserts := 0
	for _, e := range rec.events {
		if strings.HasPrefix(e, "INSERT") {
			inserts++
		}
	}
	if inserts > 3 {
		t.Fatalf("%d inserts after the cancel", inserts)
	}
}

func TestImportCSVInsideAnOpenTransactionUsesASavepoint(t *testing.T) {
	spec := importSpec(t, "id,email\n1,a\n2,b\n", 10, true)
	rec := &importRec{}
	res := ImportCSV(context.Background(), rec, spec, nil)
	if res.Err != nil || res.Rows != 2 {
		t.Fatalf("%+v", res)
	}
	got := strings.Join(rec.events, "\n")
	if strings.Contains(got, "BEGIN") || strings.Contains(got, "COMMIT") || !strings.HasPrefix(got, "SAVEPOINT termocode_import\n") || !strings.HasSuffix(got, "RELEASE SAVEPOINT termocode_import") {
		t.Fatalf("events:\n%s", got)
	}
	spec = importSpec(t, "id,email\n1,a\nBAD,b\n", 10, true)
	rec = &importRec{failValue: "BAD"}
	res = ImportCSV(context.Background(), rec, spec, nil)
	got = strings.Join(rec.events, "\n")
	if res.Err == nil || res.FailedRow != 2 || !strings.Contains(got, "ROLLBACK TO SAVEPOINT termocode_import\nRELEASE SAVEPOINT termocode_import") || strings.Contains(got, "COMMIT") {
		t.Fatalf("%+v events:\n%s", res, got)
	}
}

func TestImportCSVSkipsUnmappedAndNeedsAMapping(t *testing.T) {
	p := writeFile(t, "m.csv", []byte("a,b,c\n1,2,3\n"))
	sn, _ := SniffCSV(p)
	rec := &importRec{}
	spec := ImportSpec{Path: p, Sniff: sn, Dialect: MySQLDialect{}, Table: "t", Targets: []string{"x", "z"}, Sources: []int{2, 0}, BatchRows: 100}
	if res := ImportCSV(context.Background(), rec, spec, nil); res.Err != nil || res.Rows != 1 {
		t.Fatalf("%+v", res)
	}
	if rec.events[2] != "INSERT INTO `t` (`x`, `z`) VALUES (?, ?)" || fmt.Sprint(rec.args[1]) != "[3 1]" {
		t.Fatalf("events %q args %v", rec.events, rec.args)
	}
	empty := ImportSpec{Path: p, Sniff: sn, Dialect: MySQLDialect{}, Table: "t"}
	if res := ImportCSV(context.Background(), &importRec{}, empty, nil); res.Err == nil {
		t.Fatal("imported with no columns mapped")
	}
}

func TestImportCSVLimitsParametersPerStatement(t *testing.T) {
	spec := importSpec(t, "id,email\n1,a\n", 100000, false)
	if got := spec.rowsPerBatch(); got != 1000 {
		t.Fatalf("rows per batch %d, want the 1000 cap", got)
	}
	spec.Dialect = SQLiteDialect{}
	if got := spec.rowsPerBatch(); got != 450 {
		t.Fatalf("sqlite rows per batch %d", got)
	}
	spec.Targets, spec.Sources, spec.Dialect = make([]string, 100), make([]int, 100), PostgresDialect{}
	if got := spec.rowsPerBatch(); got != 600 {
		t.Fatalf("wide table rows per batch %d", got)
	}
}

func TestImportCSVIntoSQLiteAndStreamBack(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	db := filepath.Join(t.TempDir(), "imp.db")
	if out, err := exec.Command("sqlite3", db, "CREATE TABLE u(id INTEGER PRIMARY KEY, email TEXT NOT NULL, plan TEXT);").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var sb strings.Builder
	sb.WriteString("id,email,plan\n")
	for i := 1; i <= 2300; i++ {
		fmt.Fprintf(&sb, "%d,user%d@x.io,\"pl'an;%d\"\n", i, i, i%3)
	}
	sess, err := NewSQLiteSession(context.Background(), Conn{ID: "x", Driver: "sqlite", Path: db})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	p := writeFile(t, "u.csv", []byte(sb.String()))
	sn, err := SniffCSV(p)
	if err != nil {
		t.Fatal(err)
	}
	spec := ImportSpec{Path: p, Sniff: sn, Dialect: SQLiteDialect{}, Table: "u", Targets: []string{"id", "email", "plan"}, Sources: []int{0, 1, 2}, BatchRows: 1000}
	res := ImportCSV(context.Background(), sess, spec, nil)
	if res.Err != nil || res.Rows != 2300 {
		t.Fatalf("%+v", res)
	}
	r := sess.Run(context.Background(), "SELECT COUNT(*), MIN(plan), MAX(id) FROM u", RunOptions{})
	if r.Err != nil || r.Rows[0][0].Text != "2300" || r.Rows[0][2].Text != "2300" {
		t.Fatalf("%+v", r)
	}
	// A duplicate key in the last row rolls the whole file back.
	bad := writeFile(t, "dup.csv", []byte("id,email,plan\n9001,a,b\n9002,c,d\n1,dup,x\n"))
	sn, _ = SniffCSV(bad)
	spec = ImportSpec{Path: bad, Sniff: sn, Dialect: SQLiteDialect{}, Table: "u", Targets: []string{"id", "email", "plan"}, Sources: []int{0, 1, 2}, BatchRows: 2}
	res = ImportCSV(context.Background(), sess, spec, nil)
	if res.Err == nil || res.FailedRow != 3 {
		t.Fatalf("%+v", res)
	}
	r = sess.Run(context.Background(), "SELECT COUNT(*) FROM u", RunOptions{})
	if r.Rows[0][0].Text != "2300" {
		t.Fatalf("rows after a failed import: %+v", r)
	}
	// Export everything through the paging fallback into a file and count lines.
	out := filepath.Join(t.TempDir(), "u.csv")
	n, err := ExportToFile(context.Background(), out, FormatCSV, ExportOptions{}, false, func(ctx context.Context, sink RowSink) error {
		return StreamRows(ctx, sess, "sqlite", "SELECT * FROM u ORDER BY id;", sink)
	}, nil)
	if err != nil || n != 2300 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	b, _ := os.ReadFile(out)
	if got := strings.Count(string(b), "\n"); got != 2301 {
		t.Fatalf("%d lines", got)
	}
}

func TestCSVSniffWithHeaderTogglesNamesAndSample(t *testing.T) {
	p := writeFile(t, "h.csv", []byte("name,city\nada,rome\ngrace,paris\n"))
	sn, err := SniffCSV(p)
	if err != nil || !sn.Header {
		t.Fatalf("%+v %v", sn, err)
	}
	off := sn.WithHeader(false)
	if off.Header || strings.Join(off.Columns, "|") != "column 1|column 2" || len(off.Sample) != 3 || off.Sample[0][0] != "name" {
		t.Fatalf("off: %+v", off)
	}
	on := off.WithHeader(true)
	if !on.Header || strings.Join(on.Columns, "|") != "name|city" || len(on.Sample) != 2 || on.Sample[0][0] != "ada" {
		t.Fatalf("on: %+v", on)
	}
}
