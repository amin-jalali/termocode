package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBRedisGroupBySeparator(t *testing.T) {
	keys := []string{"session:ab12", "session:cd34", "user:1", "user:2:events", "counter", "session:", "a::b"}
	ns, counts, leaves := dbRedisGroup(keys, "", ":")
	if strings.Join(ns, ",") != "a:,session:,user:" || counts["session:"] != 2 || counts["user:"] != 2 || counts["a:"] != 1 {
		t.Fatalf("namespaces %q counts %v", ns, counts)
	}
	if strings.Join(leaves, ",") != "counter,session:" {
		t.Fatalf("leaves %q (a key equal to its namespace stays a key)", leaves)
	}
	ns, counts, leaves = dbRedisGroup(keys, "user:", ":")
	if strings.Join(ns, ",") != "user:2:" || counts["user:2:"] != 1 || strings.Join(leaves, ",") != "user:1" {
		t.Fatalf("under user: %q %v %q", ns, counts, leaves)
	}
	ns, _, leaves = dbRedisGroup(keys, "", "")
	if len(ns) != 0 || len(leaves) != len(keys) {
		t.Fatalf("no separator must not group: %q %q", ns, leaves)
	}
}

func TestDBRedisParseTTL(t *testing.T) {
	for in, want := range map[string]int64{"": 0, "90": 90, "30m": 1800, "2h": 7200, "1d": 86400, " 15 ": 15} {
		if got, err := dbRedisParseTTL(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	for _, bad := range []string{"0", "-5", "soon", "500ms"} {
		if _, err := dbRedisParseTTL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestDBDiscoveryFindsRedisAndValkeyImages(t *testing.T) {
	for image, want := range map[string]string{"redis:7-alpine": "redis", "valkey/valkey:8": "redis", "bitnami/redis:7.2": "redis",
		"redis/redis-stack-server:latest": "redis", "redis/redisinsight:2": "", "postgres:16": "postgres", "nginx": ""} {
		if got := dbDriverForImage(image); got != want {
			t.Errorf("%s: %q, want %q", image, got, want)
		}
	}
	if defaultDBPort("redis") != 6379 {
		t.Fatal("redis default port")
	}
}

func TestDBRedisURLFromDotenvKeepsThePasswordOutOfTheFile(t *testing.T) {
	root := t.TempDir()
	c, password, err := databaseEnvConnection(root, "REDIS_URL", "rediss://app:pw-in-env@cache.example:6380/2")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "redis" || c.ID != "redis-url" || password != "pw-in-env" || c.Password != "${dotenv:REDIS_URL}" || strings.Contains(c.URL, "pw-in-env") {
		t.Fatalf("conn %+v password %q", c, password)
	}
	if got := dbPasswordFromValue("rediss://app:pw-in-env@cache.example:6380/2"); got != "pw-in-env" {
		t.Fatalf("password from the whole URL = %q", got)
	}
	if got := dbPasswordFromValue("plain-secret"); got != "plain-secret" {
		t.Fatal(got)
	}
	if id := dbRedisConnectionID(dbclient.Conn{Driver: "redis", Host: "Cache.Local", DB: 3}); id != "redis-cache-local-3" {
		t.Fatalf("id = %q", id)
	}
}

func TestDBRedisSummaryAndHintsFitEightyColumns(t *testing.T) {
	v := &dbRedisView{key: "session:ab12", info: dbclient.RedisKeyInfo{Type: "hash", TTL: 23 * 60 * 1000, Len: 7, Bytes: 1229}, next: "17"}
	got := dbRedisSummary(dbclient.Conn{Environment: "production"}, v)
	if got[0] != "hash · 7 fields · TTL 23 min · 1.2 KB · page 1 · prod" {
		t.Fatalf("summary = %q", got[0])
	}
	if h := dbRedisHints(44, v); len([]rune(h)) > 44 || !strings.Contains(h, "e edit field") {
		t.Fatalf("hints = %q", h)
	}
}
