package dbclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func exportResult() Result {
	return Result{
		Columns: []Column{{Name: "id"}, {Name: "name"}, {Name: "note"}},
		Rows: [][]Cell{
			{{Text: "1", Kind: "number"}, {Text: "O'Hara"}, {Null: true}},
			{{Text: "2", Kind: "number"}, {Text: `back\slash`}, {Text: "line1\nline2"}},
			{{Text: "-3", Kind: "number"}, {Text: "=1+1"}, {Text: "a,b \"q\""}},
		},
	}
}

func exportString(t *testing.T, f ExportFormat, o ExportOptions, r Result) string {
	t.Helper()
	var b bytes.Buffer
	w, err := NewRowWriter(f, &b, o)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteResult(w, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestExportCSVProtectsFormulaCellsAndQuotes(t *testing.T) {
	got := exportString(t, FormatCSV, ExportOptions{}, exportResult())
	want := "id,name,note\r\n1,O'Hara,\r\n2,back\\slash,\"line1\r\nline2\"\r\n-3,'=1+1,\"a,b \"\"q\"\"\"\r\n"
	if got != want {
		t.Fatalf("csv:\n%q\nwant:\n%q", got, want)
	}
}

func TestExportProtectsEveryFormulaStarter(t *testing.T) {
	for _, in := range []string{"=A1", "+1", "-1", "@SUM(A1)", "\tx", "\rx"} {
		if got := ProtectFormula(in); got != "'"+in {
			t.Fatalf("ProtectFormula(%q) = %q", in, got)
		}
	}
	for _, in := range []string{"", "plain", "a=b", "1", " =x"} {
		if got := ProtectFormula(in); got != in {
			t.Fatalf("ProtectFormula(%q) = %q, want unchanged", in, got)
		}
	}
	// A real number keeps its minus sign; text that only looks like one does not.
	r := Result{Columns: []Column{{Name: "=h"}}, Rows: [][]Cell{{{Text: "-5", Kind: "number"}}, {{Text: "-5"}}}}
	got := exportString(t, FormatCSV, ExportOptions{}, r)
	if got != "'=h\r\n-5\r\n'-5\r\n" {
		t.Fatalf("csv %q", got)
	}
}

func TestExportTSVProtectsFormulaCells(t *testing.T) {
	r := Result{Columns: []Column{{Name: "a"}, {Name: "b"}}, Rows: [][]Cell{{{Text: "@x"}, {Text: "plain"}}}}
	if got := exportString(t, FormatTSV, ExportOptions{}, r); got != "a\tb\r\n'@x\tplain\r\n" {
		t.Fatalf("tsv %q", got)
	}
}

func TestExportJSONKeepsTypesAndNull(t *testing.T) {
	got := exportString(t, FormatJSON, ExportOptions{}, exportResult())
	want := "[\n" +
		`{"id":1,"name":"O'Hara","note":null},` + "\n" +
		`{"id":2,"name":"back\\slash","note":"line1\nline2"},` + "\n" +
		`{"id":-3,"name":"=1+1","note":"a,b \"q\""}` + "\n]\n"
	if got != want {
		t.Fatalf("json:\n%s\nwant:\n%s", got, want)
	}
	if empty := exportString(t, FormatJSON, ExportOptions{}, Result{Columns: []Column{{Name: "a"}}}); empty != "[]\n" {
		t.Fatalf("empty json %q", empty)
	}
}

func TestExportMarkdownEscapesPipesAndNewlines(t *testing.T) {
	r := Result{Columns: []Column{{Name: "a|b"}, {Name: "c"}}, Rows: [][]Cell{{{Text: "x|y"}, {Text: "l1\nl2"}}, {{Null: true}, {Text: "z"}}}}
	want := "| a\\|b | c |\n| --- | --- |\n| x\\|y | l1<br>l2 |\n| NULL | z |\n"
	if got := exportString(t, FormatMarkdown, ExportOptions{}, r); got != want {
		t.Fatalf("md:\n%q\nwant:\n%q", got, want)
	}
}

func TestExportSQLInsertsPerDialect(t *testing.T) {
	r := Result{
		Columns: []Column{{Name: `we"ird`}, {Name: "back`tick"}, {Name: "v"}},
		Rows: [][]Cell{
			{{Text: "1", Kind: "number"}, {Text: `it's a\b`}, {Null: true}},
			{{Text: "true", Kind: "bool"}, {Text: "0xDEADBEEF", Kind: "binary"}, {Text: "1e5x", Kind: "number"}},
		},
	}
	cases := []struct {
		driver, want string
	}{
		{"postgres", `INSERT INTO "public"."t""x" ("we""ird", "back` + "`" + `tick", "v") VALUES (1, 'it''s a\b', NULL);` + "\n" +
			`INSERT INTO "public"."t""x" ("we""ird", "back` + "`" + `tick", "v") VALUES (TRUE, '\xDEADBEEF', '1e5x');` + "\n"},
		{"mysql", "INSERT INTO `public`.`t\"x` (`we\"ird`, `back``tick`, `v`) VALUES (1, 'it''s a\\\\b', NULL);\n" +
			"INSERT INTO `public`.`t\"x` (`we\"ird`, `back``tick`, `v`) VALUES (TRUE, 0xDEADBEEF, '1e5x');\n"},
		{"sqlite", `INSERT INTO "public"."t""x" ("we""ird", "back` + "`" + `tick", "v") VALUES (1, 'it''s a\b', NULL);` + "\n" +
			`INSERT INTO "public"."t""x" ("we""ird", "back` + "`" + `tick", "v") VALUES (TRUE, X'DEADBEEF', '1e5x');` + "\n"},
	}
	for _, c := range cases {
		got := exportString(t, FormatSQL, ExportOptions{Driver: c.driver, Schema: "public", Table: `t"x`}, r)
		if got != c.want {
			t.Fatalf("%s:\n%s\nwant:\n%s", c.driver, got, c.want)
		}
	}
}

func TestExportSQLNeedsAKnownDialect(t *testing.T) {
	if _, err := NewRowWriter(FormatSQL, &bytes.Buffer{}, ExportOptions{Driver: "redis", Table: "t"}); err == nil {
		t.Fatal("accepted a driver without SQL")
	}
	if _, err := NewRowWriter(FormatSQL, &bytes.Buffer{}, ExportOptions{Driver: "postgres"}); err == nil {
		t.Fatal("accepted INSERT export without a table name")
	}
}

func TestExportFormatTable(t *testing.T) {
	want := map[ExportFormat]string{FormatCSV: ".csv", FormatTSV: ".tsv", FormatJSON: ".json", FormatSQL: ".sql", FormatMarkdown: ".md"}
	if len(ExportFormats) != len(want) {
		t.Fatalf("%d formats", len(ExportFormats))
	}
	for _, f := range ExportFormats {
		if want[f.Format] != f.Ext || f.Title == "" {
			t.Fatalf("%+v", f)
		}
	}
}

func TestExportToFileNeverOverwritesWithoutPermission(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	if err := os.WriteFile(path, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := func(ctx context.Context, sink RowSink) error { return emitResult(sink, exportResult()) }
	_, err := ExportToFile(context.Background(), path, FormatCSV, ExportOptions{}, false, src, nil)
	if !errors.Is(err, ErrExportExists) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "keep me" {
		t.Fatalf("file changed: %q", b)
	}
	n, err := ExportToFile(context.Background(), path, FormatCSV, ExportOptions{}, true, src, nil)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), "id,name,note") {
		t.Fatalf("file %q", b)
	}
	assertNoTemp(t, dir)
}

func assertNoTemp(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "termocode-export") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestExportToFileFailureLeavesNoFileAndKeepsTheOldOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	boom := errors.New("server went away")
	src := func(ctx context.Context, sink RowSink) error {
		_ = sink.Columns([]Column{{Name: "a"}})
		_ = sink.Row([]Cell{{Text: "1"}})
		return boom
	}
	if _, err := ExportToFile(context.Background(), path, FormatJSON, ExportOptions{}, false, src, nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("partial file exists: %v", err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportToFile(context.Background(), path, FormatJSON, ExportOptions{}, true, src, nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Fatalf("old file replaced: %q", b)
	}
	assertNoTemp(t, dir)
}

func TestExportToFileStopsOnCancelAndReportsProgress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.csv")
	ctx, cancel := context.WithCancel(context.Background())
	var seen []int64
	src := func(ctx context.Context, sink RowSink) error {
		_ = sink.Columns([]Column{{Name: "n"}})
		for i := 0; ; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := sink.Row([]Cell{{Text: "x"}}); err != nil {
				return err
			}
			if i == 2500 {
				cancel()
			}
		}
	}
	_, err := ExportToFile(ctx, path, FormatCSV, ExportOptions{}, false, src, func(n int64) { seen = append(seen, n) })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cancelled export left a file")
	}
	if len(seen) == 0 {
		t.Fatal("no progress reported")
	}
	assertNoTemp(t, dir)
}

