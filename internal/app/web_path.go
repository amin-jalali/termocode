package app

import (
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Paths from the page (web-ui-design §2, T40). The page names files
// workspace-relative with forward slashes; the hub turns them into an
// absolute path inside the root and never sends an absolute path back.
// On a local workspace symlinks are resolved: a link that points out of
// the root is refused, whatever its name.

// webPathOutside is the err for a path outside the workspace.
func webPathOutside() error {
	return webErr(webproto.CodeForbidden, "That path is outside the workspace.")
}

// webCleanRel cleans a workspace-relative path: "" for the root.
func webCleanRel(rel string) string {
	rel = path.Clean("/" + strings.ReplaceAll(rel, `\`, "/"))
	return strings.TrimPrefix(rel, "/")
}

// webResolve maps rel to an absolute path inside the workspace root. A
// path that does not exist is not_found; one outside the root (by name,
// or through a symlink) is forbidden.
func (m *Model) webResolve(rel string) (string, error) {
	if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) || filepath.IsAbs(rel) {
		return "", webPathOutside()
	}
	for _, part := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return "", webPathOutside()
		}
	}
	root, err := m.rootDir()
	if err != nil {
		return "", webErr(webproto.CodeNotFound, "No workspace is open.")
	}
	rel = webCleanRel(rel)
	if _, ok := m.remoteFS(); ok { // a container: names only (ADR 0018)
		return path.Join(root, rel), nil
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", webErr(webproto.CodeNotFound, "The workspace folder is gone.")
	}
	real, err := filepath.EvalSymlinks(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return "", webErr(webproto.CodeNotFound, "That file or folder does not exist.")
	}
	if err != nil {
		return "", webErr(webproto.CodeNotFound, "That file or folder cannot be read.")
	}
	if !webInside(realRoot, real) {
		return "", webPathOutside()
	}
	return abs, nil
}

// webInside reports whether p is root or under it (both clean).
func webInside(root, p string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
}

// webRel is abs as a workspace-relative path with forward slashes, and
// false when abs is not inside the root.
func (m *Model) webRel(abs string) (string, bool) {
	root, err := m.rootDir()
	if err != nil || abs == "" {
		return "", false
	}
	if _, ok := m.remoteFS(); ok {
		if !webInside(path.Clean(root), path.Clean(abs)) {
			return "", false
		}
		return strings.TrimPrefix(strings.TrimPrefix(path.Clean(abs), path.Clean(root)), "/"), true
	}
	root, abs = filepath.Clean(root), filepath.Clean(abs)
	if !webInside(root, abs) {
		// nvim may name a file by its resolved path (/private/var on macOS).
		if rr, err := filepath.EvalSymlinks(root); err == nil {
			if ra, err := filepath.EvalSymlinks(abs); err == nil && webInside(rr, ra) {
				root, abs = rr, ra
			} else {
				return "", false
			}
		} else {
			return "", false
		}
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	if rel == "." {
		rel = ""
	}
	return filepath.ToSlash(rel), true
}
