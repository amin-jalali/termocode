package app

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// File edits from the web (WEB-21, web-ui-design §2.4): one atomic
// set_text guarded by the changedtick the page last saw (a laptop edit
// in between is a conflict; the page keeps the user's text), save
// through nvim's own :write, and undo of one step. Only a file the
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
	switch res.Err {
	case "":
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
