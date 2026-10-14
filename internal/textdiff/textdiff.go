// Package textdiff is a small, dependency-free text diff engine. It is a
// port of mobocode's lib/core/diff/text_diff.dart and gives the diff
// viewer (internal/diffview) two things:
//
//  1. A line diff: a list of Rows (added / removed / context) from an
//     LCS table with the common prefix and suffix trimmed first, so the
//     result is minimal for the usual edit that touches a few lines.
//  2. A word-level (intra-line) diff for paired changed lines: a removed
//     line followed by an added one is a "modify", and both carry Spans
//     that mark the exact words that differ, so the viewer can highlight
//     them brighter than the line's wash (the git-delta / Sublime Merge
//     look).
//
// Hunks splits the diff into maximal runs of changed rows, and Apply
// builds a file from a chosen subset of hunks (accept / reject per hunk).
package textdiff

import "strings"

// Kind is the kind of a Row.
type Kind uint8

const (
	Context Kind = iota
	Added
	Removed
)

// Span is one run of an intra-line diff: text that is the same on both
// sides, or text that changed.
type Span struct {
	Text    string
	Changed bool
}

// Row is one displayed row of a unified / side-by-side diff. Context rows
// have both line numbers; added rows only NewLine, removed rows only
// OldLine. A removed row paired with an added row (a modify) has Paired
// set and Spans describing the intra-line change.
type Row struct {
	Kind    Kind
	Text    string
	OldLine int // 1-based; 0 when the row has no old line
	NewLine int // 1-based; 0 when the row has no new line
	Spans   []Span
	Paired  bool
}

// Hunk is a maximal run of consecutive added / removed rows (context rows
// separate hunks). OldStart / NewStart are 0-based offsets into the old /
// new line lists, so the hunk replaces old[OldStart:OldStart+OldCount]
// with new[NewStart:NewStart+NewCount].
type Hunk struct {
	Index              int
	OldStart, OldCount int
	NewStart, NewCount int
	// First and End are the hunk's rows in Diff.Rows: Rows[First:End].
	First, End int
}

// Added / Removed count the hunk's added and removed lines.
func (h Hunk) Added() int   { return h.NewCount }
func (h Hunk) Removed() int { return h.OldCount }

// Diff is the computed diff of two texts.
type Diff struct {
	Rows           []Row
	Added, Removed int
	Hunks          []Hunk
	// OldEOL / NewEOL report whether each text ends with a newline. Rows
	// ignore a missing final newline (as editors count lines); EOLOnly
	// is set when that is the only difference.
	OldEOL, NewEOL bool
}

// HasChanges reports whether any line changed.
func (d *Diff) HasChanges() bool { return d.Added > 0 || d.Removed > 0 }

// EOLOnly reports whether the texts differ only in the final newline.
func (d *Diff) EOLOnly() bool { return !d.HasChanges() && d.OldEOL != d.NewEOL }

// SplitLines splits text into lines without a phantom empty last line
// for a final newline ("a\n" is one line).
func SplitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func hasEOL(text string) bool { return strings.HasSuffix(text, "\n") }

// Compute diffs oldText against newText: the line diff, then word-level
// spans for paired removed / added runs, then the hunks.
func Compute(oldText, newText string) *Diff {
	ops := lineOps(SplitLines(oldText), SplitLines(newText))
	d := &Diff{OldEOL: hasEOL(oldText), NewEOL: hasEOL(newText)}
	d.Rows = make([]Row, 0, len(ops))
	oldNo, newNo := 0, 0
	for i := 0; i < len(ops); {
		if ops[i].kind == opEqual {
			oldNo++
			newNo++
			d.Rows = append(d.Rows, Row{Kind: Context, Text: ops[i].text, OldLine: oldNo, NewLine: newNo})
			i++
			continue
		}
		// A run of deletes, then a run of inserts: pair del[k] with ins[k]
		// as a modify; the rest is a pure delete or insert.
		d0 := i
		for i < len(ops) && ops[i].kind == opDelete {
			i++
		}
		i0 := i
		for i < len(ops) && ops[i].kind == opInsert {
			i++
		}
		dels, ins := ops[d0:i0], ops[i0:i]
		d.Removed += len(dels)
		d.Added += len(ins)
		pairs := min(len(dels), len(ins))
		for k := 0; k < pairs; k++ {
			oldNo++
			newNo++
			os, ns := wordDiff(dels[k].text, ins[k].text)
			d.Rows = append(d.Rows,
				Row{Kind: Removed, Text: dels[k].text, OldLine: oldNo, Spans: os, Paired: true},
				Row{Kind: Added, Text: ins[k].text, NewLine: newNo, Spans: ns, Paired: true})
		}
		for k := pairs; k < len(dels); k++ {
			oldNo++
			d.Rows = append(d.Rows, Row{Kind: Removed, Text: dels[k].text, OldLine: oldNo})
		}
		for k := pairs; k < len(ins); k++ {
			newNo++
			d.Rows = append(d.Rows, Row{Kind: Added, Text: ins[k].text, NewLine: newNo})
		}
	}
	d.Hunks = hunks(d.Rows)
	return d
}

