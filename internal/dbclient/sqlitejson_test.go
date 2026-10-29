package dbclient

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

var sqliteJSONCases = []string{
	"",
	"   \n",
	"[]",
	`[{"id":1,"name":"a"},` + "\n" + `{"id":2,"name":"b"}]`,
	`[{"id":-0.5e+10,"x":null,"y":true,"z":false}]`,
	`[{"a":"tab\there","b":"quote \" and \\ slash","c":"\u00e9\u20ac","d":"\ud83d\ude00"}]`,
	`[{"a":"héllo wörld","b":"日本"}]`,
	"[{\"a\":\"bad \xff byte\"}]",
	`[{"id":1,"id":2,"3":3}]`,
	`[{}]`,
	`[{},{}]`,
	`[{"a":1}] trailing`,
	`[{"a":{"nested":1}}]`,
	`[{"a":[1,2]}]`,
	`[{"a":01}]`,
	`[{"a":1.}]`,
	`[{"a":-}]`,
	`[{"a":1e}]`,
	`[{"a":12x}]`,
	`[{"a":nul}]`,
	`[{"a":1},]`,
	`[{"a":1}`,
	`[{"a" 1}]`,
	`[{"a":1,}]`,
	"[{\"a\":\"line\nbreak\"}]",
	`{"a":1}`,
	`Error: no such table: x`,
	`[{"a":"u00000001 blob"}]`,
	`[ { "a" : 1 , "b" : "x" } , { "a" : 2 , "b" : "y" } ]`,
	`[{"a":1},{"b":2,"c":3}]`,
}

// The fast scan gives the decoder's rows and columns for every input, and
// an error exactly when the decoder does.
func TestParseSQLiteJSONMatchesDecoder(t *testing.T) {
	for _, in := range sqliteJSONCases {
		lines := strings.Split(in, "\n")
		checkSQLiteJSONSame(t, lines)
	}
}

func checkSQLiteJSONSame(t *testing.T, lines []string) {
	t.Helper()
	gotRows, gotCols, gotErr := parseSQLiteJSON(lines)
	wantRows, wantCols, wantErr := parseSQLiteJSONDecoder(lines)
	if (gotErr != nil) != (wantErr != nil) {
		t.Fatalf("%q: err = %v, decoder err = %v", lines, gotErr, wantErr)
	}
	if gotErr != nil {
		return
	}
	if !reflect.DeepEqual(gotRows, wantRows) || !reflect.DeepEqual(gotCols, wantCols) {
		t.Fatalf("%q:\nrows %#v cols %#v\nwant rows %#v cols %#v", lines, gotRows, gotCols, wantRows, wantCols)
	}
}

func FuzzParseSQLiteJSON(f *testing.F) {
	for _, in := range sqliteJSONCases {
		f.Add(in)
	}
	f.Fuzz(func(t *testing.T, in string) {
		checkSQLiteJSONSame(t, strings.Split(in, "\n"))
	})
}

func sqliteJSONRows(n int) []string {
	lines := make([]string, 0, n+1)
	for i := 1; i <= n; i++ {
		prefix, suffix := "", ","
		if i == 1 {
			prefix = "["
		}
		if i == n {
			suffix = "]"
		}
		lines = append(lines, fmt.Sprintf(`%s{"id":%d,"name":"name %d","amount":%d.5,"note":"note for row %d"}%s`, prefix, i, i, i, i, suffix))
	}
	return append(lines, "changes:   0   total_changes: 0")
}

// A page of rows costs a few allocations, not several per cell.
func TestParseSQLiteJSONAllocs(t *testing.T) {
	lines := sqliteJSONRows(1000)
	allocs := testing.AllocsPerRun(5, func() {
		rows, _, err := parseSQLiteJSON(lines)
		if err != nil || len(rows) != 1000 {
			t.Fatalf("rows %d, err %v", len(rows), err)
		}
	})
	if allocs > 100 {
		t.Fatalf("parsing 1000 rows took %.0f allocations, want at most 100", allocs)
	}
}

func BenchmarkParseSQLiteJSON(b *testing.B) {
	lines := sqliteJSONRows(10000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := parseSQLiteJSON(lines); err != nil {
			b.Fatal(err)
		}
	}
}
