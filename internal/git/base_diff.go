package git

import (
	"sort"
	"strings"
)

// BaseDiffFiles lists what a worktree changed since HEAD left base (the
// diff vs base of a lane, CMP-55): the tracked files that differ between
// the work tree and the merge base of base and HEAD, plus new files git
// does not ignore. mb is that merge base (the old side of every file).
func BaseDiffFiles(dir, base string) (mb string, files []string, err error) {
	if err := noOption("ref", base); err != nil {
		return "", nil, err
	}
	mb, err = MergeBase(dir, base, "HEAD")
	if err != nil {
		return "", nil, err
	}
	files, err = WorkTreeDiffFiles(dir, mb)
	if err != nil {
		return "", nil, err
	}
	out, err := runOut(dir, "git", "-c", "core.quotepath=off", "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return "", nil, err
	}
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		seen[f] = true
	}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" && !seen[p] {
			files = append(files, p)
			seen[p] = true
		}
	}
	sort.Strings(files)
	return mb, files, nil
}
