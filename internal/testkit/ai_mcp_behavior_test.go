//go:build unix

package testkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// MCP client (MC-3 / MC-4): the chat agent calls the tools of the fake
// MCP server (internal/mcp/mcptest, this test binary re-run as a stdio
// server). The servers come from mcp.json in the sandboxed config dir
// (user) or the workspace's .termocode/mcp.json (project).

// mcpFake is one fake server's files: FAKE_LOG (every method it got) and
// FAKE_MARKER (exists while it runs).
type mcpFake struct{ log, marker string }

func newMCPFake(t *testing.T) mcpFake {
	d := t.TempDir()
	return mcpFake{log: filepath.Join(d, "fake.log"), marker: filepath.Join(d, "running")}
}

func (f mcpFake) config(name string) string {
	return mcptest.Config(map[string]map[string]string{name: {"FAKE_LOG": f.log, "FAKE_MARKER": f.marker}})
}

func (f mcpFake) received() string {
	b, _ := os.ReadFile(f.log)
	return string(b)
}

func (f mcpFake) running() bool {
	_, err := os.Stat(f.marker)
	return err == nil
}

// mcpDriver is aiDriverConfig plus mcp.json files: user is the config
// dir's mcp.json, project the workspace's .termocode/mcp.json.
func mcpDriver(t *testing.T, srvURL, user, project, extra string, auto ...string) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	files := textFiles()
	if project != "" {
		files[".termocode/mcp.json"] = project
	}
	approve, _ := json.Marshal(auto)
	cfg := map[string]string{"config.json": `{` + extra + comma(extra) + `"ai_auto_approve":` + string(approve) + `}`}
	if user != "" {
		cfg["mcp.json"] = user
	}
	return New(t, Options{W: 160, H: 40, Nvim: true, Files: files, Config: cfg,
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srvURL+"/v1")
		}})
}

func waitDone(t *testing.T, d *Driver) {
	t.Helper()
	if !d.WaitForText("fake-done", 15*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
}

// Spec: Given a user mcp.json with a server, nothing runs at startup;
// when the model calls a read-only MCP tool it runs without a dialog,
// the tool was offered to the model under its wire name, the result goes
// back to the model, and the MCP log shows the lifecycle.
func TestAIMCPReadOnlyToolBehavior(t *testing.T) {
	f := newMCPFake(t)
	s, srv := fakeToolModel(t, toolCall{"mcp__fake__echo", map[string]any{"text": "hi"}})
	d := mcpDriver(t, srv.URL, f.config("fake"), "", "")
	if f.received() != "" || f.running() {
		t.Fatalf("the MCP server started before it was needed: %q", f.received())
	}
	askAI(d, "echo hi")
	waitDone(t, d)
	if strings.Contains(screen(d), "AI wants to") {
		t.Errorf("a read-only MCP tool asked:\n%s", screen(d))
	}
	if r := s.toolResults(); len(r) != 1 || !strings.HasPrefix(r[0], "echo: hi") {
		t.Errorf("tool results %q", r)
	}
	s.mu.Lock()
	first := s.bodies[0]
	s.mu.Unlock()
	if !strings.Contains(first, `"mcp__fake__echo"`) || !strings.Contains(first, "[MCP fake]") {
		t.Errorf("first request does not offer the MCP tool")
	}
	if !strings.Contains(f.received(), "call echo") {
		t.Errorf("fake got %q", f.received())
	}
	// The chat header chip counts the connected server.
	if !d.WaitForText("1 MCP", 5*time.Second) {
		t.Errorf("no MCP chip in the chat header:\n%s", screen(d))
	}

	runPalette(d, "AI: MCP: Show Log")
	if !d.WaitForText("fake: connected: fake-mcp 1.0.0", 5*time.Second) {
		t.Errorf("MCP log not shown:\n%s", screen(d))
	}
}

// Spec: a mutating MCP tool opens the confirm dialog titled with its
// display name; "Always allow mcp:fake" runs it and saves the class, so
// the next call runs without asking. A destructive name always asks,
// with no "Always allow".
func TestAIMCPConfirmBehavior(t *testing.T) {
	f := newMCPFake(t)
	s, srv := fakeToolModel(t,
		toolCall{"mcp__fake__write_note", map[string]any{"title": "one"}},
		toolCall{"mcp__fake__write_note", map[string]any{"title": "two"}},
		toolCall{"mcp__fake__delete_all", map[string]any{}},
	)
	d := mcpDriver(t, srv.URL, f.config("fake"), "", "")
	askAI(d, "write notes")
	if !d.WaitForText("AI wants to: mcp.fake.write_note", 15*time.Second) {
		t.Fatalf("no confirm dialog:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "title=one") {
		t.Errorf("dialog does not show the arguments:\n%s", screen(d))
	}
	clickTextOn(d, "Always allow mcp:fake")
	if !d.WaitForText("AI wants to: mcp.fake.delete_all", 15*time.Second) {
		t.Fatalf("no dialog for delete_all (or write_note asked twice):\n%s", screen(d))
	}
	if strings.Contains(screen(d), "Always allow") {
		t.Errorf("destructive tool offers Always allow:\n%s", screen(d))
	}
	d.Key("esc") // deny
	waitDone(t, d)
	r := s.toolResults()
	if len(r) != 3 || !strings.HasPrefix(r[0], "wrote one") || !strings.HasPrefix(r[1], "wrote two") ||
		!strings.Contains(r[2], "declined") {
		t.Errorf("tool results %q", r)
	}
	b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "config.json"))
	if !strings.Contains(string(b), `"mcp:fake"`) {
		t.Errorf("config.json has no mcp:fake auto-approve: %s", b)
	}
	if strings.Contains(f.received(), "call delete_all") {
		t.Error("the denied tool ran")
	}
}

