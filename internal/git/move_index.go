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
	return MoveIndexAll([]Move{{From: from, To: to}})
}

// Move is one rename for MoveIndexAll.
type Move struct{ From, To string }

// MoveIndexAll is MoveIndex for several renames done together. The
// renames of one folder change the index in one step (all old entries go
// first), so swapped names (a → b, b → a) keep each staged text.
func MoveIndexAll(moves []Move) error {
	var dirs []string
	byDir := map[string][]Move{}
	for _, mv := range moves {
		dir := filepath.Dir(mv.To)
		if filepath.Dir(mv.From) != dir {
			return errors.New("move index: the new name must be in the same folder")
		}
		if _, ok := byDir[dir]; !ok {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], mv)
	}
	var errs []error
	for _, dir := range dirs {
		errs = append(errs, moveIndexIn(dir, byDir[dir]))
	}
	return errors.Join(errs...)
}

// moveIndexIn moves the index entries of renames inside dir.
func moveIndexIn(dir string, moves []Move) error {
	prefix, havePrefix := "", false
	var drop, add strings.Builder
	for _, mv := range moves {
		oldName, newName := filepath.Base(mv.From), filepath.Base(mv.To)
		ls, err := runOut(dir, "git", "--literal-pathspecs", "ls-files", "-s", "-z", "--full-name", "--", oldName)
		if err != nil {
			return nil // not in a repo
		}
		if ls == "" {
			continue // nothing tracked there
		}
		if !havePrefix {
			p, err := runOut(dir, "git", "rev-parse", "--show-prefix")
			if err != nil {
				return err
			}
			prefix, havePrefix = strings.TrimSuffix(p, "\n"), true
		}
		if err := moveEntries(ls, prefix+oldName, prefix+newName, &drop, &add); err != nil {
			return err
		}
	}
	if add.Len() == 0 {
		return nil
	}
	return runStdin(dir, drop.String()+add.String(), "git", "update-index", "-z", "--index-info")
}

// moveEntries writes the update-index lines that move the ls-files -s -z
// entries at oldRel (or under it) to newRel. A rename with an entry in a
// merge conflict is left alone.
func moveEntries(ls, oldRel, newRel string, drop, add *strings.Builder) error {
	var d, a strings.Builder
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
		fmt.Fprintf(&d, "0 %s\t%s\x00", strings.Repeat("0", len(f[1])), path)
		fmt.Fprintf(&a, "%s %s\t%s\x00", f[0], f[1], newRel+path[len(oldRel):])
	}
	drop.WriteString(d.String())
	add.WriteString(a.String())
	return nil
}
