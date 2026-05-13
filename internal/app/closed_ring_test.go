package app

import (
	"fmt"
	"testing"
)

func TestClosedRingPushPop(t *testing.T) {
	var m Model

	if _, ok := m.popClosed(); ok {
		t.Fatalf("popClosed on empty ring should return ok=false")
	}

	m.pushClosed("/a")
	m.pushClosed("/b")
	m.pushClosed("/c")

	if got, ok := m.popClosed(); !ok || got != "/c" {
		t.Fatalf("expected /c, got %q ok=%v", got, ok)
	}
	if got, ok := m.popClosed(); !ok || got != "/b" {
		t.Fatalf("expected /b, got %q ok=%v", got, ok)
	}
	if got, ok := m.popClosed(); !ok || got != "/a" {
		t.Fatalf("expected /a, got %q ok=%v", got, ok)
	}
	if _, ok := m.popClosed(); ok {
		t.Fatalf("popClosed should be empty after draining")
	}
}

func TestClosedRingDropsEmptyAndDuplicates(t *testing.T) {
	var m Model

	m.pushClosed("")
	m.pushClosed("/x")
	m.pushClosed("/x") // duplicate of most recent — skipped
	m.pushClosed("/y")

	if got, ok := m.popClosed(); !ok || got != "/y" {
		t.Fatalf("expected /y, got %q ok=%v", got, ok)
	}
	if got, ok := m.popClosed(); !ok || got != "/x" {
		t.Fatalf("expected /x, got %q ok=%v", got, ok)
	}
	if _, ok := m.popClosed(); ok {
		t.Fatalf("ring should be empty (empty + duplicate were both skipped)")
	}
}

func TestClosedRingEvictsOldestAtCap(t *testing.T) {
	var m Model

	// Push closedRingCap+5 distinct paths; oldest 5 should be evicted.
	for i := 0; i < closedRingCap+5; i++ {
		m.pushClosed(fmt.Sprintf("/p%d", i))
	}

	if len(m.closedRing) != closedRingCap {
		t.Fatalf("ring size after overflow: got %d want %d", len(m.closedRing), closedRingCap)
	}
	// Most recent push is /p<closedRingCap+4>; oldest surviving is /p5.
	want := fmt.Sprintf("/p%d", closedRingCap+4)
	if got, ok := m.popClosed(); !ok || got != want {
		t.Fatalf("top of ring: got %q want %q", got, want)
	}
	// Drain to oldest.
	var last string
	for {
		got, ok := m.popClosed()
		if !ok {
			break
		}
		last = got
	}
	if last != "/p5" {
		t.Fatalf("oldest surviving: got %q want /p5", last)
	}
}
