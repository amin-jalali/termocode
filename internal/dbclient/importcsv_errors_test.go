package dbclient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf16"
)

// failSess is an import session that fails the statement fail picks.
type failSess struct {
	importRec
	beginErr error
	fail     func(sql string, args []any) error
}

func (f *failSess) Begin(ctx context.Context) error {
	if f.beginErr != nil {
		return f.beginErr
	}
	return f.importRec.Begin(ctx)
}

func (f *failSess) Run(ctx context.Context, sql string, o RunOptions) Result {
	if f.fail != nil {
		if err := f.fail(sql, o.Args); err != nil {
			f.events = append(f.events, "FAIL "+sql)
			return Result{Err: err}
		}
	}
	return f.importRec.Run(ctx, sql, o)
}

func lastEvent(f *failSess) string { return f.events[len(f.events)-1] }

func TestSniffCSVFolderAndMissingFile(t *testing.T) {
	if _, err := SniffCSV(t.TempDir()); err == nil || err.Error() != "that path is a folder" {
		t.Errorf("folder = %v", err)
	}
	if _, err := SniffCSV(t.TempDir() + "/nope.csv"); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("missing = %v", err)
	}
	if _, err := CountCSVRows(context.Background(), t.TempDir()+"/nope.csv", CSVSniff{}); err == nil {
		t.Error("count of a missing file")
	}
	r := ImportCSV(context.Background(), &importRec{}, ImportSpec{Path: t.TempDir() + "/nope.csv", Targets: []string{"a"}, Sources: []int{0}}, nil)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "cannot open the file") {
		t.Errorf("import of a missing file = %v", r.Err)
	}
}

// A first row with an empty or repeated name is data, not a header.
func TestSniffCSVHeaderNeedsDistinctNames(t *testing.T) {
	for _, data := range []string{"a,,c\nx,y,z\n", "id,ID\nx,y\n"} {
		sn, err := SniffCSV(writeFile(t, "h.csv", []byte(data)))
		if err != nil || sn.Header {
			t.Errorf("%q: header=%v err=%v", data, sn.Header, err)
		}
	}
}

// Only the first 256 KB are sniffed; a cut inside a UTF-8 character or
// a row does not break the guess.
func TestSniffCSVBigFileCutInsideACharacter(t *testing.T) {
	var b strings.Builder
	b.WriteString("name;city\n")
	for b.Len() < sniffBytes+1000 {
		b.WriteString("Zoë;Zürich\n")
	}
	sn, err := SniffCSV(writeFile(t, "big.csv", []byte(b.String())))
	if err != nil {
		t.Fatal(err)
	}
	if sn.Encoding != "UTF-8" || sn.Delimiter != ';' || !sn.Header || sn.Sample[0][1] != "Zürich" {
		t.Fatalf("%+v", sn)
	}
}

// UTF-16 files with characters outside the BMP (emoji) import intact.
func TestImportCSVUTF16Emoji(t *testing.T) {
	for _, be := range []bool{false, true} {
		units := utf16.Encode([]rune("id,email\n1,🙂@x\n"))
		data := []byte{0xFF, 0xFE}
		if be {
			data = []byte{0xFE, 0xFF}
		}
		for _, u := range units {
			if be {
				data = append(data, byte(u>>8), byte(u))
			} else {
				data = append(data, byte(u), byte(u>>8))
			}
		}
		p := writeFile(t, "u16.csv", data)
		sn, err := SniffCSV(p)
		if err != nil {
			t.Fatal(err)
		}
		rec := &importRec{}
		res := ImportCSV(context.Background(), rec, ImportSpec{Path: p, Sniff: sn, Dialect: PostgresDialect{}, Table: "u", Targets: []string{"id", "email"}, Sources: []int{0, 1}}, nil)
		if res.Err != nil || res.Rows != 1 {
			t.Fatalf("be=%v: %+v", be, res)
		}
		var got []any
		for _, a := range rec.args {
			got = append(got, a...)
		}
		if len(got) != 2 || got[1] != "🙂@x" {
			t.Fatalf("be=%v: args %#v", be, got)
		}
	}
}

