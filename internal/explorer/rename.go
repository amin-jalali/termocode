package explorer

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Rename struct{ Old, New string }
type RenamePlan struct {
	Changes   []Rename
	Conflicts []string
}

// PlanRename validates one basename per source. exists contains absolute paths.
func PlanRename(paths, names []string, exists map[string]bool) RenamePlan {
	var p RenamePlan
	if len(paths) != len(names) {
		p.Conflicts = append(p.Conflicts, "Keep one name per line")
		return p
	}
	sources := map[string]bool{}
	targets := map[string]bool{}
	for _, path := range paths {
		sources[path] = true
	}
	for i, old := range paths {
		name := names[i]
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\r\n") {
			p.Conflicts = append(p.Conflicts, filepath.Base(old)+": invalid name")
			continue
		}
		next := filepath.Join(filepath.Dir(old), name)
		if targets[next] {
			p.Conflicts = append(p.Conflicts, name+": two files use this name")
		}
		targets[next] = true
		if exists[next] && !sources[next] {
			p.Conflicts = append(p.Conflicts, name+": name exists")
		}
		for _, other := range paths {
			if old != other && strings.HasPrefix(old, other+string(filepath.Separator)) {
				p.Conflicts = append(p.Conflicts, "Select a folder or its files, not both")
				break
			}
		}
		if old != next {
			p.Changes = append(p.Changes, Rename{old, next})
		}
	}
	return p
}

func CheckRename(paths, names []string) RenamePlan {
	exists := map[string]bool{}
	for i, path := range paths {
		if i < len(names) {
			next := filepath.Join(filepath.Dir(path), names[i])
			if _, err := os.Lstat(next); !os.IsNotExist(err) {
				exists[next] = true
			}
		}
	}
	return PlanRename(paths, names, exists)
}

// RenameStamp fingerprints names, metadata and contents, including folder
// children. Undo refuses when any renamed item has changed.
func RenameStamp(path string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(path, p)
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\x00", rel, fi.Mode(), fi.Size(), fi.ModTime().UnixNano())
		if fi.Mode().IsRegular() {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.Copy(h, f)
			closeErr := f.Close()
			return errors.Join(err, closeErr)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			io.WriteString(h, target)
		}
		return nil
	})
	return fmt.Sprintf("%x", h.Sum(nil)), err
}

// ApplyRename stages all sources under private temporary directories before
// moving to final names, so swaps work. On failure it rolls back completed
// moves. follow is called after each move, including rollback.
func ApplyRename(changes []Rename, follow func(string, string)) error {
	paths, names := []string{}, []string{}
	for _, c := range changes {
		paths = append(paths, c.Old)
		names = append(names, filepath.Base(c.New))
		if filepath.Dir(c.Old) != filepath.Dir(c.New) {
			return errors.New("Rename must stay in the same folder")
		}
	}
	plan := CheckRename(paths, names)
	if len(plan.Conflicts) > 0 {
		return errors.New(strings.Join(plan.Conflicts, "\n"))
	}
	var dirs []string
	var moves []Rename
	defer func() {
		for _, dir := range dirs {
			_ = os.Remove(dir)
		}
	}()
	rollback := func(cause error) error {
		for i := len(moves) - 1; i >= 0; i-- {
			c := moves[i]
			if err := os.Rename(c.New, c.Old); err != nil {
				cause = errors.Join(cause, fmt.Errorf("restore %s: %w", c.Old, err))
			} else if follow != nil {
				follow(c.New, c.Old)
			}
		}
		return cause
	}
	move := func(old, next string) error {
		if err := os.Rename(old, next); err != nil {
			return err
		}
		moves = append(moves, Rename{old, next})
		if follow != nil {
			follow(old, next)
		}
		return nil
	}
	temp := make([]string, len(changes))
	for i, c := range changes {
		dir, err := os.MkdirTemp(filepath.Dir(c.Old), ".termocode-rename-")
		if err != nil {
			return rollback(err)
		}
		dirs = append(dirs, dir)
		temp[i] = filepath.Join(dir, "item")
		if err := move(c.Old, temp[i]); err != nil {
			return rollback(err)
		}
	}
	for i, c := range changes {
		if _, err := os.Lstat(c.New); !os.IsNotExist(err) {
			return rollback(fmt.Errorf("%s: name exists", c.New))
		}
		if err := move(temp[i], c.New); err != nil {
			return rollback(err)
		}
	}
	return nil
}
