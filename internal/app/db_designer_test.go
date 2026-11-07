package app

import (
	"strings"
	"testing"
)

func TestDBDesignerPaletteAndPolicy(t *testing.T) {
	want := map[string]bool{"db-create-table": false, "db-alter-table": false}
	for _, it := range databasesPaletteItems() {
		if _, ok := want[it.ID]; ok {
			want[it.ID] = true
		}
	}
	for id, ok := range want {
		if !ok {
			t.Fatalf("no %s palette item", id)
		}
		if _, ok := aiActionPolicies[id]; !ok {
			t.Fatalf("%s has no AI policy", id)
		}
	}
}

func TestDBDesignerTemplates(t *testing.T) {
	if s := dbDesignTemplate(true, ""); !strings.Contains(s, "table ") || !strings.Contains(s, "column ") {
		t.Fatalf("create template = %q", s)
	}
	if s := dbDesignTemplate(false, "users"); !strings.Contains(s, "users") || !strings.Contains(s, "add column") {
		t.Fatalf("alter template = %q", s)
	}
}

func TestDBDesignerCardText(t *testing.T) {
	title, body := dbDesignCardText("local", "public.users", "ALTER TABLE x DROP COLUMN y;\n", 1, false)
	if !strings.Contains(title, "public.users") || !strings.Contains(body, "DROP COLUMN") || !strings.Contains(body, "drops") {
		t.Fatalf("card = %q / %q", title, body)
	}
	title, _ = dbDesignCardText("prod", "t", "CREATE TABLE t (a int);\n", 0, true)
	if !strings.Contains(strings.ToLower(title), "production") {
		t.Fatalf("production title = %q", title)
	}
}
