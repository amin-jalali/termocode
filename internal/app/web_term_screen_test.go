package app

import (
	"testing"
	"time"
)

// webScreenStep is one recorded change: ms after the start, the rows
// replaced [first, last) by n rows, and the row count after it.
type webScreenStep struct{ ms, first, last, n, total int }

// Synthetic fixtures shaped like the three cases of the design: a long
// listing (ls -R) that scrolls, a pager drawing full pages, and a
// monitor that redraws most of a fixed screen every second. A shell
// prompt after the pager quits grows the buffer again.
func webScreenFixtures() map[string]struct {
	height int
	steps  []webScreenStep
	want   []bool // screen mode after each step
} {
	listing := []webScreenStep{}
	for i := 0; i < 20; i++ {
		listing = append(listing, webScreenStep{ms: i * 30, first: 100 + i*5, last: 100 + i*5, n: 5, total: 105 + i*5})
	}
	page := func(ms int) webScreenStep { return webScreenStep{ms: ms, first: 70, last: 100, n: 30, total: 100} }
	return map[string]struct {
		height int
		steps  []webScreenStep
		want   []bool
	}{
		"ls -R": {30, listing, make([]bool, len(listing))},
		"pager": {30, []webScreenStep{page(0), page(400), page(900), {ms: 1500, first: 100, last: 100, n: 1, total: 101}},
			[]bool{false, true, true, false}},
		"monitor": {30, []webScreenStep{
			{ms: 0, first: 70, last: 75, n: 5, total: 100}, {ms: 50, first: 75, last: 92, n: 17, total: 100}, // 22 rows: 73 %
			{ms: 1000, first: 70, last: 90, n: 20, total: 100}, // 20 rows: 67 %
			{ms: 2000, first: 99, last: 100, n: 1, total: 100},
		}, []bool{false, false, true, true}},
		"typing": {30, []webScreenStep{
			{ms: 0, first: 99, last: 100, n: 1, total: 100}, {ms: 300, first: 99, last: 100, n: 1, total: 100},
			{ms: 600, first: 70, last: 100, n: 30, total: 100}, {ms: 900, first: 99, last: 100, n: 1, total: 100},
			{ms: 1200, first: 70, last: 100, n: 30, total: 100},
		}, []bool{false, false, false, false, false}},
	}
}

// Spec (WEB-19): the screen-mode guess on the fixtures: a listing never,
// a pager and a monitor after two full windows, typing never; output
// that grows turns it off.
func TestScreenModeHeuristicFixtures(t *testing.T) {
	for name, fx := range webScreenFixtures() {
		var s webScreen
		s.total = fx.steps[0].total
		t0 := time.Unix(1000, 0)
		for i, st := range fx.steps {
			s.feed(t0.Add(time.Duration(st.ms)*time.Millisecond), st.first, st.last, st.n, st.total, fx.height)
			if s.on != fx.want[i] {
				t.Errorf("%s step %d: screen %v, want %v", name, i, s.on, fx.want[i])
			}
		}
	}
}

// FuzzScreenModeHeuristic: any change sequence never panics, and growing
// output always ends screen mode.
func FuzzScreenModeHeuristic(f *testing.F) {
	f.Add(int64(0), 70, 100, 30, 100, 30, 1)
	f.Add(int64(5), -3, 2, 1<<20, -1, 0, 9)
	f.Fuzz(func(t *testing.T, ms int64, first, last, n, total, height, reps int) {
		var s webScreen
		if n > 1<<12 {
			n = 1 << 12
		}
		t0 := time.Unix(1000, 0)
		for i := 0; i < reps%16; i++ {
			s.feed(t0.Add(time.Duration(ms*int64(i))*time.Millisecond), first, last, n, total, height)
		}
		s.feed(t0.Add(time.Hour), total+1, total+1, 1, total+2, height)
		if s.on && total+2 > total {
			t.Fatal("growing output left screen mode on")
		}
	})
}
