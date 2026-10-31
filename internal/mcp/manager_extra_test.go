package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Spec (RT-2): "autostart": false never starts the server just to list
// its tools; Connect starts it, and then its tools are offered.
func TestManagerAutostartOff(t *testing.T) {
	e := newManagerEnv(t)
	ent := mcptest.Entry(map[string]string{"FAKE_LOG": e.log})
	ent["autostart"] = false
	e.write(t, e.user, map[string]any{"fake": ent})
	m := e.manager(t, 0, nil)
	if n := countTools(m.Tools(context.Background(), nil)); n != 0 {
		t.Fatalf("listed %d tools", n)
	}
	if _, err := os.Stat(e.log); err == nil {
		t.Fatal("the server started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Connect(ctx, nil, "fake"); err != nil {
		t.Fatal(err)
	}
	if n := countTools(m.Tools(ctx, nil)); n != 10 {
		t.Fatalf("after Connect: %d tools", n)
	}
}
