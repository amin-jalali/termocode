package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/transfer"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// Plan RC, phase 8: File: Download / Upload in a container workspace.
// The paths of the explorer and the editor are container paths. A
// download first copies the file or folder out of the container into a
// private local stage (one `cp` tar stream), then runs the normal
// download of that copy (local copy, kitty, iTerm2 or the scp card). An
// upload lands in a local stage first (chooser, path prompt, kitty or
// iTerm2, as for a local folder), then a Cancel-first card copies it
// into the container. Nothing in the container is ever replaced: a taken
// name gets " (1)".

const confirmKindContainerWSUpload confirmKindEnum = 7731 // upload into the container workspace

// containerXferTimeout bounds one copy into or out of the container.
var containerXferTimeout = 10 * time.Minute

// containerXferHost is what the transfer needs from a container host
// (containerhost.Host).
type containerXferHost interface {
	CopyOut(ctx context.Context, p, dest string, maxBytes int64) (string, error)
	CopyIn(ctx context.Context, srcs []string, dir string, maxBytes int64) ([]string, error)
	Label() string
}

// ctrXferState is the container transfer in progress: at most one
// download stage and one upload stage.
type ctrXferState struct {
	downStage string // local stage of the last download ("" = none)
	upStage   string // local stage of the upload ("" = none)
	upDir     string // the container folder the upload goes to
	upNames   []string
}

// ctrPulledMsg is the end of the copy out of the container.
type ctrPulledMsg struct {
	stage, local string
	err          error
}

// ctrPushedMsg is the end of the copy into the container.
type ctrPushedMsg struct {
	dir   string
	names []string
	err   error
}

// containerXfer is the container host of the workspace, when the
// workspace is in a container.
func (m Model) containerXfer() (containerXferHost, workspace.FS, bool) {
	ws := m.workspace()
	if ws.Kind != workspace.KindContainer || ws.Host == nil {
		return nil, nil, false
	}
	h, ok := ws.Host.(containerXferHost)
	if !ok {
		return nil, nil, false
	}
	return h, ws.Host.FS(), true
}

// newXferStage is a private local folder for one transfer.
var newXferStage = func(kind string) (string, error) {
	return os.MkdirTemp("", "termocode-container-"+kind+"-")
}

// dropStage removes a stage folder termocode made ("" = nothing).
func dropStage(dir string) {
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

// xferDirLabel names an upload folder for titles: the container folder
// for a container upload stage, else the folder's own name.
func (m Model) xferDirLabel(dir string) string {
	if m.ctrXfer.upStage != "" && filepath.Clean(dir) == filepath.Clean(m.ctrXfer.upStage) {
		return path.Base(m.ctrXfer.upDir)
	}
	return filepath.Base(dir)
}

// containerDownload copies p out of the container, then downloads the
// copy.
func (m *Model) containerDownload(h containerXferHost, p string) tea.Cmd {
	if m.xfer != nil {
		return m.pushToastDetail(toast.Warn, "A download is running", "Wait for it, or run File: Cancel Download.")
	}
	dropStage(m.ctrXfer.downStage)
	m.ctrXfer.downStage = ""
	stage, err := newXferStage("download")
	if err != nil {
		return m.toastErr("Download failed", err)
	}
	m.ctrXfer.downStage = stage
	var c tea.Cmd
	m.toast, c = m.toast.PushKeyed(xferToastKey, toast.Info, "Copying "+path.Base(p)+" out of "+h.Label()+"…", "")
	return tea.Batch(c, ctrPullCmd(h, p, stage))
}

// ctrPullCmd copies p out of the container into stage.
func ctrPullCmd(h containerXferHost, p, stage string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), containerXferTimeout)
		defer cancel()
		name, err := h.CopyOut(ctx, p, stage, xferMaxUpload)
		return ctrPulledMsg{stage: stage, local: filepath.Join(stage, name), err: err}
	}
}

// containerUpload asks for files from the user's computer for the
// container folder dir (a file means its folder).
func (m *Model) containerUpload(h containerXferHost, fsys workspace.FS, dir string) tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	fi, err := fsys.Stat(ctx, dir)
	cancel()
	if err != nil {
		return m.toastErr("Upload failed", err)
	}
	if !fi.IsDir() {
		dir = path.Dir(dir)
	}
	if m.ctrXfer.upStage != "" {
		return m.pushToastDetail(toast.Warn, "An upload is waiting", "Finish or cancel it first.")
	}
	stage, err := newXferStage("upload")
	if err != nil {
		return m.toastErr("Upload failed", err)
	}
	m.ctrXfer.upStage, m.ctrXfer.upDir, m.ctrXfer.upNames = stage, dir, nil
	sup := transfer.Detect(os.Getenv)
	switch sup.Upload {
	case transfer.Local:
		return m.localUpload(stage)
	case transfer.Kitty:
		return m.kittyUploadPrompt(stage)
	case transfer.ITerm2:
		return uploadExecCmd(transfer.ITerm2, "", stage)
	}
	dropStage(stage)
	m.ctrXfer.upStage, m.ctrXfer.upDir = "", ""
	why := sup.Why
	if why == "" {
		why = "This terminal cannot send files."
	}
	return m.pushToastDetail(toast.Warn, "Upload is not possible here",
		why+" Copy the file with: docker cp <file> "+h.Label()+":"+dir)
}

