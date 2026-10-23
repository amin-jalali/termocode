package statusbar

import (
	"slices"
	"sync"

	"github.com/amin-jalali/termocode/internal/theme"
)

// viewCache keeps the last View: the bar is drawn on every frame, but its
// inputs (State, width, theme) change far less often. View is a pure
// function of them.
var viewCache struct {
	sync.Mutex
	ok  bool
	s   State
	w   int
	rk  theme.RenderKey
	out string
}

// sameState reports whether a and b draw the same bar.
// (cache_test.go checks that every State field is compared.)
func sameState(a, b *State) bool {
	return a.Path == b.Path && a.Project == b.Project && a.Lang == b.Lang &&
		a.Line == b.Line && a.Col == b.Col && a.Dirty == b.Dirty && a.Err == b.Err &&
		a.Errors == b.Errors && a.Warnings == b.Warnings && a.Branch == b.Branch &&
		a.Ahead == b.Ahead && a.Behind == b.Behind && a.Indent == b.Indent &&
		a.Encoding == b.Encoding && a.Term == b.Term && a.Debug == b.Debug &&
		a.TermFocused == b.TermFocused && a.TermLabel == b.TermLabel &&
		a.Busy == b.Busy && a.BusyStep == b.BusyStep &&
		a.ShowLSP == b.ShowLSP && slices.Equal(a.LSP, b.LSP) &&
		a.ShowTests == b.ShowTests && a.TestsRunning == b.TestsRunning &&
		a.TestsPassed == b.TestsPassed && a.TestsFailed == b.TestsFailed &&
		a.ShowServices == b.ShowServices && a.ServicesUp == b.ServicesUp &&
		a.ServicesTotal == b.ServicesTotal && a.ServicesBad == b.ServicesBad && a.ServicesStale == b.ServicesStale && a.Agents == b.Agents && a.AgentsReady == b.AgentsReady &&
		a.ShowDatabase == b.ShowDatabase && a.DatabaseName == b.DatabaseName && a.DatabaseProd == b.DatabaseProd &&
		slices.Equal(a.Ext, b.Ext) && a.AI == b.AI && a.AIState == b.AIState
}

func cachedView(w int, s State, draw func() string) string {
	rk := theme.CurrentRenderKey()
	viewCache.Lock()
	defer viewCache.Unlock()
	if viewCache.ok && viewCache.w == w && viewCache.rk == rk && sameState(&viewCache.s, &s) {
		return viewCache.out
	}
	out := draw()
	s.LSP, s.Ext = slices.Clone(s.LSP), slices.Clone(s.Ext)
	viewCache.ok, viewCache.s, viewCache.w, viewCache.rk, viewCache.out = true, s, w, rk, out
	return out
}
