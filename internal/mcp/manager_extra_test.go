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

// Spec (RT-14): ResetToolCache forgets the saved tool lists (all or one
// server): the files go, and a stopped server lists again on next use.
func TestManagerResetToolCache(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}, "other": map[string]string{}})
	m := e.manager(t, 0, nil)
	if n := countTools(m.Tools(context.Background(), nil)); n != 20 {
		t.Fatalf("tools %d", n)
	}
	m.Close()
	m2 := e.manager(t, 0, nil)
	if !status(m2, "fake").Cached {
		t.Fatal("no cache to reset")
	}
	if n := m2.ResetToolCache("fake"); n != 1 {
		t.Fatalf("reset one: %d", n)
	}
	if st := status(m2, "fake"); st.Cached || st.Tools != 0 {
		t.Fatalf("after reset: %+v", st)
	}
	if !status(m2, "other").Cached {
		t.Fatal("reset of one cleared the other")
	}
	if n := m2.ResetToolCache(""); n != 1 {
		t.Fatalf("reset all: %d", n)
	}
	if _, err := os.Stat(m2.cachePath("other")); err == nil {
		t.Fatal("cache file kept")
	}
}
