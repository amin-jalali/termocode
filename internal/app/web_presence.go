package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Live editing together (G-COL): each side sees where the other is. The
// laptop's cursor in a buffer a page follows rides on that buffer's
// topic (a cursor delta, and the snap); a page's caret (buf.cursor) is a
// mark in the laptop's editor with the page's name, which goes when the
// page goes (webReleaseBufs). Nothing here changes the text.

// webLaptopAt is the laptop's cursor as last sent: in buffer buf (0:
// none followed).
type webLaptopAt struct {
	buf int
	at  *webproto.Cursor
}

// webLuaPeer shows the caret of page key in buffer b.
const webLuaPeer = webLuaPrelude + `
local b, key, name, row, col = ...
return W.peer(b, key, name, row, col)`

// webLuaUnpeer drops the carets of every page not in keep.
const webLuaUnpeer = webLuaPrelude + `
local keep = ...
W.unpeer(keep)`

func init() {
	webOps[webproto.OpBufCursor] = webOpBufCursor
	registerNotifyHandler("termocode_web_cursor", func(m *Model, a []any) tea.Cmd {
		var at *webproto.Cursor
		buf := notifyArgInt(a, 0)
		if buf > 0 {
			at = &webproto.Cursor{Row: notifyArgInt(a, 1), Col: notifyArgInt(a, 2)}
			if sr := notifyArgInt(a, 3); sr >= 0 {
				at.Sel = &webproto.Pos{Row: sr, Col: max(notifyArgInt(a, 4), 0)}
			}
		} else {
			buf = 0
		}
		m.webLaptopCursor(buf, at)
		return nil
	})
}

// webLaptopCursor sends the laptop's cursor to the pages that follow its
// buffer, and tells the buffer it left that it is gone from there.
func (m *Model) webLaptopCursor(buf int, at *webproto.Cursor) {
	if m.web == nil || m.web.files == nil {
		return
	}
	f := m.web.files
	old := f.laptop
	f.laptop = webLaptopAt{buf: buf, at: at}
	if old.buf != 0 && old.buf != buf {
		if b := f.bufs[old.buf]; b != nil && !b.term {
			m.webPublish(b.topic(), webproto.CursorDelta{Kind: "cursor"})
		}
	}
	if b := f.bufs[buf]; b != nil && !b.term {
		m.webPublish(b.topic(), webproto.CursorDelta{Kind: "cursor", At: at})
	}
}

// webLaptopIn is the laptop's cursor when it is in buffer n.
func (m *Model) webLaptopIn(n int) *webproto.Cursor {
	if m.web == nil || m.web.files == nil || m.web.files.laptop.buf != n {
		return nil
	}
	return m.web.files.laptop.at
}

// webOpBufCursor shows the page's caret in the laptop's editor.
func webOpBufCursor(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.BufCursorArgs)
	b, ok := m.webEditBuf(call, a.Buf)
	if !ok {
		return nil
	}
	name := webName(call.conn.name)
	if name == "" {
		name = "web"
	}
	var shown bool
	if err := m.nvim.ExecLuaResult(webLuaPeer, &shown, b.buf, call.conn.id, name, a.Row, a.Col); err != nil {
		webFail(call, err)
		return nil
	}
	if shown {
		if m.web.files.peers == nil {
			m.web.files.peers = map[string]bool{}
		}
		m.web.files.peers[call.conn.id] = true
	}
	call.reply(nil)
	return nil
}

// webDropPeers takes away the carets of pages that are gone. It calls
// nvim only when a page that showed one is gone.
func (m *Model) webDropPeers(h *webHub, f *webFiles) {
	if m.nvim == nil || len(f.peers) == 0 {
		return
	}
	keep := map[string]bool{"": true} // never empty: an empty Lua table must still be a table
	for c := range h.live {
		keep[c.id] = true
	}
	gone := false
	for id := range f.peers {
		if !keep[id] {
			gone = true
			delete(f.peers, id)
		}
	}
	if !gone {
		return
	}
	if err := m.nvim.ExecLuaArgs(webLuaUnpeer, keep); err != nil {
		recordError("[web] carets: " + err.Error())
	}
}
