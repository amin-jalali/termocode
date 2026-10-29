package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// modalOverlayFresh is modalOverlay with an empty backdrop cache.
func modalOverlayFresh(base, box string, w, h int, o *ModalOpts) string {
	modalDimCacheV = nil
	return modalOverlay(base, box, w, h, o)
}

// The per-row memo on the cached backdrop (parsed rows, splice cuts)
// never leaks into another frame: a new base, a box of another width
// or another place gives the same frame as a cold cache.
func TestModalOverlayRowMemoInvalidates(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev); modalDimCacheV = nil })
	lipgloss.SetColorProfile(termenv.TrueColor)
	const w, h = 40, 10
	page := func(ch string) string {
		st := lipgloss.NewStyle().Foreground(lipgloss.Color("#c0c0c0")).Background(lipgloss.Color("#202020"))
		rows := make([]string, h)
		for i := range rows {
			rows[i] = st.Render(strings.Repeat(ch, w))
		}
		return strings.Join(rows, "\n")
	}
	box := func(bw int, ch string) string {
		st := lipgloss.NewStyle().Background(lipgloss.Color("#303030"))
		return st.Border(lipgloss.RoundedBorder()).Width(bw - 2).Render(strings.Repeat(ch, bw-2) + "\n" + strings.Repeat(ch, bw-2))
	}
	steps := []struct {
		base, box string
		o         *ModalOpts
	}{
		{page("a"), box(12, "x"), nil},
		{page("a"), box(12, "y"), nil},                              // new card, same page: memo reused
		{page("a"), box(16, "y"), nil},                              // wider card
		{page("b"), box(16, "y"), nil},                              // new page
		{page("b"), box(16, "y"), &ModalOpts{At: true, X: 3, Y: 2}}, // moved card
		{page("b"), box(16, "z"), &ModalOpts{At: true, X: 3, Y: 2}},
		{page("b"), box(10, "z"), &ModalOpts{At: true, X: 3, Y: 2}}, // narrower, same place
	}
	modalDimCacheV = nil
	for i, s := range steps {
		got := modalOverlay(s.base, s.box, w, h, s.o)
		want := modalOverlayFresh(s.base, s.box, w, h, s.o)
		if got != want {
			t.Fatalf("step %d: cached frame differs from a cold one:\ngot:  %q\nwant: %q", i, got, want)
		}
		// Leave the cache warm from the cached call for the next step.
		_ = modalOverlay(s.base, s.box, w, h, s.o)
	}
}
