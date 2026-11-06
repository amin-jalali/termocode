package app

import (
	"errors"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// gitReader reads the Source Control snapshot where the workspace lives
// (FIX-3): git on this machine for a local folder, git in the container
// through the Host Exec for a container workspace. A container workspace
// never falls back to the local disk.
type gitReader struct {
	dir    string
	remote bool
	x      git.Runner // the Host Exec of a remote workspace (nil: none)
}

// errNoHostExec: the workspace host cannot run commands.
var errNoHostExec = errors.New("the workspace host cannot run commands")

func gitReaderOf(ws workspace.Workspace) (gitReader, error) {
	dir, err := rootOf(ws)
	if err != nil {
		return gitReader{}, err
	}
	r := gitReader{dir: dir}
	if !isLocal(ws) {
		r.remote = true
		if ws.Host != nil {
			if x := ws.Host.Exec(); x != nil {
				r.x = x
			}
		}
	}
	return r, nil
}

func (r gitReader) status() (git.BranchStatus, error) {
	if !r.remote {
		return git.StatusBranch(r.dir)
	}
	if r.x == nil {
		return git.BranchStatus{}, errNoHostExec
	}
	return git.StatusBranchVia(r.x, r.dir)
}

func (r gitReader) commits(limit int) ([]git.GraphCommit, bool, error) {
	if !r.remote {
		return git.Commits(r.dir, 0, limit)
	}
	if r.x == nil {
		return nil, false, errNoHostExec
	}
	return git.CommitsVia(r.x, r.dir, 0, limit)
}

// rebase is the stopped interactive rebase, if any. It reads files in
// .git, so a container workspace shows none for now.
func (r gitReader) rebase() git.RebaseState {
	if r.remote {
		return git.RebaseState{}
	}
	return git.RebaseStatus(r.dir)
}
