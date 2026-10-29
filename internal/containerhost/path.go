package containerhost

import (
	"errors"
	"path"
	"strings"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// resolve maps p (relative to root, or absolute) to a clean absolute
// container path inside root, like workspace.Resolve but always with
// "/" (a container path, whatever the local OS spells). ".." can never
// climb out; a NUL byte is refused.
func resolve(root, p string) (string, error) {
	if !path.IsAbs(root) {
		return "", errors.New("container root is not absolute: " + root)
	}
	if strings.ContainsRune(p, 0) {
		return "", errors.New("path has a NUL byte")
	}
	root = path.Clean(root)
	if p == "" || p == "." {
		return root, nil
	}
	abs := p
	if !path.IsAbs(abs) {
		abs = path.Join(root, p)
	}
	abs = path.Clean(abs)
	if abs != root && !strings.HasPrefix(abs, strings.TrimSuffix(root, "/")+"/") {
		return "", workspace.ErrOutsideRoot
	}
	return abs, nil
}