func TestExportToFileRefusesADirectoryAndMissingFolder(t *testing.T) {
	dir := t.TempDir()
	src := func(ctx context.Context, sink RowSink) error { return emitResult(sink, exportResult()) }
	if _, err := ExportToFile(context.Background(), dir, FormatCSV, ExportOptions{}, true, src, nil); err == nil {
		t.Fatal("wrote over a directory")
	}
	if _, err := ExportToFile(context.Background(), filepath.Join(dir, "nope", "x.csv"), FormatCSV, ExportOptions{}, false, src, nil); err == nil {
		t.Fatal("wrote into a folder that does not exist")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("stray files: %v", entries)
	}
}

// pagedSession answers wrapped SELECTs with LIMIT / OFFSET from memory.
type pagedSession struct {
	Session
	total   int
	queries []string
	maxSeen int
}

func (p *pagedSession) Run(_ context.Context, sql string, o RunOptions) Result {
	p.queries = append(p.queries, sql)
	var limit, offset int
	i := strings.LastIndex(sql, " LIMIT ")
	if i < 0 {
		return Result{Err: errors.New("no limit: " + sql)}
	}
	if _, err := fmtSscanf(sql[i:], &limit, &offset); err != nil {
		return Result{Err: err}
	}
	p.maxSeen = max(p.maxSeen, limit)
	r := Result{Columns: []Column{{Name: "n"}}}
	for k := offset; k < offset+limit && k < p.total; k++ {
		r.Rows = append(r.Rows, []Cell{{Text: itoa(k), Kind: "number"}})
	}
	return r
}

