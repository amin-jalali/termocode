package webproto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// Spec (MOB-3): a chat message and a step carry typed blocks, the page
// gets their types, and a block over the caps never passes Validate.
func TestChatMsgCarriesBlocks(t *testing.T) {
	b := Block{ID: "b1", Kind: blocks.KindStatus, Title: "CI", State: blocks.ToneOK}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	m := ChatMsg{I: 0, Role: "assistant", Blocks: []Block{b}, Steps: []ChatStep{{ID: "s", Blocks: []Block{b}}}}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"blocks":[{"id":"b1","kind":"status","title":"CI","state":"ok"}]`) {
		t.Errorf("message JSON: %s", raw)
	}
	var back ChatMsg
	if err := json.Unmarshal(raw, &back); err != nil || len(back.Blocks) != 1 || len(back.Steps[0].Blocks) != 1 {
		t.Errorf("round trip: %+v %v", back, err)
	}
	empty, _ := json.Marshal(ChatMsg{Role: "user"})
	if strings.Contains(string(empty), "blocks") {
		t.Errorf("a message without blocks names them: %s", empty)
	}
}

func TestProtoHasBlockTypes(t *testing.T) {
	ts, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"export interface Block {",
		`kind: "list" | "table" | "diff" | "card" | "detail" | "chart" | "status" | "form" | "json" | "text" | "board";`,
		"export interface ViewSpec {", "export interface BlockBoardColumn {", "export interface MdNode {",
	} {
		if !strings.Contains(string(ts), want) {
			t.Errorf("proto.d.ts has no %q", want)
		}
	}
	if n := strings.Count(string(ts), "  blocks?: Block[];"); n != 2 {
		t.Errorf("blocks on %d types, want ChatMsg and ChatStep", n)
	}
	sc, err := GenerateSchema()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]json.RawMessage `json:"defs"`
	}
	if err := json.Unmarshal(sc, &doc); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"Block", "BlockItem", "BlockDiff", "BlockFormField", "ViewSpec", "ViewCond"} {
		if _, ok := doc.Defs[n]; !ok {
			t.Errorf("schema.json has no def %s", n)
		}
	}
}

// The guard: a block over a cap is refused, as webproto's own error.
func TestBlockOverCapsIsRefused(t *testing.T) {
	items := make([]BlockItem, blocks.MaxItems+1)
	for i := range items {
		items[i] = BlockItem{ID: "i", Title: "t"}
	}
	b := Block{ID: "b", Kind: blocks.KindList, Title: "x", Items: items}
	if err := b.Validate(); err == nil {
		t.Error("a list of 201 items passed")
	}
}
