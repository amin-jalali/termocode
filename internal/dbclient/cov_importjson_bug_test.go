package dbclient

import "testing"

// A JSON array that is cut off is an error: importing the rows before
// the cut as if the file were whole would lose data without a word.
func TestReadJSONRowsRefusesACutArray(t *testing.T) {
	for _, in := range []string{`[{"a":1}`, `[{"a":1},{"a":2}`, `[`} {
		if rows, err := readJSONRows([]byte(in)); err == nil {
			t.Errorf("%q: want an error, got %d rows", in, len(rows))
		}
	}
	if rows, err := readJSONRows([]byte(`[{"a":1}]`)); err != nil || len(rows) != 1 {
		t.Errorf("a whole array = %v, %v", rows, err)
	}
}
