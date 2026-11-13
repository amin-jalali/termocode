package webproto

// Results of the file edit ops (web-ui-design §2.4, WEB-21).

// EditRes is the result of buf.set_text, buf.save and buf.undo: the
// buffer's changedtick after it and whether it has unsaved changes.
// Rebased (set_text, G-COL): the edit was moved over the laptop's edits
// made since the page's tick, so the text at Tick is not the page's
// text; the page waits for the buffer at Tick before it sends more.
type EditRes struct {
	Tick    int  `json:"tick"`
	Dirty   bool `json:"dirty"`
	Rebased bool `json:"rebased,omitempty"`
}

// ConflictData is the data of a conflict err from buf.set_text: the
// buffer changed on the laptop since Tick the page sent; Tick is now.
type ConflictData struct {
	Tick int `json:"tick"`
}

func init() {
	registerResult(OpBufSetText, EditRes{})
	registerResult(OpBufSave, EditRes{})
	registerResult(OpBufUndo, EditRes{})
}
