package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

func writeUserMCP(t *testing.T, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(mcpUserPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcpUserPath(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Spec (CX-6): with no service configured the chat header chip reads
// "⌁ Connect…" and a click starts the Connect wizard (not the servers
// list).
func TestMCPChipConnect(t *testing.T) {
	m := aiTestModel(t)
	m.actionsOpen, m.actionsPinned = true, true
	m.aiMCP()
	head := ansi.Strip(m.renderActionsHeader(60))
	i := strings.Index(head, mcpGlyph()+" Connect…")
	if i < 0 {
		t.Fatalf("no Connect chip in %q", head)
	}
	x := m.w - m.actionsColumnWidth() + ansi.StringWidth(head[:i]) + 2
	if id := m.aiPanelTargetAt(x, 0); id != "ai:mcp" {
		t.Fatalf("target at chip = %q", id)
	}
	cmd, _ := m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 0, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil || m.pickerOpen {
		t.Errorf("the Connect chip did not start the wizard (cmd %v, picker %v)", cmd != nil, m.pickerOpen)
	}
}

// Spec: a server that answered 401 makes the chip "⌁ 1 needs sign-in"
// and the row above the chat input offers "Sign in" and "Not now".
func TestMCPChipNeedsSignIn(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer()})
	m := aiTestModel(t)
	m.actionsOpen, m.actionsPinned = true, true
	writeUserMCP(t, `{"mcpServers":{"github":{"type":"http","url":"`+srv.MCPURL()+`"}}}`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = m.aiMCP().Connect(ctx, nil, "github")
	if head := ansi.Strip(m.renderActionsHeader(60)); !strings.Contains(head, mcpGlyph()+" 1 needs sign-in") {
		t.Fatalf("chip in %q", head)
	}
	line := m.aiDividerLine(60)
	plain := ansi.Strip(line.styled)
	if !strings.Contains(plain, "GitHub needs a sign-in") || !strings.Contains(plain, "Sign in") || !strings.Contains(plain, "Not now") {
		t.Fatalf("divider %q", plain)
	}
	ids := map[string]bool{}
	for _, b := range line.buttons {
		ids[b.id] = true
	}
	if !ids["mcp-signin:github"] || !ids["mcp-notnow:github"] {
		t.Fatalf("buttons %+v", line.buttons)
	}
	m.onAIButton("mcp-notnow:github")
	if strings.Contains(ansi.Strip(m.aiDividerLine(60).styled), "needs a sign-in") {
		t.Error("Not now kept the row")
	}
}

// Spec: the setup card shows a Services step only once AI is set up;
// it is optional and done when a service is connected or skipped.
func TestSetupServicesStep(t *testing.T) {
	f := setupFacts{settled: map[string]bool{}}
	for _, s := range buildSetupSteps(f) {
		if s.id == "services" {
			t.Fatal("services step before AI is set up")
		}
	}
	f.aiConfigured, f.aiProvider = true, "Ollama"
	var st setupStep
	for _, s := range buildSetupSteps(f) {
		if s.id == "services" {
			st = s
		}
	}
	if st.id == "" || !st.optional || st.done || st.action != "Connect…" || !strings.Contains(st.status, "GitHub") {
		t.Fatalf("step %+v", st)
	}
	f.services = 2
	for _, s := range buildSetupSteps(f) {
		if s.id == "services" && (!s.done || !strings.Contains(s.status, "2 connected")) {
			t.Errorf("connected: %+v", s)
		}
	}
}

// Spec: the palette row is found by "github" and "slack" and has an AI
// action policy (TestAIActionPolicyCoversEveryPaletteID checks all).
func TestConnectPaletteRow(t *testing.T) {
	for _, e := range aiPaletteItems() {
		if e.id != "ai-mcp-connect" {
			continue
		}
		low := strings.ToLower(e.title)
		if !strings.Contains(low, "github") || !strings.Contains(low, "slack") || !strings.Contains(low, "connect a service") {
			t.Errorf("title %q", e.title)
		}
		if _, ok := aiActionPolicies[e.id]; !ok {
			t.Error("no policy")
		}
		return
	}
	t.Fatal("no ai-mcp-connect row")
}
