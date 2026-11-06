package app

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// file.text (WEBP-9): a workspace file from disk for File: Compare Two
// Files…, with the same checks as opening it (inside the workspace, no
// file that may hold secrets).

func init() {
	webOps[webproto.OpFileText] = webOpFileText
}

func webOpFileText(m *Model, call *webCall) tea.Cmd {
	rel := call.args.(*webproto.PathArgs).Path
	abs, err := m.webResolve(rel)
	if err != nil {
		webFail(call, err)
		return nil
	}
	if ai.IsSecretFileName(abs) {
		webFail(call, webErrSecret())
		return nil
	}
	if _, remote := m.remoteFS(); remote {
		call.fail(webproto.CodeNotFound, "Files of a container workspace open on the laptop only.")
		return nil
	}
	call.later()
	return func() tea.Msg {
		f, err := os.Open(abs)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				call.fail(webproto.CodeNotFound, "That file does not exist.")
			} else {
				call.fail(webproto.CodeForbidden, "That file cannot be read.")
			}
			return nil
		}
		defer f.Close()
		if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
			call.fail(webproto.CodeBadRequest, "That is not a file.")
			return nil
		}
		b, err := io.ReadAll(io.LimitReader(f, webproto.MaxGitText+1))
		if err != nil {
			call.fail(webproto.CodeForbidden, "That file cannot be read.")
			return nil
		}
		out := webproto.FileText{Path: rel}
		if len(b) > webproto.MaxGitText {
			b, out.Truncated = b[:webproto.MaxGitText], true
		}
		if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 || !utf8.Valid(b) {
			out.Binary = true
		} else {
			out.Text = string(b)
		}
		call.reply(out)
		return nil
	}
}
