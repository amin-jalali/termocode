package jsonc

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

// FuzzStrip checks Strip on any input: it never panics, never grows the
// input, leaves real JSON byte-for-byte alone, and still yields the same
// value after a comment and a trailing comma are added to real JSON.
func FuzzStrip(f *testing.F) {
	for _, s := range []string{
		``,
		`{}`,
		`[]`,
		`{"a":1}`,
		`[1,2,3]`,
		`{// comment
"url":"https://x/*y*/", "n": [1,],}`,
		`{"s":"a\"b//c"}`,
		`/* only a comment */`,
		`{"a": [1, 2,
  ],
}`,
		`"unterminated`,
		`/* unterminated`,
		`// line at end`,
		`{"x":"\\"}`,
		`[1,,]`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		got := Strip(src)
		if len(got) > len(src) {
			t.Fatalf("Strip grew the input: %d > %d bytes", len(got), len(src))
		}
		if !json.Valid(src) {
			return
		}
		if !bytes.Equal(got, src) {
			t.Fatalf("Strip changed valid JSON:\n in: %q\nout: %q", src, got)
		}
		trimmed := bytes.TrimSpace(src)
		last := trimmed[len(trimmed)-1]
		if last != '}' && last != ']' {
			return
		}
		var want any
		if err := json.Unmarshal(src, &want); err != nil {
			t.Fatal(err)
		}
		mixed := append([]byte("// lead\n"), trimmed[:len(trimmed)-1]...)
		mixed = append(mixed, ",/* tail */"...)
		mixed = append(mixed, last)
		var back any
		if err := json.Unmarshal(Strip(mixed), &back); err != nil {
			t.Fatalf("Strip(%q) is not JSON: %v (%q)", mixed, err, Strip(mixed))
		}
		if !reflect.DeepEqual(back, want) {
			t.Fatalf("value changed: %#v != %#v", back, want)
		}
	})
}
