package difforder

import (
	"strings"
	"testing"
)

func names(files []File, p []Placed) string {
	var out []string
	for _, x := range p {
		out = append(out, files[x.Index].Name)
	}
	return strings.Join(out, ",")
}

// Spec (CMP-58): docs come first, then contracts, then code, tests last;
// inside a group a file that defines a new name comes before the files
// that use it, and the rest keep the proposed order.
func TestOrderExplainFirst(t *testing.T) {
	files := []File{
		{Name: "internal/app/handler_test.go", New: "func TestHandler(t *testing.T) { ParseQuery(\"x\") }\n"},
		{Name: "internal/app/handler.go", Old: "package app\n", New: "package app\n\nfunc Handle() { ParseQuery(q) }\n"},
		{Name: "internal/query/parse.go", New: "package query\n\nfunc ParseQuery(s string) Query { return Query{} }\n"},
		{Name: "docs/guides/query.md", New: "# Query\n"},
		{Name: "internal/query/types.go", New: "package query\n\ntype Query struct{}\n"},
		{Name: "internal/app/other.go", Old: "a\n", New: "b\n"},
	}
	p := Order(files)
	want := "docs/guides/query.md,internal/query/types.go,internal/query/parse.go,internal/app/handler.go,internal/app/other.go,internal/app/handler_test.go"
	if got := names(files, p); got != want {
		t.Fatalf("order:\n got %s\nwant %s", got, want)
	}
	why := map[string]string{}
	for _, x := range p {
		why[files[x.Index].Name] = x.Reason
	}
	if r := why["internal/query/parse.go"]; r != "defines ParseQuery, used by handler.go" {
		t.Errorf("parse.go reason: %q", r)
	}
	if !strings.Contains(why["docs/guides/query.md"], "read first") || !strings.Contains(why["internal/app/handler_test.go"], "read last") {
		t.Errorf("docs / test reasons: %+v", why)
	}
	if why["internal/app/other.go"] != "" {
		t.Errorf("a plain code file needs no reason: %q", why["internal/app/other.go"])
	}
	if !Changed(p) {
		t.Error("Changed: the order moved files")
	}
}

// A name the old text already defined is no new definition, a local
// variable is none, and a cycle keeps the proposed order.
func TestOrderOnlyNewTopLevelNames(t *testing.T) {
	files := []File{
		{Name: "b.go", New: "func useIt() { Helper() }\n"},
		{Name: "a.go", Old: "func Helper() {}\n", New: "func Helper() {}\nfunc extra() {}\n"},
		{Name: "c.js", New: "function run() {\n  let counter = 1\n}\n"},
		{Name: "d.js", New: "counter++\n"},
	}
	if got := names(files, Order(files)); got != "b.go,a.go,c.js,d.js" {
		t.Errorf("order: %s", got)
	}
	cyc := []File{
		{Name: "x.go", New: "func Alpha() { Beta() }\n"},
		{Name: "y.go", New: "func Beta() { Alpha() }\n"},
	}
	p := Order(cyc)
	if got := names(cyc, p); got != "x.go,y.go" || Changed(p) {
		t.Errorf("cycle order: %s", got)
	}
}

// The use must be a whole word: ParseQueryAll does not use ParseQuery.
func TestOrderWholeWord(t *testing.T) {
	files := []File{
		{Name: "a.go", New: "x := ParseQueryAll()\n"},
		{Name: "b.go", New: "func ParseQuery() {}\n"},
	}
	if got := names(files, Order(files)); got != "a.go,b.go" {
		t.Errorf("order: %s", got)
	}
}

func TestClassOf(t *testing.T) {
	for name, want := range map[string]Class{
		"README.md":                     ClassDocs,
		"docs/notes.txt":                ClassDocs,
		"api/service.proto":             ClassContract,
		"db/migrations/0001_init.up.go": ClassContract,
		"go.mod":                        ClassContract,
		"web/src/types.ts":              ClassContract,
		"web/src/app.d.ts":              ClassContract,
		"internal/app/model.go":         ClassContract,
		"internal/app/view.go":          ClassCode,
		"web/src/app.test.ts":           ClassTest,
		"tests/test_parse.py":           ClassTest,
		"pkg/testdata/golden.md":        ClassTest,
		"src/test/java/AppTest.java":    ClassTest,
	} {
		if got := classOf(name); got != want {
			t.Errorf("%s: class %d, want %d", name, got, want)
		}
	}
}
