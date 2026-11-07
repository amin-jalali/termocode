package dbclient

import (
	"strings"
	"testing"
	"time"
)

// DBX-27: the zero Display shows every cell as it came.
func TestDisplayZeroKeepsCells(t *testing.T) {
	var d Display
	for _, c := range []Cell{
		{Text: "1234.5", Kind: "number"},
		{Text: "true", Kind: "bool"},
		{Text: "2026-11-07T10:20:30.5Z", Kind: "time"},
		{Text: "0x0101000000000000000000f03f0000000000000040", Kind: "binary"},
		{Null: true},
		{Text: "plain"},
	} {
		if got := d.Cell(Column{Type: "GEOMETRY"}, c); got != c {
			t.Fatalf("zero Display changed %+v to %+v", c, got)
		}
	}
}

func TestDisplayNumberPatterns(t *testing.T) {
	cases := []struct{ pattern, in, want string }{
		{"#,##0", "1234567", "1,234,567"},
		{"#,##0", "-1234567.6", "-1,234,568"},
		{"#,##0.00", "1234.5", "1,234.50"},
		{"0.00", "0.005", "0.01"},
		{"0.##", "2.5", "2.5"},
		{"0.##", "2", "2"},
		{"#,##0.0#", "12345678901234567890.125", "12,345,678,901,234,567,890.13"},
		{"#,##0", "999", "999"},
		{"#,##0", "NaN", "NaN"},
		{"#,##0", "abc", "abc"},
	}
	for _, c := range cases {
		d := Display{Number: c.pattern}
		if got := d.Cell(Column{Type: "NUMERIC"}, Cell{Text: c.in, Kind: "number"}).Text; got != c.want {
			t.Errorf("%q on %q = %q, want %q", c.pattern, c.in, got, c.want)
		}
	}
	// only number cells
	if got := (Display{Number: "#,##0"}).Cell(Column{}, Cell{Text: "1234"}).Text; got != "1234" {
		t.Fatalf("text cell was formatted: %q", got)
	}
}

func TestDisplayTimeAndZone(t *testing.T) {
	berlin, err := ParseDisplayZone("Europe/Berlin")
	if err != nil {
		t.Skip("no tz database:", err)
	}
	c := Cell{Text: "2026-11-07T10:20:30.123456Z", Kind: "time"}
	d := Display{Time: "yyyy-MM-dd HH:mm:ss.SSS XXX", Zone: berlin}
	if got := d.Cell(Column{Type: "TIMESTAMPTZ"}, c).Text; got != "2026-11-07 11:20:30.123 +01:00" {
		t.Fatalf("time = %q", got)
	}
	// zone alone keeps the RFC 3339 shape
	if got := (Display{Zone: time.UTC}).Cell(Column{}, Cell{Text: "2026-11-07T11:20:30+01:00", Kind: "time"}).Text; got != "2026-11-07T10:20:30Z" {
		t.Fatalf("zone only = %q", got)
	}
	// quoted text and letters that are not tokens stay as they are
	if got := (Display{Time: "dd.MM.yy 'at' h:mm a"}).Cell(Column{}, Cell{Text: "2026-01-02T15:04:05Z", Kind: "time"}).Text; got != "02.01.26 at 3:04 PM" {
		t.Fatalf("quoted = %q", got)
	}
	if got := (Display{Time: "EEE d MMM yyyy"}).Cell(Column{}, Cell{Text: "2026-01-02T15:04:05Z", Kind: "time"}).Text; got != "Fri 2 Jan 2026" {
		t.Fatalf("names = %q", got)
	}
	// a value that does not parse is kept
	bad := Cell{Text: "infinity", Kind: "time"}
	if got := d.Cell(Column{}, bad); got != bad {
		t.Fatalf("bad time changed: %+v", got)
	}
}

func TestDisplayDateColumns(t *testing.T) {
	d := Display{Date: "dd/MM/yyyy"}
	if got := d.Cell(Column{Type: "date"}, Cell{Text: "2026-11-07"}).Text; got != "07/11/2026" {
		t.Fatalf("date = %q", got)
	}
	if got := d.Cell(Column{Type: "TEXT"}, Cell{Text: "2026-11-07"}).Text; got != "2026-11-07" {
		t.Fatalf("text column formatted as date: %q", got)
	}
}

func TestParseDisplayZone(t *testing.T) {
	for _, name := range []string{"", "local", "Local", "UTC", "utc"} {
		if _, err := ParseDisplayZone(name); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
	}
	if z, _ := ParseDisplayZone(""); z != nil {
		t.Fatalf("empty zone = %v, want nil (keep)", z)
	}
	if _, err := ParseDisplayZone("Mars/Olympus"); err == nil {
		t.Fatal("unknown zone accepted")
	}
}

