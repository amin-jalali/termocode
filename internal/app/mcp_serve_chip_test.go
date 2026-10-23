package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/statusbar"
)

// mcpServeChipModel is a serving model with a status bar.
func mcpServeChipModel(t *testing.T) (*Model, *mcpServeHub) {
	t.Helper()
	m, _, hub := mcpServeTestModel(t)
	m.w, m.h = 160, 40
	m.status = statusbar.New()
	return m, hub
}

func mcpServeChipText(m *Model) string {
	_, bw := m.statusBarRect()
	sb := m.status
	sb.SetWidth(bw)
	return ansi.Strip(sb.View(m.statusState()))
}

// Spec (MS-3): the status bar shows "⇄ n agent(s)" while outside agents
// are connected and nothing otherwise; it follows connect and disconnect
// events only (no timer: the events return no tick).
func TestMCPServeChipFollowsEvents(t *testing.T) {
	m, hub := mcpServeChipModel(t)
	if strings.Contains(mcpServeChipText(m), "agent") || m.statusState().Agents != 0 {
		t.Fatal("a chip with no agent")
	}
	m.onMCPServeClient(mcpServeClientMsg{hub: hub, kind: mcpClientConnected, id: 1})
	if st := m.statusState(); st.Agents != 1 {
		t.Fatalf("Agents = %d after a connect", st.Agents)
	}
	if got := mcpServeChipText(m); !strings.Contains(got, " 1 agent") {
		t.Errorf("no chip after a connect: %q", got)
	}
	m.onMCPServeClient(mcpServeClientMsg{hub: hub, kind: mcpClientConnected, id: 2})
	if got := mcpServeChipText(m); !strings.Contains(got, " 2 agents") {
		t.Errorf("no plural chip: %q", got)
	}
	m.onMCPServeClient(mcpServeClientMsg{hub: hub, kind: mcpClientGone, id: 1})
	m.onMCPServeClient(mcpServeClientMsg{hub: hub, kind: mcpClientGone, id: 2})
	if strings.Contains(mcpServeChipText(m), "agent") {
		t.Errorf("the chip stayed after the last agent left: %q", mcpServeChipText(m))
	}
	if m.mcpServe.ticking {
		t.Error("the events started a ticker")
	}
	// A stopped server shows nothing, and an event of an old hub is ignored.
	m.onMCPServeClient(mcpServeClientMsg{hub: hub, kind: mcpClientConnected, id: 3})
	m.stopMCPServe()
	if m.statusState().Agents != 0 {
		t.Error("a chip for a stopped server")
	}
	m.onMCPServeClient(mcpServeClientMsg{hub: hub, kind: mcpClientConnected, id: 4})
	if m.mcpServe != nil {
		t.Error("an old hub's event revived the server")
	}
}

// Spec (MS-3): a click on the chip opens the status screen.
func TestMCPServeChipClickOpensStatus(t *testing.T) {
	m, hub := mcpServeChipModel(t)
	m.onMCPServeClient(mcpServeClientMsg{hub: hub, kind: mcpClientConnected, id: 1})
	var span statusSpan
	for _, sp := range m.statusSpans() {
		if sp.id == "status:agents" {
			span = sp
		}
	}
	if span.id == "" {
		t.Fatalf("no agents span in %v", m.statusSpans())
	}
	bx, _ := m.statusBarRect()
	x := bx + span.x0 + 1
	if got := m.statusHitAt(x, m.h-1); got != "status:agents" {
		t.Fatalf("hit = %q", got)
	}
	got, _ := m.handleMouse(tea.MouseMsg{X: x, Y: m.h - 1, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !got.pickerOpen || got.pickerKind != pickerKindMCPServeStatus {
		t.Errorf("the click did not open the status screen (picker %v kind %v)", got.pickerOpen, got.pickerKind)
	}
}

// Spec (MCP-5): while the server listens with no agent the bar shows the
// quiet "ready" chip; an agent replaces it with the count; a stopped
// server shows nothing. Still event-driven: no ticker.
func TestMCPServeChipReadyWhileListening(t *testing.T) {
	m, hub := mcpServeChipModel(t)
	if got := mcpServeChipText(m); !strings.Contains(got, "ready") {
		t.Fatalf("no ready chip while listening: %q", got)
	}
	m.onMCPServeClient(mcpServeClientMsg{hub: hub, kind: mcpClientConnected, id: 1})
	if got := mcpServeChipText(m); strings.Contains(got, "ready") || !strings.Contains(got, " 1 agent") {
		t.Errorf("a connected agent: %q", got)
	}
	m.onMCPServeClient(mcpServeClientMsg{hub: hub, kind: mcpClientGone, id: 1})
	if got := mcpServeChipText(m); !strings.Contains(got, "ready") {
		t.Errorf("no ready chip after the agent left: %q", got)
	}
	if m.mcpServe.ticking {
		t.Error("the chip started a ticker")
	}
	m.stopMCPServe()
	if got := mcpServeChipText(m); strings.Contains(got, "ready") {
		t.Errorf("a ready chip for a stopped server: %q", got)
	}
}
