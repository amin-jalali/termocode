package app

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// File ops from the page (WEB-41, web design 2.3): new file, new folder,
// rename and delete. Every path goes through webResolve (inside the
// workspace with every link followed); a new name is made in a folder
// that resolves inside, with O_EXCL, so an existing file is never
// replaced. Delete waits for a laptop card. A container workspace (ADR
// 0018) keeps these on the laptop for now.

func init() {
	webOps[webproto.OpTreeNewFile] = func(m *Model, call *webCall) tea.Cmd { return m.webTreeNew(call, false) }
	webOps[webproto.OpTreeNewDir] = func(m *Model, call *webCall) tea.Cmd { return m.webTreeNew(call, true) }
}

// webFileOpsLocal refuses a file op on a container workspace.
func (m *Model) webFileOpsLocal(call *webCall) bool {
	if _, ok := m.remoteFS(); ok {
		call.fail(webproto.CodeConflict, "Do this on the laptop for a container workspace.")
		return false
	}
	return true
}

// webResolveNew maps a new workspace path to its absolute path: its folder
// must resolve inside the workspace and the name must be free.
func (m *Model) webResolveNew(rel string) (string, error) {
	rel = webCleanRel(rel)
	if rel == "" {
		return "", webErr(webproto.CodeBadRequest, "Give it a name.")
	}
	dir, name := path.Split(rel)
	parent, err := m.webResolve(dir)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(parent); err != nil || !fi.IsDir() {
		return "", webErr(webproto.CodeNotFound, "That folder does not exist.")
	}
	abs := filepath.Join(parent, name)
	if _, err := os.Lstat(abs); !errors.Is(err, fs.ErrNotExist) {
		return "", webErr(webproto.CodeConflict, "That name exists already.")
	}
	return abs, nil
}

// webFailErr answers with a webErr's code and text, or a plain message.
func webFailErr(call *webCall, err error, fallback string) {
	var we *webError
	if errors.As(err, &we) {
		call.fail(we.code, we.msg)
		return
	}
	call.fail(webproto.CodeInternal, fallback+" "+fileOpFailText(err))
}

// webTreeNew makes an empty file or a folder.
func (m *Model) webTreeNew(call *webCall, dir bool) tea.Cmd {
	rel := webCleanRel(call.args.(*webproto.ItemArgs).Path)
	if !m.webFileOpsLocal(call) {
		return nil
	}
	abs, err := m.webResolveNew(rel)
	if err != nil {
		webFailErr(call, err, "")
		return nil
	}
	if dir {
		err = os.Mkdir(abs, 0o755)
	} else {
		var f *os.File
		if f, err = os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644); err == nil {
			err = f.Close()
		}
	}
	if err != nil {
		m.webAuditOp(call, rel, "failed")
		webFailErr(call, err, "Could not create it:")
		return nil
	}
	m.webAuditOp(call, rel, "ok")
	m.explorer.Reload()
	call.reply(webproto.TreeChanged{Path: rel})
	return tea.Batch(m.webTreeRefresh(), fetchGitCmd())
}
