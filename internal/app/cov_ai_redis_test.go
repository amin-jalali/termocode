package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// More redis_* AI tool flows (ai_redis_tools.go) on the fake Redis:
// bad command lines, writes, server errors, paging and size caps.

func covRedisRun(t *testing.T, e *aiRedisEnv, name string, a map[string]any) (string, error) {
	t.Helper()
	return e.tool(t, name).Run(context.Background(), a)
}

// Connection and command refusals before anything is sent.
func TestCovAIRedisRefusals(t *testing.T) {
	e := newAIRedisEnv(t)
	root := e.m.databases.root
	for id, want := range map[string]string{"": "give conn (one of: cache, prod-cache, ro-cache", "nope": `no connection "nope"; Redis connections:`} {
		if _, err := redisAIConn(root, id); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", id, err)
		}
	}
	cmd := e.tool(t, "redis_command")
	for line, want := range map[string]string{"GET a\nDEL b": "one command per call", `GET "open`: "could not read the command", "  ": "give command"} {
		err := cmd.Validate(map[string]any{"conn": "cache", "command": line})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", line, err)
		}
		if !strings.HasPrefix(cmd.Describe(map[string]any{"conn": "cache", "command": line}), "redis_command (invalid: ") {
			t.Errorf("%q card", line)
		}
		if ask, class := cmd.Gate(map[string]any{"conn": "cache", "command": line}); !ask || class != "" {
			t.Errorf("%q gate = %v %q", line, ask, class)
		}
		if _, err := cmd.Run(context.Background(), map[string]any{"conn": "cache", "command": line}); err == nil {
			t.Errorf("%q ran", line)
		}
	}
	if err := e.tool(t, "redis_get").Validate(map[string]any{"conn": "cache"}); err == nil || err.Error() != "give key" {
		t.Errorf("get without key: %v", err)
	}
	if e.tool(t, "redis_get").Validate(map[string]any{"conn": "nope", "key": "k"}) == nil || e.tool(t, "redis_keys").Validate(map[string]any{"conn": "nope"}) == nil {
		t.Error("unknown conn accepted")
	}
	for _, n := range []string{"redis_keys", "redis_get"} {
		if _, err := covRedisRun(t, e, n, map[string]any{"conn": "nope", "key": "k"}); err == nil {
			t.Errorf("%s on an unknown conn ran", n)
		}
	}
	// A problem connection says so.
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".termocode"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".termocode", "databases.json"), []byte(`{"connections":[{"id":"lit","driver":"redis","host":"h","password":"plain-literal"}]}`), 0o644)
	if _, err := redisAIConn(dir, "lit"); err == nil || !strings.Contains(err.Error(), "has a problem") {
		t.Errorf("problem conn: %v", err)
	}
}

// A write runs in a plain session and is shown; a server error reaches
// the model; the card shows the target with its database.
func TestCovAIRedisWriteAndErrors(t *testing.T) {
	e := newAIRedisEnv(t)
	cmd := e.tool(t, "redis_command")
	a := map[string]any{"conn": "cache", "command": "SET greeting hello"}
	if got := cmd.Describe(a); !strings.HasPrefix(got, "Run on cache (redis cache.internal:6379/0, development):\n\nSET greeting hello") {
		t.Errorf("write card = %q", got)
	}
	if ask, class := cmd.Gate(map[string]any{"conn": "prod-cache", "command": "SET a b"}); !ask || class != "" {
		t.Errorf("production write gate = %v %q", ask, class)
	}
	out, err := cmd.Run(context.Background(), a)
	if err != nil || e.f.String(0, "greeting") != "hello" {
		t.Errorf("write: %q %v", out, err)
	}
	long := "GET " + strings.Repeat("k", 200)
	if got := cmd.Describe(map[string]any{"conn": "cache", "command": long}); !strings.HasSuffix(got, "…") {
		t.Errorf("long read card = %q", got)
	}
	if _, err := cmd.Run(context.Background(), map[string]any{"conn": "cache", "command": "LSET nokey 0 x"}); err == nil || !strings.Contains(err.Error(), "no such key") {
		t.Errorf("a server error: %v", err)
	}
	e.f.Password = "another-password"
	for _, n := range []string{"redis_keys", "redis_get"} {
		if _, err := covRedisRun(t, e, n, map[string]any{"conn": "cache", "key": "counter"}); err == nil {
			t.Errorf("%s with a wrong password gave no error", n)
		}
	}
	if _, err := cmd.Run(context.Background(), a); err == nil {
		t.Error("write with a wrong password gave no error")
	}
}

// Paging and caps: a pattern with more keys than max, the size cap, a
// long list read in pages, a long string, another database.
func TestCovAIRedisPagingAndCaps(t *testing.T) {
	e := newAIRedisEnv(t)
	for i := 0; i < 12; i++ {
		e.f.SetString(0, "item:"+itoa(i)+strings.Repeat("x", 120), "v")
	}
	var items []string
	for i := 0; i < 150; i++ {
		items = append(items, strings.Repeat("e", 40)+itoa(i))
	}
	e.f.RPush(0, "biglist", items...)
	e.f.SetString(0, "huge", strings.Repeat("h", 70*1024))
	e.f.SetString(2, "other-db", "y")
	out, err := covRedisRun(t, e, "redis_keys", map[string]any{"conn": "cache", "pattern": "item:*", "max": 5})
	if err != nil || !strings.Contains(out, "… more keys match") {
		t.Errorf("max 5: %q %v", out, err)
	}
	out, err = covRedisRun(t, e, "redis_keys", map[string]any{"conn": "cache", "db": 2})
	if err != nil || !strings.Contains(out, "db 2 matching *") || !strings.Contains(out, "other-db") {
		t.Errorf("db 2: %q %v", out, err)
	}
	if got := e.tool(t, "redis_keys").Describe(map[string]any{"conn": "cache"}); got != "List Redis keys * on cache" {
		t.Errorf("keys card = %q", got)
	}
	out, err = covRedisRun(t, e, "redis_get", map[string]any{"conn": "cache", "key": "biglist", "max": 20})
	if err != nil || !strings.Contains(out, "… more items; this is the first page (max 20)") {
		t.Errorf("list page: %q %v", out, err)
	}
	out, err = covRedisRun(t, e, "redis_get", map[string]any{"conn": "cache", "key": "huge"})
	if err != nil || !strings.Contains(out, "longer than 64 KB") || !strings.Contains(out, "…") {
		t.Errorf("huge string: %.200q %v", out, err)
	}
	dir, _ := configDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"db_ai_max_kb": 1}`), 0o644)
	out, _ = covRedisRun(t, e, "redis_keys", map[string]any{"conn": "cache", "pattern": "item:*"})
	if !strings.Contains(out, "[truncated (db_ai_max_kb); 12 keys found]") {
		t.Errorf("keys cap: %q", out)
	}
	out, _ = covRedisRun(t, e, "redis_get", map[string]any{"conn": "cache", "key": "biglist", "max": 100})
	if !strings.Contains(out, "items shown (db_ai_max_kb)") {
		t.Errorf("get cap: %q", out)
	}
	if got := e.tool(t, "redis_get").Describe(map[string]any{"conn": "cache", "key": "k"}); got != "Read Redis key k on cache" {
		t.Errorf("get card = %q", got)
	}
}
