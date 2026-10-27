// Package workspace is the folder termocode works on and the machine it
// lives on (ADR 0018). termocode itself always runs locally; a Host says
// how to reach the files and run commands: on this machine
// (workspace/localhost), or later inside a container through the engine
// CLI, without installing or changing anything there.
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/services"
)

// Kind is where a workspace lives.
type Kind string

const (
	KindLocal     Kind = "local"
	KindContainer Kind = "container"
)

// Workspace is one opened folder.
type Workspace struct {
	// ID names the workspace across runs: "local:/abs/path" or
	// "container:<context>/<name>:/app". Sessions are keyed by it (Key).
	ID   string
	Kind Kind
	// Root is the absolute folder, as the Host sees it.
	Root string
	Host Host
}

// IsZero reports whether w was never set.
func (w Workspace) IsZero() bool { return w.ID == "" && w.Root == "" && w.Host == nil }

// Host reaches a workspace's machine.
type Host interface {
	FS() FS
	// Exec runs commands where the workspace lives.
	Exec() services.Exec
	// TermArgv is the command line a terminal tab runs to start argv in
	// dir on this host (the argv itself on the local machine).
	TermArgv(dir string, argv []string) []string
	Caps() Caps
	Online() bool
	// Label is a short name for the status bar ("local", "api-1").
	Label() string
}

// Caps is what a host offers.
type Caps struct {
	HasShell bool     // a POSIX sh is there (one-line file ops)
	Tools    []string // known commands found on the host (git, rg, …)
	User     string
	Arch     string
	Libc     string
	MountOf  string // the local folder bind-mounted at Root, if any
}

// FS is a workspace's file system. Paths are relative to the root, or
// absolute inside it; a path that leaves the root fails with
// ErrOutsideRoot (see Resolve).
type FS interface {
	Stat(ctx context.Context, p string) (fs.FileInfo, error)
	ReadDir(ctx context.Context, p string) ([]fs.DirEntry, error)
	ReadFile(ctx context.Context, p string) ([]byte, error)
	// WriteFile creates missing parent folders. An existing file keeps
	// its mode; perm applies to a new one.
	WriteFile(ctx context.Context, p string, data []byte, perm fs.FileMode) error
	// Mkdir creates the folder and missing parents.
	Mkdir(ctx context.Context, p string, perm fs.FileMode) error
	// Rename creates the missing parent folders of to.
	Rename(ctx context.Context, from, to string) error
	// Remove deletes a file or a whole folder; never the root.
	Remove(ctx context.Context, p string) error
}

// ErrOutsideRoot is returned for a path that leaves the workspace root.
var ErrOutsideRoot = errors.New("path is outside the workspace")

// ErrIsRoot is returned when an operation would delete the root.
var ErrIsRoot = errors.New("refusing to change the workspace root itself")

// Resolve maps p (relative to root, or absolute) to a clean absolute
// path inside root. The check is by name only, like the AI file tools:
// ".." can never climb out, and a symlink inside the root is followed.
// root must be absolute.
func Resolve(root, p string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", errors.New("workspace root is not absolute: " + root)
	}
	root = filepath.Clean(root)
	if p == "" || p == "." {
		return root, nil
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, p)
	}
	abs = filepath.Clean(abs)
	if abs != root && !strings.HasPrefix(abs, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator)) {
		return "", ErrOutsideRoot
	}
	return abs, nil
}

// LocalID is the ID of a local folder.
func LocalID(root string) string {
	return string(KindLocal) + ":" + filepath.Clean(root)
}

// ParseID splits an ID into its kind and the rest ("" kind when the ID
// has no known kind).
func ParseID(id string) (Kind, string) {
	kind, rest, ok := strings.Cut(id, ":")
	switch Kind(kind) {
	case KindLocal, KindContainer:
		if ok {
			return Kind(kind), rest
		}
	}
	return "", id
}

// Key is a short, file-name safe key for a workspace ID. A local ID
// gives the key termocode always used for that folder (a hash of the
// clean path), so sessions, backups and per-workspace state written
// before IDs existed still load.
func Key(id string) string {
	src := id
	if kind, rest := ParseID(id); kind == KindLocal {
		src = filepath.Clean(rest)
	}
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:8])
}

// Base is the last element of the root, for titles ("proj").
func (w Workspace) Base() string {
	if w.Kind == KindLocal || w.Kind == "" {
		return filepath.Base(w.Root)
	}
	return path.Base(w.Root)
}
