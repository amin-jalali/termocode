package ghci

import (
	"context"
	"strconv"
	"strings"
)

// FileDiff is one file of `gh pr diff`, rebuilt from its hunks: Old and
// New hold the hunk lines only (context, removed / context, added), so
// the diff card shows exactly the changed parts.
type FileDiff struct {
	Path     string
	OldPath  string // differs from Path for a rename
	Old, New string
	Add, Del int
	Binary   bool
	Created  bool // a new file
	Deleted  bool
}

// PRDiff reads the diff of a pull request (`gh pr diff N`) per file.
func PRDiff(ctx context.Context, run Runner, dir string, number int) ([]FileDiff, error) {
	out, errOut, err := run(ctx, dir, "pr", "diff", strconv.Itoa(number))
	if err != nil {
		return nil, classify(err, errOut)
	}
	return ParseDiff(out), nil
}

// ParseDiff splits a unified git diff into files.
func ParseDiff(text string) []FileDiff {
	var files []FileDiff
	var cur *FileDiff
	var oldB, newB []string
	flush := func() {
		if cur == nil {
			return
		}
		cur.Old, cur.New = strings.Join(oldB, "\n"), strings.Join(newB, "\n")
		if len(oldB) > 0 {
			cur.Old += "\n"
		}
		if len(newB) > 0 {
			cur.New += "\n"
		}
		files = append(files, *cur)
		cur, oldB, newB = nil, nil, nil
	}
	inHunk := false
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			inHunk = false
			a, b := splitGitHeader(strings.TrimPrefix(line, "diff --git "))
			cur = &FileDiff{Path: b, OldPath: a}
		case cur == nil:
		case !inHunk && strings.HasPrefix(line, "new file mode"):
			cur.Created = true
		case !inHunk && strings.HasPrefix(line, "deleted file mode"):
			cur.Deleted = true
		case !inHunk && strings.HasPrefix(line, "rename from "):
			cur.OldPath = strings.TrimPrefix(line, "rename from ")
		case !inHunk && strings.HasPrefix(line, "rename to "):
			cur.Path = strings.TrimPrefix(line, "rename to ")
		case !inHunk && strings.HasPrefix(line, "Binary files "):
			cur.Binary = true
		case strings.HasPrefix(line, "@@"):
			inHunk = true
			if len(oldB) > 0 || len(newB) > 0 {
				oldB, newB = append(oldB, "…"), append(newB, "…") // a gap between hunks
			}
		case !inHunk:
		case strings.HasPrefix(line, "+"):
			newB = append(newB, line[1:])
			cur.Add++
		case strings.HasPrefix(line, "-"):
			oldB = append(oldB, line[1:])
			cur.Del++
		case strings.HasPrefix(line, " "):
			oldB, newB = append(oldB, line[1:]), append(newB, line[1:])
		case line == "":
			oldB, newB = append(oldB, ""), append(newB, "")
		}
	}
	flush()
	return files
}

// splitGitHeader reads "a/x b/x" of a diff --git line.
func splitGitHeader(s string) (a, b string) {
	if i := strings.Index(s, " b/"); i >= 0 {
		return strings.TrimPrefix(s[:i], "a/"), s[i+3:]
	}
	f := strings.Fields(s)
	if len(f) == 2 {
		return strings.TrimPrefix(f[0], "a/"), strings.TrimPrefix(f[1], "b/")
	}
	return s, s
}
