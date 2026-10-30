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

// webLinesDelta replaces lines [First, Last) (0-based, end-exclusive, as
// nvim_buf_set_lines) with Lines. A delta that replaces exactly the lines
// the queued one wrote merges with it: the result replaces the queued
// one's range with the newest lines.
type webLinesDelta struct {
	First int      `json:"first"`
	Last  int      `json:"last"`
	Lines []string `json:"lines"`
}

func (d webLinesDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webLinesDelta)
	if !ok || n.First != d.First || n.Last != d.First+len(d.Lines) {
		return nil, false
	}
	return webLinesDelta{First: d.First, Last: d.Last, Lines: n.Lines}, true
}
