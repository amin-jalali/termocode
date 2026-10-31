package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

// Spec (MCPX-20, ttlMs): a tools/list with ttlMs makes the saved list
// expire: once it is old, the next use lists again (a new Manager does
// not trust the stale disk cache).
func TestManagerToolsTTL(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{"FAKE_TTL_MS": "30000"}})
	m := e.manager(t, 0, nil)
	if n := countTools(m.Tools(context.Background(), nil)); n != 10 {
		t.Fatalf("tools %d", n)
	}
	m.Close()
	later := time.Now().Add(time.Minute)
	defer clock.Set(func() time.Time { return later })()
	before, _ := os.ReadFile(e.log)
	m2 := e.manager(t, 0, nil)
	if n := countTools(m2.Tools(context.Background(), nil)); n != 10 {
		t.Fatalf("tools after expiry %d", n)
	}
	after, _ := os.ReadFile(e.log)
	if !strings.Contains(strings.TrimPrefix(string(after), string(before)), "tools/list") {
		t.Fatal("an expired list was used from the cache")
	}
}

// Spec (MCPX-20, cacheScope): a tool list the server marks "private"
// holds data for this user only: it stays in memory and is never
// written to the disk cache; a "public" one is saved as before.
func TestManagerToolsPrivateNotOnDisk(t *testing.T) {
	for _, scope := range []string{"private", "public"} {
		e := newManagerEnv(t)
		e.write(t, e.user, map[string]any{"fake": map[string]string{"FAKE_CACHE_SCOPE": scope}})
		m := e.manager(t, 0, nil)
		if n := countTools(m.Tools(context.Background(), nil)); n != 10 {
			t.Fatalf("%s: tools %d", scope, n)
		}
		_, err := os.Stat(filepath.Join(e.cache, "fake.tools.json"))
		if saved := err == nil; saved != (scope == "public") {
			t.Fatalf("%s: saved on disk %v", scope, saved)
		}
		m.Close()
	}
}
