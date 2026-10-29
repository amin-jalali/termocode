package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// ErrMarkersLeft is returned by MarkResolved while the file still has
// conflict markers.
var ErrMarkersLeft = errors.New("conflict markers are still in the file")

// MarkResolved stages path (relative to dir) as resolved, but only when
// the working file has no conflict markers left.
func MarkResolved(dir, path string) error {
	b, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return err
	}
	if HasConflicts(string(b)) {
		// A lone start marker (half deleted by hand) counts as one.
		n := max(len(ParseConflicts(string(b))), 1)
		return fmt.Errorf("%w (%d left)", ErrMarkersLeft, n)
	}
	return gitRun(dir, "add", "--", path)
}

// ResolveWithSide settles a whole-file conflict (binary, or deleted by one
// side) with "ours" or "theirs": that side's file is checked out and
// staged. When the chosen side deleted the file, the file is deleted.
func ResolveWithSide(dir, path, side string) error {
	if side != "ours" && side != "theirs" {
		return fmt.Errorf("unknown side %q", side)
	}
	st, err := ReadConflictStages(dir, path)
	if err != nil {
		return err
	}
	keep := st.Ours
	if side == "theirs" {
		keep = st.Theirs
	}
	if !keep.Present {
		return ResolveDelete(dir, path)
	}
	if err := gitRun(dir, "checkout", "--"+side, "--", path); err != nil {
		return err
	}
	return gitRun(dir, "add", "--", path)
}

// ResolveDelete settles a conflict by deleting the file.
func ResolveDelete(dir, path string) error {
	return gitRun(dir, "rm", "-q", "-f", "--", path)
}

// gitRun runs git in dir and returns git's own reason on failure.
func gitRun(dir string, args ...string) error {
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmdError(err, out)
	}
	return nil
}
