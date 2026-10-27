package app

import (
	"errors"
	"sync/atomic"

	"github.com/amin-jalali/termocode/internal/workspace"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// processWS is the workspace this termocode process serves (ADR 0018).
// cmd/termocode sets it once at startup (SetWorkspace), before New.
var processWS atomic.Pointer[workspace.Workspace]

// SetWorkspace sets the workspace of this process. New copies it into
// the Model; code with no Model at hand reads it through procWorkspace.
func SetWorkspace(ws workspace.Workspace) { processWS.Store(&ws) }

// procWorkspace is the process workspace. When none was set (tests and
// tools that only call New) it is the process folder, as termocode read
// it before workspaces existed.
func procWorkspace() workspace.Workspace {
	if w := processWS.Load(); w != nil {
		return *w
	}
	return localhost.Cwd()
}

// errNoWorkspace stands in for the error os.Getwd gave when the process
// folder could not be read.
var errNoWorkspace = errors.New("workspace folder unknown")

// procRootDir is the root of the process workspace.
func procRootDir() (string, error) {
	return rootOf(procWorkspace())
}

func rootOf(ws workspace.Workspace) (string, error) {
	if ws.Root == "" {
		return "", errNoWorkspace
	}
	return ws.Root, nil
}

// workspace is the Model's workspace; a Model not built by New (tests)
// falls back to the process workspace.
func (m Model) workspace() workspace.Workspace {
	if !m.ws.IsZero() {
		return m.ws
	}
	return procWorkspace()
}

// rootDir is the root folder of the Model's workspace. It replaces the
// os.Getwd calls of the time when the workspace was always the process
// folder.
func (m Model) rootDir() (string, error) {
	return rootOf(m.workspace())
}
