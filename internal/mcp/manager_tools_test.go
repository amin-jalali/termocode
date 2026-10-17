package mcp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// toolsRec stands in for config.json mcp_trusted_tools.
type toolsRec struct {
	mu    sync.Mutex
	saved map[string]string
}

func (r *toolsRec) get(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saved[name]
}

func (r *toolsRec) save(name, hash string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saved == nil {
		r.saved = map[string]string{}
	}
	r.saved[name] = hash
}

func (e *managerEnv) managerTools(t *testing.T, tr *toolsRec) *Manager {
	t.Helper()
	m := NewManager(ManagerOptions{
		UserPath: e.user, ProjectPath: e.project, CacheDir: e.cache,
		Expander:     func() Expander { return Expander{Workspace: e.dir} },
		CallTimeout:  5 * time.Second,
		TrustedTools: tr.get, SaveTrustedTools: tr.save,
	})
	t.Cleanup(m.Close)
	return m
}

// The hash ignores tool order and changes with a description.
func TestToolsHash(t *testing.T) {
	a := []Tool{{Name: "a", Description: "x"}, {Name: "b"}}
	b := []Tool{{Name: "b"}, {Name: "a", Description: "x"}}
	if ToolsHash(a) != ToolsHash(b) {
		t.Error("order changes the hash")
	}
	c := []Tool{{Name: "a", Description: "y"}, {Name: "b"}}
	if ToolsHash(a) == ToolsHash(c) {
		t.Error("a new description keeps the hash")
	}
}

// Spec (rug pull): the first tool list of a server is remembered. When a
// later list differs, the user is asked again (reason: tools changed).
// Deny keeps the tools but every one of them asks first (no "Always
// allow"); Always saves the new list so it is not asked again.
func TestManagerToolsChangedAsksAgain(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	tr := &toolsRec{}
	var asked []TrustReason
	answer := TrustDeny
	ask := func(ctx context.Context, s Server) TrustAnswer {
		asked = append(asked, s.TrustReason)
		return answer
	}
	m := e.managerTools(t, tr)
	list := m.Tools(context.Background(), ask)
	if countTools(list) != 10 || len(asked) != 0 || tr.get("fake") == "" {
		t.Fatalf("first list: %d tools, asked %v, saved %q", countTools(list), asked, tr.get("fake"))
	}
	first := tr.get("fake")
	if list[0].ToolsChanged {
		t.Error("first list marked changed")
	}

	// The server now lists other tools (FAKE_TOOLSET=2).
	e.write(t, e.user, map[string]any{"fake": map[string]string{"FAKE_TOOLSET": "2"}})
	m.Reload()
	list = m.Tools(context.Background(), ask)
	if len(asked) != 1 || asked[0] != ReasonToolsChanged {
		t.Fatalf("asked %v", asked)
	}
	if len(list) != 1 || !list[0].ToolsChanged || len(list[0].Tools) != 1 {
		t.Fatalf("after deny: %+v", list)
	}
	if !status(m, "fake").ToolsChanged {
		t.Error("status does not say tools changed")
	}
	if tr.get("fake") != first {
		t.Error("deny saved the new list")
	}
	// Deny is remembered for the session.
	m.Tools(context.Background(), ask)
	if len(asked) != 1 {
		t.Errorf("asked again after deny: %v", asked)
	}
	// The adapter makes every tool of a changed server ask, without
	// "Always allow".
	tools := AITools(list[0].Server, list[0].Tools, nil, AdapterOptions{TrustAnnotations: true, ToolsChanged: true})
	if len(tools) != 1 || !tools[0].Mutating || tools[0].Class != "" {
		t.Errorf("changed tools gate: %+v", tools)
	}

	answer = TrustAlways
	m2 := e.managerTools(t, tr)
	list = m2.Tools(context.Background(), ask)
	if len(asked) != 2 || len(list) != 1 || list[0].ToolsChanged {
		t.Fatalf("always: asked %v, list %+v", asked, list)
	}
	if tr.get("fake") == first {
		t.Error("always did not save the new list")
	}
}

// A call that starts a server lists its tools again; when they changed
// since the user trusted them and nobody was asked yet, the call is
// refused (the next turn asks).
func TestManagerCallRefusesChangedTools(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{"FAKE_TOOLSET": "2"}})
	tr := &toolsRec{saved: map[string]string{"fake": ToolsHash([]Tool{{Name: "echo"}})}}
	m := e.managerTools(t, tr)
	_, err := m.Call(context.Background(), nil, "fake", "echo", map[string]any{"text": "x"})
	if !errors.Is(err, ErrToolsChanged) || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("err = %v", err)
	}
}
