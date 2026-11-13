package dbclient

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// failRec is importRec with failures at chosen statements.
type failRec struct {
	*importRec
	beginErr  error
	failAt    string // a statement starting with this fails
	failCount int
}

func (f *failRec) Begin(ctx context.Context) error {
	if f.beginErr != nil {
		return f.beginErr
	}
	return f.importRec.Begin(ctx)
}

func (f *failRec) Run(ctx context.Context, sql string, o RunOptions) Result {
	if f.failAt != "" && strings.HasPrefix(sql, f.failAt) && f.failCount != 0 {
		f.failCount--
		f.importRec.events = append(f.importRec.events, sql)
		return Result{Err: errors.New("boom at " + f.failAt)}
	}
	return f.importRec.Run(ctx, sql, o)
}

func TestImportCSVRefusalsBeforeAnythingIsWritten(t *testing.T) {
	ctx := context.Background()
	spec := importSpec(t, "id,email\n1,a\n", 10, false)
	none := spec
	none.Sources = []int{-1, -1}
	if res := ImportCSV(ctx, &importRec{}, none, nil); res.Err == nil || !strings.Contains(res.Err.Error(), "map at least one column") {
		t.Errorf("no mapping = %v", res.Err)
	}
	badOpts := spec
	badOpts.Sniff.Options.SkipLines = -1
	if res := ImportCSV(ctx, &importRec{}, badOpts, nil); res.Err == nil {
		t.Error("bad options: want error")
	}
	badDate := spec
	badDate.Formats = []ColumnFormat{{DateLayout: "nonsense"}, {}}
	if res := ImportCSV(ctx, &importRec{}, badDate, nil); res.Err == nil {
		t.Error("a bad date layout: want error")
	}
	missing := spec
	missing.Path = missing.Path + ".gone"
	if res := ImportCSV(ctx, &importRec{}, missing, nil); res.Err == nil || !strings.Contains(res.Err.Error(), "cannot open the file") {
		t.Errorf("a missing file = %v", res.Err)
	}
	multi := spec
	multi.MorePaths = []string{spec.Path + ".gone"}
	rec := &importRec{}
	if res := ImportCSV(ctx, rec, multi, nil); res.Err == nil || !strings.Contains(res.Err.Error(), ".gone") || len(rec.events) != 0 {
		t.Errorf("a second file that is missing is named, nothing runs: %v %v", res.Err, rec.events)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if res := ImportCSV(cancelled, &importRec{}, spec, nil); !res.Cancelled {
		t.Errorf("a cancelled context = %+v", res)
	}
	if res := ImportCSV(ctx, &failRec{importRec: &importRec{}, beginErr: errors.New("no tx")}, spec, nil); res.Err == nil || !strings.Contains(res.Err.Error(), "no tx") {
		t.Errorf("begin fails = %v", res.Err)
	}
	inTx := importSpecCopy(spec, true)
	if res := ImportCSV(ctx, &failRec{importRec: &importRec{}, failAt: "SAVEPOINT termocode_import", failCount: 1}, inTx, nil); res.Err == nil {
		t.Error("the savepoint fails: want error")
	}
	if res := ImportCSV(ctx, &failRec{importRec: &importRec{}, failAt: "RELEASE SAVEPOINT termocode_import", failCount: 99}, inTx, nil); res.Err == nil {
		t.Error("the final release fails: want error")
	}
}

func importSpecCopy(s ImportSpec, inTx bool) ImportSpec {
	s.InTx = inTx
	return s
}

func TestImportCSVMultipleFilesAndFormats(t *testing.T) {
	spec := importSpec(t, "id,email\n1,a\n2,b\n", 10, false)
	second := writeFile(t, "two.csv", []byte("id,email\n3,c\nBAD,d\n"))
	spec.MorePaths = []string{second}
	rec := &importRec{failValue: "BAD"}
	res := ImportCSV(context.Background(), rec, spec, nil)
	if res.Err == nil || res.FailedFile != second || res.FailedRow != 2 {
		t.Fatalf("a bad row in the second file = %+v", res)
	}
	if !strings.Contains(res.Err.Error(), second) {
		t.Errorf("the file is named: %v", res.Err)
	}
	// A cell that fails its format names the row and the column.
	f := importSpec(t, "id,email\n1,not a date\n", 10, false)
	f.Formats = []ColumnFormat{{}, {DateLayout: "DD/MM/YYYY"}}
	res = ImportCSV(context.Background(), &importRec{}, f, nil)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "column email") || res.FailedRow != 1 {
		t.Errorf("a bad format = %+v", res)
	}
	f.SkipBad = true
	res = ImportCSV(context.Background(), &importRec{}, f, nil)
	if res.Err != nil || res.SkippedRows != 1 || res.Rows != 0 {
		t.Errorf("a bad format with SkipBad = %+v", res)
	}
	log := ImportErrorLog("in.csv", "t", res)
	if !strings.Contains(log, "0 rows imported, 1 skipped") || !strings.Contains(log, "row 1") {
		t.Errorf("error log = %q", log)
	}
	many := ImportResult{SkippedRows: importSkipKeep + 5, Skipped: make([]ImportSkip, importSkipKeep)}
	if log := ImportErrorLog("f", "t", many); !strings.Contains(log, "and 5 more rows") {
		t.Errorf("long log = %q", log[len(log)-80:])
	}
	for i := 0; i < importSkipKeep+3; i++ {
		many.skip(i, i, "x", errors.New("e"))
	}
	if len(many.Skipped) != importSkipKeep {
		t.Errorf("only the first %d are kept: %d", importSkipKeep, len(many.Skipped))
	}
}

