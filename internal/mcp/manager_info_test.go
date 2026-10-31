package mcp

import (
	"context"
	"testing"
)

// MCPX-4: Info is what initialize said (server name, version, MCP
// version) while the server runs; after a stop the cached name and
// version stay; an unknown server has none.
func TestManagerInfo(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	if _, _, ok := m.Info("fake"); ok {
		t.Error("info before any connect")
	}
	if err := m.Connect(context.Background(), nil, "fake"); err != nil {
		t.Fatal(err)
	}
	info, proto, ok := m.Info("fake")
	if !ok || info.Name != "fake-mcp" || info.Version != "1.0.0" || proto == "" {
		t.Errorf("running: %+v %q %v", info, proto, ok)
	}
	m.Stop("fake")
	m2 := e.manager(t, 0, nil)
	info, proto, ok = m2.Info("fake")
	if !ok || info.Name != "fake-mcp" || proto != "" {
		t.Errorf("cached: %+v %q %v", info, proto, ok)
	}
	if _, _, ok := m.Info("nope"); ok {
		t.Error("info for an unknown server")
	}
}
