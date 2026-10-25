package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
	"github.com/amin-jalali/termocode/internal/picker"
)

// fakeServiceEntry is a stdio entry that runs the fake MCP server.
func fakeServiceEntry(env map[string]string) string {
	return mcptest.Config(map[string]map[string]string{"fake": env})
}

func mcpActionIDs(m *Model, name string) map[string]picker.Item {
	out := map[string]picker.Item{}
	for _, it := range mcpServerActionItems(m, name) {
		out[it.ID] = it
	}
	return out
}

func mcpToolCount(m *Model) int {
	n := 0
	for _, st := range m.aiMCP().Tools(context.Background(), nil) {
		n += len(st.Tools)
	}
	return n
}

// Spec: Disable writes "disabled": true (other keys stay), the row reads
// "off", the AI gets no tools of it; Enable brings everything back.
func TestMCPServiceDisableEnable(t *testing.T) {
	m := aiTestModel(t)
	writeUserMCP(t, fakeServiceEntry(nil))
	m.aiMCP()
	if n := mcpToolCount(&m); n == 0 {
		t.Fatal("no tools before Disable")
	}
	ids := mcpActionIDs(&m, "fake")
	if _, ok := ids["disable"]; !ok {
		t.Fatalf("no Disable row: %v", ids)
	}
	if _, ok := ids["enable"]; ok {
		t.Error("Enable offered on an enabled service")
	}
	m.aiS().mcpSel = "fake"
	m.onMCPServerAction("disable")
	b, _ := os.ReadFile(mcpUserPath())
	if !strings.Contains(string(b), `"disabled": true`) || !strings.Contains(string(b), `"command"`) {
		t.Fatalf("mcp.json after Disable: %s", b)
	}
	st := serverStatus(&m, "fake")
	if st.State != mcp.StateDisabled || st.Pid != 0 {
		t.Fatalf("status %+v", st)
	}
	if h := mcpStatusHint(st); !strings.HasPrefix(h, "off") {
		t.Errorf("hint %q", h)
	}
	if n := mcpToolCount(&m); n != 0 {
		t.Errorf("the AI still has %d tools of a disabled service", n)
	}
	// Survives a reload from disk (a fresh manager).
	fresh := mcp.NewManager(mcp.ManagerOptions{UserPath: mcpUserPath(), CacheDir: t.TempDir()})
	defer fresh.Close()
	if ss := fresh.Servers(); len(ss) != 1 || ss[0].State != mcp.StateDisabled {
		t.Errorf("after reload: %+v", ss)
	}

	ids = mcpActionIDs(&m, "fake")
	if _, ok := ids["enable"]; !ok {
		t.Fatalf("no Enable row: %v", ids)
	}
	if _, ok := ids["connect"]; ok {
		t.Error("Connect offered on a disabled service")
	}
	m.aiS().mcpSel = "fake"
	m.onMCPServerAction("enable")
	b, _ = os.ReadFile(mcpUserPath())
	if strings.Contains(string(b), "disabled") {
		t.Errorf("mcp.json after Enable: %s", b)
	}
	if n := mcpToolCount(&m); n == 0 {
		t.Error("no tools after Enable")
	}
}

// Spec: the AI can not remove, disable or enable a service: the services
// list is a picker (never for the AI) and there is no palette command or
// IDE tool for it.
func TestMCPServiceManageIsUserOnly(t *testing.T) {
	if p, ok := aiActionPolicyFor("ai-mcp-servers"); !ok || p.Gate != aiGateNever {
		t.Errorf("ai-mcp-servers policy %+v ok=%v, want never", p, ok)
	}
	for _, e := range PaletteCatalog() {
		id := strings.ToLower(e.ID)
		if strings.Contains(id, "mcp") && (strings.Contains(id, "remove") || strings.Contains(id, "disable") || strings.Contains(id, "enable")) {
			if p, _ := aiActionPolicyFor(e.ID); p.Gate != aiGateNever {
				t.Errorf("%s: gate %q, want never", e.ID, p.Gate)
			}
		}
	}
	for _, tl := range aiIDETools(t.TempDir(), func(context.Context, aiIDEFunc) (string, error) { return "", nil }) {
		n := strings.ToLower(tl.Name)
		if strings.Contains(n, "mcp") && (strings.Contains(n, "remove") || strings.Contains(n, "disable")) {
			t.Errorf("the AI has the tool %q", tl.Name)
		}
	}
}
