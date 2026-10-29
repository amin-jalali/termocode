package containerhost

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// shfs: `sh -c` one-liners for the file operations that need more than
// a tar stream. The script is a constant; every path is a positional
// argument ("$1"), always absolute, so no path is ever read as shell
// code or as an option.

// ErrNoShell is returned for an operation that needs sh in an image
// without it.
var ErrNoShell = errors.New("the image has no shell (sh)")

// sh runs script with args as $1… in the container, as the host's user.
func (h *Host) sh(ctx context.Context, op, p, script string, args ...string) (string, error) {
	argv := append([]string{"sh", "-c", script, "sh"}, args...)
	out, stderr, code, err := h.call(ctx, nil, h.execArgs("-i", "/", argv)...)
	if err != nil {
		return "", &fs.PathError{Op: op, Path: p, Err: err}
	}
	if code != 0 {
		return "", h.engineErr(op, p, stderr, code)
	}
	return out, nil
}

// readDirScript lists $1: one "<type><name>\0" per entry (l symlink,
// d folder, f anything else), with shell built-ins only.
const readDirScript = `cd "$1" || exit 3
for f in * .[!.]* ..?*; do
  if [ -L "$f" ]; then t=l; elif [ -d "$f" ]; then t=d; elif [ -e "$f" ]; then t=f; else continue; fi
  printf '%s%s\0' "$t" "$f"
done`

// ReadDir lists a folder, sorted by name like os.ReadDir. Info() of an
// entry is read on demand (one cp header).
func (f containerFS) ReadDir(ctx context.Context, p string) ([]fs.DirEntry, error) {
	h := f.h
	abs, err := resolve(h.cfg.Root, p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := h.opCtx(ctx)
	defer cancel()
	if !h.Caps().HasShell {
		return f.readDirTar(ctx, abs)
	}
	out, err := h.sh(ctx, "readdir", abs, readDirScript, abs)
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) && !errors.Is(err, ErrNotRunning) {
			// cd failed: say why, like os.ReadDir.
			hd, serr := h.header(ctx, "readdir", abs, true)
			if serr != nil {
				return nil, serr
			}
			if !hd.FileInfo().IsDir() {
				return nil, &fs.PathError{Op: "readdir", Path: abs, Err: errors.New("not a folder")}
			}
		}
		return nil, err
	}
	var ents []fs.DirEntry
	for _, rec := range strings.Split(out, "\x00") {
		if len(rec) < 2 {
			continue
		}
		typ := fs.FileMode(0)
		switch rec[0] {
		case 'd':
			typ = fs.ModeDir
		case 'l':
			typ = fs.ModeSymlink
		}
		ents = append(ents, &dirEntry{f: f, dir: abs, name: rec[1:], typ: typ})
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
	return ents, nil
}

// dirEntry is one ReadDir entry; info is filled by a cp listing or on
// demand.
type dirEntry struct {
	f    containerFS
	dir  string
	name string
	typ  fs.FileMode
	info fs.FileInfo
}

func (e *dirEntry) Name() string      { return e.name }
func (e *dirEntry) IsDir() bool       { return e.typ.IsDir() }
func (e *dirEntry) Type() fs.FileMode { return e.typ }
func (e *dirEntry) String() string    { return fs.FormatDirEntry(e) }

// Info is the entry's own info (a symlink is not followed, like
// os.DirEntry).
func (e *dirEntry) Info() (fs.FileInfo, error) {
	if e.info != nil {
		return e.info, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), e.f.h.cfg.Timeout)
	defer cancel()
	hd, err := e.f.h.header(ctx, "lstat", path.Join(e.dir, e.name), false)
	if err != nil {
		return nil, err
	}
	return hd.FileInfo(), nil
}

// mkdirScript makes $1 and its parents; mode $2 applies only to folders
// it creates (an existing folder is never changed).
const mkdirScript = `[ -d "$1" ] || mkdir -p -m "$2" -- "$1"`

// Mkdir makes the folder and its missing parents. With sh it is `mkdir
// -p` as the host's user; without sh, a tar of the missing folders.
func (f containerFS) Mkdir(ctx context.Context, p string, perm fs.FileMode) error {
	h := f.h
	abs, err := resolve(h.cfg.Root, p)
	if err != nil {
		return err
	}
	ctx, cancel := h.opCtx(ctx)
	defer cancel()
	if !h.Caps().HasShell {
		return f.mkdirTar(ctx, abs, perm)
	}
	_, err = h.sh(ctx, "mkdir", abs, mkdirScript, abs, fmt.Sprintf("%o", perm.Perm()))
	return err
}
