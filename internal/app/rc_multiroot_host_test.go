package app

import (
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

func rcMultiRootFiles() fstest.MapFS {
	return fstest.MapFS{
		"main.go":     {Data: []byte("package main\n")},
		"libs/a/a.go": {Data: []byte("package a\n")},
	}
}

// Spec (FIX-3): the extra workspace folders of a container workspace are
// checked in the container (Host FS), never on the local disk: a folder
// only the container has stays, one only the local disk has is dropped.
func TestRCWorkspaceRootsCheckedOnHost(t *testing.T) {
	withConfigDir(t)
	ws, _ := newRCWorkspace(t, rcMultiRootFiles())
	setProcWorkspace(t, ws)
	libs, local := filepath.Join(ws.Root, "libs"), filepath.Join(ws.Root, "localdir")
	saveSession(SessionState{Roots: []string{libs, local}})
	if got, want := workspaceRoots(), []string{ws.Root, libs}; !slices.Equal(got, want) {
		t.Errorf("workspaceRoots %q, want %q", got, want)
	}

	m := Model{ws: ws}
	m.explorer = newExplorerFor(ws)
	m.loadAndApplyWorkspaceRoots()
	if got, want := m.explorer.Roots(), []string{ws.Root, libs}; !slices.Equal(got, want) {
		t.Errorf("restored roots %q, want %q", got, want)
	}
	if got := loadSession().Roots; !slices.Equal(got, []string{libs}) {
		t.Errorf("saved roots %q, want only libs", got)
	}
}

// Spec (FIX-3): Add Folder to Workspace in a container workspace adds a
// container folder (listed through the Host FS) and refuses a folder that
// only exists on the local disk.
func TestRCAddRootUsesHost(t *testing.T) {
	withConfigDir(t)
	ws, h := newRCWorkspace(t, rcMultiRootFiles())
	setProcWorkspace(t, ws)
	m := Model{ws: ws}
	m.explorer = newExplorerFor(ws)
	libs := filepath.Join(ws.Root, "libs")
	m.addRootFromPrompt("libs")
	if got, want := m.explorer.Roots(), []string{ws.Root, libs}; !slices.Equal(got, want) {
		t.Fatalf("roots %q, want %q", got, want)
	}
	if !slices.Contains(h.calls(), "readdir "+libs) {
		t.Errorf("libs was not listed through the Host: %q", h.calls())
	}
	m.addRootFromPrompt(filepath.Join(ws.Root, "localdir"))
	if got := m.explorer.Roots(); len(got) != 2 {
		t.Errorf("a local-only folder was added: %q", got)
	}
}
