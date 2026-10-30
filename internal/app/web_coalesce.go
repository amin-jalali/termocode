package app

// Delta shapes that merge while queued (WEB-7). High-rate topics (a chat
// stream, terminal rows, buffer lines) publish these, so a slow link gets
// fewer, bigger frames instead of a resync.

// webTextAppend appends Append to the text with ID (a chat message being
// streamed, a log). Two appends to the same ID merge.
type webTextAppend struct {
	ID     string `json:"id"`
	Append string `json:"append"`
}

func (d webTextAppend) coalesce(next any) (any, bool) {
	n, ok := next.(webTextAppend)
	if !ok || n.ID != d.ID {
		return nil, false
	}
	return webTextAppend{ID: d.ID, Append: d.Append + n.Append}, true
}
