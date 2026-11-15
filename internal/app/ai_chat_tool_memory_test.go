package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The built-in chat keeps what its tools gave across turns: the next
// turn's request has the earlier turn's tool calls and results as native
// messages (not only the one-line "[tools I ran]" row), so the model does
// not have to read the same file again.

func readCall(id, path string) ai.Chunk {
	return ai.Chunk{ToolCalls: []ai.ToolCall{{ID: id, Name: "read_file", Args: map[string]any{"path": path}}}}
}

// toolResultIn is the content of the tool result for call id in msgs
// ("" when there is none), and whether an assistant message before it
// asked for that call.
func toolResultIn(msgs []ai.Message, id string) (string, bool) {
	asked := false
	for _, m := range msgs {
		if m.Role == ai.RoleAssistant {
			for _, c := range m.ToolCalls {
				asked = asked || c.ID == id
			}
		}
		if m.Role == ai.RoleTool && m.ToolCallID == id {
			return m.Content, asked
		}
	}
	return "", asked
}

func TestAIChatKeepsToolResultsAcrossTurns(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{
		readCall("c1", "a.txt"), {Delta: "It holds the marker."},
		{Delta: "The marker is marker-7781."},
	}}
	m, dir := aiIDETestModel(t, p)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line one: marker-7781\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	driveAIChat(t, m, m.sendAIChat("read a.txt"), nil)
	driveAIChat(t, m, m.sendAIChat("what exactly was the marker?"), nil)

	p.mu.Lock()
	turn2 := p.seen[len(p.seen)-1]
	p.mu.Unlock()
	res, asked := toolResultIn(turn2, "c1")
	if !asked || !strings.Contains(res, "marker-7781") {
		t.Fatalf("turn 2's request misses turn 1's tool call / result (asked %v, result %q):\n%+v", asked, res, turn2)
	}
	if last := turn2[len(turn2)-1]; last.Role != ai.RoleUser || !strings.Contains(last.Content, "what exactly was the marker?") {
		t.Errorf("the new message is not last: %+v", last)
	}

	// The chat file keeps it: a reload rebuilds the same history.
	saved, err := m.ai.store.Load(m.ai.conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	m.ai.conv = *saved
	if res, _ := toolResultIn(m.aiChatTurnHistory("sys", true, m.ai.cfg.Kind), "c1"); !strings.Contains(res, "marker-7781") {
		t.Errorf("after a reload the history misses the tool result %q", res)
	}
}

// Only the last ai_tool_turns turns keep their tool results in full;
// older ones shrink to the one-line rows.
func TestAIChatOlderTurnsShrinkToolResults(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{
		readCall("c1", "a.txt"), {Delta: "read a"},
		readCall("c2", "b.txt"), {Delta: "read b"},
		{Delta: "both read"},
	}}
	m, dir := aiIDETestModel(t, p)
	one := 1
	m.ai.settings.ToolTurns = &one
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("content of "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	driveAIChat(t, m, m.sendAIChat("read a.txt"), nil)
	driveAIChat(t, m, m.sendAIChat("read b.txt"), nil)
	driveAIChat(t, m, m.sendAIChat("compare them"), nil)
	p.mu.Lock()
	turn3 := p.seen[len(p.seen)-1]
	p.mu.Unlock()
	if res, _ := toolResultIn(turn3, "c2"); !strings.Contains(res, "content of b.txt") {
		t.Errorf("the last turn's tool result is not kept: %q", res)
	}
	if res, asked := toolResultIn(turn3, "c1"); res != "" || asked {
		t.Errorf("an older turn kept its native tool messages: %q", res)
	}
	found := false
	for _, msg := range turn3 {
		found = found || (msg.Role == ai.RoleAssistant && strings.Contains(msg.Content, "[tools I ran]") && strings.Contains(msg.Content, "a.txt"))
	}
	if !found {
		t.Errorf("the older turn has no one-line tool row:\n%+v", turn3)
	}
	// The chat file keeps native messages only for the turns that use
	// them.
	if n := len(m.ai.conv.Messages); n > 0 {
		for i, sm := range m.ai.conv.Messages {
			if sm.Role == ai.StoredUser && strings.HasPrefix(sm.Text, "read a.txt") && len(sm.Native) > 0 {
				t.Errorf("message %d still stores %d native messages", i, len(sm.Native))
			}
		}
	}
}

// A chat saved before this change (no native messages) loads and
// rebuilds the one-line form, as before.
func TestAIChatOldFileToolRows(t *testing.T) {
	m, _ := aiIDETestModel(t, &scriptedAIProvider{})
	old := `{"id":"old","title":"t","messages":[
{"role":"user","text":"read a.txt"},
{"role":"tool","text":"⚙ Read a.txt → 1 line"},
{"role":"assistant","text":"It holds the marker."},
{"role":"user","text":"and now?"}]}`
	var c ai.Conversation
	if err := json.Unmarshal([]byte(old), &c); err != nil {
		t.Fatal(err)
	}
	m.ai.conv = c
	h := m.aiChatTurnHistory("sys", true, m.ai.cfg.Kind)
	if len(h) != 4 || h[2].Role != ai.RoleAssistant || h[2].Content != "[tools I ran]\n⚙ Read a.txt → 1 line\n\nIt holds the marker." {
		t.Fatalf("history %+v", h)
	}
}
