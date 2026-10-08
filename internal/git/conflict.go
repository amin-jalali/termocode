package git

import "strings"

// Merge-conflict parsing — a Go port of mobocode's conflict_parser.dart,
// extended to understand diff3-style base sections.
//
// A conflict region looks like:
//
//	<<<<<<< ours-label
//	...ours lines...
//	||||||| base-label      (optional, merge.conflictStyle=diff3/zdiff3)
//	...base lines...
//	=======
//	...theirs lines...
//	>>>>>>> theirs-label
//
// Everything here is plain string/line manipulation — no git process, no
// nvim — so it is unit-testable and shared by the editor code actions, the
// conflict navigation, and the Source Control panel.

const (
	conflictOursMarker   = "<<<<<<<"
	conflictBaseMarker   = "|||||||"
	conflictSepMarker    = "======="
	conflictTheirsMarker = ">>>>>>>"
)

// ConflictBlock is one parsed merge conflict. Line numbers are 0-based
// indices into the file's line list. Start..End is inclusive and covers the
// `<<<<<<<` line through the `>>>>>>>` line.
type ConflictBlock struct {
	Start int // index of the `<<<<<<<` line
	Base  int // index of the `|||||||` line, or -1 when there is no base section
	Sep   int // index of the `=======` line
	End   int // index of the `>>>>>>>` line

	OursLabel   string // text after `<<<<<<< ` (e.g. "HEAD"); "" when absent
	BaseLabel   string // text after `||||||| `; "" when absent
	TheirsLabel string // text after `>>>>>>> ` (e.g. a branch name)

	Ours   []string // the "current" side body (may be empty)
	BaseLn []string // the common-ancestor body (diff3 only; may be empty)
	Theirs []string // the "incoming" side body (may be empty)
}

// Contains reports whether the 0-based line index falls inside the block
// (markers included).
func (b ConflictBlock) Contains(line int) bool {
	return line >= b.Start && line <= b.End
}

// Resolution picks which side(s) of a conflict to keep.
type Resolution int

const (
	AcceptOurs   Resolution = iota // keep the current (HEAD) side
	AcceptTheirs                   // keep the incoming side
	AcceptBoth                     // keep ours, then theirs
)

// Replacement returns the lines that replace the whole marker region for the
// given resolution. An empty side contributes nothing.
func (b ConflictBlock) Replacement(r Resolution) []string {
	switch r {
	case AcceptOurs:
		return append([]string{}, b.Ours...)
	case AcceptTheirs:
		return append([]string{}, b.Theirs...)
	default:
		out := make([]string, 0, len(b.Ours)+len(b.Theirs))
		out = append(out, b.Ours...)
		return append(out, b.Theirs...)
	}
}

// isConflictMarker reports whether line (already CR-stripped) is the given
// marker: the 7-char marker alone, or followed by a space and a label. A
// longer run (e.g. a Markdown "========" underline) is NOT a marker.
func isConflictMarker(line, marker string) bool {
	if !strings.HasPrefix(line, marker) {
		return false
	}
	rest := line[len(marker):]
	return rest == "" || rest[0] == ' ' || rest[0] == '\t'
}

func conflictLabel(line, marker string) string {
	return strings.TrimSpace(line[len(marker):])
}

// SplitLines splits text on "\n", strips a trailing "\r" from each line, and
// drops the empty element a trailing newline would produce ("a\n" → ["a"]).
func SplitLines(text string) []string {
	if text == "" {
		return nil
	}
	parts := strings.Split(text, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	for i, p := range parts {
		parts[i] = strings.TrimSuffix(p, "\r")
	}
	return parts
}

// HasConflicts is a fast check for any conflict start marker in text.
func HasConflicts(text string) bool {
	for _, l := range SplitLines(text) {
		if isConflictMarker(l, conflictOursMarker) {
			return true
		}
	}
	return false
}

// ParseConflicts parses text into its conflict blocks, in document order.
func ParseConflicts(text string) []ConflictBlock {
	return ParseConflictLines(SplitLines(text))
}

// ParseConflictLines parses an already-split line list. Malformed or nested
// markers are skipped: a `<<<<<<<` not followed by a matching `=======` and
// then `>>>>>>>` (with no intervening `<<<<<<<`) is ignored and scanning
// resumes on the next line.
func ParseConflictLines(lines []string) []ConflictBlock {
	var blocks []ConflictBlock
	i := 0
	for i < len(lines) {
		if !isConflictMarker(lines[i], conflictOursMarker) {
			i++
			continue
		}
		start := i
		base, sep, end := -1, -1, -1

		// Find the separator (optionally passing a diff3 base marker first),
		// bailing if another start marker appears.
		for j := start + 1; j < len(lines); j++ {
			l := lines[j]
			if isConflictMarker(l, conflictOursMarker) || isConflictMarker(l, conflictTheirsMarker) {
				break
			}
			if base < 0 && isConflictMarker(l, conflictBaseMarker) {
				base = j
				continue
			}
			if isConflictMarker(l, conflictSepMarker) {
				sep = j
				break
			}
		}
		if sep < 0 {
			i = start + 1
			continue
		}
		for j := sep + 1; j < len(lines); j++ {
			l := lines[j]
			if isConflictMarker(l, conflictOursMarker) || isConflictMarker(l, conflictSepMarker) ||
				isConflictMarker(l, conflictBaseMarker) {
				break
			}
			if isConflictMarker(l, conflictTheirsMarker) {
				end = j
				break
			}
		}
		if end < 0 {
			i = start + 1
			continue
		}

		b := ConflictBlock{
			Start:       start,
			Base:        base,
			Sep:         sep,
			End:         end,
			OursLabel:   conflictLabel(lines[start], conflictOursMarker),
			TheirsLabel: conflictLabel(lines[end], conflictTheirsMarker),
			Theirs:      append([]string{}, lines[sep+1:end]...),
		}
		if base >= 0 {
			b.Ours = append([]string{}, lines[start+1:base]...)
			b.BaseLabel = conflictLabel(lines[base], conflictBaseMarker)
			b.BaseLn = append([]string{}, lines[base+1:sep]...)
		} else {
			b.Ours = append([]string{}, lines[start+1:sep]...)
		}
		blocks = append(blocks, b)
		i = end + 1
	}
	return blocks
}

// ConflictAt returns the block containing the 0-based line, if any.
func ConflictAt(blocks []ConflictBlock, line int) (ConflictBlock, bool) {
	for _, b := range blocks {
		if b.Contains(line) {
			return b, true
		}
	}
	return ConflictBlock{}, false
}

// ResolveConflict rewrites the full text with block b resolved. It keeps the
// file's line endings (CRLF when the text uses them) and its trailing newline
// (or lack of one). Other blocks are left untouched.
func ResolveConflict(text string, b ConflictBlock, r Resolution) string {
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	trailing := strings.HasSuffix(text, "\n")
	lines := SplitLines(text)
	if b.Start < 0 || b.End >= len(lines) || b.Start > b.End {
		return text
	}
	out := make([]string, 0, len(lines))
	out = append(out, lines[:b.Start]...)
	out = append(out, b.Replacement(r)...)
	out = append(out, lines[b.End+1:]...)
	s := strings.Join(out, eol)
	if trailing && len(out) > 0 {
		s += eol
	}
	return s
}

// Conflicted reports whether the status code is one of git's unmerged
// states (DD, AU, UD, UA, DU, AA, UU).
func (s FileStatus) Conflicted() bool {
	switch s.Code {
	case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
		return true
	}
	return false
}
