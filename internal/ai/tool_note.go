package ai

import "context"

// Tool notes (MCPX-38): a running tool can show the user a note the
// model never gets (an MCP result item marked for the user only). The
// agent emits it as EventToolNote; the chat shows it under the tool line.

// EventToolNote: a running tool shows the user a note (Tool, Label,
// Detail). Far from the iota list, like EventToolProgress.
const EventToolNote EventKind = 101

// ShowUser shows text to the user, not to the model. It may be called
// from any goroutine; outside a call it does nothing.
func ShowUser(ctx context.Context, text string) {
	s, _ := ctx.Value(progressKey{}).(*progressSink)
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.note != nil {
		s.note(text)
	}
}
