package lspinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// State is a tool's install state on this host.
type State int

const (
	// Missing: neither a managed shim nor a system binary.
	Missing State = iota
	// Managed: installed by termocode into the tools dir.
	Managed
	// System: found on the user's PATH (not managed by termocode).
	System
)

// Status is the resolved state of one tool.
type Status struct {
	State   State
	Path    string // absolute path of the executable / shim
	Version string // managed installs only; "" when unknown
}

// lookPath is swapped in tests.
var lookPath = exec.LookPath

// versionFile records the installed version inside tools/<name>/.
const versionFile = ".termocode-version"

// StatusOf resolves t: a managed shim in tools/bin wins over a system
// binary on PATH.
func StatusOf(t Tool) Status {
	if bin, err := BinDir(); err == nil {
		shim := filepath.Join(bin, t.Bin)
		if isExecutable(shim) {
			st := Status{State: Managed, Path: shim}
			if dir, err := ToolDir(t.Name); err == nil {
				if b, err := os.ReadFile(filepath.Join(dir, versionFile)); err == nil {
					st.Version = strings.TrimSpace(string(b))
				}
			}
			return st
		}
	}
	if p, err := lookPath(t.Bin); err == nil {
		return Status{State: System, Path: p}
	}
	return Status{State: Missing}
}

// Available reports whether t can be launched (managed or system).
func Available(t Tool) bool { return StatusOf(t).State != Missing }

// AnyAvailable reports whether at least one tool in ts is available.
func AnyAvailable(ts []Tool) bool {
	for _, t := range ts {
		if Available(t) {
			return true
		}
	}
	return false
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode().Perm()&0o111 != 0
}
