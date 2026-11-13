package dbclient

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// wkb builds little-endian WKB pieces.
type wkb []byte

func (w wkb) u32(v uint32) wkb { return binary.LittleEndian.AppendUint32(w, v) }
func (w wkb) f64(vs ...float64) wkb {
	for _, v := range vs {
		w = binary.LittleEndian.AppendUint64(w, math.Float64bits(v))
	}
	return w
}
func (w wkb) head(typ uint32) wkb { return append(w, 1).u32(typ) }

// compactWKT writes the expected text the way the package does: no space
// before a bracket or after a comma.
var compactWKT = strings.NewReplacer(" (", "(", ", ", ",")

func TestWKBToWKTEveryShape(t *testing.T) {
	cases := map[string]struct {
		in   wkb
		want string
	}{
		"point":      {wkb{}.head(1).f64(1, 2), "POINT (1 2)"},
		"point z":    {wkb{}.head(1001).f64(1, 2, 3), "POINT Z (1 2 3)"},
		"point m":    {wkb{}.head(2001).f64(1, 2, 3), "POINT M (1 2 3)"},
		"point zm":   {wkb{}.head(3001).f64(1, 2, 3, 4), "POINT ZM (1 2 3 4)"},
		"ewkb z":     {wkb{}.head(0x80000001).f64(1, 2, 3), "POINT Z (1 2 3)"},
		"ewkb m":     {wkb{}.head(0x40000001).f64(1, 2, 3), "POINT M (1 2 3)"},
		"ewkb zm":    {wkb{}.head(0xC0000001).f64(1, 2, 3, 4), "POINT ZM (1 2 3 4)"},
		"ewkb srid":  {wkb{}.head(0x20000001).u32(4326).f64(1, 2), "SRID=4326;POINT (1 2)"},
		"line":       {wkb{}.head(2).u32(2).f64(0, 0, 1, 1), "LINESTRING (0 0, 1 1)"},
		"empty line": {wkb{}.head(2).u32(0), "LINESTRING EMPTY"},
		"polygon":    {wkb{}.head(3).u32(1).u32(4).f64(0, 0, 1, 0, 1, 1, 0, 0), "POLYGON ((0 0, 1 0, 1 1, 0 0))"},
		"empty poly": {wkb{}.head(3).u32(0), "POLYGON EMPTY"},
		"multipoint": {append(wkb{}.head(4).u32(2), wkb{}.head(1).f64(1, 2)...), ""},
		"collection": {append(wkb{}.head(7).u32(1), wkb{}.head(1).f64(5, 6)...), "GEOMETRYCOLLECTION (POINT (5 6))"},
		"empty coll": {wkb{}.head(7).u32(0), "GEOMETRYCOLLECTION EMPTY"},
	}
	for name, c := range cases {
		got, err := WKBToWKT(c.in)
		if name == "multipoint" {
			// The second point is missing: counts past the data are errors.
			if err == nil {
				t.Errorf("%s: a count past the data must fail, got %q", name, got)
			}
			continue
		}
		want := compactWKT.Replace(c.want)
		if err != nil || got != want {
			t.Errorf("%s = %q, %v, want %q", name, got, err, want)
		}
	}
	bad := map[string]wkb{
		"nothing":        {},
		"bad byte order": {2, 1, 0, 0, 0},
		"truncated":      wkb{}.head(1).f64(1),
		"bad type":       wkb{}.head(99).f64(1, 2),
		"type zero":      wkb{}.head(0),
		"bad dims":       wkb{}.head(4001).f64(1, 2, 3, 4),
		"huge count":     wkb{}.head(2).u32(1 << 30),
		"leftover":       append(wkb{}.head(1).f64(1, 2), 0),
	}
	for name, in := range bad {
		if got, err := WKBToWKT(in); err == nil {
			t.Errorf("%s: want error, got %q", name, got)
		}
	}
	// Collections nested past the limit are refused.
	deep := wkb{}.head(1).f64(1, 2)
	for i := 0; i < 40; i++ {
		deep = append(wkb{}.head(7).u32(1), deep...)
	}
	if _, err := WKBToWKT(deep); err == nil {
		t.Error("nesting past the limit: want error")
	}
}

