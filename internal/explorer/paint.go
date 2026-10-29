package explorer

import (
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// paint is lipgloss.NewStyle().Foreground(fg).Background(bg).Bold(bold)
// .Render(s) (fg "" = no foreground), made cheap for the tree rows: a
// row has about ten styled pieces and Render works out the whole style
// each time. For one line without tabs, Render is always
// "<start>s<end>" with the start and end set only by the style and the
// colour profile, so those two are kept per style. Text with a tab or
// line break still goes through Render.
func paint(fg, bg lipgloss.Color, bold bool, s string) string {
	if strings.ContainsAny(s, "\t\r\n") {
		return paintStyle(fg, bg, bold).Render(s)
	}
	k := paintKey{fg: fg, bg: bg, bold: bold, prof: lipgloss.ColorProfile()}
	paintCache.Lock()
	pe, ok := paintCache.m[k]
	if !ok {
		out := paintStyle(fg, bg, bold).Render("x")
		i := strings.IndexByte(out, 'x')
		pe = paintEnds{out[:i], out[i+1:]}
		if paintCache.m == nil || len(paintCache.m) >= 4096 {
			paintCache.m = map[paintKey]paintEnds{}
		}
		paintCache.m[k] = pe
	}
	paintCache.Unlock()
	return pe.start + s + pe.end
}

func paintStyle(fg, bg lipgloss.Color, bold bool) lipgloss.Style {
	st := lipgloss.NewStyle()
	if fg != "" {
		st = st.Foreground(fg)
	}
	return st.Background(bg).Bold(bold)
}

type paintKey struct {
	fg, bg lipgloss.Color
	bold   bool
	prof   termenv.Profile
}

type paintEnds struct{ start, end string }

var paintCache struct {
	sync.Mutex
	m map[paintKey]paintEnds
}
