package ai

import (
	"encoding/json"
	"strconv"
	"strings"
)

// G-FORK: fork a chat from one of its messages. The fork is a new chat
// with the history up to that message; the original stays as it was.

// ForkInfo says which chat a fork came from (shown as one line at the
// top of the fork).
type ForkInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Message is the number of the message the fork starts at, counting
	// only questions and answers (1 = the first question).
	Message int `json:"message"`
}

const forkSuffix = " (fork)"

// ForkConversation returns a new chat made from src up to message at.
// At a user message the history stops before it and input is its text
// (for the input box, not resent); at any other message the history
// keeps it and input is "". ok is false when at is out of range. The
// fork holds deep copies: changing it never changes src.
func ForkConversation(src Conversation, at int) (fork Conversation, input string, ok bool) {
	if at < 0 || at >= len(src.Messages) {
		return Conversation{}, "", false
	}
	end := at + 1
	if src.Messages[at].Role == StoredUser {
		end = at
		input = src.Messages[at].Text
	}
	num := 0
	for _, m := range src.Messages[:at+1] {
		if m.Role == StoredUser || m.Role == StoredAssistant {
			num++
		}
	}
	if num == 0 {
		num = 1
	}
	title := src.Title
	if title == "" || title == defaultChatTitle {
		title = DeriveTitle(src.Messages)
	}
	base := strings.TrimSuffix(title, forkSuffix)
	fork = NewConversation()
	fork.Title = base + forkSuffix
	fork.ForkedFrom = &ForkInfo{ID: src.ID, Title: title, Message: num}
	copyJSON(src.Messages[:end], &fork.Messages)
	if len(src.Folded) > 0 {
		copyJSON(src.Folded, &fork.Folded)
	}
	return fork, input, true
}

// ForkNote is the line at the top of a fork ("" when c is no fork).
func (c Conversation) ForkNote() string {
	if c.ForkedFrom == nil {
		return ""
	}
	return "Forked from " + c.ForkedFrom.Title + " at message " + strconv.Itoa(c.ForkedFrom.Message)
}

// copyJSON deep-copies src into dst through JSON (the stored form, so
// the copy is exactly what the chat file would hold).
func copyJSON(src, dst any) {
	b, err := json.Marshal(src)
	if err == nil {
		_ = json.Unmarshal(b, dst)
	}
}
