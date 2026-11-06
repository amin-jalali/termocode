package git

import (
	"fmt"
	"strings"
)

// HunkPeek is the change under one line of a file, as Stage and Revert see
// it (GITX-21): the work tree against the index, or, when that has nothing
// there, the index against HEAD (Staged).
type HunkPeek struct {
	Staged bool     // the change is only in the index; Unstage takes it back
	Header string   // "@@ -a,b +c,d @@ …"
	Lines  []string // the hunk body: " context", "-old", "+new"
	Index  int      // this hunk's number, 1-based
	Count  int      // how many hunks the diff has
	Starts []int    // first changed line of every hunk (where its mark is drawn)
}

// PeekHunk finds the change under line (1-based) of file. It matches the
// line the same way StageHunk / DiscardHunk do, so the card's buttons act
// on exactly the hunk it shows.
func PeekHunk(dir, file string, line int) (HunkPeek, error) {
	for _, cached := range []bool{false, true} {
		args := []string{"diff", "--no-color"}
		if cached {
			args = append(args, "--cached")
		}
		diff, err := runOut(dir, "git", append(args, "--", file)...)
		if err != nil {
			return HunkPeek{}, err
		}
		if p, ok := peekIn(diff, line); ok {
			p.Staged = cached
			return p, nil
		}
	}
	return HunkPeek{}, fmt.Errorf("no change on line %d", line)
}

// peekIn reads the hunks of one file's diff and picks the one whose new
// side holds line (the extractHunkPatch rule: an empty side counts as one
// line).
func peekIn(diff string, line int) (HunkPeek, bool) {
	var p HunkPeek
	found := false
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "@@") {
			continue
		}
		header := lines[i]
		j := i + 1
		for j < len(lines) && !strings.HasPrefix(lines[j], "@@") {
			j++
		}
		body := lines[i+1 : j]
		start, count := parseHunkNewRange(header)
		p.Count++
		p.Starts = append(p.Starts, firstChanged(start, body))
		if count <= 0 {
			count = 1
		}
		if !found && line >= start && line < start+count {
			found = true
			p.Header, p.Lines, p.Index = header, append([]string(nil), body...), p.Count
		}
		i = j - 1
	}
	return p, found
}

// firstChanged is the new-side line of the first change of a hunk: the
// first added line, or for a run that only removes lines the line above
// the gap (where the git signs draw the deletion mark).
func firstChanged(start int, body []string) int {
	n := start
	for k, l := range body {
		switch {
		case strings.HasPrefix(l, "+"):
			return n
		case strings.HasPrefix(l, "-"):
			for _, r := range body[k:] {
				if strings.HasPrefix(r, "+") {
					return n
				}
				if !strings.HasPrefix(r, "-") && !strings.HasPrefix(r, `\`) {
					break
				}
			}
			return max(n-1, 1)
		case strings.HasPrefix(l, `\`):
		default:
			n++
		}
	}
	return max(start, 1)
}
