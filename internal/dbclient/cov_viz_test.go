package dbclient

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"testing"
)

func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

func TestDecodeGeoByteOrdersFlagsAndTruncation(t *testing.T) {
	f64be := func(vs ...float64) []byte {
		var b []byte
		for _, v := range vs {
			b = binary.BigEndian.AppendUint64(b, math.Float64bits(v))
		}
		return b
	}
	cat := func(parts ...[]byte) string {
		var b []byte
		for _, p := range parts {
			b = append(b, p...)
		}
		return hex.EncodeToString(b)
	}
	be := cat([]byte{0}, be32(1), f64be(1, 2))
	if w, ok := GeoWKT(be); !ok || !strings.Contains(w, "POINT") {
		t.Errorf("big endian point = %q %v", w, ok)
	}
	for name, h := range map[string]string{
		"z flag":    cat([]byte{0}, be32(0x80000001), f64be(1, 2, 3)),
		"m flag":    cat([]byte{0}, be32(0x40000001), f64be(1, 2, 3)),
		"zm flag":   cat([]byte{0}, be32(0xC0000001), f64be(1, 2, 3, 4)),
		"iso z":     cat([]byte{0}, be32(1001), f64be(1, 2, 3)),
		"iso zm":    cat([]byte{0}, be32(3001), f64be(1, 2, 3, 4)),
		"srid":      cat([]byte{0}, be32(0x20000001), be32(4326), f64be(1, 2)),
		"0x":        "0x" + cat([]byte{0}, be32(1), f64be(1, 2)),
		"backslash": `\x` + cat([]byte{0}, be32(1), f64be(1, 2)),
	} {
		if _, ok := GeoWKT(h); !ok {
			t.Errorf("%s should decode", name)
		}
		if _, ok := GeoJSON(h); !ok {
			t.Errorf("%s should give GeoJSON", name)
		}
	}
	poly := cat([]byte{0}, be32(3), be32(1), be32(3), f64be(0, 0, 1, 0, 0, 0))
	if _, ok := GeoWKT(poly); !ok {
		t.Fatal("a polygon decodes")
	}
	raw, _ := hex.DecodeString(poly)
	for cut := 0; cut < len(raw); cut++ {
		if _, ok := decodeGeo(hex.EncodeToString(raw[:cut])); ok {
			t.Errorf("a polygon cut at %d bytes must not decode", cut)
		}
	}
	if _, ok := decodeGeo(poly + "00"); ok {
		t.Error("bytes after the geometry: not a geometry")
	}
	for name, h := range map[string]string{
		"bad order":  cat([]byte{2}, be32(1), f64be(1, 2)),
		"unknown":    cat([]byte{0}, be32(99), f64be(1, 2)),
		"huge count": cat([]byte{0}, be32(2), be32(1<<30)),
		"huge rings": cat([]byte{0}, be32(3), be32(1<<30)),
		"huge parts": cat([]byte{0}, be32(7), be32(1<<30)),
		"srid cut":   cat([]byte{0}, be32(0x20000001)),
		"short":      "00",
	} {
		if _, ok := decodeGeo(h); ok {
			t.Errorf("%s should not decode", name)
		}
	}
	deep := cat([]byte{0}, be32(1), f64be(1, 2))
	for i := 0; i < 12; i++ {
		deep = cat([]byte{0}, be32(7), be32(1)) + deep
	}
	if _, ok := decodeGeo(deep); ok {
		t.Error("nesting past the limit: want no geometry")
	}
}

func TestChartsAndHeatLevels(t *testing.T) {
	r := Result{
		Columns: []Column{{Name: "name"}, {Name: "n"}, {Name: "txt"}},
		Rows: [][]Cell{
			{{Text: "a"}, {Text: "10"}, {Text: "x"}},
			{{Text: "bb"}, {Text: "-5"}, {Text: "y"}},
			{{Text: "ccc"}, {Null: true}, {Text: "z"}},
			{{Text: "d\ne"}, {Text: "text"}},
			{{Text: "f"}},
		},
	}
	if got := NumericColumns(r); len(got) != 0 {
		t.Errorf("a column with text is not numeric: %v", got)
	}
	r.Rows[3][1] = Cell{Text: "2.5"}
	if got := NumericColumns(r); len(got) != 1 || got[0] != 1 {
		t.Errorf("numeric columns = %v", got)
	}
	if got := ColumnValues(r, 1); len(got) != 3 || got[0] != 10 {
		t.Errorf("column values = %v", got)
	}
	if got := ColumnValues(r, 7); len(got) != 0 {
		t.Errorf("a missing column = %v", got)
	}
	lv := HeatLevels(r, 1, 5)
	if lv[0] != 4 || lv[1] != 0 || lv[2] != -1 || lv[4] != -1 {
		t.Errorf("heat levels = %v", lv)
	}
	if lv := HeatLevels(r, 2, 5); lv[0] != -1 {
		t.Errorf("a text column has no levels: %v", lv)
	}
	if lv := HeatLevels(r, 1, 0); lv[0] != -1 {
		t.Errorf("no levels: %v", lv)
	}
	same := Result{Rows: [][]Cell{{{Text: "3"}}, {{Text: "3"}}}}
	if lv := HeatLevels(same, 0, 4); lv[0] != 0 || lv[1] != 0 {
		t.Errorf("equal values = %v", lv)
	}
	bars := BarChart(r, 0, 1, 8)
	if len(bars) != 5 || !strings.Contains(bars[0], "████████") || strings.Contains(bars[2], "█") || strings.Contains(bars[3], "\n") {
		t.Errorf("bars = %q", bars)
	}
	if got := BarChart(r, 0, 1, 0); len(got) != 5 {
		t.Errorf("zero width = %v", got)
	}
	if BrailleLine(nil, 5, 2) != nil || BrailleLine([]float64{1}, 0, 2) != nil || BrailleLine([]float64{1}, 5, 0) != nil {
		t.Error("nothing to plot")
	}
	for _, vals := range [][]float64{{1}, {5, 5, 5}, {0, 10, 0, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}} {
		got := BrailleLine(vals, 6, 2)
		if len(got) != 2 || len([]rune(got[0])) != 6 {
			t.Errorf("braille of %v = %q", vals, got)
		}
	}
}

