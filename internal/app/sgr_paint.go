package app

import (
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// sgrPaint is lipgloss.NewStyle().Foreground(fg).Background(bg).Render(s)
// ("" = no color) for the many short one-line segments of a list row.
// For one line without tabs Render only wraps the text in the style's
// escape codes, so those are worked out once per color pair and color
// profile and reused; other text goes through lipgloss itself.
func sgrPaint(fg, bg lipgloss.Color, s string) string {
	if strings.ContainsAny(s, "\t\n\r") {
		return sgrStyle(fg, bg).Render(s)
	}
	w := sgrWrapFor(fg, bg)
	if w.pre == "" && w.post == "" {
		return s
	}
	return w.pre + s + w.post
}

func sgrStyle(fg, bg lipgloss.Color) lipgloss.Style {
	st := lipgloss.NewStyle()
	if fg != "" {
		st = st.Foreground(fg)
	}
	if bg != "" {
		st = st.Background(bg)
	}
	return st
}

type sgrKey struct {
	fg, bg  lipgloss.Color
	profile termenv.Profile
}

type sgrWrap struct{ pre, post string }

var sgrCache = struct {
	sync.Mutex
	m map[sgrKey]sgrWrap
}{m: map[sgrKey]sgrWrap{}}

func sgrWrapFor(fg, bg lipgloss.Color) sgrWrap {
	key := sgrKey{fg: fg, bg: bg, profile: lipgloss.ColorProfile()}
	sgrCache.Lock()
	defer sgrCache.Unlock()
	if w, ok := sgrCache.m[key]; ok {
		return w
	}
	pre, post, _ := strings.Cut(sgrStyle(fg, bg).Render("x"), "x")
	w := sgrWrap{pre: pre, post: post}
	if len(sgrCache.m) > 4096 { // themes come and go: keep it small
		clear(sgrCache.m)
	}
	sgrCache.m[key] = w
	return w
}
