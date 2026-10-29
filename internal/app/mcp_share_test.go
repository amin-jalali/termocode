//go:build unix

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// Spec (G-MCP): a shared server's row says "shared · N sessions" after
// the warnings and the state, before the pid.
func TestMCPStatusHintShared(t *testing.T) {
	st := mcp.Status{Name: "fake", Type: mcp.TypeStdio, State: mcp.StateRunning, Pid: 42, Tools: 5,
		ToolsChanged: true, Shared: true, Sessions: 3}
	want := "tools changed · not trusted · local · running · shared · 3 sessions · pid 42 · 5 tools"
	if h := mcpStatusHint(st); h != want {
		t.Fatalf("hint %q\nwant %q", h, want)
	}
	st.Sessions, st.ToolsChanged, st.Trusted = 1, false, true
	if h := mcpStatusHint(st); !strings.HasPrefix(h, "local · running · shared · 1 session · pid 42") {
		t.Fatalf("hint %q", h)
	}
	st.Shared = false
	if h := mcpStatusHint(st); strings.Contains(h, "shared") {
		t.Fatalf("hint %q", h)
	}
}

// useShareBroker makes the app's managers share servers through this
// test binary, in a temp socket folder.
func useShareBroker(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "tcs")
	if err != nil {
		t.Fatal(err)
	}
	// As long as macOS's $TMPDIR (/var/folders/…/T/): the sockets must
	// still work (a short /tmp folder), on every system.
	share := filepath.Join(dir, "var", "folders", "n6", strings.Repeat("x", 30), "T", "mcp")
	oldExe, oldArgs, oldDir := mcpShareExe, mcpShareArgs, mcpShareDir
	mcpShareExe, mcpShareArgs, mcpShareDir = os.Args[0], []string{"-test.run=^$"}, share
	t.Cleanup(func() {
		mcpShareExe, mcpShareArgs, mcpShareDir = oldExe, oldArgs, oldDir
		_ = os.RemoveAll(dir)
		if short := mcp.ShareSocketDir(share); short != share {
			_ = os.RemoveAll(short)
		}
	})
}

// connectFake starts the fake server in m's session (the tool cache
// alone would not start it).
func connectFake(t *testing.T, m *Model) {
	t.Helper()
	if err := m.aiMCP().Connect(context.Background(), nil, "fake"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.aiMCP().Close() })
}

// Spec (G-MCP): Stop on a server two sessions share asks first ("this
// stops it for 2 sessions"); Cancel is first and focused and keeps it;
// "Stop for all" stops it for the other session too.
func TestMCPStopSharedAsksFirst(t *testing.T) {
	useShareBroker(t)
	a := aiTestModel(t)
	writeUserMCP(t, fakeServiceEntry(nil))
	b := Model{w: 120, h: 40, ai: newAIState()}
	connectFake(t, &a)
	connectFake(t, &b)
	deadline := time.Now().Add(10 * time.Second)
	for serverStatus(&a, "fake").Sessions != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("status %+v", serverStatus(&a, "fake"))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if it := mcpActionIDs(&a, "fake")["stop"]; !strings.Contains(it.Hint, "stops it for 2 sessions") {
		t.Fatalf("Stop hint %q", it.Hint)
	}
	a.aiS().mcpSel = "fake"
	a.onMCPServerAction("stop")
	if !a.confirmOpen || a.confirmKind != confirmKindMCPShareStop {
		t.Fatal("no confirm card")
	}
	if a.confirm.SafeID() != "cancel" || a.confirm.Buttons()[0].ID != "cancel" {
		t.Fatalf("buttons %+v", a.confirm.Buttons())
	}
	if !strings.Contains(a.confirm.View(), "2 sessions") {
		t.Fatal("the card does not say how many sessions")
	}
	_, cmd := a.confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if sel, ok := cmd().(confirm.SelectMsg); !ok || sel.ID != "cancel" {
		t.Fatalf("Enter picked %v", cmd())
	}
	a.answerMCPShareStop("cancel")
	if serverStatus(&b, "fake").State != mcp.StateRunning {
		t.Fatal("Cancel stopped it")
	}
	a.answerMCPShareStop("stop")
	deadline = time.Now().Add(10 * time.Second)
	for serverStatus(&b, "fake").State != mcp.StateStopped {
		if time.Now().After(deadline) {
			t.Fatalf("the other session still runs it: %+v", serverStatus(&b, "fake"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Spec (G-MCP): with one session, Stop does not ask.
func TestMCPStopSharedAloneDoesNotAsk(t *testing.T) {
	useShareBroker(t)
	m := aiTestModel(t)
	writeUserMCP(t, fakeServiceEntry(nil))
	connectFake(t, &m)
	if st := serverStatus(&m, "fake"); !st.Shared || st.Sessions != 1 {
		t.Fatalf("status %+v", st)
	}
	m.aiS().mcpSel = "fake"
	m.onMCPServerAction("stop")
	if m.confirmOpen {
		t.Fatal("asked with one session")
	}
	if st := serverStatus(&m, "fake"); st.State != mcp.StateStopped {
		t.Fatalf("status %+v", st)
	}
}

// Spec (G-MCP): mcp_share false runs the session's own process.
func TestMCPShareSettingOff(t *testing.T) {
	useShareBroker(t)
	m := aiTestModel(t)
	writeUserMCP(t, fakeServiceEntry(nil))
	if err := mcp.SaveSetting(mcpConfigPath(), "mcp_share", false); err != nil {
		t.Fatal(err)
	}
	connectFake(t, &m)
	if st := serverStatus(&m, "fake"); st.Shared || st.Pid == 0 {
		t.Fatalf("status %+v, want its own process", st)
	}
}
