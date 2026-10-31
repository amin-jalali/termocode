package app

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// startBgCall runs one MCP tool of the turn's background set until
// release is closed.
func startBgCall(t *testing.T, bg *mcp.Background) (release chan struct{}, out chan string) {
	t.Helper()
	release, started, out := make(chan struct{}), make(chan struct{}), make(chan string, 1)
	call := func(ctx context.Context, _, _ string, _ map[string]any) (mcp.CallResult, error) {
		close(started)
		<-release
		return mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: "build ok"}}}, nil
	}
	tool := mcp.AITools(mcp.Server{Name: "ci"}, []mcp.Tool{{Name: "build"}}, call, mcp.AdapterOptions{Background: bg})[0]
	go func() { s, _ := tool.Run(context.Background(), nil); out <- s }()
	<-started
	return release, out
}

// MCPX-19: while an MCP call runs, the activity line offers Ctrl+B;
// Ctrl+B moves it to the background (the model goes on); its result
// shows in the chat and goes with the next message.
func TestAIMCPBackgroundKey(t *testing.T) {
	m := covMCPModel(t)
	s := m.aiS()
	s.focused, s.streaming = true, true
	m.actionsOpen, m.actionsPinned = true, true
	bg := mcp.NewBackground()
	s.mcpx.bg = bg
	release, out := startBgCall(t, bg)
	s.act.onEvent(ai.Event{Kind: ai.EventToolStart, Label: "mcp.ci.build"}, s.act.since)
	if v := s.act.view(s.act.since.Add(6e9)); !s.withBgHint(v).bg {
		t.Fatal("no Ctrl+B hint while an MCP call runs")
	}
	cmd, ok := m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	if !ok || cmd == nil {
		t.Fatal("Ctrl+B not taken while an MCP call runs")
	}
	if s := <-out; !strings.Contains(s, "still running in the background") {
		t.Fatalf("model got %q", s)
	}
	close(release)
	done, ok := covCollect[aiMCPBackgroundMsg](cmd)
	if !ok || done.res.Label != "mcp.ci.build" {
		t.Fatalf("done %+v", done)
	}
	m.onAIMCPBackground(done)
	last := s.conv.Messages[len(s.conv.Messages)-1]
	if last.Role != ai.StoredTool || !strings.Contains(last.Text, "mcp.ci.build (background) → build ok") {
		t.Fatalf("chat line %+v", last)
	}
	if len(s.attachments) != 1 || !strings.Contains(s.attachments[0].Text, "build ok") {
		t.Fatalf("attachments %+v", s.attachments)
	}
	// Nothing runs: Ctrl+B is the global key again.
	if _, ok := m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyCtrlB}); ok {
		t.Error("Ctrl+B taken with nothing to move")
	}
}
