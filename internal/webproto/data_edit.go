package webproto

// Results of the file edit ops (web-ui-design §2.4, WEB-21).

// EditRes is the result of buf.set_text, buf.save and buf.undo: the
// buffer's changedtick after it and whether it has unsaved changes.
type EditRes struct {
	Tick  int  `json:"tick"`
	Dirty bool `json:"dirty"`
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