// Every step of the transaction can fail: the error is shown, nothing is
// committed, and what was begun is undone.
func TestImportCSVTransactionStepFailures(t *testing.T) {
	csv := "id,email\n1,a\n2,b\n"
	boom := errors.New("server closed the connection")
	t.Run("begin", func(t *testing.T) {
		f := &failSess{beginErr: boom}
		if res := ImportCSV(context.Background(), f, importSpec(t, csv, 10, false), nil); !errors.Is(res.Err, boom) || len(f.events) != 0 {
			t.Fatalf("%+v %q", res, f.events)
		}
	})
	t.Run("commit", func(t *testing.T) {
		f := &failSess{}
		f.commitErr = boom
		res := ImportCSV(context.Background(), f, importSpec(t, csv, 10, false), nil)
		if !errors.Is(res.Err, boom) || res.Rows != 0 || lastEvent(f) != "ROLLBACK" {
			t.Fatalf("%+v %q", res, f.events)
		}
	})
	for _, step := range []string{"SAVEPOINT termocode_import", "RELEASE SAVEPOINT termocode_import", "SAVEPOINT termocode_import_batch", "RELEASE SAVEPOINT termocode_import_batch"} {
		t.Run(step, func(t *testing.T) {
			f := &failSess{fail: func(sql string, _ []any) error {
				if sql == step {
					return boom
				}
				return nil
			}}
			res := ImportCSV(context.Background(), f, importSpec(t, csv, 10, true), nil)
			if res.Err == nil || !strings.Contains(res.Err.Error(), boom.Error()) || res.Rows != 0 {
				t.Fatalf("%+v %q", res, f.events)
			}
			for _, e := range f.events {
				if e == "COMMIT" {
					t.Fatal("committed")
				}
			}
		})
	}
}

// The batch statement fails (too big for the server) but each row alone
// goes in: the import still succeeds with every row.
func TestImportCSVBatchFailsButRowsAlonePass(t *testing.T) {
	f := &failSess{fail: func(sql string, args []any) error {
		if strings.HasPrefix(sql, "INSERT") && len(args) > 2 {
			return errors.New("statement too large")
		}
		return nil
	}}
	res := ImportCSV(context.Background(), f, importSpec(t, "id,email\n1,a\n2,b\n3,c\n", 10, false), nil)
	if res.Err != nil || res.Rows != 3 || lastEvent(f) != "COMMIT" {
		t.Fatalf("%+v %q", res, f.events)
	}
}

// When even going back to the batch savepoint fails, the batch's own
// error is shown (not the rollback's).
func TestImportCSVBatchRollbackFails(t *testing.T) {
	f := &failSess{fail: func(sql string, args []any) error {
		switch {
		case strings.HasPrefix(sql, "INSERT"):
			return errors.New("duplicate key value violates unique constraint")
		case strings.HasPrefix(sql, "ROLLBACK TO"):
			return errors.New("connection lost")
		}
		return nil
	}}
	res := ImportCSV(context.Background(), f, importSpec(t, "id,email\n1,a\n2,b\n", 10, false), nil)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "duplicate key") || res.FailedRow != 1 {
		t.Fatalf("%+v", res)
	}
}

// Cancel while a batch runs: the import stops as cancelled, not failed.
func TestImportCSVCancelDuringBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &failSess{fail: func(sql string, _ []any) error {
		if strings.HasPrefix(sql, "INSERT") {
			cancel()
			return context.Canceled
		}
		return nil
	}}
	res := ImportCSV(ctx, f, importSpec(t, "id,email\n1,a\n2,b\n", 10, false), nil)
	if !res.Cancelled || res.Err != nil || res.Rows != 0 {
		t.Fatalf("%+v", res)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if res := ImportCSV(ctx2, &importRec{}, importSpec(t, "id,email\n1,a\n", 10, false), nil); !res.Cancelled {
		t.Fatalf("cancelled before start: %+v", res)
	}
}
