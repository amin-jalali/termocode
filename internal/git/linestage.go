package git

import (
	"errors"
	"fmt"
	"strings"
)

// Errors that tell why a file cannot be staged line by line.
var (
	ErrLineBinary    = errors.New("a binary file has no lines: stage the whole file")
	ErrLineUntracked = errors.New("this file is new and not tracked: stage it first, or delete it")
	ErrLineRename    = errors.New("this file was renamed: stage or unstage the whole file")
)

// StageLines stages the changes on lines from..to (1-based, inclusive) of the
// working file, and nothing else. It is StageHunk for single lines.
func StageLines(dir, file string, from, to int) error {
	return applyLines(dir, file, from, to, false, true, false)
}

// UnstageLines takes the staged changes on lines from..to of the working file
// out of the index. When the file also has unstaged edits, the line numbers
// are moved to where those lines are in the index.
func UnstageLines(dir, file string, from, to int) error {
	unstaged, err := runOut(dir, "git", "diff", "--no-color", "--no-ext-diff", "--", file)
	if err != nil {
		return err
	}
	if unstaged != "" {
		toIndex := workToIndex(unstaged)
		lo, hi := -1, -1
		for l := from; l <= to; l++ {
			if i := toIndex(l); i > 0 {
				if lo < 0 {
					lo = i
				}
				hi = i
			}
		}
		if lo < 0 {
			return ErrNoLineChange
		}
		from, to = lo, hi
	}
	return applyLines(dir, file, from, to, true, true, true)
}

// DiscardLines reverts the unstaged changes on lines from..to of the working
// file. The working file is rewritten: this cannot be undone.
func DiscardLines(dir, file string, from, to int) error {
	return applyLines(dir, file, from, to, false, false, true)
}

// applyLines is applyOneHunk for lines: it reads the diff of the file, cuts
// the chosen lines out of it and hands that patch to `git apply`.
func applyLines(dir, file string, from, to int, diffCached, applyCached, applyReverse bool) error {
	if !diffCached {
		if _, err := runOut(dir, "git", "ls-files", "--error-unmatch", "--", file); err != nil {
			if applyReverse {
				return ErrLineUntracked // nothing in git to go back to
			}
			// A new file has no index entry to cut lines from: record it as
			// "to be added" first, so that its lines show up as a diff.
			if err := runStdin(dir, "", "git", "add", "-N", "--", file); err != nil {
				return err
			}
		}
	}
	diffArgs := []string{"diff", "--no-color", "--no-ext-diff"}
	if diffCached {
		diffArgs = append(diffArgs, "--cached")
	}
	diffArgs = append(diffArgs, "--", file)
	diff, err := runOut(dir, "git", diffArgs...)
	if err != nil {
		return err
	}
	if strings.Contains(diff, "\nBinary files ") || strings.HasPrefix(diff, "Binary files ") || strings.Contains(diff, "GIT binary patch") {
		return ErrLineBinary
	}
	if diffCached && stagedRename(dir, file) {
		return ErrLineRename
	}
	patch, err := BuildLinePatch(diff, from, to, applyReverse)
	if err != nil {
		return err
	}
	args := []string{"apply", "--recount", "--whitespace=nowarn"}
	if applyCached {
		args = append(args, "--cached")
	}
	if applyReverse {
		args = append(args, "--reverse")
	}
	if err := runStdin(dir, patch, "git", append(args, "-")...); err != nil {
		return fmt.Errorf("could not apply the lines: %s", strings.TrimPrefix(err.Error(), "exit status 1: "))
	}
	return nil
}

// stagedRename tells if the index holds file as the new name of a rename.
func stagedRename(dir, file string) bool {
	out, err := runOut(dir, "git", "diff", "--cached", "--no-color", "--name-status", "-M")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Split(l, "\t")
		if len(f) == 3 && strings.HasPrefix(f[0], "R") && f[2] == file {
			return true
		}
	}
	return false
}

// workToIndex reads the diff of the working file against the index and
// returns a function that turns a working line into the index line it came
// from, or -1 for a line that the index does not have.
func workToIndex(diff string) func(int) int {
	_, hunks := parseFileDiff(diff)
	return func(work int) int {
		delta := 0 // index line = working line + delta outside hunks
		for _, h := range hunks {
			n, o := h.newStart, h.oldStart
			if h.newCount == 0 {
				n++
			}
			if h.oldCount == 0 {
				o++
			}
			if work < n {
				break
			}
			for _, l := range h.body {
				switch {
				case l == "" || l[0] == ' ':
					if n == work {
						return o
					}
					n++
					o++
				case l[0] == '+':
					if n == work {
						return -1
					}
					n++
				case l[0] == '-':
					o++
				}
			}
			delta = o - n
		}
		return work + delta
	}
}
