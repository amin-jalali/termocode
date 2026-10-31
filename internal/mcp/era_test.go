package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

// Spec (MCPX-20): the manager remembers in its tool cache folder that a
// server speaks only the old handshake, so its next starts skip the
// server/discover probe. It asks again after a day, when the entry
// changes, or when the server reports another version.
func TestManagerRemembersOldHandshake(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	probes := func() int {
		b, _ := os.ReadFile(e.log)
		return strings.Count(string(b), "server/discover")
	}
	start := func() {
		t.Helper()
		m := e.manager(t, 0, nil)
		if _, err := m.Call(context.Background(), nil, "fake", "echo", map[string]any{"text": "x"}); err != nil {
			t.Fatal(err)
		}
		m.Close()
	}
	start()
	if probes() != 1 {
		t.Fatalf("first start: %d probes", probes())
	}
	start()
	if probes() != 1 {
		t.Fatalf("second start probed again (%d)", probes())
	}
	later := time.Now().Add(25 * time.Hour)
	restore := clock.Set(func() time.Time { return later })
	start()
	restore()
	if probes() != 2 {
		t.Fatalf("a day later: %d probes, want 2", probes())
	}
	e.write(t, e.user, map[string]any{"fake": map[string]string{"FAKE_X": "1"}}) // the entry changed
	start()
	if probes() != 3 {
		t.Fatalf("after an entry change: %d probes, want 3", probes())
	}
	// The server reports another version: the next start asks again.
	writeFile(t, e.eraPath("fake"), strings.Replace(readFile(t, e.eraPath("fake")), `"1.0.0"`, `"0.9.0"`, 1))
	start()
	start()
	if probes() != 4 {
		t.Fatalf("after a new server version: %d probes, want 4", probes())
	}
}

func (e *managerEnv) eraPath(name string) string { return e.cache + "/" + name + ".era.json" }

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
