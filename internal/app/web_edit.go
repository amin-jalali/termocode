package app

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/coedit"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// File edits from the web (WEB-21, web-ui-design §2.4): one atomic
// set_text guarded by the changedtick the page last saw. A laptop edit
// in between is moved over when it did not touch the page's range
// (G-COL, internal/coedit); a real overlap is a conflict (the page keeps
// the user's text). Save through nvim's own :write, and undo of one
// step. Only a file the
// browser opened (or follows) can be edited: those passed the workspace
// and secret checks.

func init() {
	webOps[webproto.OpBufSetText] = webOpBufSetText
	webOps[webproto.OpBufSave] = webOpBufSave
	webOps[webproto.OpBufUndo] = webOpBufUndo
}

// webLuaEdit is what the edit chunks return.
type webLuaEdit struct {
	Err   string `msgpack:"err"`
	Msg   string `msgpack:"msg"`
	Tick  int    `msgpack:"tick"`
	Dirty bool   `msgpack:"dirty"`
	// On a conflict: the laptop's edits since the page's tick, as
	// on_bytes numbers, and whether the log reached back that far.
	Edits  [][]int `msgpack:"edits"`
	Rebase bool    `msgpack:"rebase"`
}

// webEditBuf returns the file buffer an edit may touch.
func (m *Model) webEditBuf(call *webCall, n int) (*webBuf, bool) {
	if m.web == nil || m.nvim == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return nil, false
	}
	b := m.web.filesState().bufs[n]
	if b == nil || b.term {
		call.fail(webproto.CodeNotFound, "Open the file first.")
		return nil, false
	}
	return b, true
}

// webRebaseSetText moves the page's edit a over the laptop's edits
// since a.Tick (nvim on_bytes rows: start row, start col, old end row,
// old end col, new end row, new end col) and puts it on tick. ok is
// false when a row is malformed or a laptop edit overlaps the range: a
// real conflict, which the page shows as before. a is not changed.
func webRebaseSetText(a *webproto.SetTextArgs, tick int, rows [][]int) (webproto.SetTextArgs, bool) {
	edits := make([]coedit.Edit, 0, len(rows))
	for _, r := range rows {
		if len(r) != 6 {
			return *a, false
		}
		edits = append(edits, coedit.FromBytes(r[0], r[1], r[2], r[3], r[4], r[5]))
	}
	s, e, ok := coedit.Rebase(coedit.Pos{Row: a.Start.Row, Col: a.Start.Col}, coedit.Pos{Row: a.End.Row, Col: a.End.Col}, edits)
	if !ok {
		return *a, false
	}
	out := *a
	out.Tick = tick
	out.Start = webproto.Pos{Row: s.Row, Col: s.Col}
	out.End = webproto.Pos{Row: e.Row, Col: e.Col}
	return out, true
}

func webOpBufSetText(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.SetTextArgs)
	b, ok := m.webEditBuf(call, a.Buf)
	if !ok {
		return nil
	}
	lines := a.Lines
	if len(lines) == 0 {
		lines = []string{""}
	}
	var res webLuaEdit
	if err := m.nvim.ExecLuaResult(webLuaSetText, &res, b.buf, a.Tick, a.Start.Row, a.Start.Col, a.End.Row, a.End.Col, lines); err != nil {
		webFail(call, err)
		return nil
	}
	if res.Err == "conflict" && res.Rebase {
		// Live editing together (G-COL): the laptop typed since the page's
		// tick. When it did not touch this range, move the range over the
		// laptop's edits and try once more on the tick now.
		if moved, ok := webRebaseSetText(a, res.Tick, res.Edits); ok {
			var again webLuaEdit
			if err := m.nvim.ExecLuaResult(webLuaSetText, &again, b.buf, moved.Tick, moved.Start.Row, moved.Start.Col, moved.End.Row, moved.End.Col, lines); err != nil {
				webFail(call, err)
				return nil
			}
			res = again
		}
	}
	switch res.Err {
	case "":
		device := call.conn.device
		if device == "" {
			device = "web"
		}
		// The mark is a hint: an error leaves the edit as it is.
		_ = m.nvim.ExecLuaArgs(webLuaEditMark, b.buf, a.Start.Row+len(lines)-1, device)
		call.reply(webproto.EditRes{Tick: res.Tick, Dirty: res.Dirty})
	case "conflict":
		webFailData(call, webproto.CodeConflict, "The file changed on the laptop. Check and send again.", webproto.ConflictData{Tick: res.Tick})
	case "range":
		call.fail(webproto.CodeBadRequest, "That part of the file does not exist any more.")
	case "readonly":
		call.fail(webproto.CodeForbidden, "This file is read only.")
	case "gone":
		call.fail(webproto.CodeGone, "This file was closed on the laptop.")
	default:
		call.fail(webproto.CodeInternal, m.webNvimText(res.Msg, "The edit did not work."))
	}
	return nil
}

