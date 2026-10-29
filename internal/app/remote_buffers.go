package app

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// Remote buffers (ADR 0018, plan RC phase 2): a file of a container
// workspace is edited in a normal nvim buffer named by its container
// path (/app/main.go). nvim never reads or writes it itself: BufReadCmd
// and BufWriteCmd (remoteBuffersLua) ask Go, which goes through the
// workspace Host FS. These answers run on the RPC goroutine while nvim
// waits, so they never call back into nvim or touch the Model.

// remoteFiles answers the read and write requests of remote buffers.
type remoteFiles struct {
	host  workspace.Host
	fs    workspace.FS
	label string
	// owner is the uid new files get (nil = unknown).
	owner      func() (uid, gid int, ok bool)
	rootWarned atomic.Bool
}

// remoteFilesByID shares one remoteFiles per workspace between the nvim
// answers and the explorer's file operations (one root warning).
var remoteFilesByID sync.Map // workspace ID → *remoteFiles

// remoteFilesFor is the shared remoteFiles of the container workspace ws.
func remoteFilesFor(ws workspace.Workspace) *remoteFiles {
	if r, ok := remoteFilesByID.Load(ws.ID); ok && r.(*remoteFiles).host == ws.Host {
		return r.(*remoteFiles)
	}
	r := newRemoteFiles(ws)
	remoteFilesByID.Store(ws.ID, r)
	return r
}

// newRemoteFiles serves the files of ws (a container workspace).
func newRemoteFiles(ws workspace.Workspace) *remoteFiles {
	r := &remoteFiles{host: ws.Host, fs: ws.Host.FS(), label: ws.Host.Label()}
	if o, ok := ws.Host.(interface{ Owner() (int, int, bool) }); ok {
		r.owner = o.Owner
	}
	return r
}

// read is the text of p: {ok, text, new}; new = the file is not there
// yet (an empty buffer that a save creates). On failure {ok=false, err}.
func (r *remoteFiles) read(p string) map[string]any {
	data, err := r.fs.ReadFile(context.Background(), p)
	switch {
	case err == nil:
		return map[string]any{"ok": true, "text": string(data), "new": false}
	case errors.Is(err, fs.ErrNotExist):
		return map[string]any{"ok": true, "text": "", "new": true}
	}
	return map[string]any{"ok": false, "err": remoteErrText(r.label, err)}
}

// write saves text to p: {ok, warn}. On failure {ok=false, err}:
// nothing is changed in the editor.
func (r *remoteFiles) write(p, text string) map[string]any {
	warn, err := r.writeFile(p, []byte(text))
	if err != nil {
		return map[string]any{"ok": false, "err": remoteErrText(r.label, err)}
	}
	return map[string]any{"ok": true, "warn": warn}
}

// writeFile writes data to p. A new file written as root is accepted
// with one warning per session (plan decision 1): warn is that warning.
func (r *remoteFiles) writeFile(p string, data []byte) (warn string, err error) {
	ctx := context.Background()
	asRoot := false
	if r.owner != nil && !r.rootWarned.Load() {
		if uid, _, ok := r.owner(); ok && uid == 0 {
			_, err := r.fs.Stat(ctx, p)
			asRoot = errors.Is(err, fs.ErrNotExist)
		}
	}
	if err := r.fs.WriteFile(ctx, p, data, 0o644); err != nil {
		return "", err
	}
	if asRoot && r.rootWarned.CompareAndSwap(false, true) {
		warn = "New files in " + r.label + " are owned by root."
	}
	return warn, nil
}

// remoteErrText is one short sentence for a failed file call on the
// host label.
func remoteErrText(label string, err error) string {
	switch {
	case errors.Is(err, containerhost.ErrNotRunning):
		return label + " is not running."
	case errors.Is(err, context.DeadlineExceeded):
		return label + " did not answer in time."
	case errors.Is(err, workspace.ErrOutsideRoot):
		return "The path is outside the workspace."
	case errors.Is(err, workspace.ErrIsRoot):
		return "The workspace folder itself cannot be changed."
	case errors.Is(err, containerhost.ErrNoShell):
		return label + " has no shell (sh), so this is not possible."
	case errors.Is(err, containerhost.ErrTooLarge):
		return "The file is too large to open."
	case errors.Is(err, fs.ErrNotExist):
		return "The file is not there any more."
	case errors.Is(err, fs.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "permission denied"):
		return "No permission for this file in " + label + "."
	}
	return err.Error()
}

// startRemoteBuffers turns on remote buffers for the container workspace
// ws in n: the Go answers first, then the autocmds that ask them.
func startRemoteBuffers(n *nvim.Client, ws workspace.Workspace) error {
	r := remoteFilesFor(ws)
	arg := func(a []any, i int) string {
		if i < len(a) {
			if s, ok := a[i].(string); ok {
				return s
			}
		}
		return ""
	}
	if err := n.RegisterRequest("termocode_fs_read", func(a []any) (any, error) {
		return r.read(arg(a, 0)), nil
	}); err != nil {
		return err
	}
	if err := n.RegisterRequest("termocode_fs_write", func(a []any) (any, error) {
		return r.write(arg(a, 0), arg(a, 1)), nil
	}); err != nil {
		return err
	}
	return n.ExecLuaArgs(remoteBuffersLua, ws.Root)
}

// saveErrText is the message of a failed save for the user. A remote
// buffer's save error comes wrapped in nvim's autocmd and Lua text; only
// our own sentence (after "termocode: ") is kept.
func saveErrText(err error) string {
	msg := err.Error()
	if _, rest, ok := strings.Cut(msg, "termocode: "); ok {
		msg = rest
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
	}
	return strings.TrimSpace(msg)
}

// enableRemoteBuffers turns remote buffers on in n when the Model's
// workspace is a container (a local folder needs nothing).
func (m Model) enableRemoteBuffers(n *nvim.Client) error {
	ws := m.workspace()
	if _, ok := remoteFS(ws); !ok {
		return nil
	}
	return startRemoteBuffers(n, ws)
}
