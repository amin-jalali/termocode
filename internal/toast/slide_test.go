package toast

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/anim"
)

func withAnim(t *testing.T) {
	t.Helper()
	saved := anim.Enabled
	anim.Enabled = true
	t.Cleanup(func() { anim.Enabled = saved })
}

// With animations on, an expired toast slides out first and goes away
// once it is off screen.
func TestExpiredToastSlidesOut(t *testing.T) {
	withAnim(t)
	m := New()
	m, _ = m.Push(Info, "Saved")
	for i := 0; i < 500 && m.Animating(); i++ { // the slide in
		m, _ = m.Step()
	}
	m.toasts[0].ExpiresAt = time.Now().Add(-time.Second)
	m, cmd := m.Tick(time.Now())
	if len(m.toasts) != 1 || !m.Animating() {
		t.Fatalf("expired toast: %d toasts, animating %v; want it still there, sliding", len(m.toasts), m.Animating())
	}
	if cmd != nil {
		t.Error("a leaving toast scheduled another expiry tick")
	}
	m, _ = m.Tick(time.Now()) // a second tick does not restart the slide
	steps := 0
	for moving := true; moving && steps < 500; steps++ {
		m, moving = m.Step()
	}
	if !m.Empty() {
		t.Errorf("toast still there after %d frames", steps)
	}
	if _, moving := m.Step(); moving {
		t.Error("an empty stack is still moving")
	}
}

func TestNextTickWaitsForTheSoonestToast(t *testing.T) {
	m := New()
	m, _ = m.Push(Info, "later")
	m, _ = m.Push(Info, "sooner")
	m.toasts[0].ExpiresAt = time.Now().Add(time.Hour)
	m.toasts[1].ExpiresAt = time.Now().Add(10 * time.Millisecond)
	_, cmd := m.Tick(time.Now())
	if cmd == nil {
		t.Fatal("no tick scheduled for a toast that expires soon")
	}
	start := time.Now()
	if _, ok := cmd().(TickMsg); !ok {
		t.Error("the scheduled message is not a TickMsg")
	}
	if time.Since(start) > 5*time.Second {
		t.Error("the tick waited for the later toast")
	}
}

func TestCardsOnNarrowAndZeroWidths(t *testing.T) {
	m := New()
	if m.CompactTitleWidth() != 0 {
		t.Error("an empty stack has a title width")
	}
	m, _ = m.PushDetail(Warn, "A rather long toast title", "with a detail line that is long too")
	if m.Cards(0) != nil || m.CompactCards(0, 2) != nil || m.CompactCards(30, 0) != nil {
		t.Error("cards at zero width or count")
	}
	for _, w := range []int{4, 16, 40} {
		want := w
		if want < 16 {
			want = 16 // the smallest card
		}
		for _, c := range append(m.Cards(w), m.CompactCards(w, 5)...) {
			for i, l := range c.Lines {
				if got := ansi.StringWidth(l); got != want {
					t.Errorf("width %d: line %d is %d cells, want %d", w, i, got, want)
				}
			}
		}
	}
	if got := len(m.CompactCards(40, 5)); got != 1 {
		t.Errorf("CompactCards(…, 5) on one toast = %d cards", got)
	}
	compact := ansi.Strip(strings.Join(m.CompactCards(60, 1)[0].Lines, "\n"))
	if !strings.Contains(compact, "A rather long toast title · ") {
		t.Errorf("compact card lacks title · detail: %q", compact)
	}
}
