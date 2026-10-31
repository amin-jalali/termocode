package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
	"github.com/amin-jalali/termocode/internal/picker"
)

// mcpFakeConnected is a model with the fake server in the user mcp.json,
// connected once so its tools are known.
func mcpFakeConnected(t *testing.T) *Model {
	t.Helper()
	t.Chdir(t.TempDir())
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	m.aiMCP().Reload()
	m.aiS().mcpSel = "fake"
	done, ok := covCollect[mcpConnectDoneMsg](m.onMCPServerAction("connect"))
	if !ok || done.err != nil {
		t.Fatalf("connect: %+v", done)
	}
	return m
}

// MCPX-3: a tool turned off in Show tools is never sent to the model;
// the other tools of the server still are.
func TestMCPTurnToolsSkipsDisabled(t *testing.T) {
	srv := mcp.Server{Name: "fake", Type: mcp.TypeStdio, Source: mcp.SourceUser}
	yes := true
	ro := &mcp.ToolAnnotations{ReadOnlyHint: &yes}
	list := []mcp.ServerTools{{Server: srv, Tools: []mcp.Tool{{Name: "echo", Annotations: ro}, {Name: "write_note"}}}}
	set := mcp.Settings{TrustAnnotations: true, DisabledTools: map[string][]string{"fake": {"write_note"}}}
	var names []string
	for _, tl := range mcpTurnTools(list, set, nil, mcp.AdapterOptions{}) {
		names = append(names, tl.Display)
	}
	if strings.Join(names, ",") != "mcp.fake.echo" {
		t.Errorf("tools sent = %v", names)
	}
	set.DisabledTools = nil
	if got := len(mcpTurnTools(list, set, nil, mcp.AdapterOptions{})); got != 2 {
		t.Errorf("with nothing off: %d tools", got)
	}
	var _ []ai.Tool = mcpTurnTools(nil, set, nil, mcp.AdapterOptions{})
}

// MCPX-3: Show tools marks each tool on or off; Space turns the
// highlighted tool off (saved in config.json, the row says so, the list
// stays open) and on again.
func TestMCPShowToolsSpaceToggles(t *testing.T) {
	m := mcpFakeConnected(t)
	m.openMCPTools("fake")
	if !m.pickerOpen || m.pickerKind != pickerKindMCPTools {
		t.Fatal("Show tools did not open")
	}
	box := ansi.Strip(m.picker.Box())
	if !strings.Contains(box, "[x] echo") || !strings.Contains(box, "on/off") {
		t.Fatalf("no on marks or no Space hint:\n%s", box)
	}
	m.picker.SetCursorID("echo")
	mm, _ := m.update(picker.ActionMsg{Key: "space", ID: "echo"})
	m = &mm
	if !mcp.LoadSettings(mcpConfigPath()).ToolOff("fake", "echo") {
		t.Fatal("echo was not saved as off")
	}
	if !m.pickerOpen || m.pickerKind != pickerKindMCPTools {
		t.Fatal("the list closed")
	}
	it, ok := m.picker.Current()
	if !ok || it.ID != "echo" || !strings.HasPrefix(it.Title, "[ ] echo") || !strings.HasPrefix(it.Hint, "off") {
		t.Errorf("row after off = %+v", it)
	}
	if h := mcpToolsHint(m, "fake"); !strings.Contains(h, "1 off") {
		t.Errorf("Show tools hint = %q", h)
	}
	mm, _ = m.update(picker.ActionMsg{Key: "space", ID: "echo"})
	m = &mm
	if mcp.LoadSettings(mcpConfigPath()).ToolOff("fake", "echo") {
		t.Error("echo stayed off")
	}
	if it, _ := m.picker.Current(); !strings.HasPrefix(it.Title, "[x] echo") {
		t.Errorf("row after on = %+v", it)
	}
}
