package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBTypePickerListsClickHouse(t *testing.T) {
	for _, it := range dbTypePickerItems(false) {
		if it.ID == "clickhouse" && strings.Contains(it.Title, "ClickHouse") {
			return
		}
	}
	t.Fatal("the add picker has no ClickHouse row")
}

func TestDBAddClickHouseFillsDefaults(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	m.databaseStartAdd("clickhouse")
	if m.promptKind != promptKindDBHost {
		t.Fatal("no host prompt")
	}
	if c := s.pendingConn; c.Driver != "clickhouse" || c.Port != 8123 || c.User != "default" || c.Database != "default" {
		t.Fatalf("pending = %+v", c)
	}
}

func TestDBTestAllowsClickHouse(t *testing.T) {
	// A closed port: the test must try to connect, not refuse the type.
	c := dbclient.Conn{ID: "ch", Driver: "clickhouse", Host: "127.0.0.1", Port: 1, User: "default", Database: "default"}
	msg, _ := dbTestCommandSSH(c, "", t.TempDir(), 0, dbSSHSecrets{}, false)().(dbTestMsg)
	if msg.err == nil || strings.Contains(msg.err.Error(), "not available yet") {
		t.Fatalf("err = %v", msg.err)
	}
	_ = context.Background
}
