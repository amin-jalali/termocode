package app

import (
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
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
	if r, sub, ok := m.webExtraRootOf(rel); ok {
		root, rel = r.abs, sub
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
		if rel, ok := m.webRelExtra(abs); ok {
			return rel, true
		}
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

// The workspace's added folders (WEBP-8): the page names a file of an
// added folder ":<name>/<path inside it>", <name> being the folder's
// name ("lib", then "lib~2" for a second folder of that name). Its
// absolute path never goes out. A container workspace has none here.

// webRootMark starts the name of an added folder.
const webRootMark = ":"

// webAddedRoot is one added folder: its name on the page and its path.
type webAddedRoot struct {
	key, label, abs string
}

// webExtraRoots lists the added folders in the explorer's order.
func (m *Model) webExtraRoots() []webAddedRoot {
	if _, remote := m.remoteFS(); remote {
		return nil
	}
	primary := m.explorer.PrimaryRoot()
	root, _ := m.rootDir()
	var out []webAddedRoot
	used := map[string]bool{}
	for _, r := range m.explorer.Roots() {
		if r == primary || r == root {
			continue
		}
		label := webName(filepath.Base(r))
		key := webRootMark + strings.ReplaceAll(label, "/", "_")
		for n := 2; used[key]; n++ {
			key = webRootMark + strings.ReplaceAll(label, "/", "_") + "~" + strconv.Itoa(n)
		}
		used[key] = true
		out = append(out, webAddedRoot{key: key, label: label, abs: filepath.Clean(r)})
	}
	return out
}

// webExtraRootOf splits a clean page path that names an added folder
// into the folder and the path inside it.
func (m *Model) webExtraRootOf(rel string) (webAddedRoot, string, bool) {
	if !strings.HasPrefix(rel, webRootMark) {
		return webAddedRoot{}, "", false
	}
	head, sub, _ := strings.Cut(rel, "/")
	for _, r := range m.webExtraRoots() {
		if r.key == head {
			return r, sub, true
		}
	}
	return webAddedRoot{}, "", false
}

// webRelExtra is abs as a page path inside an added folder.
func (m *Model) webRelExtra(abs string) (string, bool) {
	for _, r := range m.webExtraRoots() {
		root, a := r.abs, abs
		if !webInside(root, a) {
			rr, err1 := filepath.EvalSymlinks(root)
			ra, err2 := filepath.EvalSymlinks(a)
			if err1 != nil || err2 != nil || !webInside(rr, ra) {
				continue
			}
			root, a = rr, ra
		}
		rel, err := filepath.Rel(root, a)
		if err != nil {
			continue
		}
		if rel == "." {
			return r.key, true
		}
		return r.key + "/" + filepath.ToSlash(rel), true
	}
	return "", false
}
