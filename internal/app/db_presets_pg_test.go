package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBTypePickerListsPGPresets(t *testing.T) {
	var ids []string
	for _, it := range dbTypePickerItems(false) {
		ids = append(ids, it.ID)
	}
	got := strings.Join(ids, ",")
	for _, want := range []string{"preset:cockroachdb", "preset:timescaledb", "preset:redshift", "preset:supabase", "preset:citus"} {
		if !strings.Contains(got, want) {
			t.Errorf("type picker lacks %s: %s", want, got)
		}
	}
}

func TestDBAddFromPGPreset(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	m.databaseStartAdd("preset:redshift")
	if m.promptKind != promptKindDBHost {
		t.Fatal("no host prompt")
	}
	if c := s.pendingConn; c.Driver != "postgres" || c.Preset != "redshift" || c.Port != 5439 || c.User == "" {
		t.Fatalf("pending = %+v", c)
	}
	m.databaseStartAdd("preset:cockroachdb")
	if c := s.pendingConn; c.Preset != "cockroachdb" || c.Port != 26257 {
		t.Fatalf("cockroach pending = %+v", c)
	}
}

func TestDBTreeHypertablesGroup(t *testing.T) {
	s := newDBState()
	s.addObjects("c/db/public", "c", dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "metrics", Kind: "HYPERTABLE"}, {Name: "users", Kind: "BASE TABLE"}}})
	n, ok := s.nodes["c/db/public/Hypertables/metrics"]
	if !ok || n.kind != dbNodeTable {
		t.Fatalf("hypertable node = %+v ok=%v", n, ok)
	}
	if _, ok := s.nodes["c/db/public/Tables/users"]; !ok {
		t.Fatal("plain table moved")
	}
}

func TestDBConnDetailShowsPGPreset(t *testing.T) {
	if got := dbConnDetail(dbclient.Conn{Driver: "postgres", Preset: "redshift", Host: "h", Port: 5439}); !strings.HasPrefix(got, "redshift") {
		t.Fatalf("detail = %q", got)
	}
}
