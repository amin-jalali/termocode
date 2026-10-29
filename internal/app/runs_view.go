package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Runs: the sidebar list ───────────────────────────────────────────────
//
//	RUNS ◉            ? keys  / filter
//	─────────────────────────────────
//	today  $0.41 ━━━━━━──────── of $2
//
//	◉ NOW                            3
//	▎ ◉ fix the toast …         $0.031
//	▎   review ●─◉ Fixer · 41 s
//	  ⚠ add a key card…        stalled
//	    plan ●─●─⚠─○─○ 1 m 32 s
//
//	◷ TODAY                          2
//	  ✓ why does the t…         $0.031
//	    review ●─● 58 s
//	  older…              12 this week
//
// Rows are runs grouped by time (NOW, TODAY, older…): two lines each, the
// task with its one word (live $ or a state word), then the profile, a
// mini pipeline and where the run is. A narrow window draws the same list
// on the page with one line per run.

// runsDrawKey is everything the drawn list depends on (cache key).
func (m Model) runsDrawKey(w, h int, focused bool, now time.Time, extra string) string {
	st := m.runs
	s := m.ai
	gen, usage, conv, stream := 0, 0, "", false
	if s != nil {
		gen, usage, conv, stream = s.runGen, s.usageGen, s.conv.ID+strconv.Itoa(len(s.conv.Messages)), s.streaming
	}
	secs := int64(0)
	if stream {
		secs = now.Unix()
	}
	return fmt.Sprintf("%d|%d|%s|%t|%d|%s|%s|%s|%t|%t|%d|%d|%d|%t|%v|%d|%s|%d", gen, usage, conv, stream, secs, st.sel, st.filter,
		st.profile, st.typing, st.older, st.scroll, w, h, focused, theme.CurrentRenderKey(), len(st.chats), extra, st.pageScroll)
}

// renderRunsSidebar draws the view in the sidebar (w×h).
func (m Model) renderRunsSidebar(w, h int) string {
	if m.runs == nil || m.ai == nil || h <= 0 || w <= 0 {
		return m.renderRunsSidebarBare(w, h)
	}
	st := m.runs
	now := runsNow()
	focused := m.focus == FocusExplorer
	key := m.runsDrawKey(w, h, focused, now, "side")
	st.cache.mu.Lock()
	defer st.cache.mu.Unlock()
	if st.cache.sideKey == key && st.cache.side != nil {
		return strings.Join(st.cache.side, "\n")
	}
	c := runsCol()
	cv := newRCCanvas(w, h, c.bgSide)
	items := m.runsItems(now)
	hits := m.drawRunsList(cv, w, h, false, focused, items, now, c)
	st.cache.sideKey, st.cache.side, st.cache.sideHits = key, cv.lines(), hits
	return strings.Join(st.cache.side, "\n")
}

// renderRunsSidebarBare is the view before its state exists.
func (m Model) renderRunsSidebarBare(w, h int) string {
	c := runsCol()
	cv := newRCCanvas(w, h, c.bgSide)
	cv.text(2, 0, "RUNS", rcSt{fg: c.secondary}.B())
	return strings.Join(cv.lines(), "\n")
}

