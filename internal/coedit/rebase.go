// Package coedit lets two people edit one buffer at the same time
// (G-COL). The editor (nvim on the laptop) stays the one place where the
// text lives. Every edit there is logged with the changedtick it made.
// An edit that was made on an older tick (a page that had not seen the
// laptop's typing yet) is moved over the logged edits: when the text it
// changes was not touched in between it lands where it belongs, and only
// a real overlap is still a conflict.
//
// Positions are zero-based rows and byte columns, the way nvim counts.
// The package is pure: no I/O, no nvim.
package coedit

// Pos is a place in a buffer.
type Pos struct {
	Row int
	Col int
}

// Less reports whether p comes before q.
func (p Pos) Less(q Pos) bool {
	return p.Row < q.Row || (p.Row == q.Row && p.Col < q.Col)
}

// Edit is one change: the text from Start to OldEnd was replaced, and the
// new text ends at NewEnd. All three are absolute positions.
type Edit struct {
	Start  Pos
	OldEnd Pos
	NewEnd Pos
}

// FromBytes makes an Edit from nvim's on_bytes numbers: the end rows
// count from the start row, and an end col counts from the start col
// when its end row is 0.
func FromBytes(startRow, startCol, oldEndRow, oldEndCol, newEndRow, newEndCol int) Edit {
	start := Pos{Row: startRow, Col: startCol}
	return Edit{Start: start, OldEnd: relEnd(start, oldEndRow, oldEndCol), NewEnd: relEnd(start, newEndRow, newEndCol)}
}

func relEnd(start Pos, rows, col int) Pos {
	if rows == 0 {
		return Pos{Row: start.Row, Col: start.Col + col}
	}
	return Pos{Row: start.Row + rows, Col: col}
}

// shift moves p, which is at or after e.OldEnd, by what e did.
func (e Edit) shift(p Pos) Pos {
	if p.Row == e.OldEnd.Row {
		return Pos{Row: e.NewEnd.Row, Col: e.NewEnd.Col + p.Col - e.OldEnd.Col}
	}
	return Pos{Row: p.Row + e.NewEnd.Row - e.OldEnd.Row, Col: p.Col}
}

// Rebase moves the range [start, end) over edits made after the range
// was read, in the order they were made. ok is false when one of them
// changed text inside the range (or replaced text around an insert
// point): that is a real conflict, and the range is not moved.
//
// Edges are not overlaps. An edit that ends where the range starts moves
// the range; one that starts where the range ends leaves it. When both
// are inserts at one point, the range's text goes after the edit's.
func Rebase(start, end Pos, edits []Edit) (Pos, Pos, bool) {
	for _, e := range edits {
		if start.Less(e.OldEnd) && e.Start.Less(end) {
			return start, end, false
		}
		if start == end {
			if start.Less(e.OldEnd) {
				continue // the insert point is at or before the edit's start
			}
			start = e.shift(start)
			end = start
			continue
		}
		if !start.Less(e.OldEnd) {
			start = e.shift(start)
		}
		if e.Start.Less(end) {
			end = e.shift(end)
		}
	}
	return start, end, true
}

// Entry is a logged edit and the changedtick it made.
type Entry struct {
	Tick int
	Edit Edit
}

// Log is the edits of one buffer since the tick Since (when logging
// started, or the oldest edit it still keeps).
type Log struct {
	Since   int
	Entries []Entry
}

// Covers reports whether every edit after tick is in the log.
func (l Log) Covers(tick int) bool { return tick >= l.Since }

// After returns the edits made after tick, oldest first.
func (l Log) After(tick int) []Edit {
	var out []Edit
	for _, e := range l.Entries {
		if e.Tick > tick {
			out = append(out, e.Edit)
		}
	}
	return out
}