func TestGeometryWKTReadsHexForms(t *testing.T) {
	pt := wkb{}.head(1).f64(3, 4)
	if got, ok := geometryWKT("0x" + hex.EncodeToString(pt)); !ok || got != "POINT(3 4)" {
		t.Errorf("0x hex = %q %v", got, ok)
	}
	// MySQL: 4 bytes of SRID in front.
	my := append(wkb{}.u32(4326), pt...)
	if got, ok := geometryWKT(hex.EncodeToString(my)); !ok || got != "SRID=4326;POINT(3 4)" {
		t.Errorf("mysql form = %q %v", got, ok)
	}
	my0 := append(wkb{}.u32(0), pt...)
	if got, ok := geometryWKT(hex.EncodeToString(my0)); !ok || got != "POINT(3 4)" {
		t.Errorf("mysql srid 0 = %q %v", got, ok)
	}
	for _, in := range []string{"", "0x12", "zz" + strings.Repeat("0", 12), "0x" + strings.Repeat("ab", 8)} {
		if _, ok := geometryWKT(in); ok {
			t.Errorf("%q is not a geometry", in)
		}
	}
}

func TestGeoWKTAndGeoJSONOfEveryKind(t *testing.T) {
	poly := wkb{}.head(3).u32(1).u32(4).f64(0, 0, 1, 0, 1, 1, 0, 0)
	multi := append(wkb{}.head(6).u32(1), poly...)
	multiPt := append(append(wkb{}.head(4).u32(2), wkb{}.head(1).f64(1, 2)...), wkb{}.head(1).f64(3, 4)...)
	multiLn := append(wkb{}.head(5).u32(1), wkb{}.head(2).u32(2).f64(0, 0, 1, 1)...)
	coll := append(wkb{}.head(7).u32(2), append(wkb{}.head(1).f64(5, 6), wkb{}.head(2).u32(2).f64(0, 0, 1, 1)...)...)
	z := wkb{}.head(0xA0000001).u32(4326).f64(1, 2, 3)
	for name, in := range map[string]wkb{
		"point": wkb{}.head(1).f64(1, 2), "line": wkb{}.head(2).u32(2).f64(0, 0, 1, 1), "polygon": poly,
		"multipolygon": multi, "multipoint": multiPt, "multiline": multiLn, "collection": coll, "z with srid": z,
	} {
		h := hex.EncodeToString(in)
		wkt, ok := GeoWKT(h)
		if !ok || wkt == "" {
			t.Errorf("%s: GeoWKT = %q %v", name, wkt, ok)
		}
		js, ok := GeoJSON(h)
		if !ok || !strings.Contains(js, `"type"`) {
			t.Errorf("%s: GeoJSON = %q %v", name, js, ok)
		}
	}
	if w, _ := GeoWKT(hex.EncodeToString(z)); !strings.HasPrefix(w, "SRID=4326;POINT Z") {
		t.Errorf("z with srid = %q", w)
	}
	for _, in := range []string{"", "zz", "0x", hex.EncodeToString(wkb{}.head(1).f64(1)), hex.EncodeToString(wkb{3, 1, 0, 0, 0})} {
		if _, ok := GeoWKT(in); ok {
			t.Errorf("GeoWKT(%q) should fail", in)
		}
		if _, ok := GeoJSON(in); ok {
			t.Errorf("GeoJSON(%q) should fail", in)
		}
	}
}

