package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Spec (MCPX-33, CF-22): "startTimeout" (seconds) caps the start of one
// server instead of the built-in 15 s.
func TestManagerStartTimeoutKey(t *testing.T) {
	e := newManagerEnv(t)
	entry := mcptest.Entry(map[string]string{"FAKE_LOG": e.log, "FAKE_INIT_DELAY": "3s"})
	entry["startTimeout"] = 1
	e.write(t, e.user, map[string]any{"slow": entry})
	m := NewManager(ManagerOptions{UserPath: e.user, ProjectPath: e.project, CacheDir: e.cache,
		Expander: func() Expander { return Expander{Env: os.LookupEnv} }, CallTimeout: 5 * time.Second})
	t.Cleanup(m.Close)
	start := time.Now()
	err := m.Connect(context.Background(), nil, "slow")
	if err == nil {
		t.Fatal("a server slower than its startTimeout started")
	}
	if d := time.Since(start); d > 2500*time.Millisecond {
		t.Fatalf("waited %v; startTimeout is 1s", d)
	}
}

func TestParseStartTimeout(t *testing.T) {
	list, _, _ := ParseFile([]byte(`{"mcpServers":{"a":{"command":"x","startTimeout":90},"b":{"command":"x","startTimeout":-1}}}`), SourceUser, "f")
	if list[0].StartTimeout != 90 || list[0].Problem != "" {
		t.Fatalf("a: %+v", list[0])
	}
	if list[1].Problem == "" {
		t.Fatal("a negative startTimeout was accepted")
	}
}
