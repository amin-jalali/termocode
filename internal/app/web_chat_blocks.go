package app

import (
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
func (m *Model) webChatBlockText(id string) (text string, ok bool) {
	if id == "" {
		return "", false
	}
	msgs := m.aiS().conv.Messages
	for i := len(msgs) - 1; i >= 0; i-- {
		if b := msgs[i].Block; b != nil && b.ID == id {
			sent := webChatBlock(b)
			if sent == nil {
				return "", false
			}
			head := "A block the user points at, as the chat showed it:\n"
			body := sent.PlainText(webproto.MaxAttachBytes - len(head))
			return head + strings.TrimSpace(ai.RedactSecrets(body)), true
		}
	}
	return "", false
}
