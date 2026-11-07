package coedit

import "testing"

func p(r, c int) Pos { return Pos{Row: r, Col: c} }

// laptop types "abc" at row 0 col 2: ends move only on that row.
func insertAt(r, c, n int) Edit { return Edit{Start: p(r, c), OldEnd: p(r, c), NewEnd: p(r, c+n)} }

func TestRebaseEditBeforeShiftsTheRange(t *testing.T) {
	// The laptop adds two lines above: the page's range moves down two rows.
	e := Edit{Start: p(1, 0), OldEnd: p(1, 0), NewEnd: p(3, 0)}
	s, end, ok := Rebase(p(5, 2), p(5, 4), []Edit{e})
	if !ok || s != p(7, 2) || end != p(7, 4) {
		t.Fatalf("got %v %v %v, want {7 2} {7 4} true", s, end, ok)
	}
}

func TestRebaseSameRowShiftsColumns(t *testing.T) {
	s, end, ok := Rebase(p(0, 5), p(0, 8), []Edit{insertAt(0, 1, 3)})
	if !ok || s != p(0, 8) || end != p(0, 11) {
		t.Fatalf("got %v %v %v, want {0 8} {0 11} true", s, end, ok)
	}
}

func TestRebaseEditAfterLeavesTheRange(t *testing.T) {
	s, end, ok := Rebase(p(0, 1), p(0, 3), []Edit{insertAt(4, 0, 9)})
	if !ok || s != p(0, 1) || end != p(0, 3) {
		t.Fatalf("got %v %v %v, want unchanged", s, end, ok)
	}
}

func TestRebaseOverlapIsAConflict(t *testing.T) {
	// The laptop deleted text the page also changed.
	e := Edit{Start: p(0, 2), OldEnd: p(0, 6), NewEnd: p(0, 2)}
	if _, _, ok := Rebase(p(0, 4), p(0, 8), []Edit{e}); ok {
		t.Fatal("an overlapping edit must be a conflict")
	}
	// A page insert inside text the laptop replaced.
	if _, _, ok := Rebase(p(0, 3), p(0, 3), []Edit{e}); ok {
		t.Fatal("an insert inside a replaced range must be a conflict")
	}
	// A laptop insert inside the page's range.
	if _, _, ok := Rebase(p(0, 0), p(0, 9), []Edit{insertAt(0, 4, 1)}); ok {
		t.Fatal("an insert inside the page's range must be a conflict")
	}
}

func TestRebaseTouchingEdgesIsNoConflict(t *testing.T) {
	// The laptop inserts right where the page's range ends: the range stays.
	s, end, ok := Rebase(p(0, 0), p(0, 4), []Edit{insertAt(0, 4, 2)})
	if !ok || s != p(0, 0) || end != p(0, 4) {
		t.Fatalf("end edge: got %v %v %v", s, end, ok)
	}
	// The laptop inserts right where the page's range starts: it moves.
	s, end, ok = Rebase(p(0, 4), p(0, 6), []Edit{insertAt(0, 4, 2)})
	if !ok || s != p(0, 6) || end != p(0, 8) {
		t.Fatalf("start edge: got %v %v %v", s, end, ok)
	}
}

func TestRebaseTwoInsertsAtOnePointPutThePageAfter(t *testing.T) {
	s, end, ok := Rebase(p(2, 3), p(2, 3), []Edit{insertAt(2, 3, 5)})
	if !ok || s != p(2, 8) || end != p(2, 8) {
		t.Fatalf("got %v %v %v, want {2 8} {2 8} true", s, end, ok)
	}
}

func TestRebaseManyEditsInOrder(t *testing.T) {
	edits := []Edit{
		insertAt(0, 0, 2), // "xx" at the start of row 0
		{Start: p(0, 10), OldEnd: p(2, 1), NewEnd: p(0, 10)}, // rows 0..2 joined
	}
	// The page's range was on row 3 before both edits: two rows up after
	// the join, the columns stay (the join ends on an earlier row).
	s, end, ok := Rebase(p(3, 1), p(3, 4), edits)
	if !ok || s != p(1, 1) || end != p(1, 4) {
		t.Fatalf("got %v %v %v, want {1 1} {1 4} true", s, end, ok)
	}
}

func TestRebaseRangeOnTheLastRowOfAMultiRowEdit(t *testing.T) {
	// The laptop replaced rows 1..2 col 3 with one short line ending at col 1.
	e := Edit{Start: p(1, 0), OldEnd: p(2, 3), NewEnd: p(1, 1)}
	s, end, ok := Rebase(p(2, 5), p(2, 7), []Edit{e})
	if !ok || s != p(1, 3) || end != p(1, 5) {
		t.Fatalf("got %v %v %v, want {1 3} {1 5} true", s, end, ok)
	}
}

func TestFromBytesMakesAbsoluteEnds(t *testing.T) {
	// nvim on_bytes: an end row of 0 means the end col counts from the start col.
	e := FromBytes(4, 6, 0, 2, 1, 3)
	if e.Start != p(4, 6) || e.OldEnd != p(4, 8) || e.NewEnd != p(5, 3) {
		t.Fatalf("got %+v", e)
	}
}

func TestLogCovers(t *testing.T) {
	l := Log{Since: 10}
	if l.Covers(9) {
		t.Fatal("a tick before the log started is not covered")
	}
	if !l.Covers(10) || !l.Covers(12) {
		t.Fatal("a tick from the log's start on is covered")
	}
}

func TestLogAfterKeepsOnlyNewerEdits(t *testing.T) {
	l := Log{Since: 1, Entries: []Entry{
		{Tick: 2, Edit: insertAt(0, 0, 1)},
		{Tick: 3, Edit: insertAt(0, 1, 1)},
		{Tick: 3, Edit: insertAt(0, 2, 1)},
		{Tick: 5, Edit: insertAt(0, 3, 1)},
	}}
	got := l.After(3)
	if len(got) != 1 || got[0] != insertAt(0, 3, 1) {
		t.Fatalf("got %+v", got)
	}
}