// Spec: a server from the project's .termocode/mcp.json is not trusted
// by itself: the first turn asks (naming the project file and the
// command); Deny starts nothing and offers no tools; Allow once runs it.
func TestAIMCPProjectTrustBehavior(t *testing.T) {
	f := newMCPFake(t)
	s, srv := fakeToolModel(t, toolCall{"mcp__proj__echo", map[string]any{"text": "a"}})
	d := mcpDriver(t, srv.URL, "", f.config("proj"), "")
	askAI(d, "use the project server")
	if !d.WaitForText("Start MCP server proj?", 15*time.Second) {
		t.Fatalf("no trust dialog:\n%s", screen(d))
	}
	if sc := screen(d); !strings.Contains(sc, ".termocode/mcp.json") || !strings.Contains(sc, "FAKE_LOG") {
		t.Errorf("trust dialog misses the file or the env keys:\n%s", sc)
	}
	clickTextOn(d, "Deny")
	waitDone(t, d)
	if f.received() != "" {
		t.Errorf("denied server started: %q", f.received())
	}
	if r := s.toolResults(); len(r) != 1 || !strings.Contains(r[0], "does not exist") {
		t.Errorf("tool results %q", r)
	}
}

// Spec: "Allow once" on the trust dialog starts the project server and
// the call runs.
func TestAIMCPProjectTrustOnceBehavior(t *testing.T) {
	f := newMCPFake(t)
	s, srv := fakeToolModel(t, toolCall{"mcp__proj__echo", map[string]any{"text": "a"}})
	d := mcpDriver(t, srv.URL, "", f.config("proj"), "")
	askAI(d, "use the project server")
	if !d.WaitForText("Start MCP server proj?", 15*time.Second) {
		t.Fatalf("no trust dialog:\n%s", screen(d))
	}
	clickTextOn(d, "Allow once")
	waitDone(t, d)
	if r := s.toolResults(); len(r) != 1 || !strings.HasPrefix(r[0], "echo: a") {
		t.Errorf("tool results %q", r)
	}
}

// Spec: with mcp_idle_timeout = 1s the server stops by itself after the
// call (no process left), and AI: MCP Servers… lists it as stopped.
func TestAIMCPIdleStopBehavior(t *testing.T) {
	f := newMCPFake(t)
	_, srv := fakeToolModel(t, toolCall{"mcp__fake__echo", map[string]any{"text": "x"}})
	d := mcpDriver(t, srv.URL, f.config("fake"), "", `"mcp_idle_timeout":"1s"`)
	askAI(d, "echo")
	waitDone(t, d)
	if !d.WaitFor(func() bool { return !f.running() && strings.Contains(f.received(), "call echo") }, 10*time.Second) {
		t.Fatalf("server still running after the idle timeout (log %q)", f.received())
	}
	runPalette(d, "AI: MCP Servers")
	if !d.WaitForText("○ fake", 5*time.Second) || !strings.Contains(screen(d), "local · stopped · 10") {
		t.Errorf("servers list:\n%s", screen(d))
	}
}

// Spec (rug pull): when a server's tool list differs from the one the
// user trusted, the next turn asks again ("its tools changed"). "Not
// now" keeps the tools, but each one asks first, even a read-only one,
// with no "Always allow"; the servers list says "tools changed".
func TestAIMCPToolsChangedBehavior(t *testing.T) {
	f := newMCPFake(t)
	s, srv := fakeToolModel(t, toolCall{"mcp__fake__echo", map[string]any{"text": "v2"}})
	cfg := mcptest.Config(map[string]map[string]string{"fake": {"FAKE_LOG": f.log, "FAKE_TOOLSET": "2"}})
	d := mcpDriver(t, srv.URL, cfg, "", `"mcp_trusted_tools":{"fake":"an-older-tool-list"}`)
	askAI(d, "echo")
	if !d.WaitForText("its tools changed", 15*time.Second) {
		t.Fatalf("no tools-changed dialog:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "Show tools") {
		t.Errorf("dialog does not say how to review:\n%s", screen(d))
	}
	clickTextOn(d, "Not now")
	if !d.WaitForText("AI wants to: mcp.fake.echo", 15*time.Second) {
		t.Fatalf("a read-only tool of a changed server did not ask:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "Always allow") {
		t.Errorf("changed server offers Always allow:\n%s", screen(d))
	}
	clickAIAllow(d)
	waitDone(t, d)
	if r := s.toolResults(); len(r) != 1 || !strings.HasPrefix(r[0], "echo: v2") {
		t.Errorf("tool results %q", r)
	}
	runPalette(d, "AI: MCP Servers")
	if !d.WaitForText("tools changed", 5*time.Second) {
		t.Errorf("servers list does not say tools changed:\n%s", screen(d))
	}
}

// runPalette runs a palette command by its title.
func runPalette(d *Driver, title string) {
	d.T.Helper()
	d.Key("f1")
	d.Type(title)
	if !d.WaitForText(title, 3*time.Second) {
		d.T.Fatalf("palette has no %q:\n%s", title, screen(d))
	}
	d.Key("enter")
}
