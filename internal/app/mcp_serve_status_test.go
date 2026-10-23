package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/picker"
)

// mcpServeItemsText is the status screen's rows as plain text.
func mcpServeItemsText(items []picker.Item) string {
	var lines []string
	for _, it := range items {
		lines = append(lines, it.Title+"  "+it.Hint)
	}
	return strings.Join(lines, "\n")
}

// mcpServeStatusModel is a listening model with one connected agent and a
// few audit lines (some from the chat, which must not show).
func mcpServeStatusModel(t *testing.T) *Model {
	t.Helper()
	m, _, _ := mcpServeTestModel(t)
	m.mcpServe.path = "/tmp/tc123/k.mcp.sock"
	m.mcpServe.clients = []*mcpServeAgent{{id: 1, name: "claude-code", since: time.Date(2026, 10, 22, 12, 3, 9, 0, time.Local), calls: 14, asked: 2}}
	ch := "12:03:40  read_file  path=main.go  gate=auto  by=mcp:claude-code  → package main"
	m.output.Append(aiAuditChannel, ch)
	m.output.Append(aiAuditChannel, "12:03:41  git_status  gate=auto  by=chat  → clean")
	m.output.Append(aiAuditChannel, "12:03:42  tasks_run  label=\"say hi\"  gate=confirm:allowed  by=mcp:claude-code  → started")
	return m
}

// Spec (MS-3, MCP-3): the status screen says it listens, who is connected
// (name, since, calls) and shows the last calls of outside agents only, as
// readable rows (time, agent, tool, what happened), never the raw audit
// text or the socket path.
func TestMCPServeStatusListening(t *testing.T) {
	m := mcpServeStatusModel(t)
	text := mcpServeItemsText(m.mcpServeStatusItems())
	for _, want := range []string{"Listening", "1 agent connected",
		"claude-code", "12:03", "14 calls", "2 asked",
		"12:03 claude-code · read_file · ran without asking",
		"12:03 claude-code · tasks_run · asked, you allowed"} {
		if !strings.Contains(text, want) {
			t.Errorf("status has no %q:\n%s", want, text)
		}
	}
	for _, raw := range []string{"gate=", "by=mcp:", "/tmp/tc123/k.mcp.sock", "Socket", "path=main.go"} {
		if strings.Contains(text, raw) {
			t.Errorf("status shows raw %q:\n%s", raw, text)
		}
	}
	if strings.Contains(text, "by=chat") || strings.Contains(text, "git_status") {
		t.Errorf("status shows the chat's audit lines:\n%s", text)
	}
}

// Spec (MS-3): the audit lines on the screen are cut to the last few.
func TestMCPServeStatusLastLines(t *testing.T) {
	m := mcpServeStatusModel(t)
	for i := 0; i < 20; i++ {
		m.output.Append(aiAuditChannel, "12:04:00  tool_"+string(rune('a'+i))+"  gate=auto  by=mcp:codex  → shown")
	}
	text := mcpServeItemsText(m.mcpServeStatusItems())
	if strings.Count(text, "codex · tool_") != mcpServeStatusLines {
		t.Errorf("call rows shown = %d, want %d:\n%s", strings.Count(text, "codex · tool_"), mcpServeStatusLines, text)
	}
	if strings.Contains(text, "tool_a ") || !strings.Contains(text, "tool_t ") {
		t.Errorf("not the newest lines:\n%s", text)
	}
}

