package source

import (
	"encoding/json"
	"strings"
	"testing"
)

func selectStrings(t *testing.T, doc, path string) []string {
	t.Helper()
	vals, err := Select([]byte(doc), path)
	if err != nil {
		t.Fatalf("Select(%q): %v", path, err)
	}
	out := []string{}
	for _, v := range vals {
		out = append(out, string(v))
	}
	return out
}

func TestSelectPaths(t *testing.T) {
	doc := `{"a":{"b":[1,2,3]},"issues":[{"id":"X-1","l":["p"]},{"id":"X-2","l":["q","r"]}],"one":{"k":1},"n":null}`
	cases := []struct {
		path string
		want string
	}{
		{"", doc},
		{"$", doc},
		{"$.a.b", "1|2|3"},
		{"$.a.b[*]", "1|2|3"},
		{"$.a.b[1]", "2"},
		{"$.a.b[9]", ""},
		{"$.issues[*].id", `"X-1"|"X-2"`},
		{"$.issues[*].l[*]", `"p"|"q"|"r"`},
		{"$.issues[*].l", `["p"]|["q","r"]`},
		{"$.one", `{"k":1}`},
		{"$.missing", ""},
		{"$.missing[*].x", ""},
		{"$.n", ""},
		{"$.a.b.c", ""},
	}
	for _, c := range cases {
		got := strings.Join(selectStrings(t, doc, c.path), "|")
		if got != c.want {
			t.Errorf("%q: got %s, want %s", c.path, got, c.want)
		}
	}
	// A top-level list gives its elements.
	if got := strings.Join(selectStrings(t, `[{"a":1},{"a":2}]`, "$"), "|"); got != `{"a":1}|{"a":2}` {
		t.Errorf("$ on a list: %s", got)
	}
}

func TestSelectBadPaths(t *testing.T) {
	for _, p := range []string{"a.b", "$..a", "$.a[", "$.a[-1]", "$.a[x]", "$a", "$." + strings.Repeat("x", 300)} {
		if _, err := Select([]byte(`{}`), p); err == nil {
			t.Errorf("%q: want an error", p)
		}
	}
	if _, err := Select([]byte(`{not json`), "$"); err == nil {
		t.Error("bad JSON: want an error")
	}
}

func FuzzSelect(f *testing.F) {
	f.Add(`{"a":[{"b":1}]}`, "$.a[*].b")
	f.Add(`[1,[2,[3]]]`, "$[1][1][0]")
	f.Fuzz(func(t *testing.T, doc, path string) {
		vals, err := Select([]byte(doc), path)
		if err != nil {
			return
		}
		for _, v := range vals {
			if !json.Valid(v) {
				t.Fatalf("invalid value %q", v)
			}
		}
	})
}
