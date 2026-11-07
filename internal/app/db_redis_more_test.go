package app

import (
	"strings"
	"testing"
)

var dbRedisMoreIDs = []string{
	"redis-search-keys", "redis-new-key", "redis-json-path", "redis-key-module-info", "redis-modules",
	"redis-search-indexes", "redis-memory", "redis-pubsub-channels", "redis-listen", "redis-stream-groups",
	"redis-cluster-nodes", "redis-export-keys",
}

func TestDBRedisMorePaletteIDsHaveAPolicyAndTitle(t *testing.T) {
	titles := map[string]string{}
	for _, it := range databasesPaletteItems() {
		titles[it.ID] = it.Title
	}
	for _, id := range dbRedisMoreIDs {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Errorf("%s has no AI policy", id)
		}
		if !strings.HasPrefix(titles[id], "Databases: ") {
			t.Errorf("%s not in the palette (%q)", id, titles[id])
		}
	}
}

// The pattern search is KEYS in the console, which the session runs as a
// server-side SCAN MATCH (never KEYS).
func TestDBRedisPatternLine(t *testing.T) {
	if got, err := dbRedisPatternLine(" user:* "); err != nil || got != "KEYS user:*" {
		t.Errorf("got %q %v", got, err)
	}
	if got, _ := dbRedisPatternLine("a b*"); got != `KEYS "a b*"` {
		t.Errorf("quoted %q", got)
	}
	if _, err := dbRedisPatternLine("  "); err == nil {
		t.Error("empty pattern accepted")
	}
}

func TestDBRedisNewKeyTypeOf(t *testing.T) {
	for in, want := range map[string]string{"": "string", " Hash ": "hash", "zset": "zset", "sorted set": "zset", "JSON": "json"} {
		if got, err := dbRedisNewKeyType(in); err != nil || got != want {
			t.Errorf("%q = %q %v", in, got, err)
		}
	}
	if _, err := dbRedisNewKeyType("table"); err == nil {
		t.Error("unknown type accepted")
	}
}

func TestDBRedisExportFile(t *testing.T) {
	if got := dbRedisExportDefault("cache prod"); got != "redis-export-cache-prod.json" {
		t.Errorf("default %q", got)
	}
}
