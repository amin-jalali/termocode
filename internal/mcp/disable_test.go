package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// Spec: SetDisabled writes "disabled": true and removes it again; other
// keys (also unknown ones) stay.
func TestSetDisabledKeepsOtherKeys(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]any{"command": "x", "future": map[string]any{"k": 1}}})
	if err := SetDisabled(e.user, "fake", true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(e.user)
	var f struct {
		S map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if f.S["fake"]["disabled"] != true || f.S["fake"]["future"] == nil || f.S["fake"]["command"] != "x" {
		t.Fatalf("after disable: %s", b)
	}
	if err := SetDisabled(e.user, "fake", false); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(e.user)
	if strings.Contains(string(b), "disabled") || !strings.Contains(string(b), "future") {
		t.Fatalf("after enable: %s", b)
	}
}

// Spec: a disabled server stays listed ("off"), is never started, and
// gives the AI no tools; enabling brings them back.
func TestManagerDisabledServer(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.managerTools(t, &toolsRec{})
	ctx := context.Background()
	if n := countTools(m.Tools(ctx, nil)); n != 10 {
		t.Fatalf("before: %d tools", n)
	}
	if err := SetDisabled(e.user, "fake", true); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	st := status(m, "fake")
	if st.Name == "" || st.State != StateDisabled || st.Pid != 0 {
		t.Fatalf("status %+v", st)
	}
	if list := m.Tools(ctx, nil); len(list) != 0 {
		t.Fatalf("disabled server gave tools: %+v", list)
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.Connect(cctx, nil, "fake"); err == nil {
		t.Error("Connect started a disabled server")
	}
	if err := SetDisabled(e.user, "fake", false); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	if n := countTools(m.Tools(ctx, nil)); n != 10 {
		t.Fatalf("after enable: %d tools", n)
	}
}