func TestFormatTimePatterns(t *testing.T) {
	ts := time.Date(2024, 3, 9, 15, 4, 5, 123456789, time.FixedZone("CET", 3600))
	for pat, want := range map[string]string{
		"yyyy-MM-dd":    "2024-03-09",
		"yy M d":        "24 3 9",
		"MMMM MMM":      "March Mar",
		"EEEE EEE":      "Saturday Sat",
		"hh:mm a":       "03:04 PM",
		"h a":           "3 PM",
		"HH:mm:ss.SSS":  "15:04:05.123",
		"S":             "1",
		"SSSSSSSSSSS":   "12345678900",
		"X XX XXX":      "+01 +0100 +01:00",
		"Z z":           "+0100 CET",
		"'at' HH":       "at 15",
		"''":            "'",
		"'unterminated": "unterminated",
		"q":             "q",
		"qqq":           "qqq",
	} {
		if got := formatTime(pat, ts); got != want {
			t.Errorf("formatTime(%q) = %q, want %q", pat, got, want)
		}
	}
	midnight := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := formatTime("h a", midnight); got != "12 AM" {
		t.Errorf("midnight = %q", got)
	}
	if got := formatTime("X", midnight); got != "Z" {
		t.Errorf("UTC X = %q", got)
	}
}

func TestFormatBoolAndNumberStyles(t *testing.T) {
	for _, c := range []struct{ style, in, want string }{
		{"yes/no", "t", "yes"}, {"yes/no", "off", "no"}, {"1/0", "true", "1"}, {"1/0", "false", "0"},
		{"check", "y", "✓"}, {"check", "n", "✗"}, {"true/false", "1", "true"}, {"true/false", "0", "false"},
	} {
		if got, ok := formatBool(c.style, c.in); !ok || got != c.want {
			t.Errorf("formatBool(%s, %s) = %q %v", c.style, c.in, got, ok)
		}
	}
	if _, ok := formatBool("yes/no", "maybe"); ok {
		t.Error("a word that is no boolean")
	}
	if _, ok := formatBool("weird", "true"); ok {
		t.Error("an unknown style")
	}
	for _, c := range []struct{ pat, in, want string }{
		{"#,##0.00", "1234567.891", "1,234,567.89"}, {"#,##0", "999", "999"}, {"0.0#", "2.5", "2.5"},
		{"#,##0.00", "-0.001", "0.00"}, {"#,##0.00", "-12345.5", "-12,345.50"}, {"0", "1e3", "1000"},
	} {
		if got, ok := formatNumber(c.pat, c.in); !ok || got != c.want {
			t.Errorf("formatNumber(%s, %s) = %q %v, want %q", c.pat, c.in, got, ok, c.want)
		}
	}
	if _, ok := formatNumber("0", "abc"); ok {
		t.Error("text is no number")
	}
}

func TestCutLOBKeepsRunesWhole(t *testing.T) {
	c := Cell{Text: "héllo wörld"}
	if got := cutLOB(c, 100); got != c.Text {
		t.Errorf("short text = %q", got)
	}
	got := cutLOB(c, 2)
	if !strings.HasPrefix(got, "h") || !strings.Contains(got, "bytes)") || !utf8Valid(got) {
		t.Errorf("cut = %q", got)
	}
	bin := Cell{Text: "0x" + strings.Repeat("ab", 10), Kind: "binary"}
	if got := cutLOB(bin, 100); got != bin.Text {
		t.Errorf("short binary = %q", got)
	}
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "?") == s }

