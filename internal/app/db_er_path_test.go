package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBERPathPaletteItem(t *testing.T) {
	for _, it := range databasesPaletteItems() {
		if it.ID == "db-er-path" {
			if it.Title != "Databases: Diagram Path Between Tables…" {
				t.Fatalf("title %q", it.Title)
			}
			if p, ok := aiActionPolicies["db-er-path"]; !ok || p.Gate != aiGateNever {
				t.Fatalf("db-er-path policy %+v %v", p, ok)
			}
			return
		}
	}
	t.Fatal("no db-er-path palette item")
}

// The format picker offers each format plain and coloured by name prefix.
func TestDBERFormatIDs(t *testing.T) {
	cases := map[string]struct {
		format string
		color  bool
	}{"mermaid": {"mermaid", false}, "dot+color": {"dot", true}, "plantuml+color": {"plantuml", true}}
	for id, want := range cases {
		f, c := dbERFormatID(id)
		if f != want.format || c != want.color {
			t.Errorf("%s = %s %v", id, f, c)
		}
	}
}

// The path diagram keeps only the path's tables, marks both ends and names
// the path in a comment line.
func TestDBERPathText(t *testing.T) {
	s := dbclient.CanonSchema{Tables: []dbclient.CanonTable{
		{Name: "a", Columns: []dbclient.CanonColumn{{Name: "id", Type: "int"}}},
		{Name: "b", Columns: []dbclient.CanonColumn{{Name: "a_id", Type: "int"}},
			Constraints: []dbclient.CanonConstraint{{Kind: "FOREIGN KEY", Name: "b_a", Columns: []string{"a_id"}, RefTable: "a"}}},
		{Name: "c", Columns: []dbclient.CanonColumn{{Name: "id", Type: "int"}}},
	}}
	text, path, err := dbERPathText(s, "b", "a")
	if err != nil || path != "b → a" {
		t.Fatalf("path %q err %v", path, err)
	}
	if !strings.HasPrefix(text, "%% path: b → a\nerDiagram\n") || strings.Contains(text, "  c {") || !strings.Contains(text, "style a fill:") {
		t.Errorf("text:\n%s", text)
	}
	if _, _, err := dbERPathText(s, "a", "c"); err == nil || !strings.Contains(err.Error(), "No foreign-key path") {
		t.Errorf("no path: %v", err)
	}
}
