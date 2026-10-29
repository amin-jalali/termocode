package app

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/amin-jalali/termocode/internal/containerhost"
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
	fs    workspace.FS
	label string
}

// newRemoteFiles serves the files of ws (a container workspace).
func newRemoteFiles(ws workspace.Workspace) *remoteFiles {
	return &remoteFiles{fs: ws.Host.FS(), label: ws.Host.Label()}
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
	if err := r.fs.WriteFile(context.Background(), p, []byte(text), 0o644); err != nil {
		return map[string]any{"ok": false, "err": remoteErrText(r.label, err)}
	}
	return map[string]any{"ok": true, "warn": ""}
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
