package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// PR-37: a message an MCP server pushes (a channel) shows in the chat as a
// note and a toast, only for the servers of mcp_channels. It is never sent
// to the model by itself.
func TestMCPChannelMessageBecomesANote(t *testing.T) {
	m := covMCPModel(t)
	cmd := m.onAIMCPChannel(aiMCPChannelMsg{server: "ci", content: "build failed on main\nkey sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789", meta: map[string]string{"severity": "high"}})
	var note string
	for _, sm := range m.aiS().conv.Messages {
		if sm.Role == ai.StoredNote {
			note = sm.Text
		}
	}
	if !strings.Contains(note, "ci") || !strings.Contains(note, "build failed on main") || !strings.Contains(note, "severity") {
		t.Fatalf("note = %q", note)
	}
	if strings.Contains(note, "abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatalf("a secret reached the chat: %q", note)
	}
	if cmd == nil {
		t.Fatal("no toast and no wait for the next message")
	}
	for _, sm := range m.aiS().conv.Messages {
		if sm.Role == ai.StoredUser {
			t.Fatal("a pushed message was sent as the user's message")
		}
	}
}

func TestMCPChannelsAreOffUntilListed(t *testing.T) {
	covMCPModel(t)
	if mcpChannelAllowed("ci") {
		t.Fatal("a server may push before mcp_channels lists it")
	}
	if err := os.MkdirAll(filepath.Dir(mcpConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcpConfigPath(), []byte(`{"mcp_channels":["ci"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !mcpChannelAllowed("ci") || mcpChannelAllowed("other") {
		t.Fatal("mcp_channels is not read per server")
	}
}

// The wait for pushed MCP messages ends when its Model closes: a closed
// Model (each test, each testkit restart) leaves no goroutine behind to
// eat the next Model's messages (NEW-5: thousands piled up in a nightly).
func TestMCPChannelWaitEndsOnClose(t *testing.T) {
	m := &Model{crash: newCrashUI()}
	wait := m.mcpChannelWaitCmd()
	got := make(chan tea.Msg, 1)
	go func() { got <- wait() }()
	m.crash.close()
	select {
	case msg := <-got:
		if msg != nil {
			t.Fatalf("closed wait returned %T, want nil", msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the wait for pushed messages outlived its Model")
	}
}
