package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// MCPX-4: a server's screen shows its details above the actions: the
// file, transport, command, env NAMES (never values), what initialize
// said, tools and state.
func TestMCPServerDetailScreen(t *testing.T) {
	t.Chdir(t.TempDir())
	m := covMCPModel(t)
	tok := "sk-" + "test-detail-0042"
	e := mcptest.Entry(map[string]string{"API_TOKEN": tok})
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fake": e,
		"web": map[string]any{"type": "http", "url": "https://mcp.example/mcp", "headers": map[string]string{"X-Key": "${secret:web_key}"}}}})
	writeUserMCP(t, string(b))
	m.aiMCP().Reload()
	m.aiS().mcpSel = "fake"
	if done, ok := covCollect[mcpConnectDoneMsg](m.onMCPServerAction("connect")); !ok || done.err != nil {
		t.Fatalf("connect: %+v", done)
	}
	m.openMCPServerActions("fake")
	if !m.pickerOpen || m.pickerKind != pickerKindMCPServer {
		t.Fatal("the server screen did not open")
	}
	box := ansi.Strip(m.picker.Box())
	for _, want := range []string{"File: ", "Transport: local (stdio)", "Command: ", "Env: ", "API_TOKEN",
		"Server: fake-mcp 1.0.0 · MCP ", "Tools: 10", "State: running", "Connect / Refresh tools"} {
		if !strings.Contains(box, want) {
			t.Errorf("screen lacks %q:\n%s", want, box)
		}
	}
	if strings.Contains(box, tok) || strings.Contains(strings.Join(mcpDetailLines(m, "fake"), "\n"), tok) {
		t.Fatal("an env value is on the screen")
	}
	lines := strings.Join(mcpDetailLines(m, "web"), "\n")
	for _, want := range []string{"Transport: remote (http)", "URL: https://mcp.example/mcp", "Headers: X-Key", "Server: not connected yet"} {
		if !strings.Contains(lines, want) {
			t.Errorf("web lacks %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "secret:web_key") {
		t.Errorf("a header value is shown:\n%s", lines)
	}
}

// The last error is on the screen when a start failed.
func TestMCPServerDetailLastError(t *testing.T) {
	t.Chdir(t.TempDir())
	m := covMCPModel(t)
	writeUserMCP(t, `{"mcpServers":{"bad":{"command":"/no/such/termocode-test-binary"}}}`)
	m.aiMCP().Reload()
	m.aiS().mcpSel = "bad"
	covCollect[mcpConnectDoneMsg](m.onMCPServerAction("connect"))
	var st mcp.Status
	for _, x := range m.aiMCP().Servers() {
		if x.Name == "bad" {
			st = x
		}
	}
	lines := strings.Join(mcpDetailLines(m, "bad"), "\n")
	if st.Problem == "" || !strings.Contains(lines, "Last error: ") {
		t.Errorf("status %+v, lines:\n%s", st, lines)
	}
}