func TestStreamRowsPagesWithoutLoadingEverything(t *testing.T) {
	p := &pagedSession{total: 2500}
	var got, cols int
	err := StreamRows(context.Background(), p, "postgres", "SELECT n FROM t ORDER BY n;", sinkFunc{
		cols: func([]Column) error { cols++; return nil },
		row:  func([]Cell) error { got++; return nil },
	})
	if err != nil || got != 2500 || cols != 1 {
		t.Fatalf("rows=%d cols=%d err=%v", got, cols, err)
	}
	if p.maxSeen > 1000 {
		t.Fatalf("a page of %d rows was asked for", p.maxSeen)
	}
	if len(p.queries) != 3 || !strings.Contains(p.queries[0], "SELECT * FROM (SELECT n FROM t ORDER BY n) AS termocode_export") {
		t.Fatalf("queries %q", p.queries)
	}
}

func TestStreamRowsStopsWhenTheSinkFails(t *testing.T) {
	p := &pagedSession{total: 5000}
	stop := errors.New("disk full")
	n := 0
	err := StreamRows(context.Background(), p, "sqlite", "SELECT n FROM t", sinkFunc{
		cols: func([]Column) error { return nil },
		row: func([]Cell) error {
			if n++; n == 10 {
				return stop
			}
			return nil
		},
	})
	if !errors.Is(err, stop) || len(p.queries) != 1 {
		t.Fatalf("err=%v queries=%d", err, len(p.queries))
	}
}

type sinkFunc struct {
	cols func([]Column) error
	row  func([]Cell) error
}

func (s sinkFunc) Columns(c []Column) error { return s.cols(c) }
func (s sinkFunc) Row(r []Cell) error       { return s.row(r) }

func itoa(n int) string { return strconv.Itoa(n) }

func fmtSscanf(s string, limit, offset *int) (int, error) {
	return fmt.Sscanf(strings.TrimSpace(s), "LIMIT %d OFFSET %d", limit, offset)
}