// webOpBufSave writes off Update: a write runs the laptop's hooks and
// may wait (a slow disk, a hook, a prompt the guard answers).
func webOpBufSave(m *Model, call *webCall) tea.Cmd {
	b, ok := m.webEditBuf(call, call.args.(*webproto.BufArgs).Buf)
	if !ok {
		return nil
	}
	client, n := m.nvim, b.buf
	root, _ := m.rootDir()
	call.later()
	return func() tea.Msg {
		var res webLuaEdit
		if err := client.ExecLuaResult(webLuaSave, &res, n); err != nil {
			call.fail(webproto.CodeInternal, webNvimText(root, err.Error(), "The file was not saved."))
			return nil
		}
		switch res.Err {
		case "":
			call.reply(webproto.EditRes{Tick: res.Tick, Dirty: res.Dirty})
		case "disk":
			call.fail(webproto.CodeConflict, "The file changed on disk. Save it on the laptop.")
		case "gone":
			call.fail(webproto.CodeGone, "This file was closed on the laptop.")
		default:
			call.fail(webproto.CodeInternal, webNvimText(root, res.Msg, "The file was not saved."))
		}
		return nil
	}
}

func webOpBufUndo(m *Model, call *webCall) tea.Cmd {
	b, ok := m.webEditBuf(call, call.args.(*webproto.BufArgs).Buf)
	if !ok {
		return nil
	}
	var res webLuaEdit
	if err := m.nvim.ExecLuaResult(webLuaUndo, &res, b.buf); err != nil {
		webFail(call, err)
		return nil
	}
	switch res.Err {
	case "":
		call.reply(webproto.EditRes{Tick: res.Tick, Dirty: res.Dirty})
	case "none":
		call.fail(webproto.CodeBadRequest, "Nothing to undo.")
	case "gone":
		call.fail(webproto.CodeGone, "This file was closed on the laptop.")
	default:
		call.fail(webproto.CodeInternal, m.webNvimText(res.Msg, "Undo did not work."))
	}
	return nil
}

func (m *Model) webNvimText(msg, fallback string) string {
	root, _ := m.rootDir()
	return webNvimText(root, msg, fallback)
}

// webNvimText is nvim's error message for the page: the Lua and Vim
// prefixes cut, the workspace path taken out, one line.
func webNvimText(root, msg, fallback string) string {
	if i := strings.Index(msg, "Vim("); i >= 0 {
		if j := strings.Index(msg[i:], "):"); j >= 0 {
			msg = msg[i+j+2:]
		}
	} else if i := strings.Index(msg, "Vim:"); i >= 0 {
		msg = msg[i+4:]
	}
	if root != "" {
		msg = strings.ReplaceAll(msg, root+string(filepath.Separator), "")
		msg = strings.ReplaceAll(msg, root, "")
	}
	msg, _, _ = strings.Cut(strings.TrimSpace(msg), "\n")
	if msg == "" {
		return fallback
	}
	return msg
}
