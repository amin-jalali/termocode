package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBSourcePaletteItems(t *testing.T) {
	want := map[string]string{
		"db-edit-source":     "Databases: Edit Object Source",
		"db-submit-source":   "Databases: Submit Object Source…",
		"db-pending-sources": "Databases: Pending Source Changes…",
	}
	for _, it := range databasesPaletteItems() {
		if w, ok := want[it.ID]; ok {
			if it.Title != w {
				t.Fatalf("%s title %q", it.ID, it.Title)
			}
			delete(want, it.ID)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing %v", want)
	}
}

func TestDBSourceKindOfNode(t *testing.T) {
	cases := map[dbNodeKind]dbclient.ObjectKind{dbNodeView: dbclient.ObjectView, dbNodeRoutine: dbclient.ObjectFunction, dbNodeTrigger: dbclient.ObjectTrigger}
	for k, want := range cases {
		if got, ok := dbSourceKindOf(k); !ok || got != want {
			t.Errorf("%v = %q %v", k, got, ok)
		}
	}
	if _, ok := dbSourceKindOf(dbNodeTable); ok {
		t.Error("a table has no editable source")
	}
}

func TestDBSourceFileNameIsSafe(t *testing.T) {
	a := dbSourceFileName(dbclient.ObjectView, "sales.big orders/../x")
	if strings.ContainsAny(a, `/\ `) || !strings.HasPrefix(a, "view-sales.big_orders") || !strings.HasSuffix(a, ".sql") {
		t.Fatalf("file name %q", a)
	}
	if a == dbSourceFileName(dbclient.ObjectView, "sales.big_orders_.._x") {
		t.Fatal("names that clean the same must still get different files")
	}
}

func TestDBSourceCardTextWarns(t *testing.T) {
	e := dbclient.NewSourceEdit("my", dbclient.ObjectFunction, "f", "CREATE FUNCTION f() RETURNS INT RETURN 1")
	e.Text = "CREATE FUNCTION f() RETURNS INT RETURN 2"
	plan := dbclient.SourcePlan{Statements: []string{"DROP FUNCTION IF EXISTS `f`", e.Text}}
	text := dbSourceCardText(e, plan, true)
	for _, w := range []string{"changed on the server", "not in one transaction", "DROP FUNCTION"} {
		if !strings.Contains(text, w) {
			t.Errorf("no %q in\n%s", w, text)
		}
	}
	plan.Transactional = true
	if strings.Contains(dbSourceCardText(e, plan, false), "changed on the server") {
		t.Error("no outdated warning when the stored source did not change")
	}
}
