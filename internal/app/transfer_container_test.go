package app

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/transfer"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// xferCtrHost is a container workspace host for the transfer tests: its
// files are a map, CopyOut / CopyIn are logged.
type xferCtrHost struct {
	workspace.Host
	files  map[string]string // container path → content ("" + dir = folder)
	dirs   map[string]bool
	outs   []string
	ins    [][]string
	inDir  string
	inData map[string]string
}

func (h *xferCtrHost) Label() string                    { return "api-1" }
func (h *xferCtrHost) FS() workspace.FS                 { return xferCtrFS{h: h} }
func (h *xferCtrHost) CheckOnline(context.Context) bool { return true }

func (h *xferCtrHost) CopyOut(_ context.Context, p, dest string, _ int64) (string, error) {
	h.outs = append(h.outs, p)
	data, ok := h.files[p]
	if !ok {
		return "", fs.ErrNotExist
	}
	return path.Base(p), os.WriteFile(filepath.Join(dest, path.Base(p)), []byte(data), 0o644)
}

func (h *xferCtrHost) CopyIn(_ context.Context, srcs []string, dir string, _ int64) ([]string, error) {
	h.ins = append(h.ins, srcs)
	h.inDir = dir
	h.inData = map[string]string{}
	var names []string
	for _, s := range srcs {
		b, err := os.ReadFile(s)
		if err != nil {
			return nil, err
		}
		h.inData[filepath.Base(s)] = string(b)
		names = append(names, filepath.Base(s))
	}
	return names, nil
}

type xferCtrFS struct {
	workspace.FS
	h *xferCtrHost
}

type xferCtrInfo struct {
	fs.FileInfo
	dir bool
}

func (i xferCtrInfo) IsDir() bool { return i.dir }

func (f xferCtrFS) Stat(_ context.Context, p string) (fs.FileInfo, error) {
	if f.h.dirs[p] {
		return xferCtrInfo{dir: true}, nil
	}
	if _, ok := f.h.files[p]; ok {
		return xferCtrInfo{}, nil
	}
	return nil, fs.ErrNotExist
}

func xferCtrModel(t *testing.T) (*Model, *xferCtrHost) {
	t.Helper()
	t.Setenv(transfer.EnvOverride, "local")
	mm := newTestModel(t)
	h := &xferCtrHost{files: map[string]string{"/app/main.go": "package main\n"}, dirs: map[string]bool{"/app": true}}
	mm.ws = workspace.Workspace{ID: "container:default/api-1:/app", Kind: workspace.KindContainer, Root: "/app", Host: h}
	return &mm, h
}

func TestContainerDownloadCopiesOutThenDownloadsTheCopy(t *testing.T) {
	m, h := xferCtrModel(t)
	downloads := t.TempDir()
	t.Cleanup(SetLocalTransfer(LocalTransferHooks{Downloads: downloads}))
	m.downloadPath("/app/main.go")
	stage := m.ctrXfer.downStage
	if stage == "" {
		t.Fatal("no local stage for the download")
	}
	msg := ctrPullCmd(h, "/app/main.go", stage)()
	if len(h.outs) != 1 || h.outs[0] != "/app/main.go" {
		t.Fatalf("CopyOut calls = %q", h.outs)
	}
	m.handleCtrXferMsg(msg)
	if m.xfer == nil {
		t.Fatal("the local download did not start")
	}
	var done tea.Msg
	select {
	case done = <-m.xfer.ch:
	case <-time.After(5 * time.Second):
		t.Fatal("the download did not end")
	}
	m.handleXferMsg(done)
	if b, err := os.ReadFile(filepath.Join(downloads, "main.go")); err != nil || string(b) != "package main\n" {
		t.Fatalf("downloaded = %q, %v", b, err)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("the local stage was left behind")
	}
}

func TestContainerDownloadErrorDropsTheStage(t *testing.T) {
	m, h := xferCtrModel(t)
	m.downloadPath("/app/missing.txt")
	stage := m.ctrXfer.downStage
	m.handleCtrXferMsg(ctrPullCmd(h, "/app/missing.txt", stage)())
	if m.xfer != nil || m.ctrXfer.downStage != "" {
		t.Fatal("a failed copy still downloads")
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("the stage was left behind")
	}
	if !strings.Contains(regToast(m), "Download failed") {
		t.Fatalf("toast = %q", regToast(m))
	}
}

func TestContainerUploadAsksFirstThenCopiesIn(t *testing.T) {
	m, h := xferCtrModel(t)
	src := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(SetLocalTransfer(LocalTransferHooks{Choose: func() ([]string, error) { return []string{src}, nil }}))
	cmd := m.uploadTo("/app/main.go") // a file: its folder
	if m.ctrXfer.upDir != "/app" || m.ctrXfer.upStage == "" {
		t.Fatalf("upload state = %+v", m.ctrXfer)
	}
	stage := m.ctrXfer.upStage
	chosen := cmd()
	c, _ := m.handleXferMsg(chosen)
	staged := c()
	m.handleXferMsg(staged)
	if !m.confirmOpen || m.confirmKind != confirmKindContainerWSUpload {
		t.Fatal("no card before writing into the container")
	}
	if bs := m.confirm.Buttons(); len(bs) != 2 || bs[0].ID != "cancel" {
		t.Fatalf("buttons = %+v (Cancel must be first)", bs)
	}
	if !strings.Contains(cardText(m), "api-1:/app") || len(h.ins) != 0 {
		t.Fatalf("card = %q, ins = %v", cardText(m), h.ins)
	}
	m.confirmOpen = false
	m.onContainerWSUploadConfirm("upload")
	msg := ctrPushCmd(h, stage, m.ctrXfer.upNames, m.ctrXfer.upDir)()
	m.handleCtrXferMsg(msg)
	if h.inDir != "/app" || h.inData["notes.txt"] != "hi" {
		t.Fatalf("CopyIn dir %q data %v", h.inDir, h.inData)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) || m.ctrXfer.upStage != "" {
		t.Fatal("the stage was left behind")
	}
	if !strings.Contains(regToast(m), "Uploaded notes.txt") {
		t.Fatalf("toast = %q", regToast(m))
	}
}

func TestContainerUploadCancelWritesNothing(t *testing.T) {
	m, h := xferCtrModel(t)
	src := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(src, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(SetLocalTransfer(LocalTransferHooks{Choose: func() ([]string, error) { return []string{src}, nil }}))
	cmd := m.uploadTo("/app")
	stage := m.ctrXfer.upStage
	c, _ := m.handleXferMsg(cmd())
	m.handleXferMsg(c())
	if got := m.onContainerWSUploadConfirm("cancel"); got != nil || len(h.ins) != 0 {
		t.Fatal("Cancel copied into the container")
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) || m.ctrXfer.upStage != "" {
		t.Fatal("Cancel left the stage")
	}
}

func TestLocalWorkspaceTransferIsUnchanged(t *testing.T) {
	t.Setenv(transfer.EnvOverride, "local")
	mm := newTestModel(t)
	if _, _, ok := mm.containerXfer(); ok {
		t.Fatal("a local workspace counts as a container")
	}
}
