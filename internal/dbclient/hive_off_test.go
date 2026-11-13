//go:build !hive

package dbclient

import (
	"context"
	"strings"
	"testing"
)

// The default build has no Hive driver: the connection says how to get it.
func TestHiveDefaultBuildSaysRebuild(t *testing.T) {
	_, err := NewSQLSession(context.Background(), Conn{ID: "h", Driver: "hive", Host: "hs2"}, "")
	if err == nil || err.Error() != "built without Hive: rebuild with -tags hive" {
		t.Fatalf("err = %v", err)
	}
	if HiveBuilt {
		t.Error("HiveBuilt in the default build")
	}
	if e, _ := ExtraSQLEngineByID("hive"); !strings.Contains(e.Hint, "-tags hive") {
		t.Errorf("picker hint does not name the tag: %q", e.Hint)
	}
}