func TestGenValueByColumnType(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	ok := func(name, typ string, d Dialect, check func(string) bool) {
		t.Helper()
		for row := 0; row < 5; row++ {
			v := genValue(d, GenColumn{Name: name, Type: typ}, row, r, GenOptions{})
			if !check(v) {
				t.Errorf("%s %s (%T) row %d = %q", name, typ, d, row, v)
			}
		}
	}
	isNum := func(v string) bool { return v != "" && strings.Trim(v, "0123456789.-") == "" }
	quoted := func(v string) bool { return strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") }
	ok("a", "tinyint", MySQLDialect{}, isNum)
	ok("a", "integer", PostgresDialect{}, isNum)
	ok("a", "decimal(10,2)", PostgresDialect{}, func(v string) bool { return strings.Contains(v, ".") })
	ok("a", "money", PostgresDialect{}, func(v string) bool { return strings.Contains(v, ".") })
	ok("a", "uuid", PostgresDialect{}, func(v string) bool { return quoted(v) && strings.Count(v, "-") == 4 })
	ok("a", "jsonb", PostgresDialect{}, func(v string) bool { return quoted(v) && strings.Contains(v, `"tag"`) })
	ok("a", "timestamp", PostgresDialect{}, func(v string) bool { return quoted(v) && strings.Contains(v, ":") })
	ok("a", "date", PostgresDialect{}, func(v string) bool { return quoted(v) && !strings.Contains(v, ":") })
	ok("a", "time", PostgresDialect{}, func(v string) bool { return quoted(v) && strings.Contains(v, ":") })
	ok("a", "year", MySQLDialect{}, isNum)
	ok("a", "bytea", PostgresDialect{}, func(v string) bool { return strings.HasPrefix(v, `'\x`) })
	ok("a", "blob", MySQLDialect{}, func(v string) bool { return strings.HasPrefix(v, "X'") })
	ok("a", "boolean", PostgresDialect{}, func(v string) bool { return v == "true" || v == "false" })
	ok("a", "tinyint(1)", MySQLDialect{}, func(v string) bool { return v == "1" || v == "0" })
	ok("a", "enum('x','y')", MySQLDialect{}, func(v string) bool { return v == "'x'" || v == "'y'" })
	// A primary key counts from one.
	if v := genValue(PostgresDialect{}, GenColumn{Name: "id", Type: "integer", PK: true}, 6, r, GenOptions{}); v != "7" {
		t.Errorf("pk row 6 = %q", v)
	}
	// Text guesses from the column name.
	for name, want := range map[string]func(string) bool{
		"first_name": func(v string) bool { return !strings.Contains(v, " ") },
		"surname":    func(v string) bool { return !strings.Contains(v, " ") },
		"login":      func(v string) bool { return v != "" },
		"user_name":  func(v string) bool { return v != "" },
		"full_name":  func(v string) bool { return strings.Contains(v, " ") },
		"phone":      func(v string) bool { return strings.HasPrefix(v, "+1-555-") },
		"website":    func(v string) bool { return strings.HasPrefix(v, "https://example.test/") },
		"city":       func(v string) bool { return v != "" },
		"country":    func(v string) bool { return len(v) == 2 },
		"status":     func(v string) bool { return v == "active" || v == "pending" || v == "closed" },
		"notes":      func(v string) bool { return strings.Count(v, " ") == 2 },
		"email":      func(v string) bool { return strings.Contains(v, "@example.test") },
	} {
		if v := genText(name, 3, r); !want(v) {
			t.Errorf("genText(%s) = %q", name, v)
		}
	}
	// A foreign key without values: NULL.
	opt := GenOptions{FKColumns: map[string]bool{"x": true}}
	if v := genValue(PostgresDialect{}, GenColumn{Name: "x", Type: "integer"}, 0, r, opt); v != "NULL" {
		t.Errorf("fk without values = %q", v)
	}
	if got := genEnumValues("varchar(3)"); got != nil {
		t.Errorf("not an enum: %v", got)
	}
	out := GenerateTestData(PostgresDialect{}, "t", []GenColumn{
		{Name: "id", Type: "integer", PK: true}, {Name: "owner", Type: "integer", NotNull: true},
		{Name: "tag", Type: "integer"},
	}, GenOptions{Rows: 2, Seed: 1, FKColumns: map[string]bool{"owner": true, "tag": true}})
	if !strings.Contains(out, "the referenced table has no rows; add some there first") || !strings.Contains(out, "NULL is used") {
		t.Errorf("foreign keys without rows are noted:\n%s", out)
	}
}
