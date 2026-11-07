package dbclient

import (
	"strings"
	"testing"
)

// DBX-6: live templates expand a short word to SQL; the cursor lands
// where the table name goes.
func TestExpandTemplate(t *testing.T) {
	for abbr, wantPrefix := range map[string]string{
		"sel": "SELECT * FROM ",
		"ins": "INSERT INTO ",
		"upd": "UPDATE ",
		"del": "DELETE FROM ",
		"jn":  "JOIN ",
		"cnt": "SELECT COUNT(*) FROM ",
		"SEL": "SELECT * FROM ",
	} {
		text, cur, ok := ExpandTemplate(abbr)
		if !ok {
			t.Errorf("%s: not a template", abbr)
			continue
		}
		if !strings.HasPrefix(text, wantPrefix) || cur != len(wantPrefix) {
			t.Errorf("%s: %q cursor %d, want prefix %q cursor %d", abbr, text, cur, wantPrefix, len(wantPrefix))
		}
		if strings.Contains(text, "|") {
			t.Errorf("%s: cursor mark left in %q", abbr, text)
		}
	}
	if _, _, ok := ExpandTemplate("selx"); ok {
		t.Error("selx is not a template")
	}
	names := Templates()
	if len(names) < 6 || names[0] > names[len(names)-1] {
		t.Errorf("Templates() = %v, want sorted, at least 6", names)
	}
}
