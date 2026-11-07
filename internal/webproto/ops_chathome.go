package webproto

import "strings"

// The phone's own chat (MOB-16, owner Q3): chat.home sends a message to a
// background chat of this workspace, "Phone · <device>", one per paired
// device. The laptop's open chat and view stay as they are; the laptop's
// main model answers with no tools, and the chat is stored like any other
// (the laptop's history lists it). One AI turn at a time per workspace:
// a send while the laptop's AI answers is busy, and so is chat.send while
// the phone chat answers. chat.home {} only reads the chat back (no
// tokens). It answers with the chat (ChatSnap) once the answer is in.

// OpChatHome is the phone chat op.
const OpChatHome = "chat.home"

// ChatHomeArgs is a message for the phone chat; no text reads it.
type ChatHomeArgs struct {
	Text string `json:"text,omitempty"`
}

func (a *ChatHomeArgs) validate(int) error {
	if len(a.Text) > MaxChatText || strings.ContainsRune(a.Text, 0) {
		return invalid("chat text is %d bytes", len(a.Text))
	}
	return nil
}

func init() {
	registerResult(OpChatHome, ChatSnap{})
}
