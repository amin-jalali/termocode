package agentusage

import (
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

// Spec (AGX-12): with nothing known the book is empty and the pill is
// hidden (owner D4: no vendor endpoint fills it).
func TestEmptyBookHidesPill(t *testing.T) {
	b := New()
	if ws := b.Windows(t0); len(ws) != 0 {
		t.Errorf("windows %v", ws)
	}
	if long, short := Pill(nil, t0); long != "" || short != "" {
		t.Errorf("pill %q %q", long, short)
	}
}

// Spec (AGX-12): a limit seen on a tab (source 1) gives "Claude limit
// ↻14:30" while it is on; after the reset the window shows nothing new
// but keeps the last limit time for the Plans rows.
func TestLimitOnly(t *testing.T) {
	b := New()
	reset := t0.Add(4*time.Hour + 30*time.Minute)
	b.Limit(Key{CLI: "claude"}, "Claude usage limit reached", reset, t0)
	ws := b.Windows(t0)
	if len(ws) != 1 || !ws[0].Limited(t0) || ws[0].PctKnown() || !ws[0].ResetAt.Equal(reset) || !ws[0].LastLimit.Equal(t0) {
		t.Fatalf("windows %+v", ws)
	}
	if long, short := Pill(ws, t0); long != "✦ Claude limit ↻14:30" || short != "✦limit" {
		t.Errorf("pill %q %q", long, short)
	}
	later := reset.Add(time.Minute)
	ws = b.Windows(later)
	if len(ws) != 1 || ws[0].Limited(later) || !ws[0].LastLimit.Equal(t0) {
		t.Errorf("after the reset %+v", ws)
	}
	if long, _ := Pill(ws, later); long != "" {
		t.Errorf("pill after the reset %q", long)
	}
}

// Spec (AGX-12): the transcripts (source 2) give the used share and
// the reset; tokens today add up per session (a session read twice
// counts once; yesterday's sessions do not count). A limit wins over a
// share while it is on.
func TestTranscriptShareAndMerge(t *testing.T) {
	b := New()
	reset := t0.Add(4*time.Hour + 30*time.Minute)
	k := Key{CLI: "codex", Account: "work"}
	b.Transcript(k, "s1", 62, reset, 1000, t0)
	b.Transcript(k, "s1", 62, reset, 1500, t0.Add(time.Minute)) // the same session, grown
	b.Transcript(k, "s2", 0, time.Time{}, 500, t0)              // no share in this one
	b.Transcript(k, "old", 0, time.Time{}, 9999, t0.Add(-24*time.Hour))
	ws := b.Windows(t0.Add(time.Minute))
	if len(ws) != 1 || !ws[0].PctKnown() || ws[0].UsedPct != 62 || ws[0].TokensToday != 2000 {
		t.Fatalf("windows %+v", ws)
	}
	if long, short := Pill(ws, t0); long != "✦ Codex work 62% ↻14:30" || short != "✦62%" {
		t.Errorf("pill %q %q", long, short)
	}
	b.Limit(k, "You've hit your usage limit", reset, t0.Add(2*time.Minute))
	if long, _ := Pill(b.Windows(t0.Add(2*time.Minute)), t0.Add(2*time.Minute)); long != "✦ Codex work limit ↻14:30" {
		t.Errorf("limit over share %q", long)
	}
	// A share whose window reset is over is not shown any more.
	after := reset.Add(time.Minute)
	if long, _ := Pill(b.Windows(after), after); long != "" {
		t.Errorf("stale share %q", long)
	}
}

// Spec (AGX-12): two accounts are two windows, Claude first; the pill
// shows at most two of them.
func TestPillOrderAndCap(t *testing.T) {
	b := New()
	r := t0.Add(time.Hour)
	b.Transcript(Key{CLI: "codex"}, "a", 10, r, 1, t0)
	b.Limit(Key{CLI: "claude", Account: "work"}, "limit", r, t0)
	b.Transcript(Key{CLI: "claude"}, "b", 40, r, 1, t0)
	ws := b.Windows(t0)
	if len(ws) != 3 || ws[0].CLI != "claude" || ws[0].Account != "" || ws[1].Account != "work" || ws[2].CLI != "codex" {
		t.Fatalf("order %+v", ws)
	}
	if long, short := Pill(ws, t0); long != "✦ Claude 40% ↻11:00 · Claude work limit ↻11:00" || short != "✦40%" {
		t.Errorf("pill %q %q", long, short)
	}
}

// Spec: the book is safe for the UI thread and the transcript readers.
func TestBookConcurrent(t *testing.T) {
	b := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Transcript(Key{CLI: "claude"}, "s", 1, t0.Add(time.Hour), 10, t0)
			b.Limit(Key{CLI: "codex"}, "x", t0.Add(time.Hour), t0)
			_ = b.Windows(t0)
		}()
	}
	wg.Wait()
}
