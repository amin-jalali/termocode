package app

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/toast"
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

func init() {
	webOps[webproto.OpTreeRename] = (*Model).webTreeRename
}

// webTreeRename gives a file or folder a new name in its folder, through
// the explorer's rename plan (no name is replaced; a swap is staged), off
// the UI thread. The laptop gets the explorer's own "Undo" toast.
func (m *Model) webTreeRename(call *webCall) tea.Cmd {
	a := call.args.(*webproto.RenameArgs)
	rel := webCleanRel(a.Path)
	if !m.webFileOpsLocal(call) {
		return nil
	}
	old, err := m.webResolve(rel)
	if err != nil {
		webFailErr(call, err, "")
		return nil
	}
	if rel == "" || m.webIsRoot(old) {
		call.fail(webproto.CodeForbidden, "A workspace folder cannot be renamed here.")
		return nil
	}
	plan := explorer.CheckRename([]string{old}, []string{a.Name})
	if len(plan.Conflicts) > 0 {
		call.fail(webproto.CodeConflict, "That name exists already.")
		return nil
	}
	if len(plan.Changes) == 0 {
		call.reply(webproto.TreeChanged{Path: rel})
		return nil
	}
	newRel := path.Join(path.Dir(rel), a.Name)
	device := webName(call.conn.device)
	call.later()
	return func() tea.Msg {
		err := explorer.ApplyRename(plan.Changes, nil)
		var stamp string
		if err == nil {
			stamp, _ = explorer.RenameStamp(plan.Changes[0].New)
		}
		return applyMsg(func(m *Model) tea.Cmd {
			summary := rel + " → " + a.Name
			if err != nil {
				m.webAuditOp(call, summary, "failed")
				m.explorer.Reload()
				webFailErr(call, err, "Could not rename it:")
				return nil
			}
			c := plan.Changes[0]
			m.followRename(c.Old, c.New)
			moveGitIndex(renameMoves(plan.Changes)...)
			m.webAuditOp(call, summary, "ok")
			call.reply(webproto.TreeChanged{Path: newRel})
			// The explorer's own undo: the laptop's toast puts the old name back
			// while nothing changed since.
			st := &m.bulkRename
			st.serial++
			st.undo = []explorer.Rename{{Old: c.New, New: c.Old}}
			st.stamps = map[string]string{c.New: stamp}
			st.bufferStamp = m.renameBufferStamp(st.undo)
			m.explorer.Reload()
			if device == "" {
				device = "web"
			}
			var t tea.Cmd
			m.toast, t = m.toast.PushClick(toast.Info, "Renamed on "+device+" · Undo", filepath.Base(c.Old)+" → "+filepath.Base(c.New), undoExplorerRenameMsg{st.serial})
			return tea.Batch(t, m.webTreeRefresh(), m.refreshSearchResults(), fetchGitCmd())
		})
	}
}

// webIsRoot reports whether abs is one of the workspace's root folders.
func (m *Model) webIsRoot(abs string) bool {
	root, err := m.rootDir()
	if err == nil && filepath.Clean(abs) == filepath.Clean(root) {
		return true
	}
	for _, r := range m.explorer.Roots() {
		if filepath.Clean(abs) == filepath.Clean(r) {
			return true
		}
	}
	return false
}
