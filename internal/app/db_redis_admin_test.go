package app

import (
	"reflect"
	"testing"
)

func TestDBRedisRenameCopyArgs(t *testing.T) {
	got, err := dbRedisRenameCopyArgs(false, "user:1", " user:2 ")
	if err != nil || !reflect.DeepEqual(got, []string{"RENAMENX", "user:1", "user:2"}) {
		t.Fatalf("rename %v %v", got, err)
	}
	got, err = dbRedisRenameCopyArgs(true, "user:1", "user:1:copy")
	if err != nil || !reflect.DeepEqual(got, []string{"COPY", "user:1", "user:1:copy"}) {
		t.Fatalf("copy %v %v", got, err)
	}
	for _, bad := range []string{"", "   ", "user:1"} {
		if _, err := dbRedisRenameCopyArgs(false, "user:1", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestDBRedisAdminPaletteIDsHaveAPolicy(t *testing.T) {
	for _, id := range []string{"redis-rename-key", "redis-copy-key", "redis-slowlog"} {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Errorf("%s has no AI policy", id)
		}
	}
}
