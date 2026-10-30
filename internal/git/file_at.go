package git

import (
	"errors"
	"strings"
)

// FileAt returns path as git keeps it at rev: "index" (the staged text),
// "HEAD", a commit hash, or a hash followed by "^" (its parent). found is
// false when the file (or the revision) is not there: a new file has no
// old side. The blob is read raw: the user's text filters never run. At
// most max bytes are read: a longer file is cut at the last line end
// before max and cut is true.
func FileAt(dir, rev, path string, max int) (text string, found, cut bool, err error) {
	if rev == "" || strings.HasPrefix(rev, "-") || strings.ContainsAny(rev, ": \t\n") || path == "" {
		return "", false, false, errors.New("bad revision or path")
	}
	spec := rev + ":" + path
	if rev == "index" {
		spec = ":0:" + path
	}
	if _, err := runOut(dir, "git", "cat-file", "-e", spec); err != nil {
		return "", false, false, nil // not in that revision (or no such revision)
	}
	out, cut, err := runCapped(dir, max, false, "cat-file", "blob", spec)
	if err != nil {
		return "", false, false, err
	}
	return out, true, cut, nil
}
