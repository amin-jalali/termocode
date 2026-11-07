package app

import "testing"

func TestDBQueryLogPaletteItems(t *testing.T) {
	want := map[string]string{
		"db-query-log":       "Databases: Show Query Log",
		"db-console-history": "Databases: Show Console Version History",
	}
	for _, it := range databasesPaletteItems() {
		if w, ok := want[it.ID]; ok {
			if it.Title != w {
				t.Errorf("%s title %q", it.ID, it.Title)
			}
			if _, ok := aiActionPolicies[it.ID]; !ok {
				t.Errorf("%s has no AI policy", it.ID)
			}
			delete(want, it.ID)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing palette items %v", want)
	}
}

// Only the files under db/consoles are consoles (they get a version on
// every run).
func TestDBIsConsolePath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p := dbConfigPath("consoles", "abc", "def.sql")
	if p == "" || !dbIsConsolePath(p) {
		t.Fatalf("console %q not seen", p)
	}
	if dbIsConsolePath("/tmp/q.sql") || dbIsConsolePath("") {
		t.Error("a project file is not a console")
	}
}
