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

// UnstageLines takes the changes on lines from..to of the staged version of
// the file out of the index. The lines are numbered in the index.
func UnstageLines(dir, file string, from, to int) error {
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