// hunks groups the maximal runs of changed rows.
func hunks(rows []Row) []Hunk {
	var out []Hunk
	oldNo, newNo := 0, 0
	for i := 0; i < len(rows); {
		if rows[i].Kind == Context {
			oldNo++
			newNo++
			i++
			continue
		}
		h := Hunk{Index: len(out), OldStart: oldNo, NewStart: newNo, First: i}
		for i < len(rows) && rows[i].Kind != Context {
			if rows[i].Kind == Removed {
				oldNo++
				h.OldCount++
			} else {
				newNo++
				h.NewCount++
			}
			i++
		}
		h.End = i
		out = append(out, h)
	}
	return out
}

// Apply returns oldText with the hunks in accepted replaced by their new
// lines; every other hunk keeps its old lines. No hunk gives oldText,
// every hunk gives newText (trailing newline included: it follows
// newText only when every hunk is applied, oldText otherwise).
func Apply(oldText, newText string, accepted map[int]bool) string {
	d := Compute(oldText, newText)
	return d.Apply(accepted)
}

// Apply builds the file for the accepted hunks of d (see the package
// func Apply).
func (d *Diff) Apply(accepted map[int]bool) string {
	all := len(d.Hunks) > 0
	for _, h := range d.Hunks {
		if !accepted[h.Index] {
			all = false
			break
		}
	}
	if len(d.Hunks) == 0 {
		all = accepted[0] // an EOL-only change is "hunk 0"
	}
	var out []string
	hunk := 0
	for i := 0; i < len(d.Rows); {
		r := d.Rows[i]
		if r.Kind == Context {
			out = append(out, r.Text)
			i++
			continue
		}
		take := accepted[hunk]
		for i < len(d.Rows) && d.Rows[i].Kind != Context {
			r := d.Rows[i]
			if (take && r.Kind == Added) || (!take && r.Kind == Removed) {
				out = append(out, r.Text)
			}
			i++
		}
		hunk++
	}
	eol := d.OldEOL
	if all {
		eol = d.NewEOL
	}
	body := strings.Join(out, "\n")
	if eol {
		return body + "\n"
	}
	return body
}

// ── LCS line diff ─────────────────────────────────────────────────────────

type opKind uint8

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

type op struct {
	kind opKind
	text string
}

// lineOps is the edit script of a → b: the common prefix and suffix are
// trimmed, the middle goes through the LCS table.
func lineOps(a, b []string) []op {
	ops := make([]op, 0, max(len(a), len(b)))
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		ops = append(ops, op{opEqual, a[start]})
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	ops = append(ops, lcsDiff(a[start:endA], b[start:endB])...)
	for k := endA; k < len(a); k++ {
		ops = append(ops, op{opEqual, a[k]})
	}
	return ops
}

// MaxLCSCells caps the LCS table (cells = n·m). Above it a big, very
// different middle is diffed as "delete all, insert all": instant, and
// still correct, just not minimal. 1.2M int32 cells is ~5 MB.
const MaxLCSCells = 1_200_000

// lcsDiff is the dynamic-programming LCS diff of two (trimmed) slices.
func lcsDiff(a, b []string) []op {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	out := make([]op, 0, n+m)
	if n == 0 || m == 0 || n*m > MaxLCSCells {
		for _, s := range a {
			out = append(out, op{opDelete, s})
		}
		for _, s := range b {
			out = append(out, op{opInsert, s})
		}
		return out
	}
	// table[i*w+j] = LCS length of a[i:] and b[j:].
	w := m + 1
	table := make([]int32, (n+1)*w)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i*w+j] = table[(i+1)*w+j+1] + 1
			} else if down, right := table[(i+1)*w+j], table[i*w+j+1]; down >= right {
				table[i*w+j] = down
			} else {
				table[i*w+j] = right
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, op{opEqual, a[i]})
			i++
			j++
		case table[(i+1)*w+j] >= table[i*w+j+1]:
			out = append(out, op{opDelete, a[i]})
			i++
		default:
			out = append(out, op{opInsert, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, op{opDelete, a[i]})
	}
	for ; j < m; j++ {
		out = append(out, op{opInsert, b[j]})
	}
	return out
}

// ── Word-level intra-line diff ────────────────────────────────────────────

// wordDiff diffs two changed lines token by token and returns the old and
// new spans, neighbours of the same kind merged.
func wordDiff(oldLine, newLine string) ([]Span, []Span) {
	ops := lcsDiff(Tokenize(oldLine), Tokenize(newLine))
	var os, ns []Span
	for _, o := range ops {
		switch o.kind {
		case opEqual:
			os = appendSpan(os, o.text, false)
			ns = appendSpan(ns, o.text, false)
		case opDelete:
			os = appendSpan(os, o.text, true)
		case opInsert:
			ns = appendSpan(ns, o.text, true)
		}
	}
	return os, ns
}

func appendSpan(spans []Span, text string, changed bool) []Span {
	if n := len(spans); n > 0 && spans[n-1].Changed == changed {
		spans[n-1].Text += text
		return spans
	}
	return append(spans, Span{Text: text, Changed: changed})
}

// Tokenize splits a line into word / whitespace / symbol tokens, so the
// intra-line diff aligns on whole identifiers. Each symbol is its own
// token so neighbouring punctuation diffs precisely.
func Tokenize(line string) []string {
	var out []string
	start, cls := 0, -1
	for i, r := range line {
		k := 2
		switch {
		case r == '_' || r > 127 || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			k = 0
		case r == ' ' || r == '\t':
			k = 1
		}
		if k != cls || k == 2 {
			if i > start {
				out = append(out, line[start:i])
			}
			start, cls = i, k
		}
	}
	if start < len(line) {
		out = append(out, line[start:])
	}
	return out
}
