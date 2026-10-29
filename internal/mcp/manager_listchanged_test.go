package mcp

import (
	"context"
	"testing"
)

// Bug (MC-4): notifications/tools/list_changed must make the next turn
// list the tools again. The disk cache (same config hash) brought the
// old list back at once, so the server was never asked again.
func TestManagerToolsListChanged(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	if n := countTools(m.Tools(context.Background(), nil)); n != 10 {
		t.Fatalf("tools %d", n)
	}
	if n := logCount(e.log, "tools/list"); n != 2 { // two pages
		t.Fatalf("tools/list ran %d times", n)
	}
	m.Tools(context.Background(), nil)
	if n := logCount(e.log, "tools/list"); n != 2 {
		t.Fatalf("a turn without a change listed again (%d)", n)
	}
	if r, err := m.Call(context.Background(), nil, "fake", "notify_changed", nil); err != nil || r.Text() != "notified" {
		t.Fatalf("call %q %v", r.Text(), err)
	}
	eventually(t, "the change to be seen", func() bool {
		_, _, have := m.KnownTools("fake")
		return !have
	})
	m.Tools(context.Background(), nil)
	if n := logCount(e.log, "tools/list"); n != 4 {
		t.Fatalf("tools/list ran %d times after the change, want 4", n)
	}
}
