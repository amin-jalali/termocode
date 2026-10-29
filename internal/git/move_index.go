package git

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// MoveIndex makes the index follow a file or folder that was just renamed
// on disk from from to to (both in the same folder): every index entry at
// from, or under it, moves to the new name with its staged content, so git
// sees a rename instead of a deleted file and an untracked one. Unsaved
// working-tree edits stay unstaged. Outside a repo, for untracked paths and
// for a path in a merge conflict it does nothing.
func MoveIndex(from, to string) error {
	dir := filepath.Dir(to)
	if filepath.Dir(from) != dir {
		return errors.New("move index: the new name must be in the same folder")
	}
	oldName, newName := filepath.Base(from), filepath.Base(to)
	ls, err := runOut(dir, "git", "--literal-pathspecs", "ls-files", "-s", "-z", "--full-name", "--", oldName)
	if err != nil || ls == "" {
		return nil // not in a repo, or nothing tracked there
	}
	prefix, err := runOut(dir, "git", "rev-parse", "--show-prefix")
	if err != nil {
		return err
	}
	prefix = strings.TrimSuffix(prefix, "\n")
	oldRel, newRel := prefix+oldName, prefix+newName
	var info strings.Builder
	for _, rec := range strings.Split(ls, "\x00") {
		if rec == "" {
			continue
		}
		meta, path, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta) // mode, object, stage
		if !ok || len(f) != 3 {
			return fmt.Errorf("move index: odd ls-files line %q", rec)
		}
		if f[2] != "0" {
			return nil // a merge conflict: leave it for the user
		}
		if path != oldRel && !strings.HasPrefix(path, oldRel+"/") {
			continue
		}
		// mode 0 drops the old entry; the new one keeps mode and content.
		fmt.Fprintf(&info, "0 %s\t%s\x00", strings.Repeat("0", len(f[1])), path)
		fmt.Fprintf(&info, "%s %s\t%s\x00", f[0], f[1], newRel+path[len(oldRel):])
	}
	if info.Len() == 0 {
		return nil
	}
	return runStdin(dir, info.String(), "git", "update-index", "-z", "--index-info")
}
