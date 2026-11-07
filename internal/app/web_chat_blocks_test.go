package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/webproto"
)

func webTestBlock(id string) *blocks.Block {
	return &blocks.Block{ID: id, Kind: blocks.KindList, Title: "Matches", Source: "search",
		Items: []blocks.BlockItem{{ID: "1", Title: "a.go", Sub: "line 3"}}}
}

// MOB-10: a tool line with a block goes to the page with the block in
// ChatMsg.Blocks; a line without one has none.
func TestChatToolLineCarriesBlock(t *testing.T) {
	m := webTestChatModel(t,
		ai.StoredMessage{Role: ai.StoredUser, Text: "find it"},
		ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ Search → 1 match", Block: webTestBlock("call-1")},
		ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ Read a.go → 10 lines"},
	)
	webTestHub(t, &m)
	c := webTestSub(t, &m, "chat")
	var s webproto.ChatSnap
	webTestData(t, webTestNow(t, c)[0], &s)
	tl := s.Messages[1]
	if tl.Text != "⚙ Search → 1 match" || len(tl.Blocks) != 1 || tl.Blocks[0].ID != "call-1" || tl.Blocks[0].Items[0].Title != "a.go" {
		t.Errorf("tool line %+v", tl)
	}
	if len(s.Messages[2].Blocks) != 0 || len(s.Messages[0].Blocks) != 0 {
		t.Errorf("blocks where none were: %+v / %+v", s.Messages[0], s.Messages[2])
	}
}

// A registered secret in a stored block is redacted before it leaves,
// and a stored block that no longer passes the caps is dropped (the line
// still goes).
func TestChatBlockRedactedAndChecked(t *testing.T) {
	secret := "chat-block-secret-9911"
	ai.RegisterSecret(secret)
	b := webTestBlock("b1")
	b.Items[0].Sub = "token " + secret
	bad := &blocks.Block{ID: "b2", Kind: "hologram", Title: "x"}
	cm, _ := webChatMsg(0, &ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ env", Block: b}, false)
	if len(cm.Blocks) != 1 || strings.Contains(cm.Blocks[0].Items[0].Sub, secret) {
		t.Errorf("block %+v", cm.Blocks)
	}
	if b.Items[0].Sub != "token "+secret {
		t.Error("the stored block was changed")
	}
	cm, _ = webChatMsg(1, &ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ odd", Block: bad}, false)
	if len(cm.Blocks) != 0 || cm.Text != "⚙ odd" {
		t.Errorf("bad block sent: %+v", cm)
	}
}

// A block added to a line already sent changes its key, so the line goes
// out again with the block.
func TestChatKeySeesBlock(t *testing.T) {
	sm := ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ Search → 1 match"}
	before := webChatKey(&sm)
	sm.Block = webTestBlock("k1")
	if webChatKey(&sm) == before {
		t.Error("key did not change with the block")
	}
}

// The agent's result event with a View stores the block on the tool
// line of the transcript.
func TestAIChatEventStoresBlock(t *testing.T) {
	m := aiTestModel(t)
	s := m.aiS()
	m.onAIChatEvent(aiChatEventMsg{gen: s.chatGen, ev: ai.Event{Kind: ai.EventToolStart, Tool: "search", Detail: "Search x"}})
	m.onAIChatEvent(aiChatEventMsg{gen: s.chatGen, ev: ai.Event{Kind: ai.EventToolResult, Tool: "search", Detail: "a.go:3", View: webTestBlock("c9")}})
	msgs := s.conv.Messages
	l := msgs[len(msgs)-1]
	if l.Role != ai.StoredTool || l.Block == nil || l.Block.ID != "c9" {
		t.Errorf("tool line %+v", l)
	}
}

// The block attach: the page sends a block id it saw in the open chat;
// the laptop gives the model the block as text (redacted), newest
// first; an unknown id is gone.
func TestAttachBlockResolves(t *testing.T) {
	secret := "attach-block-secret-5150"
	ai.RegisterSecret(secret)
	old := webTestBlock("dup")
	old.Title = "Old"
	b := webTestBlock("dup")
	b.Items[0].Sub = secret
	m := webTestChatModel(t,
		ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ a", Block: old},
		ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ b", Block: b},
	)
	text, ok := m.webChatBlockText("dup")
	if !ok || !strings.Contains(text, "Matches (list, from search)") || !strings.Contains(text, "a.go") {
		t.Fatalf("text %q %v", text, ok)
	}
	if strings.Contains(text, secret) || strings.Contains(text, "Old") {
		t.Errorf("text %q", text)
	}
	if len(text) > webproto.MaxAttachBytes {
		t.Errorf("text is %d bytes", len(text))
	}
	if _, ok := m.webChatBlockText("nope"); ok {
		t.Error("unknown block resolved")
	}
	if _, ok := m.webChatBlockText(""); ok {
		t.Error("empty id resolved")
	}
}

// chat.send with a block attach gives the model the block's text; a
// block the open chat does not have is gone.
func TestAttachPlanBlock(t *testing.T) {
	m := webTestChatModel(t, ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ a", Block: webTestBlock("b7")})
	j, err := m.webAttachPlan(&webproto.Attach{Kind: webproto.AttachBlock, ID: "b7", Label: "Matches"})
	if err != nil || !strings.Contains(j.text, "Matches (list, from search)") {
		t.Errorf("job %+v err %v", j, err)
	}
	if _, err := m.webAttachPlan(&webproto.Attach{Kind: webproto.AttachBlock, ID: "gone", Label: "x"}); err == nil {
		t.Error("an unknown block resolved")
	}
}
