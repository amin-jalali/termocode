package dbclient

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DBX-37: reading options (skip lines, quote, escape), per-column formats
// and many files in one import.

func TestSniffCSVWithSkipsLinesBeforeTheHeader(t *testing.T) {
	p := writeFile(t, "skip.csv", []byte("exported by tool\n2026-11-07\nid,name\n1,ada\n2,grace\n"))
	sn, err := SniffCSVWith(p, CSVOptions{SkipLines: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !sn.Header || strings.Join(sn.Columns, "|") != "id|name" || len(sn.Sample) != 2 || sn.Options.SkipLines != 2 {
		t.Fatalf("%+v", sn)
	}
	n, err := CountCSVRows(context.Background(), p, sn)
	if err != nil || n != 2 {
		t.Fatalf("rows %d err %v", n, err)
	}
	if _, err := SniffCSVWith(p, CSVOptions{SkipLines: 9}); err == nil || !strings.Contains(err.Error(), "skipping 9 lines") {
		t.Fatalf("skip past the end = %v", err)
	}
}

func TestSniffCSVWithSingleQuoteAndBackslashEscape(t *testing.T) {
	p := writeFile(t, "q.csv", []byte("id,note\n1,'a, b'\n2,'it\\'s \"x\"'\n"))
	sn, err := SniffCSVWith(p, CSVOptions{Quote: '\'', Escape: '\\'})
	if err != nil {
		t.Fatal(err)
	}
	if sn.Fields != 2 || len(sn.Sample) != 2 || sn.Sample[0][1] != "a, b" || sn.Sample[1][1] != `it's "x"` {
		t.Fatalf("sample %q", sn.Sample)
	}
}

func TestSniffCSVWithNoQuotingKeepsQuotesAsText(t *testing.T) {
	p := writeFile(t, "n.csv", []byte("id,size\n1,12\" pipe\n2,\"3\"\n"))
	sn, err := SniffCSVWith(p, CSVOptions{Quote: QuoteNone})
	if err != nil {
		t.Fatal(err)
	}
	if sn.Sample[0][1] != `12" pipe` || sn.Sample[1][1] != `"3"` {
		t.Fatalf("sample %q", sn.Sample)
	}
}

func TestCSVOptionsValid(t *testing.T) {
	bad := []CSVOptions{{SkipLines: -1}, {Quote: ','}, {Escape: ','}, {Quote: 'é'}, {Escape: '"'}, {Quote: ' '}}
	for _, o := range bad {
		if o.Valid(',') == nil {
			t.Errorf("%+v should be invalid", o)
		}
	}
	for _, o := range []CSVOptions{{}, {Quote: '\''}, {Quote: QuoteNone}, {Escape: '\\'}, {SkipLines: 3}} {
		if err := o.Valid(','); err != nil {
			t.Errorf("%+v: %v", o, err)
		}
	}
}

func TestImportCSVWithOptionsReportsLinesOfTheFile(t *testing.T) {
	p := writeFile(t, "o.csv", []byte("# comment\nid,email\n1,'a,b'\nBAD,'c'\n"))
	sn, err := SniffCSVWith(p, CSVOptions{SkipLines: 1, Quote: '\''})
	if err != nil {
		t.Fatal(err)
	}
	spec := ImportSpec{Path: p, Sniff: sn, Dialect: PostgresDialect{}, Table: "t", Targets: []string{"id", "email"}, Sources: []int{0, 1}}
	rec := &importRec{}
	if res := ImportCSV(context.Background(), rec, spec, nil); res.Err != nil || res.Rows != 2 {
		t.Fatalf("%+v", res)
	}
	if got := fmt.Sprintf("%v", rec.args[1]); got != "[1 a,b BAD c]" {
		t.Fatalf("args %s", got)
	}
	res := ImportCSV(context.Background(), &importRec{failValue: "BAD"}, spec, nil)
	if res.FailedRow != 2 || res.FailedLine != 4 {
		t.Fatalf("failed row %d line %d (%v)", res.FailedRow, res.FailedLine, res.Err)
	}
}

func TestColumnFormatApply(t *testing.T) {
	cases := []struct {
		f    ColumnFormat
		in   string
		want any
		err  bool
	}{
		{ColumnFormat{}, " a ", " a ", false},
		{ColumnFormat{}, "", nil, false},
		{ColumnFormat{Trim: true}, "  a ", "a", false},
		{ColumnFormat{Trim: true}, "   ", nil, false},
		{ColumnFormat{NullText: `\N`}, `\N`, nil, false},
		{ColumnFormat{NullText: "NULL"}, "null", "null", false},
		{ColumnFormat{DecimalComma: true}, "1.234,5", "1234.5", false},
		{ColumnFormat{DecimalComma: true}, "abc", nil, true},
		{ColumnFormat{DateLayout: "DD/MM/YYYY"}, "07/11/2026", "2026-11-07", false},
		{ColumnFormat{DateLayout: "DD.MM.YY HH:mm"}, "07.11.26 09:30", "2026-11-07 09:30:00", false},
		{ColumnFormat{DateLayout: "DD/MM/YYYY"}, "2026-11-07", nil, true},
	}
	for _, c := range cases {
		got, err := c.f.Apply(c.in)
		if (err != nil) != c.err || err == nil && got != c.want {
			t.Errorf("%+v %q = %#v, %v", c.f, c.in, got, err)
		}
	}
	for _, l := range []string{"", "MM/YYYY", "02/01/2006"} {
		if CheckDateLayout(l) == nil {
			t.Errorf("%q should be a bad date format", l)
		}
	}
	if d := (ColumnFormat{Trim: true, NullText: "NULL"}).Describe(); d != `trim · NULL = "NULL"` {
		t.Errorf("describe %q", d)
	}
}

func TestImportCSVColumnFormatErrorNamesTheColumn(t *testing.T) {
	spec := importSpec(t, "id,email\n1,a\nx,b\n", 10, false)
	spec.Formats = []ColumnFormat{{DecimalComma: true}, {Trim: true}}
	res := ImportCSV(context.Background(), &importRec{}, spec, nil)
	if res.Err == nil || res.FailedRow != 2 || !strings.Contains(res.Err.Error(), "column id") {
		t.Fatalf("%+v", res)
	}
	spec.SkipBad = true
	res = ImportCSV(context.Background(), &importRec{}, spec, nil)
	if res.Err != nil || res.Rows != 1 || res.SkippedRows != 1 {
		t.Fatalf("skip: %+v", res)
	}
}

func TestImportCSVManyFilesInOneTransaction(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.csv")
	b := filepath.Join(dir, "b.csv")
	mustWrite(t, a, "id,email\n1,a\n2,b\n")
	mustWrite(t, b, "id,email\n3,c\nBAD,d\n")
	sn, err := SniffCSV(a)
	if err != nil {
		t.Fatal(err)
	}
	spec := ImportSpec{Path: a, MorePaths: []string{b}, Sniff: sn, Dialect: PostgresDialect{}, Table: "t", Targets: []string{"id", "email"}, Sources: []int{0, 1}}
	rec := &importRec{}
	res := ImportCSV(context.Background(), rec, spec, nil)
	if res.Err != nil || res.Rows != 4 {
		t.Fatalf("%+v", res)
	}
	if n := strings.Count(strings.Join(rec.events, "\n"), "BEGIN"); n != 1 {
		t.Fatalf("%d transactions: %v", n, rec.events)
	}
	res = ImportCSV(context.Background(), &importRec{failValue: "BAD"}, spec, nil)
	if res.FailedFile != b || res.FailedRow != 2 || res.FailedLine != 3 || !strings.Contains(res.Err.Error(), b+": row 2 (line 3)") {
		t.Fatalf("%+v", res)
	}
	spec.SkipBad = true
	res = ImportCSV(context.Background(), &importRec{failValue: "BAD"}, spec, nil)
	if res.Rows != 3 || res.SkippedRows != 1 || res.Skipped[0].File != b {
		t.Fatalf("skip: %+v", res)
	}
	if log := ImportErrorLog("a.csv + 1", "t", res); !strings.Contains(log, b+": row 2 (line 3)") {
		t.Fatalf("log %q", log)
	}
	spec.MorePaths = []string{filepath.Join(dir, "missing.csv")}
	rec = &importRec{}
	if res := ImportCSV(context.Background(), rec, spec, nil); res.Err == nil || len(rec.events) != 0 {
		t.Fatalf("a missing file must stop before anything is written: %+v %v", res, rec.events)
	}
}

func mustWrite(t *testing.T, p, text string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
