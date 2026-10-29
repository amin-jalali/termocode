package git

import (
	"bytes"
	"errors"
	"strings"
)

// The three index stages of an unmerged path. git keeps them for every
// kind of stop: merge, rebase, cherry-pick, revert and stash pop. Stage 1
// is the common ancestor, 2 is "ours" (the current side) and 3 is
// "theirs" (the incoming side). A side that deleted the file has no
// stage; an add/add conflict has no base.

// ErrNotConflicted is returned for a path with no unmerged index entries.
var ErrNotConflicted = errors.New("file has no merge conflict")

// conflictMaxBytes is the largest side read as text; a bigger one counts
// as binary (the merge view only offers keep ours / theirs / delete).
const conflictMaxBytes = 8 << 20

// ConflictSide is one stage of an unmerged path.
type ConflictSide struct {
	Present bool   // the stage exists (false: this side deleted the file)
	Text    string // the blob, when Present and not Binary
	Binary  bool   // NUL bytes or too big to show as text
}

// ConflictStages is an unmerged path's base, ours and theirs.
type ConflictStages struct {
	Path               string
	Base, Ours, Theirs ConflictSide
}

// Binary reports whether any present side is binary.
func (s ConflictStages) Binary() bool {
	return s.Base.Binary || s.Ours.Binary || s.Theirs.Binary
}

// DeletedBy is "ours" or "theirs" when one side deleted the file (or
// "both"), else "".
func (s ConflictStages) DeletedBy() string {
	switch {
	case !s.Ours.Present && !s.Theirs.Present:
		return "both"
	case !s.Ours.Present:
		return "ours"
	case !s.Theirs.Present:
		return "theirs"
	}
	return ""
}

// TextMerge reports whether the conflict can be merged line by line: both
// sides are there and none is binary.
func (s ConflictStages) TextMerge() bool { return s.DeletedBy() == "" && !s.Binary() }

// ReadConflictStages reads the index stages of path (relative to dir).
func ReadConflictStages(dir, path string) (ConflictStages, error) {
	out, err := runOut(dir, "git", "ls-files", "-u", "-z", "--", path)
	if err != nil {
		return ConflictStages{}, err
	}
	st := ConflictStages{Path: path}
	found := false
	for _, rec := range strings.Split(out, "\x00") {
		meta, p, ok := strings.Cut(rec, "\t")
		if !ok || p != path {
			continue
		}
		f := strings.Fields(meta) // mode sha stage
		if len(f) != 3 {
			continue
		}
		var side *ConflictSide
		switch f[2] {
		case "1":
			side = &st.Base
		case "2":
			side = &st.Ours
		case "3":
			side = &st.Theirs
		default:
			continue
		}
		found = true
		side.Present = true
		if strings.HasPrefix(f[0], "16") { // a submodule: no blob to read
			side.Binary = true
			continue
		}
		blob, err := runOut(dir, "git", "cat-file", "blob", f[1])
		if err != nil {
			return ConflictStages{}, err
		}
		if len(blob) > conflictMaxBytes || isBinaryText(blob) {
			side.Binary = true
			continue
		}
		side.Text = blob
	}
	if !found {
		return ConflictStages{}, ErrNotConflicted
	}
	return st, nil
}

// isBinaryText is git's own guess: a NUL byte in the first 8000 bytes.
func isBinaryText(s string) bool {
	n := min(len(s), 8000)
	return bytes.IndexByte([]byte(s[:n]), 0) >= 0
}
