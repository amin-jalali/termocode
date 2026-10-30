package git

import (
	"fmt"
	"strconv"
	"strings"
)

// FileCommits lists the last limit commits that changed path (following
// renames), newest first: short hash, author, committer date and
// subject. A file git does not know has no commits (not an error).
func FileCommits(dir, path string, limit int) ([]CommitHead, error) {
	if limit <= 0 {
		limit = 50
	}
	out, err := runOut(dir, "git", "-c", "core.quotepath=off", "log", "--format=%h%x1f%an%x1f%ct%x1f%s%x1e",
		fmt.Sprintf("-n%d", limit), "--follow", "--", path)
	if err != nil {
		if !hasCommits(dir) {
			return nil, nil
		}
		return nil, err
	}
	var cs []CommitHead
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x1f")
		if len(f) < 4 {
			continue
		}
		c := CommitHead{Hash: f[0], Author: f[1], Subject: f[3]}
		c.When, _ = strconv.ParseInt(f[2], 10, 64)
		cs = append(cs, c)
	}
	return cs, nil
}
