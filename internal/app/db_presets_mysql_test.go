package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBTypePickerListsMySQLPresets(t *testing.T) {
	var ids []string
	for _, it := range dbTypePickerItems(false) {
		ids = append(ids, it.ID)
	}
	got := strings.Join(ids, ",")
	for _, want := range []string{"mysql", "preset:mariadb", "preset:tidb", "preset:planetscale", "preset:aurora-mysql", "preset:singlestore"} {
		if !strings.Contains(got, want) {
			t.Errorf("type picker lacks %s: %s", want, got)
		}
	}
	ids = ids[:0]
	for _, it := range dbTypePickerItems(true) {
		ids = append(ids, it.ID)
	}
	if !strings.Contains(strings.Join(ids, ","), "preset:tidb") {
		t.Errorf("remote picker lacks the presets: %v", ids)
	}
}

func TestDBAddFromMySQLPreset(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	m.databaseStartAdd("preset:tidb")
	if m.promptKind != promptKindDBHost {
		t.Fatal("no host prompt")
	}
	if c := s.pendingConn; c.Driver != "mysql" || c.Preset != "tidb" || c.Port != 4000 || c.User != "root" {
		t.Fatalf("pending = %+v", c)
	}
	m.databaseStartAdd("preset:planetscale")
	if c := s.pendingConn; c.SSL == nil || c.SSL.Mode == "" || c.Preset != "planetscale" {
		t.Fatalf("planetscale pending = %+v", c)
	}
	if m.databaseStartAdd("preset:nope") != nil || s.pendingConn.Preset == "nope" {
		t.Fatal("an unknown preset started an add")
	}
}

func TestDBConnDetailShowsPreset(t *testing.T) {
	got := dbConnDetail(dbclient.Conn{Driver: "mysql", Preset: "tidb", Host: "h", Port: 4000})
	if !strings.HasPrefix(got, "tidb") || !strings.HasSuffix(got, "h:4000") {
		t.Fatalf("detail = %q", got)
	}
	if got := dbConnDetail(dbclient.Conn{Driver: "mysql", Host: "h", Port: 3306}); !strings.HasPrefix(got, "mysql") {
		t.Fatalf("plain detail = %q", got)
	}
}
