package git

import (
	"path/filepath"
	"strings"
)

// MoveIndexAcross is MoveIndex for a move that may change folders (the
// AI's rename tool can move a file anywhere in the workspace). Both ends
// must be in the same repo; a move into another repo or out of git leaves
// the index alone.
func MoveIndexAcross(from, to string) error {
	fromDir, toDir := filepath.Dir(from), filepath.Dir(to)
	if fromDir == toDir {
		return MoveIndex(from, to)
	}
	topFrom, err := runOut(fromDir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil // not in a repo
	}
	topTo, err := runOut(toDir, "git", "rev-parse", "--show-toplevel")
	if err != nil || topTo != topFrom {
		return nil
	}
	oldName := filepath.Base(from)
	ls, err := runOut(fromDir, "git", "--literal-pathspecs", "ls-files", "-s", "-z", "--full-name", "--", oldName)
	if err != nil || ls == "" {
		return nil // nothing tracked there
	}
	prefixFrom, err := runOut(fromDir, "git", "rev-parse", "--show-prefix")
	if err != nil {
		return err
	}
	prefixTo, err := runOut(toDir, "git", "rev-parse", "--show-prefix")
	if err != nil {
		return err
	}
	var drop, add strings.Builder
	oldRel := strings.TrimSuffix(prefixFrom, "\n") + oldName
	newRel := strings.TrimSuffix(prefixTo, "\n") + filepath.Base(to)
	if err := moveEntries(ls, oldRel, newRel, &drop, &add); err != nil {
		return err
	}
	if add.Len() == 0 {
		return nil
	}
	return runStdin(fromDir, drop.String()+add.String(), "git", "update-index", "-z", "--index-info")
}
