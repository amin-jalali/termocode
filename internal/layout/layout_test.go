package layout

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseReadsCommentsAndTrailingCommas(t *testing.T) {
	src := `// my debug layout
{
  "description": "editor + terminal", // why
  "sidebar": {"open": true, "view": "git", "width": 30,},
  "panel": {"open": true, "rows": 12, "active": "terminal",
    "tabs": ["problems"], "terminals": [{"name": "server", "cwd": "cmd/api"}]},
  "chat": {"open": false},
  "editor": {"files": ["main.go"], "active": "main.go", "split": "vertical", "panes": ["main.go", "util.go"]},
  "focus": "terminal",
}`
	l, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := Layout{
		Description: "editor + terminal",
		Sidebar:     &Sidebar{Open: true, View: "git", Width: 30},
		Panel: &Panel{Open: true, Rows: 12, Active: "terminal", Tabs: []string{"problems"},
			Terminals: []Terminal{{Name: "server", Cwd: "cmd/api"}}},
		Chat:   &Chat{},
		Editor: &Editor{Files: []string{"main.go"}, Active: "main.go", Split: "vertical", Panes: []string{"main.go", "util.go"}},
		Focus:  "terminal",
	}
	if !reflect.DeepEqual(l, want) {
		t.Fatalf("got %+v\nwant %+v", l, want)
	}
}

func TestParseRejectsWhatCannotBeApplied(t *testing.T) {
	cases := map[string]string{
		"unknown view":      `{"sidebar":{"open":true,"view":"nope"}}`,
		"unknown tab":       `{"panel":{"open":true,"tabs":["nope"]}}`,
		"bad split":         `{"editor":{"split":"diagonal"}}`,
		"absolute file":     `{"editor":{"files":["/etc/passwd"]}}`,
		"file leaves root":  `{"editor":{"files":["../secret.txt"]}}`,
		"cwd leaves root":   `{"panel":{"open":true,"terminals":[{"cwd":"../../x"}]}}`,
		"unknown focus":     `{"focus":"moon"}`,
		"negative rows":     `{"panel":{"open":true,"rows":-3}}`,
		"not json":          `{"sidebar":`,
		"windows backslash": `{"editor":{"files":["..\\x"]}}`,
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestMarshalRoundTripsAndDropsUnknownKeys(t *testing.T) {
	src := `{"description":"d","panel":{"open":true,"rows":9,"env":{"TOKEN":"abc"}},"scrollback":"secret output"}`
	l, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"TOKEN", "abc", "secret output", "scrollback", "env"} {
		if strings.Contains(string(out), bad) {
			t.Errorf("saved file keeps %q:\n%s", bad, out)
		}
	}
	again, err := Parse(out)
	if err != nil || !reflect.DeepEqual(again, l) {
		t.Fatalf("round trip: %v\n%+v\n%+v", err, again, l)
	}
}
