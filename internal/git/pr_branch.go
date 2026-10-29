package git

import (
	"strconv"
	"strings"
)

// BranchSubjects lists the commit subjects of HEAD that are not in base,
// oldest first. It compares with origin/<base> when that exists (the
// local base may be stale), else with <base>.
func BranchSubjects(dir, base string) ([]string, error) {
	if base == "" || strings.HasPrefix(base, "-") {
		return nil, nil
	}
	ref := base
	if _, err := runOut(dir, "git", "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+base); err == nil {
		ref = "origin/" + base
	}
	out, err := runOut(dir, "git", "log", "--reverse", "--format=%s", ref+"..HEAD")
	if err != nil {
		return nil, err
	}
	var subjects []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			subjects = append(subjects, l)
		}
	}
	return subjects, nil
}

// Unpushed counts the commits of HEAD that its upstream does not have.
// ok is false when the branch has no upstream (it was never pushed).
func Unpushed(dir string) (n int, ok bool) {
	if !HasUpstream(dir) {
		return 0, false
	}
	out, err := runOut(dir, "git", "rev-list", "--count", "@{upstream}..HEAD")
	if err != nil {
		return 0, false
	}
	n, err = strconv.Atoi(strings.TrimSpace(out))
	return n, err == nil
}

// Dirty reports changes to tracked files (staged or not). Untracked
// files do not count: they do not block a branch switch.
func Dirty(dir string) (bool, error) {
	files, err := Status(dir)
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if !f.Untracked() {
			return true, nil
		}
	}
	return false, nil
}