// onCtrUploadStaged takes the end of the upload into the stage: on
// success it opens the Cancel-first card. ok is false when msg is not
// for a container upload.
func (m *Model) onCtrUploadStaged(msg xferUploadDoneMsg) (tea.Cmd, bool) {
	st := &m.ctrXfer
	if st.upStage == "" || filepath.Clean(msg.dir) != filepath.Clean(st.upStage) {
		return nil, false
	}
	ents, _ := os.ReadDir(st.upStage)
	if msg.err != nil || len(ents) == 0 {
		dropStage(st.upStage)
		st.upStage, st.upDir, st.upNames = "", "", nil
		switch {
		case errors.Is(msg.err, transfer.ErrCanceled), errors.Is(msg.err, context.Canceled):
			return m.pushToastDetail(toast.Info, "Upload canceled", ""), true
		case msg.err != nil:
			return m.pushToastDetail(toast.Errr, "Upload failed", msg.err.Error()), true
		}
		return nil, true // nothing chosen
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	st.upNames = names
	h, _, _ := m.containerXfer()
	where := st.upDir
	if h != nil {
		where = h.Label() + ":" + st.upDir
	}
	m.confirm = confirm.New("Upload to the Container",
		fmt.Sprintf("Copy %s (%s) into %s? A name that is already there gets \" (1)\"; nothing is replaced.",
			strings.Join(names, ", "), humanBytes(dirSize(st.upStage)), where),
		[]confirm.Button{
			{ID: "cancel", Title: "Cancel"},
			{ID: "upload", Title: "Upload", Style: confirm.StylePrimary},
		})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindContainerWSUpload
	return nil, true
}

// onContainerWSUploadConfirm is the card's answer.
func (m *Model) onContainerWSUploadConfirm(id string) tea.Cmd {
	st := &m.ctrXfer
	stage, dir, names := st.upStage, st.upDir, st.upNames
	h, _, ok := m.containerXfer()
	if id != "upload" || !ok || stage == "" {
		dropStage(stage)
		st.upStage, st.upDir, st.upNames = "", "", nil
		return nil
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushKeyed(xferToastKey, toast.Info, "Copying into "+h.Label()+"…", dir)
	return tea.Batch(c, ctrPushCmd(h, stage, names, dir))
}

// ctrPushCmd copies the staged names into the container folder dir.
func ctrPushCmd(h containerXferHost, stage string, names []string, dir string) tea.Cmd {
	return func() tea.Msg {
		var srcs []string
		for _, n := range names {
			srcs = append(srcs, filepath.Join(stage, n))
		}
		ctx, cancel := context.WithTimeout(context.Background(), containerXferTimeout)
		defer cancel()
		got, err := h.CopyIn(ctx, srcs, dir, xferMaxUpload)
		return ctrPushedMsg{dir: dir, names: got, err: err}
	}
}

// handleCtrXferMsg handles the container transfer messages.
func (m *Model) handleCtrXferMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case ctrPulledMsg:
		m.toast = m.toast.Dismiss(xferToastKey)
		if msg.err != nil {
			if m.ctrXfer.downStage == msg.stage {
				dropStage(msg.stage)
				m.ctrXfer.downStage = ""
			}
			return m.toastErr("Download failed", msg.err), true
		}
		return m.downloadLocal(msg.local), true
	case ctrPushedMsg:
		dropStage(m.ctrXfer.upStage)
		m.ctrXfer.upStage, m.ctrXfer.upDir, m.ctrXfer.upNames = "", "", nil
		m.toast = m.toast.Dismiss(xferToastKey)
		m.explorer.Reload()
		if msg.err != nil {
			return m.toastErr("Upload failed", msg.err), true
		}
		if len(msg.names) > 0 {
			m.explorer.RevealPath(path.Join(msg.dir, msg.names[0]))
		}
		return m.pushToastDetail(toast.Info, "Uploaded "+strings.Join(msg.names, ", "), "into "+msg.dir), true
	}
	return nil, false
}

// dropCtrDownloadStage removes the local stage of the last download
// (its download ended).
func (m *Model) dropCtrDownloadStage() {
	dropStage(m.ctrXfer.downStage)
	m.ctrXfer.downStage = ""
}
