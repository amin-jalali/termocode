package dbclient

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func vizResult() Result {
	return Result{
		Columns: []Column{{Name: "name"}, {Name: "n"}, {Name: "note"}},
		Rows: [][]Cell{
			{{Text: "a"}, {Text: "0"}, {Text: "x"}},
			{{Text: "b"}, {Text: "5"}, {Text: "y"}},
			{{Text: "c"}, {Text: "10"}, {Text: "z"}},
			{{Text: "d"}, {Null: true}, {Text: "w"}},
		},
	}
}

func TestNumericColumnsSkipsTextAndAcceptsNull(t *testing.T) {
	got := NumericColumns(vizResult())
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("NumericColumns = %v, want [1]", got)
	}
	if got := NumericColumns(Result{Columns: []Column{{Name: "x"}}, Rows: [][]Cell{{{Null: true}}}}); len(got) != 0 {
		t.Fatalf("all-NULL column must not count as numeric: %v", got)
	}
}

func TestHeatLevelsSpreadMinToMax(t *testing.T) {
	got := HeatLevels(vizResult(), 1, 5)
	want := []int{0, 2, 4, -1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("HeatLevels = %v, want %v", got, want)
		}
	}
	flat := Result{Columns: []Column{{Name: "n"}}, Rows: [][]Cell{{{Text: "3"}}, {{Text: "3"}}}}
	if got := HeatLevels(flat, 0, 5); got[0] != 0 || got[1] != 0 {
		t.Fatalf("equal values must share level 0: %v", got)
	}
	if got := HeatLevels(vizResult(), 9, 5); len(got) != 4 || got[0] != -1 {
		t.Fatalf("out-of-range column must give -1 per row: %v", got)
	}
}

func TestBarChartScalesToWidth(t *testing.T) {
	lines := BarChart(vizResult(), 0, 1, 10)
	if len(lines) != 4 {
		t.Fatalf("want 4 lines, got %d: %q", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "a ") || strings.Contains(lines[0], "█") {
		t.Fatalf("zero row must have no bar: %q", lines[0])
	}
	if strings.Count(lines[2], "█") != 10 || !strings.HasSuffix(lines[2], " 10") {
		t.Fatalf("max row must fill the width: %q", lines[2])
	}
	if strings.Count(lines[1], "█") != 5 {
		t.Fatalf("half row must be half the width: %q", lines[1])
	}
	if !strings.HasSuffix(lines[3], "<null>") {
		t.Fatalf("NULL row must say <null>: %q", lines[3])
	}
}

func TestBarChartNegativeValuesUseMagnitude(t *testing.T) {
	r := Result{Columns: []Column{{Name: "k"}, {Name: "v"}}, Rows: [][]Cell{{{Text: "up"}, {Text: "4"}}, {{Text: "down"}, {Text: "-2"}}}}
	lines := BarChart(r, 0, 1, 8)
	if strings.Count(lines[1], "█") != 4 || !strings.Contains(lines[1], "-2") {
		t.Fatalf("negative bar: %q", lines[1])
	}
}

func TestBrailleLineShape(t *testing.T) {
	lines := BrailleLine([]float64{0, 1, 2, 3, 4, 5, 6, 7}, 4, 2)
	if len(lines) != 2 {
		t.Fatalf("want 2 rows, got %q", lines)
	}
	for _, l := range lines {
		if utf8.RuneCountInString(l) != 4 {
			t.Fatalf("each row must be 4 cells wide: %q", l)
		}
		for _, r := range l {
			if r < 0x2800 || r > 0x28FF {
				t.Fatalf("non-braille rune %U in %q", r, l)
			}
		}
	}
	// rising line: first value at the bottom-left, last at the top-right.
	if []rune(lines[1])[0] == 0x2800 || []rune(lines[0])[3] == 0x2800 {
		t.Fatalf("rising line must touch bottom-left and top-right: %q", lines)
	}
	if []rune(lines[0])[0] != 0x2800 {
		t.Fatalf("top-left must be empty for a rising line: %q", lines)
	}
	if got := BrailleLine(nil, 4, 2); len(got) != 0 {
		t.Fatalf("no values → no rows: %q", got)
	}
}

func TestGeoTextPointLineAndPolygon(t *testing.T) {
	cases := []struct{ hex, wkt, geo string }{
		// little-endian WKB POINT(1 2)
		{"0x0101000000000000000000f03f0000000000000040", "POINT(1 2)", `{"type":"Point","coordinates":[1,2]}`},
		// PostGIS EWKB with SRID 4326, POINT(1 2)
		{"0101000020e6100000000000000000f03f0000000000000040", "SRID=4326;POINT(1 2)", `{"type":"Point","coordinates":[1,2]}`},
		// LINESTRING(0 0,1 1)
		{"010200000002000000" + "00000000000000000000000000000000" + "000000000000f03f000000000000f03f", "LINESTRING(0 0,1 1)", `{"type":"LineString","coordinates":[[0,0],[1,1]]}`},
		// POLYGON((0 0,1 0,0 0)) one ring of 3 points
		{"01030000000100000003000000" + "00000000000000000000000000000000" + "000000000000f03f0000000000000000" + "00000000000000000000000000000000", "POLYGON((0 0,1 0,0 0))", `{"type":"Polygon","coordinates":[[[0,0],[1,0],[0,0]]]}`},
	}
	for _, c := range cases {
		wkt, ok := GeoWKT(c.hex)
		if !ok || wkt != c.wkt {
			t.Fatalf("GeoWKT(%s) = %q,%v want %q", c.hex, wkt, ok, c.wkt)
		}
		gj, ok := GeoJSON(c.hex)
		if !ok || gj != c.geo {
			t.Fatalf("GeoJSON(%s) = %q,%v want %q", c.hex, gj, ok, c.geo)
		}
	}
}

func TestGeoTextRejectsJunk(t *testing.T) {
	for _, s := range []string{"", "hello", "0x01", "0x0101000000", "01ff000000000000000000f03f0000000000000040", "0103000000ffffffff"} {
		if _, ok := GeoWKT(s); ok {
			t.Fatalf("GeoWKT(%q) must fail", s)
		}
		if _, ok := GeoJSON(s); ok {
			t.Fatalf("GeoJSON(%q) must fail", s)
		}
	}
}
