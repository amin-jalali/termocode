package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
)

// The owner's chat runs its turns as profile steps (a Codex CLI step
// calls termocode's tools through the step's MCP server): a tool's card
// must stay on the step too, or the chat shows only "⚙ db_connections →
// 6 lines". The step keeps the blocks of its tool calls; the desktop
// chat draws the database card on the step, the web page gets it in the
// step's blocks, and a row of it can be added to the chat.

func dbCardTestBlock(id string) *blocks.Block {
	out := "rentcar-db-1 · postgres rentcar-db-1:5432/rentcar · environment development · status: missing info: password, nothing saved · " +
		"next: Fix… on the card sets the password; then Connect · where: container rentcar-db-1 on context Baaytona (ssh://deploy@65.109.212.14:2222) · remote: yes · file: user\n"
	b := dbConnectionsView(nil, out)
	b.ID, b.Source = id, "db_connections"
	return b
}

func TestAIRunStepKeepsTheDBCard(t *testing.T) {
	r := newStallRig(t)
	r.ev(agentrun.NodeTool, ai.Event{Kind: ai.EventToolStart, Tool: "db_connections", Label: "db_connections", Detail: "List database connections"}, "left")
	r.ev(agentrun.NodeTool, ai.Event{Kind: ai.EventToolResult, Tool: "db_connections", Label: "db_connections", Detail: "1 connection", View: dbCardTestBlock("call_7")}, "left")
	st := aiFindStep(&r.m.ai.conv.Messages[r.m.ai.runMsg], "left")
	if st == nil || len(st.Blocks) != 1 || st.Blocks[0].ID != "call_7" {
		t.Fatalf("step blocks = %+v", st)
	}
	var text strings.Builder
	var ids []string
	for _, l := range r.m.buildAITranscriptAt(80, aiActivityView{}) {
		text.WriteString(l.text + ansi.Strip(l.styled) + "\n")
		for _, b := range l.buttons {
			ids = append(ids, b.id)
		}
	}
	if !strings.Contains(text.String(), "missing password") || !strings.Contains(strings.Join(ids, " "), "dbcard:fix_password:rentcar-db-1") {
		t.Errorf("the step shows no database card:\n%s\n%v", text.String(), ids)
	}
	ws := webChatStep(st)
	if len(ws.Blocks) != 1 || ws.Blocks[0].Source != "db_connections" || len(ws.Blocks[0].Items) != 1 {
		t.Errorf("web step blocks = %+v", ws.Blocks)
	}
	// Another tool without a card adds none; at most MaxMsgBlocks stay.
	for i := 0; i < 12; i++ {
		r.ev(agentrun.NodeTool, ai.Event{Kind: ai.EventToolStart, Tool: "db_connections", Detail: "List"}, "left")
		r.ev(agentrun.NodeTool, ai.Event{Kind: ai.EventToolResult, Tool: "db_connections", Detail: "1", View: dbCardTestBlock("c" + itoa(i))}, "left")
	}
	st = aiFindStep(&r.m.ai.conv.Messages[r.m.ai.runMsg], "left")
	if n := len(st.Blocks); n != 8 || st.Blocks[n-1].ID != "c11" {
		t.Errorf("kept %d blocks, last %q", n, st.Blocks[n-1].ID)
	}
}

// A step's block changes the message's key, so the page gets it.
func TestChatKeySeesStepBlock(t *testing.T) {
	sm := ai.StoredMessage{Role: ai.StoredAssistant, Steps: []ai.StoredStep{{ID: "s1", Tools: []string{"⚙ db_connections → 1"}}}}
	before := webChatKey(&sm)
	sm.Steps[0].Blocks = []blocks.Block{*dbCardTestBlock("call_1")}
	if webChatKey(&sm) == before {
		t.Error("key did not change with the step's block")
	}
}

// "Add to chat" on a row of a card: the page sends block/item; the
// model gets that row, and for a database card the connection as an
// item (its id and the tools that read it), on a tool line or a step.
func TestAttachBlockItemResolves(t *testing.T) {
	step := ai.StoredMessage{Role: ai.StoredAssistant, Steps: []ai.StoredStep{{ID: "s1", Blocks: []blocks.Block{*dbCardTestBlock("call_s")}}}}
	other := webTestBlock("srch")
	m := webTestChatModel(t,
		ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ db_connections", Block: dbCardTestBlock("call_1")},
		ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ search", Block: other},
		step,
	)
	for _, id := range []string{"call_1/rentcar-db-1", "call_s/rentcar-db-1"} {
		text, ok := m.webChatBlockText(id)
		if !ok || !strings.Contains(text, "database connection rentcar-db-1") || !strings.Contains(text, `db_schema {"conn":"rentcar-db-1"}`) {
			t.Errorf("%s: %q %v", id, text, ok)
		}
	}
	text, ok := m.webChatBlockText("srch/" + other.Items[0].ID)
	if !ok || !strings.Contains(text, other.Items[0].Title) {
		t.Errorf("item of another block: %q %v", text, ok)
	}
	if _, ok := m.webChatBlockText("call_1/nope"); ok {
		t.Error("an unknown row resolved")
	}
	if _, ok := m.webChatBlockText("call_s"); !ok {
		t.Error("a step's block does not resolve")
	}
}
