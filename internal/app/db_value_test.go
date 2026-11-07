package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-10: = on the result page sums up the column under the cursor (or the
// selected rows): count, sum, avg, min and max.
func TestDBAggregateNumbers(t *testing.T) {
	cells := []dbclient.Cell{{Text: "1"}, {Text: "2.5"}, {Null: true}, {Text: "-0.5"}}
	got := dbAggregate(cells)
	want := "count 3 · sum 3 · avg 1 · min -0.5 · max 2.5 · 1 null"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDBAggregateText(t *testing.T) {
	cells := []dbclient.Cell{{Text: "pear"}, {Text: "apple"}, {Text: "10"}}
	if got, want := dbAggregate(cells), "count 3 · min 10 · max pear"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := dbAggregate(nil); got != "count 0" {
		t.Fatalf("empty: %q", got)
	}
	if got := dbAggregate([]dbclient.Cell{{Null: true}}); got != "count 0 · 1 null" {
		t.Fatalf("nulls only: %q", got)
	}
}

func TestDBAggregateAvgIsShort(t *testing.T) {
	cells := []dbclient.Cell{{Text: "1"}, {Text: "1"}, {Text: "2"}}
	if got := dbAggregate(cells); !strings.Contains(got, "avg 1.333333 ") {
		t.Fatalf("got %q", got)
	}
}

// DBX-10: the value page shows binary as a hex dump, an image as its kind
// and size, XML and JSON pretty, and other text as it is.
func TestDBCellViewBinaryHexDump(t *testing.T) {
	got := dbCellView(dbclient.CellOf([]byte("hi\x00there")))
	if !strings.HasPrefix(got, "Binary · 8 B\n\n") {
		t.Fatalf("header: %q", got)
	}
	if !strings.Contains(got, "00000000  68 69 00 74 68 65 72 65") || !strings.Contains(got, "|hi.there|") {
		t.Fatalf("dump: %q", got)
	}
}

func TestDBCellViewImage(t *testing.T) {
	// The start of a 2×3 PNG (signature and IHDR).
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x02\x00\x00\x00\x03\x08\x06\x00\x00\x00\x9d\x74\x66\x1a")
	got := dbCellView(dbclient.CellOf(png))
	if !strings.HasPrefix(got, "PNG image · 2 × 3 · 33 B\n") {
		t.Fatalf("got %q", got)
	}
}

func TestDBCellViewBigBinaryIsCut(t *testing.T) {
	got := dbCellView(dbclient.CellOf(make([]byte, dbValueHexMax+100)))
	if !strings.Contains(got, "… 100 more bytes") {
		t.Fatalf("no cut line: %q", got[len(got)-80:])
	}
	if strings.Count(got, "\n") > dbValueHexMax/16+10 {
		t.Fatal("dump not cut")
	}
}

func TestDBCellViewXMLAndJSON(t *testing.T) {
	got := dbCellView(dbclient.Cell{Text: `<a x="1"><b>t</b><c/></a>`})
	want := "<a x=\"1\">\n  <b>t</b>\n  <c></c>\n</a>"
	if got != want {
		t.Fatalf("xml: got %q want %q", got, want)
	}
	if got := dbCellView(dbclient.Cell{Text: `{"a":1}`}); got != "{\n  \"a\": 1\n}" {
		t.Fatalf("json: %q", got)
	}
	// Not well-formed XML stays as it is.
	if got := dbCellView(dbclient.Cell{Text: "<a><b></a>"}); got != "<a><b></a>" {
		t.Fatalf("bad xml: %q", got)
	}
	if got := dbCellView(dbclient.Cell{Text: "plain\ntext"}); got != "plain\ntext" {
		t.Fatalf("text: %q", got)
	}
	if got := dbCellView(dbclient.Cell{Null: true}); got != "<null>" {
		t.Fatalf("null: %q", got)
	}
}
