package git

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Line staging builds a small patch from a file diff that holds only the
// chosen lines, the same trick `git add -p` uses when you edit a hunk.
// The lines are numbered on the new side of the diff (the working file for a
// stage or discard, the index for an unstage).

// ErrNoLineChange means the chosen lines hold no change.
var ErrNoLineChange = errors.New("no change on the selected lines")

// diffHunk is one parsed hunk of a file diff.
type diffHunk struct {
	oldStart, oldCount int
	newStart, newCount int
	tail               string   // text after the closing "@@"
	body               []string // lines with their leading ' ', '+', '-' or '\'
}

// parseFileDiff splits the diff of one file into its header lines and hunks.
func parseFileDiff(diff string) (header []string, hunks []diffHunk) {
	diff = strings.TrimSuffix(diff, "\n")
	if diff == "" {
		return nil, nil
	}
	var cur *diffHunk
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "@@ ") {
			h := parseHunkHeader(l)
			hunks = append(hunks, h)
			cur = &hunks[len(hunks)-1]
			continue
		}
		if cur == nil {
			header = append(header, l)
			continue
		}
		cur.body = append(cur.body, l)
	}
	return header, hunks
}

// parseHunkHeader reads "@@ -a,b +c,d @@ tail".
func parseHunkHeader(l string) diffHunk {
	var h diffHunk
	rest := strings.TrimPrefix(l, "@@ ")
	end := strings.Index(rest, " @@")
	if end < 0 {
		return h
	}
	h.tail = rest[end+3:]
	for _, f := range strings.Fields(rest[:end]) {
		if len(f) < 2 {
			continue
		}
		start, count := parseRange(f[1:])
		if f[0] == '-' {
			h.oldStart, h.oldCount = start, count
		} else if f[0] == '+' {
			h.newStart, h.newCount = start, count
		}
	}
	return h
}

func parseRange(s string) (start, count int) {
	a, b, hasCount := strings.Cut(s, ",")
	start, _ = strconv.Atoi(a)
	count = 1
	if hasCount {
		count, _ = strconv.Atoi(b)
	}
	return start, count
}

// BuildLinePatch returns a patch for the changes on new-side lines
// from..to (1-based, inclusive) of a one-file diff. A removed line counts
// as on the line that follows it. With reverse false the patch is for
// `git apply` (stage); with reverse true it is for `git apply --reverse`
// (unstage, discard).
func BuildLinePatch(diff string, from, to int, reverse bool) (string, error) {
	if from > to {
		from, to = to, from
	}
	header, hunks := parseFileDiff(diff)
	var out []string
	delta := 0 // lines the earlier kept hunks add to the new side
	for _, h := range hunks {
		body, oldN, newN, ok := selectLines(h, from, to, reverse)
		if !ok {
			continue
		}
		start := h.oldStart + delta
		if oldN == 0 {
			start++
		}
		if newN == 0 {
			start--
		}
		out = append(out, fmt.Sprintf("@@ -%d,%d +%d,%d @@%s", h.oldStart, oldN, start, newN, h.tail))
		out = append(out, body...)
		delta += newN - oldN
	}
	if len(out) == 0 {
		return "", ErrNoLineChange
	}
	return strings.Join(append(header, out...), "\n") + "\n", nil
}

// selectLines keeps the chosen changes of one hunk and turns the others into
// context or drops them. ok is false when the hunk holds no chosen change.
func selectLines(h diffHunk, from, to int, reverse bool) (body []string, oldN, newN int, ok bool) {
	n := h.newStart
	for _, l := range h.body {
		if l == "" {
			l = " " // a blank context line some tools strip
		}
		switch l[0] {
		case ' ':
			body = append(body, l)
			oldN++
			newN++
			n++
		case '+':
			if n >= from && n <= to {
				body = append(body, l)
				newN++
				ok = true
			} else if reverse {
				body = append(body, " "+l[1:])
				oldN++
				newN++
			}
			n++
		case '-':
			if n >= from && n <= to {
				body = append(body, l)
				oldN++
				ok = true
			} else if !reverse {
				body = append(body, " "+l[1:])
				oldN++
				newN++
			}
		}
	}
	return body, oldN, newN, ok
}