func TestDisplayBoolStyles(t *testing.T) {
	cases := []struct{ style, in, want string }{
		{"", "true", "true"},
		{"yes/no", "true", "yes"},
		{"yes/no", "f", "no"},
		{"1/0", "TRUE", "1"},
		{"1/0", "false", "0"},
		{"check", "1", "✓"},
		{"check", "0", "✗"},
		{"yes/no", "maybe", "maybe"},
	}
	for _, c := range cases {
		if got := (Display{Bool: c.style}).Cell(Column{}, Cell{Text: c.in, Kind: "bool"}).Text; got != c.want {
			t.Errorf("%q on %q = %q, want %q", c.style, c.in, got, c.want)
		}
	}
	if !ValidBoolStyle("check") || ValidBoolStyle("on/off") {
		t.Fatal("ValidBoolStyle")
	}
}

func TestDisplayLOBLimit(t *testing.T) {
	d := Display{LOBBytes: 4}
	if got := d.Cell(Column{}, Cell{Text: "0x0102030405060708", Kind: "binary"}).Text; got != "0x01020304… (8 bytes)" {
		t.Fatalf("binary = %q", got)
	}
	if got := d.Cell(Column{}, Cell{Text: "héllo world"}).Text; got != "hél… (12 bytes)" {
		t.Fatalf("text = %q", got)
	}
	short := Cell{Text: "abcd"}
	if got := d.Cell(Column{}, short); got != short {
		t.Fatalf("short text changed: %+v", got)
	}
	n := Cell{Text: "123456789", Kind: "number"}
	if got := d.Cell(Column{}, n); got != n {
		t.Fatalf("number was cut: %+v", got)
	}
}

func TestDisplayGeometryWKT(t *testing.T) {
	d := Display{Geometry: "wkt"}
	cases := []struct{ name, typ, in, want string }{
		// WKB little endian POINT(1 2)
		{"point", "GEOMETRY", "0x0101000000000000000000f03f0000000000000040", "POINT(1 2)"},
		// MySQL: 4-byte SRID 4326, then WKB
		{"mysql srid", "POINT", "0xe61000000101000000000000000000f03f0000000000000040", "SRID=4326;POINT(1 2)"},
		// PostGIS EWKB hex text with SRID 4326
		{"ewkb", "geometry", "0101000020E6100000000000000000F03F0000000000000040", "SRID=4326;POINT(1 2)"},
		// big endian LINESTRING(0 0, 1 1)
		{"line", "LINESTRING", "0x00000000020000000200000000000000000000000000000000" + "3ff00000000000003ff0000000000000", "LINESTRING(0 0,1 1)"},
		// POLYGON with one ring of 4 points
		{"polygon", "POLYGON", "0x0103000000010000000400000000000000000000000000000000000000000000000000004000000000000000000000000000000000000000000000004000000000000000000000000000000000", "POLYGON((0 0,2 0,0 2,0 0))"},
		// MULTIPOINT((1 2))
		{"multi", "MULTIPOINT", "0x0104000000010000000101000000000000000000f03f0000000000000040", "MULTIPOINT((1 2))"},
		// POINT Z (ISO type 1001)
		{"z", "GEOMETRY", "0x01e9030000000000000000f03f00000000000000400000000000000840", "POINT Z(1 2 3)"},
	}
	for _, c := range cases {
		kind := "binary"
		if !strings.HasPrefix(c.in, "0x") {
			kind = ""
		}
		if got := d.Cell(Column{Type: c.typ}, Cell{Text: c.in, Kind: kind}).Text; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	// broken or cut WKB stays hex; other columns are not touched
	for _, in := range []string{"0x0101000000000000", "0x09", "0xzz", "0x0107000000ffffffff"} {
		c := Cell{Text: in, Kind: "binary"}
		if got := d.Cell(Column{Type: "GEOMETRY"}, c); got != c {
			t.Errorf("broken %q became %q", in, got.Text)
		}
	}
	blob := Cell{Text: "0x0101000000000000000000f03f0000000000000040", Kind: "binary"}
	if got := d.Cell(Column{Type: "BLOB"}, blob); got != blob {
		t.Fatalf("BLOB decoded as geometry: %q", got.Text)
	}
}

func FuzzDisplayGeometry(f *testing.F) {
	f.Add([]byte{1, 1, 0, 0, 0})
	f.Add([]byte{0, 0, 0, 0, 7, 0, 0, 0, 1})
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = WKBToWKT(b)
	})
}
