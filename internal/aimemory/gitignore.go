package aimemory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// GitignoreLine is the line EnsureIgnored adds to a workspace's .gitignore.
const GitignoreLine = ".termocode/memory/"

// IgnoreResult says what EnsureIgnored did.
type IgnoreResult int

const (
	IgnoreAlready IgnoreResult = iota // a line of .gitignore covers the folder
	IgnoreAdded                       // the line was added to .gitignore
	IgnoreCreated                     // .gitignore was made with the line
)

// ignoreCovers are the root .gitignore lines that already ignore the
// memory folder.
var ignoreCovers = map[string]bool{
	".termocode": true, ".termocode/": true, ".termocode/*": true, ".termocode/**": true,
	".termocode/memory": true, ".termocode/memory/": true, ".termocode/memory/*": true, ".termocode/memory/**": true,
}

// Covered reports whether the lines of a root .gitignore ignore the
// memory folder (a plain line naming .termocode or .termocode/memory,
// with or without a leading slash) and no later "!" line takes it back.
func Covered(gitignore []byte) bool {
	covered := false
	for _, line := range strings.Split(string(gitignore), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		neg := strings.HasPrefix(line, "!")
		line = strings.TrimPrefix(strings.TrimPrefix(line, "!"), "/")
		switch {
		case ignoreCovers[line]:
			covered = !neg
		case neg && line == ".termocode/memory/"+File:
			covered = false // the file is taken back
		}
	}
	return covered
}

// EnsureIgnored makes the workspace's root .gitignore ignore the memory
// folder: it adds GitignoreLine at the end when no line covers it yet
// (creating the file when missing). Lines already there are never
// changed; the file is only appended to.
func EnsureIgnored(root string) (IgnoreResult, error) {
	p := filepath.Join(root, ".gitignore")
	b, err := os.ReadFile(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.WriteFile(p, []byte(GitignoreLine+"\n"), 0o644); err != nil {
			return IgnoreAlready, err
		}
		return IgnoreCreated, nil
	case err != nil:
		return IgnoreAlready, err
	}
	if Covered(b) {
		return IgnoreAlready, nil
	}
	add := GitignoreLine + "\n"
	if len(b) > 0 && b[len(b)-1] != '\n' {
		add = "\n" + add
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return IgnoreAlready, err
	}
	if _, err := f.WriteString(add); err != nil {
		_ = f.Close()
		return IgnoreAlready, err
	}
	if err := f.Close(); err != nil {
		return IgnoreAlready, err
	}
	return IgnoreAdded, nil
}
