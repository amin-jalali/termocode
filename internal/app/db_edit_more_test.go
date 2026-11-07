package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-55: a typed value is checked against the column type before it
// becomes a pending change.
func TestDBCheckCellType(t *testing.T) {
	ok := map[string][]string{
		"integer":       {"7", "-12", " 3 "},
		"BIGINT":        {"9007199254740993"},
		"int unsigned":  {"4"},
		"numeric(10,2)": {"1.5", "-3", "2e3"},
		"boolean":       {"true", "f", "1", "no"},
		"uuid":          {"3f2b8c1e-9a4d-4e2f-8b1a-0c9d8e7f6a5b", dbclient.NewCellUUID().Text},
		"jsonb":         {`{"a":1}`, "[1,2]", "3"},
		"date":          {"2026-11-07"},
		"text":          {"anything", ""},
		"integer[]":     {"{1,2}"},
	}
	for typ, vals := range ok {
		for _, v := range vals {
			if err := dbCheckCellType(typ, v); err != nil {
				t.Errorf("%s %q refused: %v", typ, v, err)
			}
		}
	}
	bad := map[string][]string{
		"integer":          {"seven", "1.5"},
		"numeric":          {"1,5"},
		"bool":             {"maybe"},
		"uuid":             {"3f2b8c1e"},
		"json":             {"{a:1}"},
		"date":             {"07/11/2026"},
		"smallint":         {""},
		"double precision": {"x"},
	}
	for typ, vals := range bad {
		for _, v := range vals {
			if err := dbCheckCellType(typ, v); err == nil {
				t.Errorf("%s %q accepted", typ, v)
			}
		}
	}
}

// The DEFAULT marker becomes the DEFAULT keyword, never a bound text.
func TestDBEditValueDefaultMarker(t *testing.T) {
	v := dbEditValue("plan", dbDefaultCell(), dbclient.Cell{Text: "pro"}, true)
	if !v.Default || v.Column != "plan" {
		t.Fatalf("value %+v", v)
	}
	v = dbEditValue("plan", dbclient.Cell{Text: "DEFAULT"}, dbclient.Cell{}, false)
	if v.Default || v.Value.Text != "DEFAULT" {
		t.Fatalf("the text DEFAULT is a value: %+v", v)
	}
}