// drawRunsList draws the list on cv (w×h). narrow: one line per run (the
// page of a narrow window). It returns the click targets.
func (m Model) drawRunsList(cv *rcCanvas, w, h int, narrow, focused bool, items []runsItem, now time.Time, c runsColours) []runsHit {
	mp := &m
	st := m.runs
	bg := cv.bg
	var hits []runsHit
	// header
	cv.text(2, 0, "RUNS", rcSt{fg: c.secondary}.B())
	cv.text(7, 0, runsIconLive.String(), rcSt{fg: c.ok})
	switch {
	case st.typing:
		cv.textMax(9, 0, "/ "+st.filter+"▏", w-10, rcSt{fg: c.primary})
	case st.profile != "" || st.filter != "":
		f := st.filter
		if st.profile != "" {
			f = "profile " + st.profile + " " + f
		}
		label := rcCut(strings.TrimSpace(f), max(w-16, 4)) + " · Esc"
		cv.textR(w-1, 0, label, rcSt{fg: c.accent})
	default:
		cv.textR(w-1, 0, "? keys  / filter", rcSt{fg: c.muted})
	}
	hair := c.subtle
	if focused {
		hair = c.accent
	}
	cv.hline(1, 1, w-2, runsDash(), rcSt{fg: hair})
	// today strip
	sp := m.ai.runsSpendOf(m.runsCollect(now))
	budget := m.ai.runsDayBudget()
	x := cv.text(2, 2, "today", rcSt{fg: c.muted})
	x = cv.text(x+2, 2, aiBudgetText(sp.today), rcSt{fg: c.primary}.B())
	if budget > 0 {
		limit := "of $" + strconv.FormatFloat(budget, 'f', -1, 64)
		if narrow {
			limit = "of " + aiBudgetText(budget)
		}
		bw := w - 1 - runeW(limit) - 1 - (x + 2)
		if narrow {
			bw = min(24, bw)
		}
		if bw >= 4 {
			runsBar(cv, x+2, 2, bw, runsSpendParts(sp, budget, c), c.dimmer)
			cv.text(x+3+bw, 2, limit, rcSt{fg: c.dim})
		}
	} else if sp.session > 0 && x+14 < w {
		cv.textMax(x+2, 2, "session "+aiBudgetText(sp.session), w-x-3, rcSt{fg: c.dim})
	}
	// groups
	y := 4
	foot := h - 2 // the hairline row
	rowH := 2
	if narrow {
		rowH = 1
	}
	var now0, today, older []runsItem
	for _, it := range items {
		switch it.group {
		case runsGroupNow:
			now0 = append(now0, it)
		case runsGroupToday:
			today = append(today, it)
		default:
			older = append(older, it)
		}
	}
	// the rows, top to bottom; scroll keeps the selection in view
	type line struct {
		draw func(y int)
		key  string
		h    int
	}
	var ls []line
	group := func(icon string, iconCol lipgloss.Color, name string, n int) {
		ls = append(ls, line{h: 1, draw: func(y int) {
			cv.text(1, y, icon, rcSt{fg: iconCol})
			cv.text(3, y, name, rcSt{fg: c.secondary}.B())
			cv.textR(w-1, y, strconv.Itoa(n), rcSt{fg: c.dim})
		}})
	}
	note := func(text string, col lipgloss.Color) {
		ls = append(ls, line{h: 1, draw: func(y int) { cv.textMax(3, y, text, w-4, rcSt{fg: col}) }})
	}
	addRun := func(it runsItem) {
		ls = append(ls, line{h: rowH, key: it.key, draw: func(y int) {
			mp.drawRunsRow(cv, y, w, it, it.key == st.sel, narrow, focused, now, c, bg, &hits)
		}})
	}
	group(runsIconLive.String(), c.ok, "NOW", len(now0))
	if len(now0) == 0 {
		note("nothing is running", c.dim)
	}
	for _, it := range now0 {
		addRun(it)
	}
	ls = append(ls, line{h: 1, draw: func(int) {}})
	group(theme.GlyphWait.String(), c.dim, "TODAY", len(today))
	if len(today) == 0 {
		note("no runs yet", c.dim)
	}
	for _, it := range today {
		addRun(it)
	}
	if len(older) > 0 {
		ls = append(ls, line{h: 1, key: runsOlderKey, draw: func(y int) {
			sel := st.sel == runsOlderKey
			if sel {
				cv.tint(0, y, w, 1, c.bgSelSide)
				if focused {
					cv.put(0, y, '▎', rcSt{fg: c.accent})
				}
			}
			fold := "older" + rcEllipsis()
			if st.older {
				fold = "older"
			}
			cv.text(3, y, fold, rcSt{fg: c.dim})
			cv.textR(w-1, y, runsOlderText(items, false), rcSt{fg: c.dimmer})
			hits = append(hits, runsHit{x0: 0, x1: w, y: y, id: "older", label: fold})
		}})
		if st.older {
			for _, it := range older {
				addRun(it)
			}
		}
	} else {
		note("older"+rcEllipsis(), c.dimmer)
	}
	// scroll: the selected line must fit between y=4 and the footer
	room := foot - y
	total, selTop, selBot := 0, -1, -1
	for _, l := range ls {
		if l.key != "" && l.key == st.sel {
			selTop, selBot = total, total+l.h
		}
		total += l.h
	}
	if total <= room {
		st.scroll = 0
	} else {
		if selTop >= 0 && selTop < st.scroll {
			st.scroll = selTop
		}
		if selBot > st.scroll+room {
			st.scroll = selBot - room
		}
		st.scroll = max(min(st.scroll, total-room), 0)
	}
	off := 0
	for _, l := range ls {
		top := y + off - st.scroll
		off += l.h
		if top < y || top+l.h > foot {
			continue
		}
		l.draw(top)
	}
	// footer
	cv.hline(1, foot, w-2, runsDash(), rcSt{fg: c.subtle})
	hint := "Enter open · x stop · ? keys"
	if narrow {
		hint = "Enter open · x stop · r resume · / filter · ? keys"
	}
	cv.textMax(2, h-1, hint, w-3, rcSt{fg: c.muted})
	return hits
}