func TestImportCSVSkipBadSavepointFailuresAndBatchFallback(t *testing.T) {
	body := "id,email\n1,a\nBAD,b\n3,c\n"
	// The batch statement fails, then each row is tried alone.
	spec := importSpec(t, body, 10, false)
	spec.SkipBad = true
	res := ImportCSV(context.Background(), &importRec{failValue: "BAD"}, spec, nil)
	if res.Err != nil || res.Rows != 2 || res.SkippedRows != 1 {
		t.Errorf("skip bad = %+v", res)
	}
	// Without SkipBad a failing batch is tried row by row to name the row.
	plain := importSpec(t, body, 10, false)
	res = ImportCSV(context.Background(), &importRec{failValue: "BAD"}, plain, nil)
	if res.Err == nil || res.FailedRow != 2 {
		t.Errorf("fallback = %+v", res)
	}
	// The transaction itself breaks: the savepoint of a batch cannot be made.
	for _, at := range []string{"SAVEPOINT termocode_import_batch", "ROLLBACK TO SAVEPOINT termocode_import_batch", "SAVEPOINT termocode_import_row", "RELEASE SAVEPOINT termocode_import_batch"} {
		for _, skip := range []bool{true, false} {
			s := importSpec(t, body, 10, false)
			s.SkipBad = skip
			rec := &failRec{importRec: &importRec{failValue: "BAD"}, failAt: at, failCount: 99}
			if res := ImportCSV(context.Background(), rec, s, nil); res.Err == nil && res.Rows == 3 {
				t.Errorf("%q failing (skip %v) must not report a clean import: %+v", at, skip, res)
			}
		}
	}
	// A commit that fails is rolled back and reported.
	c := importSpec(t, "id,email\n1,a\n", 10, false)
	rec := &importRec{commitErr: errors.New("commit failed")}
	if res := ImportCSV(context.Background(), rec, c, nil); res.Err == nil || rec.events[len(rec.events)-1] != "ROLLBACK" {
		t.Errorf("failed commit = %+v, events %v", res, rec.events)
	}
}

func TestCSVSourceAndRowCounts(t *testing.T) {
	p := writeFile(t, "c.csv", []byte("skip me\nid,name\n1,a\n2,b\n"))
	sn, err := SniffCSVWith(p, CSVOptions{SkipLines: 1})
	if err != nil {
		t.Fatal(err)
	}
	n, err := CountCSVRows(context.Background(), p, sn)
	if err != nil || n != 2 {
		t.Errorf("count = %d, %v", n, err)
	}
	if _, err := CountCSVRows(context.Background(), p+".gone", sn); err == nil {
		t.Error("a missing file: want error")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CountCSVRows(cancelled, p, sn); err == nil {
		t.Error("a cancelled count: want error")
	}
	src := &csvSource{paths: []string{p, p}, sn: sn}
	rows := 0
	for {
		_, row, line, err := src.next()
		if err != nil {
			break
		}
		rows++
		if row < 1 || line < 3 {
			t.Errorf("row %d line %d", row, line)
		}
	}
	if rows != 4 || src.file() != "" {
		t.Errorf("two files give %d rows, file %q", rows, src.file())
	}
	src2 := &csvSource{paths: []string{p}, sn: sn}
	_, _, _, _ = src2.next()
	src2.done()
	src2.done()
}
