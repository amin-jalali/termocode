package jsonc

import (
	"encoding/json"
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
