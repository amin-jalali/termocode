package layout

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// FuzzParse feeds any bytes as a layout file. A layout that parses must
// be a single valid value, pass Validate, render a preview, and survive
// Marshal -> Parse unchanged.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		`// my debug layout
{
  "description": "editor + terminal", // why
  "sidebar": {"open": true, "view": "git", "width": 30,},
  "panel": {"open": true, "rows": 12, "active": "terminal",
    "tabs": ["problems"], "terminals": [{"name": "server", "cwd": "cmd/api"}]},
  "chat": {"open": false},
  "editor": {"files": ["main.go"], "active": "main.go", "split": "vertical", "panes": ["main.go", "util.go"]},
  "focus": "terminal",
}`,
		`{"sidebar":{"open":true,"view":"nope"}}`,
		`{"editor":{"files":["/etc/passwd"]}}`,
		`{"panel":{"open":true,"terminals":[{"cwd":"../../x"}]}}`,
		`{"editor":{"files":["..\\x"]}}`,
		`{"sidebar":`,
		`{"description":"d","panel":{"open":true,"rows":9,"env":{"TOKEN":"abc"}},"scrollback":"secret output"}`,
		`null`,
	} {
		f.Add([]byte(s))
	}
	for _, e := range Builtins() {
		if b, err := e.Layout.Marshal(); err == nil {
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		l, err := Parse(src)
		if err != nil {
			return
		}
		if !json.Valid(jsonc.Strip(src)) {
			t.Fatalf("Parse accepted text that is not one JSON value: %q", src)
		}
		if err := l.Validate(); err != nil {
			t.Fatalf("parsed layout does not validate: %v", err)
		}
		_ = l.Preview()
		out, err := l.Marshal()
		if err != nil {
			t.Fatalf("Marshal of a parsed layout: %v", err)
		}
		l2, err := Parse(out)
		if err != nil {
			t.Fatalf("Parse(Marshal(l)): %v\n%s", err, out)
		}
		out2, err := l2.Marshal()
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("round trip changed the layout:\n%s\n---\n%s (%v)", out, out2, err)
		}
	})
}
