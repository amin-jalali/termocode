package extproc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutside is returned for a path outside the workspace.
var ErrOutside = errors.New("path is outside the workspace")

// InWorkspace resolves p (relative paths are relative to root) and
// returns its absolute path when it is root or inside it. Symlinks are
// followed for the part of the path that exists, so a link that points
// out of the workspace does not count as inside.
func InWorkspace(root, p string) (string, error) {
	if root == "" {
		return "", ErrOutside
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realP, err := evalExisting(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, realP)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrOutside
	}
	return p, nil
}

// evalExisting resolves symlinks in the longest existing prefix of p and
// appends the rest unchanged.
func evalExisting(p string) (string, error) {
	rest := ""
	cur := p
	for {
		r, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(r, rest), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
