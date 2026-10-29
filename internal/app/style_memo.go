package app

import (
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/theme"
)

// paintMemo keeps short styled strings that the frame draws the same way
// every time (padding runs, the ⋮ glyph): lipgloss rebuilt them on each
// frame. Entries hold for one theme.RenderKey (theme, icons, colour
// profile); a new key starts over.
var paintMemo struct {
	sync.Mutex
	rk theme.RenderKey
	m  map[paintKey]string
}

type paintKey struct {
	fg, bg theme.Color256
	hasFg  bool
	bold   bool
	text   string
	n      int // n spaces when text == ""
}

// maxPaintMemo bounds paintMemo; past it the memo starts over.
const maxPaintMemo = 1024

func paintCached(k paintKey, draw func() string) string {
	rk := theme.CurrentRenderKey()
	c := &paintMemo
	c.Lock()
	defer c.Unlock()
	if c.m == nil || c.rk != rk || len(c.m) > maxPaintMemo {
		c.m, c.rk = make(map[paintKey]string), rk
	}
	if s, ok := c.m[k]; ok {
		return s
	}
	s := draw()
	c.m[k] = s
	return s
}

// bgSpaces is theme.Bg(bg).Render(strings.Repeat(" ", n)).
func bgSpaces(bg theme.Color256, n int) string {
	return paintCached(paintKey{bg: bg, n: n}, func() string {
		return theme.Bg(bg).Render(strings.Repeat(" ", n))
	})
}

// paintFgBg is theme.FgBg(fg, bg).Bold(bold).Render(text).
func paintFgBg(fg, bg theme.Color256, bold bool, text string) string {
	return paintCached(paintKey{fg: fg, bg: bg, hasFg: true, bold: bold, text: text}, func() string {
		return theme.FgBg(fg, bg).Bold(bold).Render(text)
	})
}
