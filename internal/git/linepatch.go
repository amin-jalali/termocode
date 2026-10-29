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

// ErrLineNewline means the chosen lines cannot be cut out of a change at
// the end of a file that has no final newline: choose the whole change.
var ErrLineNewline = errors.New("this change touches the end of the file, which has no final newline; choose all the changed lines around it, or stage the hunk")

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
		body, oldN, newN, ok, err := selectLines(h, from, to, reverse)
		if err != nil {
			return "", err
		}
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
func selectLines(h diffHunk, from, to int, reverse bool) (body []string, oldN, newN int, ok bool, err error) {
	n := h.newStart
	prev := lineKept // what became of the line before a "\\" mark
	badMark := -1    // body index of a mark that can not stay
	for i, l := range h.body {
		if l == "" {
			l = " " // a blank context line some tools strip
		}
		switch l[0] {
		case ' ':
			body = append(body, l)
			prev = lineKept
			oldN++
			newN++
			n++
		case '+':
			if n >= from && n <= to {
				body = append(body, l)
				prev = lineKept
				newN++
				ok = true
			} else if reverse {
				body = append(body, " "+l[1:])
				prev = lineContext
				oldN++
				newN++
			} else {
				prev = lineDropped
			}
			n++
		case '-':
			// Lines removed at the very end of the file have no line after
			// them, so the last line before them chooses them too.
			if (n >= from && n <= to) || (onlyRemovals(h.body[i:]) && n-1 >= from && n-1 <= to) {
				body = append(body, l)
				prev = lineKept
				oldN++
				ok = true
			} else if !reverse {
				body = append(body, " "+l[1:])
				prev = lineContext
				oldN++
				newN++
			} else {
				prev = lineDropped
			}
		case '\\':
			// "\ No newline at end of file" belongs to the line before it.
			switch prev {
			case lineDropped:
				continue
			case lineContext:
				badMark = len(body)
			}
			body = append(body, l)
		}
	}
	if badMark >= 0 && badMark < len(body)-1 {
		return nil, 0, 0, false, ErrLineNewline
	}
	return body, oldN, newN, ok, nil
}

// What became of a changed line when the patch was cut.
const (
	lineKept = iota
	lineContext
	lineDropped
)

// onlyRemovals tells that nothing but removed lines (and their no-newline
// marks) are left in the hunk: the removal runs to the end of the file.
func onlyRemovals(rest []string) bool {
	for _, l := range rest {
		if l != "" && l[0] != '-' && l[0] != '\\' {
			return false
		}
	}
	return true
}
