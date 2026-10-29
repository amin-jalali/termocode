package dbclient

import (
	"bytes"
	"testing"
)

var jsonCellCases = []Cell{
	{Null: true}, {Text: ""}, {Text: "plain"}, {Text: `quote " slash \`}, {Text: "<b>&</b>"},
	{Text: "tab\t"}, {Text: "héllo"}, {Text: "bad \xff"}, {Text: " "},
	{Text: "12", Kind: "number"}, {Text: "-1.5e+10", Kind: "number"}, {Text: "", Kind: "number"},
	{Text: " 1", Kind: "number"}, {Text: "01", Kind: "number"}, {Text: `"x"`, Kind: "number"},
	{Text: "NaN", Kind: "number"}, {Text: "true", Kind: "bool"}, {Text: "no", Kind: "bool"},
	{Text: "x", Kind: "time"},
}

func checkJSONCell(t *testing.T, c Cell) {
	t.Helper()
	if got, want := appendJSONCell([]byte("prefix"), c), append([]byte("prefix"), jsonCell(c)...); !bytes.Equal(got, want) {
		t.Fatalf("appendJSONCell(%#v) = %s, want %s", c, got, want)
	}
}

func TestAppendJSONCellMatchesJSONCell(t *testing.T) {
	for _, c := range jsonCellCases {
		checkJSONCell(t, c)
	}
}

func FuzzAppendJSONCell(f *testing.F) {
	for _, c := range jsonCellCases {
		f.Add(c.Text, c.Kind, c.Null)
	}
	f.Fuzz(func(t *testing.T, text, kind string, null bool) {
		checkJSONCell(t, Cell{Text: text, Kind: kind, Null: null})
	})
}

// The JSON export writes a row without an allocation per cell.
func TestJSONWriterRowAllocs(t *testing.T) {
	w, err := NewRowWriter(FormatJSON, &bytes.Buffer{}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Columns([]Column{{Name: "id"}, {Name: "name"}, {Name: "amount"}, {Name: "note"}})
	row := []Cell{{Text: "1", Kind: "number"}, {Text: "name 1"}, {Text: "1.5", Kind: "number"}, {Null: true}}
	allocs := testing.AllocsPerRun(100, func() { _ = w.Row(row) })
	if allocs > 1 {
		t.Fatalf("a row took %.0f allocations, want at most 1", allocs)
	}
}
