package source

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutside is returned for a path that leaves the workspace.
var ErrOutside = errors.New("path is outside the workspace")

// root returns the workspace with symlinks resolved.
func (s *Sources) root() (string, error) {
	if s.Workspace == "" {
		return "", errors.New("no workspace")
	}
	abs, err := filepath.Abs(s.Workspace)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("workspace: %w", err)
	}
	return real, nil
}

// within reports whether p (clean, absolute) is root or below it.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// inside resolves rel (relative to the workspace, "" is the workspace)
// with every symlink followed, and refuses a result outside it. The path
// must exist.
func (s *Sources) inside(rel string) (string, error) {
	root, err := s.root()
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %q must be relative to the workspace", ErrOutside, rel)
	}
	p := filepath.Join(root, rel)
	if !within(root, p) {
		return "", fmt.Errorf("%w: %q", ErrOutside, rel)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%q does not exist", rel)
		}
		return "", err
	}
	if !within(root, real) {
		return "", fmt.Errorf("%w: %q points to %s", ErrOutside, rel, "a place outside it")
	}
	return real, nil
}
