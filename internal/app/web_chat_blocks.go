package app

import (
	"encoding/json"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Blocks in the chat (MOB-10, mobile plan 3.2): a tool whose View hook
// built a block keeps it on its tool line (ai.StoredMessage.Block); the
// chat topic sends it in ChatMsg.Blocks. The block was redacted and
// checked when it was built; it is redacted and checked again here, as a
// chat file may be older than a secret registered since, or hand-edited.
// A block that fails the caps is dropped: the line still goes.

// webChatBlock is a stored block as the page gets it (nil: none, or it
// does not pass the caps).
func webChatBlock(b *blocks.Block) *blocks.Block {
	if b == nil {
		return nil
	}
	out, err := b.MapStrings(webChatRedact)
	if err != nil || out == nil || out.Validate() != nil {
		return nil
	}
	return out
}

// webChatBlockText resolves a block attach (webproto.AttachBlock): the
// block with this id in the open chat, newest first, as text for the
// model, redacted and cut at MaxAttachBytes. ok is false when no message
// of the open chat has it.
// An id "block/item" is one row of the block (the page's "add to chat"
// on a row): that row, and for a database card its connection as an
// item attach (its id and the tools that read it). A step's blocks
// count too.
func (m *Model) webChatBlockText(id string) (text string, ok bool) {
	if id == "" {
		return "", false
	}
	bid, item, isItem := strings.Cut(id, "/")
	msgs := m.aiS().conv.Messages
	for i := len(msgs) - 1; i >= 0; i-- {
		b := webChatFindBlock(&msgs[i], bid)
		if b == nil {
			continue
		}
		sent := webChatBlock(b)
		if sent == nil {
			return "", false
		}
		if isItem {
			return webChatItemText(sent, item)
		}
		head := "A block the user points at, as the chat showed it:\n"
		body := sent.PlainText(webproto.MaxAttachBytes - len(head))
		return head + strings.TrimSpace(ai.RedactSecrets(body)), true
	}
	return "", false
}

// webChatFindBlock is the block id of message sm: its tool line's, or one
// of its steps'.
func webChatFindBlock(sm *ai.StoredMessage, id string) *blocks.Block {
	if b := sm.Block; b != nil && b.ID == id {
		return b
	}
	for j := range sm.Steps {
		for k := range sm.Steps[j].Blocks {
			if b := &sm.Steps[j].Blocks[k]; b.ID == id {
				return b
			}
		}
	}
	return nil
}

// webChatItemText is row item of block b as the model's text.
func webChatItemText(b *blocks.Block, item string) (string, bool) {
	for _, it := range b.Items {
		if it.ID != item {
			continue
		}
		if r := it.Ref; r != nil && r.Kind == dbCardRefKind && r.ID != "" {
			spec, _ := json.Marshal(webproto.ItemSpec{Item: webproto.ItemDBConn, Conn: r.ID})
			if t, err := webItemText(string(spec)); err == nil {
				return t, true
			}
		}
		row := []string{it.Title}
		for _, s := range []string{it.Badge, it.Sub} {
			if s != "" {
				row = append(row, s)
			}
		}
		head := "A row the user points at, from the chat's " + b.Title + ":\n"
		return head + strings.TrimSpace(ai.RedactSecrets(strings.Join(row, " · "))), true
	}
	return "", false
}
