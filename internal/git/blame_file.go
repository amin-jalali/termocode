package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// BlameOptions tune BlameFile.
type BlameOptions struct {
	// Rev blames the file as it was at this commit ("" = the working
	// file, uncommitted lines included).
	Rev string
	// IgnoreWhitespace is `-w`; DetectMoves is `-M -C` (lines moved or
	// copied keep their first commit).
	IgnoreWhitespace bool
	DetectMoves      bool
}

// BlameFile runs one `git blame --porcelain` over the whole file. A
// .git-blame-ignore-revs file at the repo root is used when present, so
// formatting commits stay out of the way.
func BlameFile(dir, path string, o BlameOptions) ([]BlameRangeLine, error) {
	args := []string{"blame", "--porcelain"}
	if o.IgnoreWhitespace {
		args = append(args, "-w")
	}
	if o.DetectMoves {
		args = append(args, "-M", "-C")
	}
	if f := ignoreRevsFile(dir); f != "" {
		args = append(args, "--ignore-revs-file", f)
	}
	if o.Rev != "" {
		args = append(args, o.Rev)
	}
	args = append(args, "--", path)
	out, err := runCombined(dir, "git", args...)
	if err != nil {
		return nil, err
	}
	return parseBlameRange(out), nil
}

// ignoreRevsFile is the repo's .git-blame-ignore-revs, "" when there is
// none.
func ignoreRevsFile(dir string) string {
	top, err := runOut(dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	f := filepath.Join(strings.TrimSpace(top), ".git-blame-ignore-revs")
	if st, err := os.Stat(f); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return f
}

// LineHistory lists the commits that changed lines start..end (1-based,
// inclusive) of path, newest first (`git log -L`). Renames are followed.
func LineHistory(dir, path string, start, end int) ([]CommitHead, error) {
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start
	}
	out, err := runCombined(dir, "git", "-c", "core.quotepath=off", "log",
		fmt.Sprintf("-L%d,%d:%s", start, end, path), "-s", "-n200",
		"--format=%h%x1f%an%x1f%ct%x1f%s%x1e")
	if err != nil {
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
