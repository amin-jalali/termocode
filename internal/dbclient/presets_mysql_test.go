package dbclient

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMySQLPresets(t *testing.T) {
	want := map[string]int{"mariadb": 3306, "tidb": 4000, "planetscale": 3306, "aurora-mysql": 3306, "singlestore": 3306}
	got := MySQLPresets()
	if len(got) != len(want) {
		t.Fatalf("presets = %+v", got)
	}
	for _, p := range got {
		port, ok := want[p.ID]
		if !ok || p.Port != port || p.Driver != "mysql" || p.Title == "" || p.Hint == "" {
			t.Errorf("preset %+v", p)
		}
		if q, ok := PresetByID(p.ID); !ok || q.ID != p.ID {
			t.Errorf("PresetByID(%q) = %+v %v", p.ID, q, ok)
		}
	}
	if _, ok := PresetByID("nope"); ok {
		t.Error("an unknown preset was found")
	}
	// Hosted services only take encrypted connections.
	for _, id := range []string{"planetscale", "aurora-mysql"} {
		if p, _ := PresetByID(id); p.SSLMode != SSLRequire && p.SSLMode != SSLVerifyFull {
			t.Errorf("%s ssl = %q", id, p.SSLMode)
		}
	}
	// Vitess has no foreign keys: the preset says so.
	if p, _ := PresetByID("planetscale"); !strings.Contains(p.Note, "foreign key") {
		t.Errorf("planetscale note = %q", p.Note)
	}
}

func TestConnPresetRoundTrip(t *testing.T) {
	c := Conn{ID: "t", Driver: "mysql", Preset: "tidb", Host: "h", Port: 4000}
	b, err := json.Marshal(c)
	if err != nil || !strings.Contains(string(b), `"preset":"tidb"`) {
		t.Fatalf("json = %s %v", b, err)
	}
	var back Conn
	if err := json.Unmarshal(b, &back); err != nil || back.Preset != "tidb" {
		t.Fatalf("back = %+v %v", back, err)
	}
	if got := c.PresetTitle(); got != "TiDB" {
		t.Fatalf("PresetTitle = %q", got)
	}
	if got := (Conn{Driver: "mysql"}).PresetTitle(); got != "" {
		t.Fatalf("no preset title = %q", got)
	}
}
