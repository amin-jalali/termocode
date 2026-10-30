package app

import "time"

// Screen mode (WEB-19, web-ui-design §2.5). nvim does not say when a
// program in a terminal switches to the alternate screen (an editor, a
// pager, a monitor), so the hub guesses from the row changes it already
// gets: the row count stopped growing and at least 60 % of the last
// window-height rows changed within 200 ms, in two windows in a row.
// Output that grows the buffer again turns it off. The guess can be
// wrong; the page has a manual toggle, and says so.

const (
	webScreenWindow = 200 * time.Millisecond
	webScreenShare  = 0.6
	webScreenHits   = 2
)

// webScreen is the detector of one terminal (Update only). It keeps
// event times, never a timer.
type webScreen struct {
	on      bool
	total   int // row count at the last event
	start   time.Time
	changed map[int]bool // screen rows changed in the current window
	counted bool         // the current window reached the share
	hits    int          // windows in a row that reached it
}

// feed takes one lines change at time now; total is the row count after
// it and height the laptop's terminal height. It reports whether screen
// mode flipped.
func (s *webScreen) feed(now time.Time, first, last, n, total, height int) bool {
	was := s.on
	grew := total > s.total
	s.total = total
	if grew && first >= last { // rows appended (output scrolls): a shell, not a screen
		s.hits, s.counted, s.changed = 0, false, nil
		s.on = false
		return s.on != was
	}
	if height <= 0 {
		return false
	}
	if s.changed == nil || now.Sub(s.start) > webScreenWindow {
		if s.changed != nil && !s.counted {
			s.hits = 0
		}
		s.start, s.changed, s.counted = now, map[int]bool{}, false
	}
	top := total - height
	for r := first; r < first+n; r++ {
		if r >= top && r < total {
			s.changed[r] = true
		}
	}
	if !s.counted && float64(len(s.changed)) >= webScreenShare*float64(height) {
		s.counted = true
		s.hits++
		if s.hits >= webScreenHits {
			s.on = true
		}
	}
	return s.on != was
}