// drawRunsRow draws one run: two lines (sidebar) or one (narrow).
func (m *Model) drawRunsRow(cv *rcCanvas, y, w int, it runsItem, sel, narrow, focused bool, now time.Time, c runsColours, bg lipgloss.Color, hits *[]runsHit) {
	h := 2
	if narrow {
		h = 1
	}
	if sel {
		bg = c.bgSelSide
		cv.tint(0, y, w, h, bg)
		if focused || narrow {
			for j := 0; j < h; j++ {
				cv.put(0, y+j, '▎', rcSt{fg: c.accent})
			}
		}
	}
	*hits = append(*hits, runsHit{x0: 0, x1: w, y: y, id: "run:" + it.key, label: it.task})
	if h == 2 {
		*hits = append(*hits, runsHit{x0: 0, x1: w, y: y + 1, id: "run:" + it.key})
	}
	g, col, word := it.look(c)
	cv.text(3, y, g, rcSt{fg: col})
	wordCol := c.muted
	if it.kind.needsYou() || it.kind == runsFailed {
		wordCol = col
	}
	title := rcSt{fg: c.primary}
	if sel {
		title = title.B()
	}
	if narrow {
		cv.textMax(5, y, it.task, min(24, w-6), title)
		px := 30
		prof := it.run.Profile
		if it.run.By != nil { // G-API: by AI
			prof = "AI·" + prof
		}
		cv.textMax(px, y, prof, 7, rcSt{fg: c.muted})
		x := runsMiniPipe(cv, px+8, y, 12, it, c) + 2
		right := w - 1
		switch it.kind {
		case runsStalled, runsRunning, runsPausing:
			if it.kind == runsStalled {
				lbl := " Stop (x) "
				px := right - rcPillW(lbl)
				cv.pill(px, y, lbl, c.bgChip, c.warn, true)
				*hits = append(*hits, runsHit{x0: px, x1: right, y: y, id: "verb:stop:" + it.key, label: "Stop (x)"})
				cv.textMax(x, y, "no output", px-x-1, rcSt{fg: c.warn})
				return
			}
			tail, tcol := m.runsTail(it, c, now)
			wx := cv.textR(right, y, word, rcSt{fg: wordCol})
			cv.textMax(x, y, tail, wx-x-1, rcSt{fg: tcol})
		case runsPaused, runsResumable:
			lbl := " Resume (r) "
			px := right - rcPillW(lbl)
			cv.pill(px, y, lbl, c.bgChip, c.warn, true)
			*hits = append(*hits, runsHit{x0: px, x1: right, y: y, id: "verb:resume:" + it.key, label: "Resume (r)"})
			tail := strconv.Itoa(it.left) + " of " + strconv.Itoa(len(it.steps)) + " left"
			if runeW(tail) > px-x-1 {
				tail = strconv.Itoa(it.left) + " left"
			}
			if it.kind == runsPaused {
				tail = it.why
			}
			cv.textMax(x, y, tail, px-x-1, rcSt{fg: c.warn})
		case runsFailed:
			wx := cv.textR(right, y, word, rcSt{fg: wordCol})
			cv.textMax(x, y, it.why, wx-x-1, rcSt{fg: c.err})
		default:
			wx := cv.textR(right, y, word, rcSt{fg: wordCol})
			cv.textMax(x, y, runsDur(it.elapsed), wx-x-1, rcSt{fg: c.muted})
		}
		return
	}
	wx := cv.textR(w-1, y, word, rcSt{fg: wordCol})
	cv.textMax(5, y, it.task, wx-5-1, title)
	x := cv.textMax(5, y+1, it.run.Profile, 10, rcSt{fg: c.dim})
	if chip := aiRunByChip(&it.run); chip != "" { // G-API
		x = cv.textMax(x+1, y+1, chip, 5, rcSt{fg: c.accent})
	}
	x = runsMiniPipe(cv, x+1, y+1, max(w-1-x-8, 3), it, c) + 1
	tail, tcol := m.runsTail(it, c, now)
	cv.textMax(x, y+1, tail, w-1-x, rcSt{fg: tcol})
}

// runsSpendParts are the day bar's parts: each model's share of budget.
func runsSpendParts(sp runsSpend, budget float64, c runsColours) []runsPart {
	var out []runsPart
	for i, ms := range sp.byModel {
		out = append(out, runsPart{frac: ms.usd / budget, col: c.model(i)})
	}
	return out
}

func runeW(s string) int { return lipgloss.Width(s) }
