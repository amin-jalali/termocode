package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-63: Oracle in the add picker, the service name prompt, the
// Packages group and the same session for the database node.

func TestDBTypePickerHasOracle(t *testing.T) {
	for _, remote := range []bool{false, true} {
		found := false
		for _, it := range dbTypePickerItems(remote) {
			if it.ID == "oracle" && strings.Contains(it.Hint, "1521") {
				found = true
			}
		}
		if !found {
			t.Errorf("remote=%v: no Oracle row", remote)
		}
	}
}

func TestDBAddOracleAsksServiceName(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	m.databaseStartAdd("oracle")
	if c := s.pendingConn; c.Driver != "oracle" || c.Port != 1521 || c.User != "system" || m.promptKind != promptKindDBHost {
		t.Fatalf("pending = %+v kind %v", c, m.promptKind)
	}
	m.onDBPrompt(promptKindDBHost, "ora.local")
	m.onDBPrompt(promptKindDBPort, "1521")
	m.onDBPrompt(promptKindDBUser, "app")
	if m.promptKind != promptKindDBName || m.prompt.Value() != "FREEPDB1" {
		t.Fatalf("service prompt = %v %q", m.promptKind, m.prompt.Value())
	}
}

func TestDBOraclePackagesGroup(t *testing.T) {
	s := newDBState()
	s.nodes["ora"] = dbNode{kind: dbNodeConnection, name: "ora", path: "ora", conn: "ora"}
	sp := "ora/db/FREE/schema/HR"
	s.nodes[sp] = dbNode{kind: dbNodeSchema, name: "HR", path: sp, conn: "ora"}
	s.addIntrospection(sp, dbclient.IntrospectionResult{Packages: []string{"PAY_PKG"}})
	g, ok := s.nodes[sp+"/Packages"]
	if !ok || g.kind != dbNodeGroup {
		t.Fatalf("no Packages group: %+v", s.nodes[sp].children)
	}
	if n := s.nodes[sp+"/Packages/PAY_PKG"]; n.kind != dbNodeRoutine || n.name != "PAY_PKG" {
		t.Fatalf("package node = %+v", n)
	}
	s2 := newDBState()
	s2.nodes[sp] = dbNode{kind: dbNodeSchema, name: "HR", path: sp, conn: "ora"}
	s2.addIntrospection(sp, dbclient.IntrospectionResult{})
	if _, ok := s2.nodes[sp+"/Packages"]; ok {
		t.Fatal("an empty Packages group is shown")
	}
}

func TestDBOracleTitle(t *testing.T) {
	if dbDriverTitle("oracle") != "Oracle" {
		t.Fatalf("title = %q", dbDriverTitle("oracle"))
	}
}
