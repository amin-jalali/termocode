package ai

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// readDefaultLines is how many lines read_file gives when no limit is set.
const readDefaultLines = 2000

// readFileParams are read_file's arguments.
func readFileParams() map[string]any {
	return map[string]any{
		"path":   strParam("file path, relative to the workspace"),
		"offset": map[string]any{"type": "integer", "description": "first line to read, from 1 (default 1)"},
		"limit":  map[string]any{"type": "integer", "description": fmt.Sprintf("how many lines to read (default %d)", readDefaultLines)},
	}
}

const readFileDescription = "Read a file. Each line comes as N|text (N is the line number; it is not part of the file: " +
	"never copy it into edit_file). The first line names the file and its line count. A long file is read " +
	"in parts: offset (first line, from 1) and limit (number of lines, default 2000); the end says how to read on."

// numberedRead is the read_file result for text (the file rel) and the
// offset / limit of args: a header, then "N|line" lines, then how to
// read on when lines are left. The whole result stays under
// maxReadBytes, cut on a line (one longer line is cut and says so).
func numberedRead(rel, text string, args map[string]any) (string, error) {
	if text == "" {
		return rel + " (empty)\n", nil
	}
	ls := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	total := len(ls)
	offset, limit := ArgInt(args, "offset"), ArgInt(args, "limit")
	if offset < 1 {
		offset = 1
	}
	if limit < 1 {
		limit = readDefaultLines
	}
	if offset > total {
		return "", fmt.Errorf("offset %d is past the end: %s has %d lines", offset, rel, total)
	}
	end := min(total, offset-1+limit)
	var body strings.Builder
	budget := maxReadBytes - 200 // room for the header and the end note
	last := offset - 1
	for i := offset; i <= end; i++ {
		line := strings.TrimSuffix(ls[i-1], "\r")
		row := strconv.Itoa(i) + "|" + line + "\n"
		if body.Len()+len(row) > budget {
			if i == offset { // one line bigger than the cap
				cut := line[:max(0, budget-body.Len()-64)]
				for len(cut) > 0 && !validCut(cut) {
					cut = cut[:len(cut)-1]
				}
				fmt.Fprintf(&body, "%d|%s… (line cut: %d more bytes)\n", i, cut, len(line)-len(cut))
				last = i
			}
			break
		}
		body.WriteString(row)
		last = i
	}
	var b strings.Builder
	if offset == 1 && last == total {
		word := "lines"
		if total == 1 {
			word = "line"
		}
		fmt.Fprintf(&b, "%s (%d %s)\n", rel, total, word)
	} else {
		fmt.Fprintf(&b, "%s: lines %d-%d of %d\n", rel, offset, last, total)
	}
	b.WriteString(body.String())
	if last < total {
		fmt.Fprintf(&b, "… %d more lines: read_file with offset=%d\n", total-last, last+1)
	}
	return b.String(), nil
}

// lineNumberPrefix is the "N|" read_file puts before each line.
var lineNumberPrefix = regexp.MustCompile(`^[0-9]+\|`)

// stripLineNumbers takes the "N|" read_file numbers off every line of s.
// ok is false when s has no lines, or some non-empty line has no number.
func stripLineNumbers(s string) (string, bool) {
	ls := strings.Split(s, "\n")
	seen := false
	for i, l := range ls {
		if l == "" {
			continue
		}
		loc := lineNumberPrefix.FindStringIndex(l)
		if loc == nil {
			return s, false
		}
		ls[i], seen = l[loc[1]:], true
	}
	return strings.Join(ls, "\n"), seen
}

// hasNumberedLine reports whether some line of s starts with "N|".
func hasNumberedLine(s string) bool {
	for _, l := range strings.Split(s, "\n") {
		if lineNumberPrefix.MatchString(l) {
			return true
		}
	}
	return false
}

// errCopiedNumbers is the edit_file error for old_text that still has
// read_file's line numbers.
var errCopiedNumbers = errors.New("old_text was not found. It starts its lines with N| line numbers: those come from read_file and are not in the file. " +
	"Copy only the text after the |, in old_text and new_text")
