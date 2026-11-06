package jsonc

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestStrip(t *testing.T) {
	got := Strip([]byte(`{// comment
"url":"https://x/*y*/", "n": [1,],}`))
	var v map[string]any
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("Strip produced invalid JSON: %s: %v", got, err)
	}
	if v["url"] != "https://x/*y*/" {
		t.Fatalf("string changed: %#v", v["url"])
	}
}

func TestStripCases(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"block comment", `{/* a */"a":1}`, `{"a":1}`},
		{"empty block comment", `[1/**/,2]`, `[1,2]`},
		{"block comment over lines", "{\"a\":1 /* x\ny */}", `{"a":1 }`},
		{"unterminated block comment drops the rest", `[1] /* never closed`, `[1] `},
		{"line comment at end without newline", `[1] // end`, `[1] `},
		{"escaped quote keeps comment text in string", `{"a":"x\"//y"}`, `{"a":"x\"//y"}`},
		{"escaped backslash ends string", `{"a":"x\\"// c` + "\n}", "{\"a\":\"x\\\\\"\n}"},
		{"comma inside string kept", `{"a":",]",}`, `{"a":",]"}`},
		{"escaped quote then comma in string", `{"a":"\",}"}`, `{"a":"\",}"}`},
		{"trailing comma with CRLF", "[1,\r\n\t]", "[1\r\n\t]"},
		{"comma at very end", `[1,`, `[1,`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(Strip([]byte(c.in))); got != c.want {
				t.Fatalf("Strip(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// Blank removes the same comments and trailing commas as Strip, but puts
// spaces in their place (newlines stay), so an offset in the result is
// the same offset in the source: a parser error can name the real line.
func TestBlankKeepsOffsets(t *testing.T) {
	cases := []string{
		"{// comment\n\"url\":\"https://x/*y*/\", \"n\": [1,],}",
		"{/* a\nb\nc */\"a\":1,\n}",
		`[1] /* never closed`,
		"{\"a\":\"x\\\"//y\", // c\n\"b\": [\n 1,\n ],\n}",
		`{"a":",]",}`,
	}
	for _, in := range cases {
		out := Blank([]byte(in))
		if len(out) != len(in) {
			t.Errorf("%q: length %d, want %d", in, len(out), len(in))
			continue
		}
		for i := range in {
			if (in[i] == '\n') != (out[i] == '\n') {
				t.Errorf("%q: newline moved at %d", in, i)
			}
		}
		var a, b any
		errA := json.Unmarshal(Strip([]byte(in)), &a)
		errB := json.Unmarshal(out, &b)
		if (errA == nil) != (errB == nil) {
			t.Errorf("%q: Strip err %v, Blank err %v", in, errA, errB)
		}
		if errA == nil && !reflect.DeepEqual(a, b) {
			t.Errorf("%q: Blank %v, Strip %v", in, b, a)
		}
	}
}
