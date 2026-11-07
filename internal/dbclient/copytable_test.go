package dbclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func copyRows(sink RowSink) error {
	if err := sink.Columns([]Column{{Name: "id"}, {Name: "note"}, {Name: "amount"}}); err != nil {
		return err
	}
	for _, r := range [][]Cell{
		{{Text: "1", Kind: "number"}, {Text: "=SUM(A1)"}, {Text: "-5", Kind: "number"}},
		{{Text: "2", Kind: "number"}, {Text: `a, "quoted" note`}, {Null: true}},
		{{Text: "3", Kind: "number"}, {Text: "-minus text"}, {Text: "7"}},
	} {
		if err := sink.Row(r); err != nil {
			return err
		}
	}
	return nil
}

func TestWriteCopyCSVKeepsTheExactTextAndIsPrivate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "copy.csv")
	n, err := WriteCopyCSV(context.Background(), p, func(_ context.Context, s RowSink) error { return copyRows(s) }, nil)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "id,note,amount\n1,=SUM(A1),-5\n2,\"a, \"\"quoted\"\" note\",\n3,-minus text,7\n"
	if string(b) != want {
		t.Fatalf("file:\n%q", b)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestWriteCopyCSVRefusesAnExistingFileAndRemovesAFailedOne(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.csv")
	if err := os.WriteFile(old, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCopyCSV(context.Background(), old, func(_ context.Context, s RowSink) error { return copyRows(s) }, nil); err == nil {
		t.Fatal("an existing file was replaced")
	}
	if b, _ := os.ReadFile(old); string(b) != "keep" {
		t.Fatalf("the old file changed: %q", b)
	}
	p := filepath.Join(dir, "fail.csv")
	_, err := WriteCopyCSV(context.Background(), p, func(_ context.Context, s RowSink) error {
		_ = copyRows(s)
		return errors.New("connection lost")
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "connection lost") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("a failed copy left its file")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := WriteCopyCSV(ctx, p, func(ctx context.Context, s RowSink) error { return copyRows(s) }, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("a cancelled copy left its file")
	}
}

func TestSniffCopyCSVAlwaysReadsCommaAndHeader(t *testing.T) {
	// Many semicolons and a numeric-looking header would fool SniffCSV.
	p := filepath.Join(t.TempDir(), "copy.csv")
	if err := os.WriteFile(p, []byte("1,2\na;b;c;d,x;y;z\ne;f;g;h,u;v;w\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sn, err := SniffCopyCSV(p)
	if err != nil {
		t.Fatal(err)
	}
	if sn.Delimiter != ',' || !sn.Header || sn.Encoding != "UTF-8" || sn.Fields != 2 {
		t.Fatalf("sniff %+v", sn)
	}
	if strings.Join(sn.Columns, "|") != "1|2" || len(sn.Sample) != 2 || sn.Sample[0][0] != "a;b;c;d" {
		t.Fatalf("columns %q sample %q", sn.Columns, sn.Sample)
	}
	n, err := CountCSVRows(context.Background(), p, sn)
	if err != nil || n != 2 {
		t.Fatalf("rows %d err %v", n, err)
	}
}
