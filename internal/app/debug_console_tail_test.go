package app

import "testing"

// Spec (WEBP-4): the Debug Console counts its changes (the web publishes
// only when it moved) and gives its newest lines without copying all of
// them; a pending partial line comes last.
func TestDebugConsoleSeqAndTail(t *testing.T) {
	c := newDebugConsole()
	if c.Seq() != 0 {
		t.Fatal("seq of an empty console")
	}
	c.AppendOutput("stdout", "one\ntwo\nthree\nfour\npart")
	s1 := c.Seq()
	if s1 == 0 {
		t.Fatal("seq did not move")
	}
	got, cut := c.Tail(3)
	if !cut || len(got) != 3 || got[0].Text != "three" || got[1].Text != "four" || got[2].Text != "part" {
		t.Fatalf("tail %+v cut=%v", got, cut)
	}
	got, cut = c.Tail(10)
	if cut || len(got) != 5 || got[0].Text != "one" {
		t.Fatalf("whole tail %+v cut=%v", got, cut)
	}
	c.Clear()
	if c.Seq() == s1 {
		t.Fatal("clear did not move the seq")
	}
	if got, cut := c.Tail(3); len(got) != 0 || cut {
		t.Fatalf("after clear %+v", got)
	}
}