// Spec (MS-3, MCP-6): not listening says why in plain words (no config
// key, no file name): turned off, auto still waiting for an agent tab, or
// the start error; and there is an action row to fix it.
func TestMCPServeStatusNotListening(t *testing.T) {
	m, _, _ := mcpServeTestModel(t)
	m.mcpServe = nil
	cases := []struct {
		setting, err, want string
	}{
		{"off", "", "The MCP server is turned off"},
		{"auto", "", "starts when you start an agent tab"},
		{"on", "address in use", "address in use"},
		{"on", "", "It did not start"},
	}
	for _, c := range cases {
		cfg, _ := settingsConfigPath()
		_ = os.MkdirAll(filepath.Dir(cfg), 0o755)
		if err := os.WriteFile(cfg, []byte(`{"mcp_server":"`+c.setting+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		m.mcpServeErr = c.err
		text := mcpServeItemsText(m.mcpServeStatusItems())
		if !strings.Contains(text, "Not listening") || !strings.Contains(text, c.want) {
			t.Errorf("%s: status has no %q:\n%s", c.setting, c.want, text)
		}
		if strings.Contains(text, "Socket") {
			t.Errorf("%s: a socket path while not listening:\n%s", c.setting, text)
		}
		for _, tech := range []string{"mcp_server", "config.json", "errors.log"} {
			if strings.Contains(text, tech) {
				t.Errorf("%s: the status names %q:\n%s", c.setting, tech, text)
			}
		}
		if id := mcpServeActionID(m.mcpServeStatusItems(), "serve-"); id == "" {
			t.Errorf("%s: no action row to fix it:\n%s", c.setting, text)
		}
	}
}

// Spec (MS-3): the screen opens from the palette id and fits narrow
// windows (no line wider than the window).
func TestMCPServeStatusScreenNarrow(t *testing.T) {
	for _, w := range []int{80, 50} {
		m := mcpServeStatusModel(t)
		m.w, m.h = w, 24
		m.dispatchPaletteAction("mcp-serve-status")
		if !m.pickerOpen {
			t.Fatal("the status screen did not open")
		}
		view := ansi.Strip(m.picker.View())
		if !strings.Contains(view, "Listening") {
			t.Errorf("w=%d: no status in\n%s", w, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > w {
				t.Errorf("w=%d: line is %d cells: %q", w, ansi.StringWidth(line), line)
			}
		}
	}
}

// Spec (MS-3): the config snippet for agent CLIs, exactly.
func TestMCPServeConfigJSONGolden(t *testing.T) {
	got := mcpServeConfigJSON("/usr/local/bin/termocode", "/home/me/proj")
	want := `{
  "mcpServers": {
    "termocode": {
      "command": "/usr/local/bin/termocode",
      "args": ["mcp", "--dir", "/home/me/proj"]
    }
  }
}
`
	if got != want {
		t.Errorf("config:\n%s\nwant:\n%s", got, want)
	}
	// A path with a quote or a backslash stays valid JSON.
	if odd := mcpServeConfigJSON(`/a "b"/termocode`, `C:\proj`); !strings.Contains(odd, `"/a \"b\"/termocode"`) || !strings.Contains(odd, `"C:\\proj"`) {
		t.Errorf("odd paths:\n%s", odd)
	}
}

// Spec (MS-3): the copy goes through the app's clipboard path (the test
// sink, never the real clipboard), with the real executable and the
// workspace, and a toast.
func TestMCPServeCopyConfig(t *testing.T) {
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	old := mcpServeExecutable
	mcpServeExecutable = func() (string, error) { return "/opt/tc/termocode", nil }
	defer func() { mcpServeExecutable = old }()
	m := aiTestModel(t)
	cmd := m.dispatchPaletteAction("mcp-serve-copy-config")
	if len(copied) != 1 {
		t.Fatalf("copies = %d", len(copied))
	}
	if want := mcpServeConfigJSON("/opt/tc/termocode", aiWorkspaceRoot()); copied[0] != want {
		t.Errorf("copied:\n%s\nwant:\n%s", copied[0], want)
	}
	if m.toast.Empty() || cmd == nil {
		t.Error("no toast after the copy")
	}
	if toast := strings.Join(m.toast.View(160, 40), " "); !strings.Contains(toast, "termocode server config") {
		t.Errorf("toast = %q", toast)
	}
	// Without a path of its own the command falls back to the name.
	mcpServeExecutable = func() (string, error) { return "", errors.New("no") }
	copied = nil
	m.dispatchPaletteAction("mcp-serve-copy-config")
	if len(copied) != 1 || !strings.Contains(copied[0], `"command": "termocode"`) {
		t.Errorf("fallback: %v", copied)
	}
}

// Spec (MS-3): both palette entries exist and are never for the AI.
func TestMCPServePaletteIDs(t *testing.T) {
	titles := map[string]string{}
	for _, it := range paletteItems() {
		titles[it.ID] = it.Title
	}
	for id, title := range map[string]string{
		"mcp-serve-status":      "MCP: Server Status",
		"mcp-serve-copy-config": "MCP: Copy termocode Server Config",
	} {
		if titles[id] != title {
			t.Errorf("palette %s = %q, want %q", id, titles[id], title)
		}
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != aiGateNever || p.Reason == "" {
			t.Errorf("%s policy = %+v (ok %v), want never with a reason", id, p, ok)
		}
	}
}

// Spec (MCP-3): each way a call can end has its own plain words.
func TestMCPServeCallRowWords(t *testing.T) {
	cases := []struct{ line, want string }{
		{"09:41:12  get_ide_state  gate=auto  by=mcp:claude-code  → 2 tabs", "09:41 claude-code · get_ide_state · ran without asking"},
		{"09:42:05  tasks_run  label=\"x\"  gate=confirm:allowed  by=mcp:codex  → started", "09:42 codex · tasks_run · asked, you allowed"},
		{"09:42:06  tasks_run  gate=confirm:always  by=mcp:codex  → started", "09:42 codex · tasks_run · ran (always allowed)"},
		{"09:42:40  debug_start  gate=confirm  by=mcp:codex  → declined: declined by the user", "09:42 codex · debug_start · asked, you denied"},
		{"09:43:00  debug_start  gate=confirm  by=mcp:codex  → declined: timed out waiting for the user", "09:43 codex · debug_start · asked, no answer in time"},
		{"09:44:00  mcp__x  gate=refused  by=mcp:codex  → not an exposed tool", "09:44 codex · mcp__x · not run"},
	}
	for _, c := range cases {
		got, ok := mcpServeCallRow(c.line)
		if !ok || got != c.want {
			t.Errorf("row(%q) = %q (%v), want %q", c.line, got, ok, c.want)
		}
	}
	if _, ok := mcpServeCallRow("09:41:12  read_file  gate=auto  by=chat  → ok"); ok {
		t.Error("a chat line became a row")
	}
}

// Spec (MCP-3): the status box grows with the window, up to a cap.
func TestMCPServeStatusWidthFollowsWindow(t *testing.T) {
	boxWidth := func(w int) int {
		m := mcpServeStatusModel(t)
		m.w, m.h = w, 42
		m.dispatchPaletteAction("mcp-serve-status")
		for _, line := range strings.Split(ansi.Strip(m.picker.View()), "\n") {
			if a, b := strings.Index(line, "╭"), strings.Index(line, "╮"); a >= 0 && b > a {
				return ansi.StringWidth(line[a : b+len("╮")])
			}
		}
		t.Fatalf("w=%d: no box", w)
		return 0
	}
	narrow, mid, wide, huge := boxWidth(60), boxWidth(80), boxWidth(150), boxWidth(300)
	if !(narrow < mid && mid < wide) {
		t.Errorf("widths 60/80/150 = %d/%d/%d, want them to grow", narrow, mid, wide)
	}
	if huge != wide || huge > 100 {
		t.Errorf("width at 300 = %d, want the cap (%d, at most 100)", huge, wide)
	}
}

// Spec (MCP-4): the status action has the same words as the palette
// command it opens.
func TestMCPServeStatusAutoApproveWording(t *testing.T) {
	m := mcpServeStatusModel(t)
	var palette string
	for _, it := range paletteItems() {
		if it.ID == "ai-auto-approve" {
			palette = it.Title
		}
	}
	if palette == "" {
		t.Fatal("no palette command for the auto-approve list")
	}
	text := mcpServeItemsText(m.mcpServeStatusItems())
	if !strings.Contains(text, palette) {
		t.Errorf("status has no %q:\n%s", palette, text)
	}
}

// mcpServeActionID is the first row ID with the prefix ("" when none).
func mcpServeActionID(items []picker.Item, prefix string) string {
	for _, it := range items {
		if strings.HasPrefix(it.ID, prefix) {
			return it.ID
		}
	}
	return ""
}

// Spec (MCP-7): a listening status has "Stop listening", and a connected
// agent's row is selectable (Enter opens a Disconnect card).
func TestMCPServeStatusHasStopAndDisconnectRows(t *testing.T) {
	m := mcpServeStatusModel(t)
	items := m.mcpServeStatusItems()
	var stop, agent bool
	for _, it := range items {
		stop = stop || (it.ID == "serve-stop" && it.Title == "Stop listening")
		agent = agent || (it.ID == "agent:1" && strings.Contains(it.Title, "claude-code"))
	}
	if !stop || !agent {
		t.Errorf("stop row %v, agent row %v in:\n%s", stop, agent, mcpServeItemsText(items))
	}
	m.mcpServe = nil
	if id := mcpServeActionID(m.mcpServeStatusItems(), "serve-stop"); id != "" {
		t.Error("Stop listening while not listening")
	}
}

// Spec (MCP-7): Stop listening asks first (Cancel has the focus); Cancel
// keeps everything; OK stops the listener for this session, leaves the
// setting alone, and the screen says so.
func TestMCPServeStatusStopListening(t *testing.T) {
	m := mcpServeStatusModel(t)
	cfg, _ := settingsConfigPath()
	mcpServeSetMode(t, "on")
	before, _ := os.ReadFile(cfg)
	m.onMCPServeStatusPicked("serve-stop")
	if !m.confirmOpen || m.confirmKind != confirmKindMCPServeStop || m.pickerOpen {
		t.Fatalf("no stop card (confirm %v kind %d picker %v)", m.confirmOpen, m.confirmKind, m.pickerOpen)
	}
	if got := mcpServeButtonIDs(m); got != "cancel,ok" {
		t.Errorf("buttons = %s", got)
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if m.mcpServe == nil || !m.pickerOpen {
		t.Fatal("Cancel stopped it, or lost the screen")
	}
	m.onMCPServeStatusPicked("serve-stop")
	m.confirmOpen = false
	m.handleConfirmSelect("ok")
	if m.mcpServe != nil {
		t.Fatal("OK did not stop it")
	}
	if after, _ := os.ReadFile(cfg); string(after) != string(before) {
		t.Errorf("the setting changed: %s -> %s", before, after)
	}
	text := mcpServeItemsText(m.mcpServeStatusItems())
	if !strings.Contains(text, "You stopped it for this session") || mcpServeActionID(m.mcpServeStatusItems(), "serve-start") == "" {
		t.Errorf("status after the stop:\n%s", text)
	}
	if !m.pickerOpen || m.pickerKind != pickerKindMCPServeStatus {
		t.Error("the status screen is not back")
	}
}

// Spec (MCP-7): Esc on one of these cards goes back to the status screen
// and changes nothing.
func TestMCPServeStatusCardEscBack(t *testing.T) {
	m := mcpServeStatusModel(t)
	m.onMCPServeStatusPicked("serve-stop")
	m.confirmOpen = false
	m.onAIConfirmClosed()
	if m.mcpServe == nil || !m.pickerOpen {
		t.Error("Esc stopped it, or lost the screen")
	}
}
