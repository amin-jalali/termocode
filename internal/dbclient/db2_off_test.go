//go:build !db2

package dbclient

import (
	"context"
	"strings"
	"testing"
)

// The default build has no Db2 driver: the connection says how to get it.
func TestDb2DefaultBuildSaysRebuild(t *testing.T) {
	_, err := NewSQLSession(context.Background(), Conn{ID: "d", Driver: "db2", Host: "db", Database: "SAMPLE"}, "pw")
	if err == nil || err.Error() != "built without Db2: rebuild with -tags db2" {
		t.Fatalf("err = %v", err)
	}
	if Db2Built {
		t.Error("Db2Built in the default build")
	}
	e, _ := ExtraSQLEngineByID("db2")
	if e.Hint == "" || !strings.Contains(e.Hint, "-tags db2") {
		t.Errorf("picker hint does not name the tag: %q", e.Hint)
	}
}
