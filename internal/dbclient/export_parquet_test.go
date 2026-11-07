package dbclient

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// parquetFooter checks the magic at both ends and returns the footer bytes.
func parquetFooter(t *testing.T, b []byte) []byte {
	t.Helper()
	if len(b) < 12 || string(b[:4]) != "PAR1" || string(b[len(b)-4:]) != "PAR1" {
		t.Fatalf("no PAR1 magic at both ends: %q", b)
	}
	n := int(binary.LittleEndian.Uint32(b[len(b)-8 : len(b)-4]))
	if n <= 0 || n > len(b)-12 {
		t.Fatalf("footer length %d in a %d byte file", n, len(b))
	}
	return b[len(b)-8-n : len(b)-8]
}

func plainValue(s string) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(len(s)))
	return append(b[:], s...)
}

func TestExportParquetWritesMagicFooterAndRawValues(t *testing.T) {
	out := []byte(exportString(t, FormatParquet, ExportOptions{}, exportResult()))
	footer := parquetFooter(t, out)
	for _, name := range []string{"id", "name", "note"} {
		if !bytes.Contains(footer, []byte(name)) {
			t.Fatalf("footer lacks column %q", name)
		}
	}
	// Parquet is not a spreadsheet: values stay as they are, no formula guard.
	for _, v := range []string{"O'Hara", "=1+1", "line1\nline2", "-3"} {
		if !bytes.Contains(out, plainValue(v)) {
			t.Fatalf("file lacks the PLAIN value %q", v)
		}
	}
	if bytes.Contains(out, plainValue("")) && bytes.Contains(out, plainValue("<null>")) {
		t.Fatal("a NULL was written as a value")
	}
}

func TestExportParquetWithNoRowsIsStillAFile(t *testing.T) {
	r := Result{Columns: []Column{{Name: "a"}}}
	parquetFooter(t, []byte(exportString(t, FormatParquet, ExportOptions{}, r)))
}

func TestExportParquetGivesEveryColumnAUniqueName(t *testing.T) {
	r := Result{Columns: []Column{{Name: "a"}, {Name: "a"}, {Name: ""}}, Rows: [][]Cell{{{Text: "1"}, {Text: "2"}, {Text: "3"}}}}
	footer := string(parquetFooter(t, []byte(exportString(t, FormatParquet, ExportOptions{}, r))))
	if !strings.Contains(footer, "a_2") || !strings.Contains(footer, "column_3") {
		t.Fatalf("names not made unique: %q", footer)
	}
}

func TestExportParquetSplitsLargeResultsIntoRowGroups(t *testing.T) {
	w, err := NewRowWriter(FormatParquet, &bytes.Buffer{}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pw := w.(*parquetWriter)
	if err := pw.Columns([]Column{{Name: "n"}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < parquetRowGroupRows+1; i++ {
		if err := pw.Row([]Cell{{Text: "x"}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(pw.groups) != 1 || pw.rows != 1 {
		t.Fatalf("groups %d, buffered rows %d", len(pw.groups), pw.rows)
	}
	if err := pw.End(); err != nil {
		t.Fatal(err)
	}
	if len(pw.groups) != 2 {
		t.Fatalf("%d row groups", len(pw.groups))
	}
}
