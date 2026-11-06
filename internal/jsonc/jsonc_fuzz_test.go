package jsonc

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
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
		if err := unmarshalNumbers(src, &want); err != nil {
			t.Fatal(err)
		}
		mixed := append([]byte("// lead\n"), trimmed[:len(trimmed)-1]...)
		mixed = append(mixed, ",/* tail */"...)
		mixed = append(mixed, last)
		var back any
		if err := unmarshalNumbers(Strip(mixed), &back); err != nil {
			t.Fatalf("Strip(%q) is not JSON: %v (%q)", mixed, err, Strip(mixed))
		}
		if !reflect.DeepEqual(back, want) {
			t.Fatalf("value changed: %#v != %#v", back, want)
		}
	})
}

// unmarshalNumbers decodes with numbers kept as text: valid JSON may
// hold a number too big for a float64 (1e400).
func unmarshalNumbers(b []byte, v any) error {
	if !json.Valid(b) {
		return json.Unmarshal(b, v) // its syntax error
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

// FuzzBlank: Blank keeps the length and the newlines of any input, and
// gives the same JSON value as Strip whenever either parses.
func FuzzBlank(f *testing.F) {
	for _, s := range []string{`{}`, "{// c\n\"a\":[1,],}", "/* a\nb */[1,\n]", `{"s":"a\"b//c",}`, `/* open`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := Blank([]byte(in))
		if len(out) != len(in) || bytes.Count(out, []byte("\n")) != strings.Count(in, "\n") {
			t.Fatalf("offsets changed: %q → %q", in, out)
		}
		var a, b any
		errA := json.Unmarshal(Strip([]byte(in)), &a)
		errB := json.Unmarshal(out, &b)
		if (errA == nil) != (errB == nil) || (errA == nil && !reflect.DeepEqual(a, b)) {
			t.Fatalf("%q: Strip %v/%v, Blank %v/%v", in, a, errA, b, errB)
		}
	})
}