type fakeIntro struct {
	byReq  map[string]IntrospectionResult
	errOn  string
	calls  int
	cut    int // cancel the context after this many calls
	cancel context.CancelFunc
}

func (f *fakeIntro) Introspect(_ context.Context, r IntrospectionRequest) (IntrospectionResult, error) {
	f.calls++
	if f.cut > 0 && f.calls >= f.cut && f.cancel != nil {
		f.cancel()
	}
	key := r.Database + "/" + r.Schema + "/" + r.Object
	if f.errOn != "" && strings.Contains(key, f.errOn) {
		return IntrospectionResult{}, errors.New("denied")
	}
	return f.byReq[key], nil
}

func TestLoadCompletionSchemaPerEngine(t *testing.T) {
	ctx := context.Background()
	intro := &fakeIntro{byReq: map[string]IntrospectionResult{
		"current//": {Schemas: []string{"public", "pg_catalog", "app", "information_schema", "pg_temp_3"}},
		"/public/":  {Objects: []SchemaObject{{Name: "t", Kind: "table"}, {Name: "v", Kind: "materialized view"}, {Name: "idx", Kind: "index"}, {Name: "plain"}}, Routines: []string{"f"}, Sequences: []string{"s"}},
		"/app/":     {Objects: []SchemaObject{{Name: "u", Kind: "BASE TABLE"}}},
		"/public/t": {Columns: []Column{{Name: "id", Type: "int"}}, ForeignKeys: []SchemaForeignKey{{Name: "fk", Column: "id", RefTable: "public.\"U\"", RefColumn: "id"}, {Name: "fk2", Column: "a", RefTable: "u", RefColumn: "b"}}},
		"/app/u":    {Columns: []Column{{Name: "id", Type: "int"}}},
	}}
	got, err := LoadCompletionSchema(ctx, intro, "postgres", "", 0)
	if err != nil || len(got.Tables) != 6 || len(got.Cols["t"]) != 1 || got.Partial {
		t.Fatalf("postgres = %+v, %v", got, err)
	}
	if len(got.FKs) != 2 || got.FKs[0].RefTable != "u" {
		t.Errorf("fks = %+v", got.FKs)
	}
	// An object that cannot be read is skipped; a cap marks the result partial.
	bad := &fakeIntro{byReq: intro.byReq, errOn: "/public/t"}
	if got, err := LoadCompletionSchema(ctx, bad, "postgresql", "", 0); err != nil || len(got.Tables) == 0 {
		t.Errorf("a column read that fails is skipped = %+v, %v", got, err)
	}
	if got, _ := LoadCompletionSchema(ctx, intro, "postgres", "", 1); !got.Partial {
		t.Error("a cap on tables marks the result partial")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancelling := &fakeIntro{byReq: intro.byReq, cut: 3, cancel: cancel}
	if got, _ := LoadCompletionSchema(cctx, cancelling, "postgres", "", 0); !got.Partial {
		t.Error("a cancelled load is partial")
	}
	// One scope: a failing list is an error. Several: the scope is skipped.
	if _, err := LoadCompletionSchema(ctx, &fakeIntro{errOn: "current"}, "postgres", "", 0); err == nil {
		t.Error("the schema list fails: want error")
	}
	if _, err := LoadCompletionSchema(ctx, &fakeIntro{errOn: "x/x/"}, "mysql", "x", 0); err == nil {
		t.Error("the only scope fails: want error")
	}
	if got, err := LoadCompletionSchema(ctx, intro, "mysql", "", 0); err != nil || len(got.Tables) != 0 {
		t.Errorf("mysql without a database = %+v, %v", got, err)
	}
	if got, err := LoadCompletionSchema(ctx, intro, "sqlite", "", 0); err != nil || got.Cols == nil {
		t.Errorf("default scope = %+v, %v", got, err)
	}
	many := &fakeIntro{byReq: intro.byReq, errOn: "/app/"}
	if got, err := LoadCompletionSchema(ctx, many, "postgres", "", 0); err != nil || len(got.Tables) == 0 {
		t.Errorf("one of several scopes fails = %+v, %v", got, err)
	}
	for _, s := range []string{"information_schema", "pg_catalog", "pg_toast", "pg_temp_1", "pg_toast_temp_1", "mysql", "performance_schema", "sys"} {
		if !systemSchema(s) {
			t.Errorf("%s is a system schema", s)
		}
	}
	if systemSchema("app") {
		t.Error("app is not a system schema")
	}
}
