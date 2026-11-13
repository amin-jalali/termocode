package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
	"github.com/amin-jalali/termocode/internal/lspmirror"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// RC-6: every 5 s the mirror takes the files changed in the container
// in the last minute (a git checkout or a build in the terminal there),
// one `find -mmin` in the image; left-out folders are never read.
func TestPollMirrorChangesCopiesRecentFiles(t *testing.T) {
	ws, _, _, c := fakeContainerWS(t, func(c *fakedocker.Container) {
		c.WriteFile(t, "/app/node_modules/x/index.js", []byte("skip"), 0o644)
	})
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(c.Local("/app/pkg/util.go"), old, old)
	c.WriteFile(t, "/app/main.go", []byte("package main // changed in the container\n"), 0o644)
	mr := lspmirror.New(t.TempDir(), "k", "/app")
	if err := os.MkdirAll(mr.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	n, err := pollMirrorChanges(context.Background(), ws.Host.Exec(), ws.Host.FS(), "/app", mr)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("copied %d files, want 1 (main.go)", n)
	}
	if b, err := os.ReadFile(filepath.Join(mr.Dir, "main.go")); err != nil || string(b) != "package main // changed in the container\n" {
		t.Fatalf("mirror main.go = %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(mr.Dir, "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("a left-out folder was copied: %v", err)
	}
}

// The poll runs only while a filled mirror serves a local server; it
// schedules the next one, and a failing poll (no find in the image)
// stops polling.
func TestContainerMirrorPollTicksOnlyWhileMirrored(t *testing.T) {
	h := &fakeLSPHost{found: map[string]string{}}
	m := lspContainerModel(t, h)
	if cmd, _ := m.handleContainerLSPMsg(containerMirrorPollMsg{}); cmd != nil {
		t.Fatal("no mirror: no poll")
	}
	m.containerLSPInitCmd()
	m.containerLSP.mirrored = true
	calls := 0
	oldPoll := pollContainerMirror
	pollContainerMirror = func(context.Context, services.Exec, workspace.FS, string, *lspmirror.Mirror) (int, error) {
		calls++
		return 0, nil
	}
	t.Cleanup(func() { pollContainerMirror = oldPoll })
	cmd, ok := m.handleContainerLSPMsg(containerMirrorPollMsg{})
	if !ok || cmd == nil {
		t.Fatal("a mirrored workspace polls")
	}
	done, ok := cmd().(containerMirrorPolledMsg)
	if !ok || calls != 1 {
		t.Fatalf("poll cmd = %T, calls %d", done, calls)
	}
	if next, _ := m.handleContainerLSPMsg(done); next == nil {
		t.Fatal("a good poll schedules the next one")
	}
	if next, _ := m.handleContainerLSPMsg(containerMirrorPolledMsg{err: os.ErrNotExist}); next != nil {
		t.Fatal("a failing poll stops polling")
	}
	if cmd, _ := m.handleContainerLSPMsg(containerMirrorPollMsg{}); cmd != nil {
		t.Fatal("polling stays off after a failure")
	}
}
