package app

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Runs: the page ───────────────────────────────────────────────────────
//
// One editor tab. The overview ("◉ Runs"): what the AI spent today, the
// live and stopped runs (NOW) as lanes with their pipeline (the step that
// works now is green, a stalled or paused one amber) and today's runs.
// One run ("◉ fix the toast flicker"): the pipeline as boxes, a timeline,
// the steps with their time, tokens, cost and share of the run's cost,
// the budget and what happened. A window narrower than runsNarrowW gets
// the compact forms (one line per run; one line of chips).

// runsPageFootH is the fixed rows at the bottom: rule, pills, gap, hints.
const runsPageFootH = 4

// renderRunsPage draws the page (w×h, the editor area).
func (m Model) renderRunsPage(w, h int) string {
	st := m.runs
	if st == nil || m.ai == nil || w <= 0 || h <= 0 {
		return strings.Join(newRCCanvas(w, h, theme.LG(theme.BgEditor)).lines(), "\n")
	}
	now := runsNow()
	key := m.runsDrawKey(w, h, m.focus == FocusEditor, now, "page|"+st.detail)
	st.cache.mu.Lock()
	defer st.cache.mu.Unlock()
	if st.cache.pageKy == key && st.cache.page != nil {
		return strings.Join(st.cache.page, "\n")
	}
	lines, hits := m.drawRunsPage(w, h, now)
	st.cache.pageKy, st.cache.page, st.cache.pageHits = key, lines, hits
	return strings.Join(lines, "\n")
}

// drawRunsPage builds the page rows and its click targets (page cells).
func (m Model) drawRunsPage(w, h int, now time.Time) ([]string, []runsHit) {
	mp := &m
	st := m.runs
	c := runsCol()
	compact := w < 100
	items := mp.runsItems(now)
	if compact && st.detail == "" { // the narrow list (the sidebar's place)
		cv := newRCCanvas(w, h, c.bgSide)
		hits := m.drawRunsList(cv, w, h, true, true, items, now, c)
		return cv.lines(), hits
	}
	var it runsItem
	have := false
	if st.detail != "" {
		it, have = mp.runsFind(st.detail)
		if !have {
			st.detail = ""
		}
	}
	bodyH := max(h-runsPageFootH, 1)
	// draw the body on a tall canvas, then show its window
	tall := newRCCanvas(w, max(bodyH, 200), c.bgEditor)
	var hits []runsHit
	var used int
	var foot func(cv *rcCanvas, y int) []runsHit
	var hint string
	switch {
	case have && compact:
		used, hits = mp.drawRunDetailCompact(tall, w, it, now, c)
		foot = func(cv *rcCanvas, y int) []runsHit { return mp.drawRunPills(cv, y, it, true, c) }
		hint = "Esc back to the list · x stop · p pause · c chat · ? keys"
	case have:
		used, hits = mp.drawRunDetail(tall, w, it, now, c)
		foot = func(cv *rcCanvas, y int) []runsHit { return mp.drawRunPills(cv, y, it, false, c) }
		hint = "Esc back to the list · x stop · p pause · r resume · c open in chat · i profile in Itermoc · ? keys"
	case len(mp.runsCollect(now)) == 0:
		used, hits = mp.drawRunsEmpty(tall, w, bodyH, now, c)
		hint = "c open the chat · i Itermoc · ? keys"
	default:
		used, hits = mp.drawRunsOverview(tall, w, items, now, c)
		foot = func(cv *rcCanvas, y int) []runsHit {
			sel, ok := mp.runsFind("")
			return mp.drawOverviewPills(cv, y, sel, ok, c)
		}
		hint = "↑↓ pick a run · Enter open · x stop · r resume · / filter · ? keys"
	}
	st.pageM = max(used-bodyH, 0)
	st.pageScroll = min(st.pageScroll, st.pageM)
	cv := newRCCanvas(w, h, c.bgEditor)
	for y := 0; y < bodyH; y++ {
		if sy := y + st.pageScroll; sy < tall.h {
			cv.cells[y] = tall.cells[sy]
		}
	}
	var out []runsHit
	for _, hit := range hits {
		hit.y -= st.pageScroll
		if hit.y >= 0 && hit.y < bodyH {
			out = append(out, hit)
		}
	}
	fy := h - runsPageFootH
	cv.hline(2, fy, w-4, runsDash(), rcSt{fg: c.subtle})
	if foot != nil {
		out = append(out, foot(cv, fy+1)...)
	}
	cv.textMax(0, h-1, hint, w-1, rcSt{fg: c.muted})
	return cv.lines(), out
}

// ── The overview ─────────────────────────────────────────────────────────

func (m *Model) drawRunsOverview(cv *rcCanvas, w int, items []runsItem, now time.Time, c runsColours) (int, []runsHit) {
	var hits []runsHit
	st := m.runs
	cv.text(2, 0, runsIconLive.String()+" Runs", rcSt{fg: c.ok}.B())
	cv.textMax(9, 0, "what the AI is doing now", w/2-9, rcSt{fg: c.secondary})
	x := w - 2
	counts := runsCounts(items, c)
	if len(counts) == 0 {
		cv.textR(x, 0, "nothing running", rcSt{fg: c.dim})
	}
	for i := len(counts) - 1; i >= 0; i-- {
		x = cv.textR(x, 0, counts[i][0].(string), rcSt{fg: counts[i][1].(lipgloss.Color)}) - 3
	}
	cv.hline(2, 1, w-4, runsDash(), rcSt{fg: c.subtle})
	y := m.drawSpendStrip(cv, 4, 3, w-8, now, c)
	y++
	var nowItems, today, older []runsItem
	for _, it := range items {
		switch it.group {
		case runsGroupNow:
			nowItems = append(nowItems, it)
		case runsGroupToday:
			today = append(today, it)
		default:
			older = append(older, it)
		}
	}
	cv.text(4, y, "NOW", rcSt{fg: c.muted}.B())
	cv.text(9, y, strconv.Itoa(len(nowItems)), rcSt{fg: c.dim})
	if w >= 110 {
		cv.textR(w-4, y, "Enter opens the run · the green step is where it is right now", rcSt{fg: c.dim})
	}
	y += 2
	if len(nowItems) == 0 {
		cv.text(6, y, "nothing is running · send a message in the chat to start a run", rcSt{fg: c.dim})
		y += 2
	}
	for _, it := range nowItems {
		y = m.drawLane(cv, y, w, it, it.key == st.sel, now, c, &hits)
	}
	y++
	cv.text(4, y, "TODAY", rcSt{fg: c.muted}.B())
	cv.text(11, y, strconv.Itoa(len(today)), rcSt{fg: c.dim})
	y += 2
	if len(today) == 0 {
		cv.text(6, y, "no other runs today", rcSt{fg: c.dim})
		y++
	}
	row := func(it runsItem) {
		sel := it.key == st.sel
		if sel {
			cv.tint(2, y, w-4, 1, c.bgSel)
			cv.put(2, y, '▎', rcSt{fg: c.accent})
		}
		g, col, _ := it.look(c)
		cv.text(4, y, g, rcSt{fg: col})
		cv.text(6, y, runsClock(it.started), rcSt{fg: c.dim})
		taskW := 30
		if w < 110 {
			taskW = max(w-13-30, 10)
		}
		cv.textMax(13, y, it.task, taskW, rcSt{fg: c.secondary})
		x := 13 + taskW + 1
		if w >= 110 {
			pt := it.run.Profile
			if t := m.ai.runsProfileTitle(it.run.Profile); t != "" {
				pt += " · " + t
			}
			cv.textMax(44, y, pt, 33, rcSt{fg: c.dim})
			x = 78
		}
		tok := ""
		if it.anyTok {
			tok = runsTok(it.tokens) + " tok"
		}
		cost := runsCostText(it)
		cv.text(x, y, padRight(runsDur(it.elapsed), 10), rcSt{fg: c.muted})
		cv.textR(x+20, y, tok, rcSt{fg: c.muted})
		wx := max(cv.text(x+23, y, cost, rcSt{fg: c.muted})+1, x+34)
		if it.kind == runsFailed || it.kind == runsCancelled || it.kind == runsResumable {
			wc := c.err
			if it.kind != runsFailed {
				wc = col
			}
			cv.textMax(wx, y, it.why, w-4-wx, rcSt{fg: wc})
		}
		hits = append(hits, runsHit{x0: 2, x1: w - 2, y: y, id: "run:" + it.key, label: it.task})
		y++
	}
	for _, it := range today {
		row(it)
	}
	if len(older) > 0 {
		sel := st.sel == runsOlderKey
		if sel {
			cv.tint(2, y, w-4, 1, c.bgSel)
			cv.put(2, y, '▎', rcSt{fg: c.accent})
		}
		lbl := "older" + rcEllipsis()
		cv.text(4, y, lbl, rcSt{fg: c.dim})
		more := runsOlderText(items, true) + " · Enter shows them"
		if st.older {
			more = runsOlderText(items, true)
		}
		cv.textMax(13, y, more, w-17, rcSt{fg: c.dimmer})
		hits = append(hits, runsHit{x0: 2, x1: w - 2, y: y, id: "older", label: lbl})
		y++
		if st.older {
			for _, it := range older {
				row(it)
			}
		}
	}
	return y + 1, hits
}

// drawSpendStrip draws SPEND TODAY at (x0, y), w wide; returns the row
// after it.
func (m *Model) drawSpendStrip(cv *rcCanvas, x0, y, w int, now time.Time, c runsColours) int {
	sp := m.ai.runsSpendOf(m.runsCollect(now))
	budget := m.ai.runsDayBudget()
	cv.text(x0, y, "SPEND TODAY", rcSt{fg: c.muted}.B())
	cv.textR(x0+w, y, "u usage by model", rcSt{fg: c.dim})
	y++
	cv.text(x0, y, aiBudgetText(sp.today), rcSt{fg: c.primary}.B())
	bw := min(w-34, 90)
	if bw >= 8 {
		var parts []runsPart
		if budget > 0 {
			parts = runsSpendParts(sp, budget, c)
		} else if sp.today > 0 {
			parts = runsSpendParts(sp, sp.today, c) // no day line: the bar is the models' shares
		}
		runsBar(cv, x0+7, y, bw, parts, c.dimmer)
		if budget > 0 {
			cv.text(x0+8+bw, y, "of "+aiBudgetText(budget)+" a day · "+runsPct(sp.today, budget), rcSt{fg: c.muted})
		} else {
			cv.text(x0+8+bw, y, "no day budget set", rcSt{fg: c.dimmer})
		}
	}
	y++
	x := x0 + 7
	if len(sp.byModel) == 0 {
		cv.text(x, y, "no runs yet today", rcSt{fg: c.dim})
		return y + 1
	}
	for i, ms := range sp.byModel {
		part := runsSwatch() + " " + ms.model + " " + aiBudgetText(ms.usd)
		if x+runeW(part)+2 > x0+w-24 && i > 0 {
			cv.text(x, y, "+"+strconv.Itoa(len(sp.byModel)-i), rcSt{fg: c.dim})
			x += 4
			break
		}
		x = cv.text(x, y, runsSwatch(), rcSt{fg: c.model(i)})
		x = cv.text(x+1, y, ms.model, rcSt{fg: c.secondary})
		x = cv.text(x+1, y, aiBudgetText(ms.usd), rcSt{fg: c.muted}) + 3
	}
	sess := "session " + aiBudgetText(sp.session) + " · " + strconv.Itoa(sp.sessionRuns) + " run" + plural(sp.sessionRuns)
	cv.textMax(x+1, y, sess, x0+w-x-1, rcSt{fg: c.dim})
	return y + 1
}

// drawLane draws one NOW run: its title line and its pipeline line.
func (m *Model) drawLane(cv *rcCanvas, y, w int, it runsItem, sel bool, now time.Time, c runsColours, hits *[]runsHit) int {
	if sel {
		cv.tint(2, y, w-4, 2, c.bgSel)
		cv.put(2, y, '▎', rcSt{fg: c.accent})
		cv.put(2, y+1, '▎', rcSt{fg: c.accent})
	}
	*hits = append(*hits, runsHit{x0: 2, x1: w - 2, y: y, id: "run:" + it.key, label: it.task},
		runsHit{x0: 2, x1: w - 2, y: y + 1, id: "run:" + it.key})
	g, col, _ := it.look(c)
	cv.text(4, y, g, rcSt{fg: col})
	sum := runsSummary(it)
	sx := cv.textR(w-4, y, sum, rcSt{fg: c.muted})
	wide := w >= 120
	taskW := 36
	if !wide {
		taskW = max(min(36, sx-8-24), 12)
	}
	cv.textMax(6, y, it.task, taskW, rcSt{fg: c.primary}.B())
	px := 6 + taskW + 2
	if wide {
		px = 44
	}
	x := cv.textMax(px, y, it.run.Profile, 12, rcSt{fg: c.secondary})
	if t := m.ai.runsProfileTitle(it.run.Profile); t != "" && wide {
		x = cv.textMax(x+1, y, "· "+t, 34, rcSt{fg: c.dim})
	}
	if it.why != "" && it.kind != runsRunning {
		wx := max(x+2, px+14)
		if wide {
			wx = max(x+2, 80)
		}
		cv.textMax(wx, y, g+" "+it.why, sx-wx-2, rcSt{fg: col})
	}
	y++
	// the pipeline: chips when they fit, else the mini pipe
	right := w - 4
	pill := ""
	switch it.kind {
	case runsStalled:
		pill = " Stop (x) "
	case runsPaused, runsResumable:
		pill = " Resume (r) "
	}
	if pill != "" {
		right = w - 4 - rcPillW(pill)
	}
	detail := 1
	pw := m.runsChipPipeWidth(it, detail, now)
	var x2 int
	if 6+pw < right-2 {
		x2 = m.runsChipPipe(cv, 6, y, it, detail, c, now)
	} else if pw0 := m.runsChipPipeWidth(it, 0, now); 6+pw0 < right-2 {
		x2 = m.runsChipPipe(cv, 6, y, it, 0, c, now)
	} else {
		x2 = cv.text(6, y, it.run.Profile, rcSt{fg: c.dim})
		x2 = runsMiniPipe(cv, x2+1, y, right-x2-4, it, c)
	}
	switch {
	case pill != "":
		px := w - 4 - rcPillW(pill)
		cv.pill(px, y, pill, c.bgChip, c.warn, true)
		verb := "resume"
		if it.kind == runsStalled {
			verb = "stop"
		}
		*hits = append(*hits, runsHit{x0: px, x1: px + rcPillW(pill), y: y, id: "verb:" + verb + ":" + it.key, label: strings.TrimSpace(pill)})
	case it.kind == runsRunning && it.cur >= 0:
		if note := m.ai.runsStepLive(it, it.cur, now).now; note != "" {
			cv.textMax(x2+4, y, "now: "+note, w-4-(x2+4), rcSt{fg: c.muted})
		}
	case it.kind == runsPausing:
		cv.textMax(x2+4, y, "pauses after this step", w-4-(x2+4), rcSt{fg: c.warn})
	}
	return y + 2
}

// drawOverviewPills draws Open run · Stop · Resume and the more line.
func (m *Model) drawOverviewPills(cv *rcCanvas, y int, it runsItem, have bool, c runsColours) []runsHit {
	var hits []runsHit
	if !have {
		return nil
	}
	x := 4
	add := func(label, verb string, chip, fg lipgloss.Color, bold bool) {
		x0 := x
		x = cv.pill(x, y, " "+label+" ", chip, fg, bold) + 1
		hits = append(hits, runsHit{x0: x0, x1: x - 1, y: y, id: "verb:" + verb + ":" + it.key, label: label})
	}
	add("Open run (Enter)", "open", c.accent, c.onAccent, true)
	if it.kind.live() {
		add("Stop (x)", "stop", c.bgChip, c.err, false)
	}
	if it.kind == runsPaused || it.kind == runsPausing || it.resume > 0 {
		add("Resume (r)", "resume", c.bgChip, c.primary, false)
	}
	more := runsMoreGlyph() + " more: Pause · Open in chat · Profile in Itermoc · Copy run id · AGENTS · Usage by model"
	cv.textMax(x+2, y, more, cv.w-x-4, rcSt{fg: c.muted})
	return hits
}

// ── One run ──────────────────────────────────────────────────────────────

// runsDetailHead draws the run's title line and the rule (rows 0 and 1).
func (m *Model) runsDetailHead(cv *rcCanvas, w int, it runsItem, compact bool, c runsColours) {
	g, col, _ := it.look(c)
	x := cv.textMax(2, 0, g+" "+it.task, w/2, rcSt{fg: col}.B())
	sub := it.run.Profile
	if t := m.ai.runsProfileTitle(it.run.Profile); t != "" && !compact {
		sub += " · " + t
	}
	if compact {
		sub += " · " + runsClock(it.started)
	} else {
		sub += " · started " + runsClock(it.started)
	}
	if it.run.Rounds > 0 {
		sub += " · " + aiRoundText(it.run.Rounds, it.run.MaxRounds)
	}
	word := runsKindWord(it)
	status := g + " " + word + " · " + runsDur(it.elapsed)
	if it.anyTok && !compact {
		status += " · " + runsTok(it.tokens) + " tok"
	}
	if c := runsCostText(it); c != "" {
		status += " · " + c
	}
	sx := cv.textR(w-2, 0, status, rcSt{fg: col})
	cv.textMax(x+1, 0, sub, sx-x-3, rcSt{fg: c.secondary})
	cv.hline(2, 1, w-4, runsDash(), rcSt{fg: c.subtle})
}

// runsKindWord is the state word of a run's title line.
func runsKindWord(it runsItem) string {
	switch it.kind {
	case runsRunning:
		return "running"
	case runsStalled:
		return "stalled"
	case runsPausing:
		return "pausing"
	case runsPaused:
		return "paused"
	case runsResumable:
		return "stopped"
	case runsFailed:
		return "failed"
	case runsCancelled:
		return "stopped"
	}
	return "done"
}

func (m *Model) drawRunDetail(cv *rcCanvas, w int, it runsItem, now time.Time, c runsColours) (int, []runsHit) {
	var hits []runsHit
	m.runsDetailHead(cv, w, it, false, c)
	y := 3
	y = m.drawPipeBoxes(cv, y, w, it, now, c)
	// timeline
	y++
	cv.text(4, y, "TIMELINE", rcSt{fg: c.muted}.B())
	cv.textR(w-4, y, "each part is one step · the line grows while it runs", rcSt{fg: c.dim})
	y += 2
	y = m.drawTimeline(cv, y, w, it, now, c) + 1
	// steps
	cv.text(4, y, "STEPS", rcSt{fg: c.muted}.B())
	cv.textR(w-4, y, "bar = share of this run's cost · colour = model", rcSt{fg: c.dim})
	y += 2
	y = m.drawStepRows(cv, y, w, it, now, c, false)
	// budget
	y++
	cv.text(4, y, "BUDGET", rcSt{fg: c.muted}.B())
	y += 2
	y = m.drawBudget(cv, y, w, it, now, c, false)
	// what happened
	y += 2
	cv.text(4, y, "WHAT HAPPENED", rcSt{fg: c.muted}.B())
	y += 2
	for _, l := range m.runsLog(it, now) {
		cv.text(4, y, l.at, rcSt{fg: c.dim})
		cv.textMax(14, y, l.text, w-18, rcSt{fg: l.col(c)})
		y++
	}
	return y + 1, hits
}

func (m *Model) drawRunDetailCompact(cv *rcCanvas, w int, it runsItem, now time.Time, c runsColours) (int, []runsHit) {
	m.runsDetailHead(cv, w, it, true, c)
	y := 3
	if pw := m.runsChipPipeWidth(it, 0, now); 2+pw <= w-2 {
		m.runsChipPipe(cv, 2, y, it, 0, c, now)
	} else {
		x := cv.text(2, y, "you "+runsArrow(2)+" ", rcSt{fg: c.muted})
		x = runsMiniPipe(cv, x, y, w-x-12, it, c)
		cv.text(x+1, y, runsArrow(2)+" answer", rcSt{fg: c.dimmer})
	}
	y += 2
	cv.text(2, y, "STEPS", rcSt{fg: c.muted}.B())
	cv.textR(w-2, y, "bar = share of this run's cost", rcSt{fg: c.dim})
	y++
	y = m.drawStepRows(cv, y, w, it, now, c, true)
	y++
	cv.text(2, y, "BUDGET", rcSt{fg: c.muted}.B())
	y++
	y = m.drawBudget(cv, y, w, it, now, c, true)
	return y + 1, nil
}

// drawPipeBoxes draws you → [steps] → answer as boxes (5 rows) and the
// "now" line under the live one; a run whose boxes do not fit gets one
// line of chips. Returns the row after it.
func (m *Model) drawPipeBoxes(cv *rcCanvas, y, w int, it runsItem, now time.Time, c runsColours) int {
	n := len(it.steps)
	avail := w - 8 - 10 - 12 - 6*(n+1)
	bw := 36
	if n > 0 {
		bw = min(36, avail/n)
	}
	if n == 0 || bw < 22 {
		m.runsChipPipe(cv, 4, y, it, 2, c, now)
		y += 2
		if k := it.cur; k >= 0 && k < n {
			if note := m.runsNowNote(it, k, now); note != "" {
				cv.textMax(6, y-1, note, w-10, rcSt{fg: m.runsNowCol(it, k, now, c)})
			}
		}
		return y + 1
	}
	x := 4
	cv.box(x, y, 10, 5, rcSt{fg: c.border})
	cv.textC(x, y+1, 10, "you", rcSt{fg: c.primary}.B())
	cv.textC(x, y+2, 10, runsClock(it.started), rcSt{fg: c.muted})
	cv.textC(x, y+3, 10, "chat", rcSt{fg: c.dim})
	x += 10
	liveX := -1
	for k, st := range it.steps {
		x = cv.text(x+1, y+2, runsArrow(5), rcSt{fg: c.dimmer}) + 1
		live := m.ai.runsStepLive(it, k, now)
		g, col, word := runsStepLook(it, k, c, live.stalled > 0)
		b := rcSt{fg: c.border}
		if live.live && live.stalled == 0 {
			b = rcSt{fg: c.ok}
		} else if k == it.cur && (live.stalled > 0 || it.kind.needsYou()) {
			b = rcSt{fg: c.warn}
		}
		if k == it.cur {
			liveX = x
		}
		cv.box(x, y, bw, 5, b)
		cv.text(x+2, y+1, strconv.Itoa(k+1), rcSt{fg: c.accent}.B())
		state := g + " " + word
		if live.elapsed > 0 {
			state += " · " + runsDur(live.elapsed)
		}
		sx := cv.textR(x+bw-2, y+1, state, rcSt{fg: col})
		cv.textMax(x+4, y+1, runsStepTitle(st), sx-x-5, rcSt{fg: c.primary}.B())
		cv.pill(x+2, y+2, rcCut(" "+runsStepOn(st)+" ", bw-6), c.bgChip, c.secondary, false)
		var parts []string
		if st.Usage != nil {
			parts = append(parts, runsTok(aiUsageTokens(*st.Usage))+" tok")
		}
		if st.Cost != nil {
			parts = append(parts, aiDollars(*st.Cost))
		} else if st.CLI != "" && st.Usage != nil {
			parts = append(parts, "subscription")
		}
		if len(parts) == 0 {
			parts = append(parts, "no tokens yet")
		}
		cv.textMax(x+3, y+3, strings.Join(parts, " · "), bw-5, rcSt{fg: c.muted})
		x += bw
	}
	x = cv.text(x+1, y+2, runsArrow(5), rcSt{fg: c.dimmer}) + 1
	cv.box(x, y, 12, 5, rcSt{fg: c.border})
	cv.textC(x, y+1, 12, "answer", rcSt{fg: c.dim}.B())
	ans, ansCol := "not yet", c.dimmer
	switch it.kind {
	case runsDone:
		ans, ansCol = "in the chat", c.muted
	case runsFailed, runsCancelled:
		ans = "none"
	}
	cv.textC(x, y+2, 12, ans, rcSt{fg: ansCol})
	if liveX >= 0 {
		if note := m.runsNowNote(it, it.cur, now); note != "" {
			col := m.runsNowCol(it, it.cur, now, c)
			cv.text(liveX+2, y+5, "▲", rcSt{fg: col})
			cv.textMax(liveX+4, y+5, note, w-liveX-8, rcSt{fg: col})
		}
	}
	return y + 7
}

// runsNowNote is the line under the step where the run is.
func (m *Model) runsNowNote(it runsItem, k int, now time.Time) string {
	live := m.ai.runsStepLive(it, k, now)
	switch {
	case live.stalled > 0:
		return "no output for " + aiSecs(live.stalled) + " · Stop (x) or wait"
	case live.live && live.now != "":
		return "now · " + live.now
	case it.kind == runsPausing:
		return "pauses after this step"
	case it.kind == runsPaused:
		return it.why + " · Resume (r) goes on"
	case it.kind == runsResumable:
		return it.why + " · Resume (r)"
	}
	return ""
}

func (m *Model) runsNowCol(it runsItem, k int, now time.Time, c runsColours) lipgloss.Color {
	if live := m.ai.runsStepLive(it, k, now); live.live && live.stalled == 0 {
		return c.ok
	}
	return c.warn
}

// drawTimeline draws the steps as parts of one line, in time order.
func (m *Model) drawTimeline(cv *rcCanvas, y, w int, it runsItem, now time.Time, c runsColours) int {
	total := time.Duration(0)
	durs := make([]time.Duration, len(it.steps))
	for k := range it.steps {
		durs[k] = m.ai.runsStepLive(it, k, now).elapsed
		total += durs[k]
	}
	cv.text(4, y, runsClockSecs(it.started), rcSt{fg: c.dim})
	tx := 14
	tw := min(80, w-tx-22)
	if total <= 0 || tw < 10 {
		cv.text(tx, y, "no step has run yet", rcSt{fg: c.dimmer})
		return y + 2
	}
	pos := 0
	for k := range it.steps {
		n := int(float64(durs[k]) / float64(total) * float64(tw))
		if durs[k] > 0 && n == 0 {
			n = 1
		}
		if n == 0 {
			continue
		}
		live := m.ai.runsStepLive(it, k, now)
		col := c.dim
		if live.live {
			col = c.ok
			if live.stalled > 0 {
				col = c.warn
			}
		}
		cv.hline(tx+pos, y, n, runsBarOn(), rcSt{fg: col})
		if theme.CurrentIconMode != theme.IconModeASCII {
			cv.put(tx+pos, y, '┣', rcSt{fg: col})
		}
		lbl := strconv.Itoa(k+1) + " " + runsStepTitle(it.steps[k]) + " · " + runsDur(durs[k])
		if runeW(lbl) <= n {
			cv.textC(tx+pos, y+1, n, lbl, rcSt{fg: col})
		} else if n >= 2 {
			cv.textC(tx+pos, y+1, n, strconv.Itoa(k+1), rcSt{fg: col})
		}
		pos += n
	}
	end := it.started.Add(it.elapsed)
	if it.kind.live() {
		endCol := c.ok
		if it.kind != runsRunning {
			endCol = c.warn // stalled or paused: the line waits
		}
		cv.text(tx+pos, y, runsArrow(1), rcSt{fg: endCol})
		cv.text(tx+pos+2, y, "now "+runsClockSecs(now), rcSt{fg: endCol})
	} else {
		cv.text(tx+pos+1, y, "ended "+runsClockSecs(end), rcSt{fg: c.dim})
	}
	return y + 2
}

// drawStepRows draws the STEPS rows; returns the row after them.
func (m *Model) drawStepRows(cv *rcCanvas, y, w int, it runsItem, now time.Time, c runsColours, compact bool) int {
	prof, _ := m.ai.profiles.Get(it.run.Profile)
	rank := map[string]int{}
	for _, ms := range m.ai.runsSpendOf(m.runsCollect(now)).byModel {
		rank[ms.model] = len(rank)
	}
	modelCol := func(model string) lipgloss.Color {
		if i, ok := rank[model]; ok {
			return c.model(i)
		}
		return c.model(2)
	}
	num := map[string]int{}
	for k, st := range it.steps {
		num[st.ID] = k + 1
	}
	for k, st := range it.steps {
		live := m.ai.runsStepLive(it, k, now)
		g, col, word := runsStepLook(it, k, c, live.stalled > 0)
		share := 0.0
		if st.Cost != nil && it.cost > 0 {
			share = *st.Cost / it.cost
		}
		tok, cost := "", ""
		if st.Usage != nil {
			tok = runsTok(aiUsageTokens(*st.Usage)) + " tok"
		}
		if st.Cost != nil {
			cost = aiDollars(*st.Cost)
		} else if st.CLI != "" && st.Usage != nil && !compact {
			cost = "included" // in the subscription (the pill says which)
		}
		dur := ""
		if live.elapsed > 0 {
			dur = runsDur(live.elapsed)
		}
		if compact {
			cv.text(2, y, strconv.Itoa(k+1), rcSt{fg: c.accent}.B())
			cv.textMax(4, y, runsStepTitle(st), 9, rcSt{fg: c.primary}.B())
			cv.text(14, y, g+" "+word, rcSt{fg: col})
			cv.text(25, y, dur, rcSt{fg: c.muted})
			cv.textR(41, y, tok, rcSt{fg: c.muted})
			cv.text(43, y, cost, rcSt{fg: c.muted})
			if w >= 70 {
				runsBar(cv, 50, y, 10, []runsPart{{share, modelCol(st.Model)}}, c.dimmer)
				cv.textMax(61, y, runsStepModelShort(st), w-63, rcSt{fg: c.dim})
			}
			y++
		} else {
			cv.text(4, y, strconv.Itoa(k+1), rcSt{fg: c.accent}.B())
			cv.textMax(7, y, runsStepTitle(st), 10, rcSt{fg: c.primary}.B())
			cv.text(18, y, g+" "+word, rcSt{fg: col})
			cv.text(30, y, dur, rcSt{fg: c.muted})
			cv.textR(48, y, tok, rcSt{fg: c.muted})
			cv.text(51, y, cost, rcSt{fg: c.muted})
			runsBar(cv, 60, y, 12, []runsPart{{share, modelCol(st.Model)}}, c.dimmer)
			if st.Cost != nil && it.cost > 0 {
				cv.textR(78, y, runsPct(*st.Cost, it.cost), rcSt{fg: c.muted})
			}
			px := 80
			if px+20 < w {
				x := cv.pill(px, y, rcCut(" "+runsStepOn(st)+" ", min(32, w-px-4)), c.bgChip, c.secondary, false)
				rule := runsToolText(prof, st.ID)
				var deps []string
				for _, d := range st.Deps {
					if n := num[d]; n > 0 {
						deps = append(deps, strconv.Itoa(n))
					}
				}
				if len(deps) > 0 {
					rule += " · waits for " + strings.Join(deps, ", ")
				}
				cv.textMax(max(x+2, 114), y, rule, w-4-max(x+2, 114), rcSt{fg: c.dim})
			}
			y++
		}
		ind := 7
		if compact {
			ind = 4
		}
		line, lc, italic := "", c.dim, false
		switch {
		case live.live && (live.now != "" || live.stalled > 0):
			line, lc = runsArrow(1)+" "+m.runsNowNote(it, k, now), m.runsNowCol(it, k, now, c)
			if live.stalled == 0 {
				line = runsArrow(1) + " " + live.now
			}
		case st.Error != "":
			line, lc = aiOneLine(aiCleanText(st.Error)), c.err
		case agentdag.State(st.State) == agentdag.StateCompleted && strings.TrimSpace(st.Text) != "":
			line, italic = "“"+aiOneLine(aiCleanText(ai.Redact(st.Text)))+"”", true
		case len(st.Notes) > 0:
			line = aiOneLine(aiCleanText(st.Notes[len(st.Notes)-1]))
		}
		if line != "" {
			stl := rcSt{fg: lc}
			if italic {
				stl = stl.I()
			}
			cv.textMax(ind, y, line, w-ind-4, stl)
			y++
		}
		if !compact {
			y++
		}
	}
	return y
}

// runsToolText says what a step may do: read-only, edit files, no tools.
func runsToolText(p ai.Profile, id string) string {
	for _, st := range p.Steps {
		if st.ID != id {
			continue
		}
		switch st.Tools.Preset {
		case agentdag.PresetNone:
			return "no tools"
		case agentdag.PresetReadOnly:
			return "read-only"
		}
		return "edit files"
	}
	return "planned step"
}

// drawBudget draws the run's budget bar and the day's.
func (m *Model) drawBudget(cv *rcCanvas, y, w int, it runsItem, now time.Time, c runsColours, compact bool) int {
	rb := m.ai.runsRunBudget(it)
	sp := m.ai.runsSpendOf(m.runsCollect(now))
	db := m.ai.runsDayBudget()
	if compact {
		spent := aiDollars(it.cost)
		if it.sub {
			spent += " + subscription"
		}
		x := cv.text(2, y, "this run "+spent, rcSt{fg: c.secondary})
		if rb > 0 {
			runsBar(cv, x+2, y, 20, []runsPart{{it.cost / rb, c.ok}}, c.dimmer)
			x = cv.text(x+23, y, "of "+aiBudgetText(rb), rcSt{fg: c.muted})
		}
		day := " · today " + aiBudgetText(sp.today)
		if db > 0 {
			day += " of " + aiBudgetText(db)
		}
		cv.textMax(x, y, day, w-x-2, rcSt{fg: c.muted})
		return y + 2
	}
	cv.text(4, y, "this run", rcSt{fg: c.muted})
	cv.text(18, y, aiDollars(it.cost), rcSt{fg: c.secondary})
	if rb > 0 {
		runsBar(cv, 26, y, 30, []runsPart{{it.cost / rb, c.ok}}, c.dimmer)
		cv.textMax(58, y, "of "+aiBudgetText(rb)+" per run · the run pauses at the limit", w-62, rcSt{fg: c.muted})
	} else if it.sub {
		cv.textMax(26, y, "+ subscription steps (Claude Code / Codex): no dollars, in no budget", w-30, rcSt{fg: c.dimmer})
	} else {
		cv.textMax(26, y, "no run budget (set budget_usd in the profile)", w-30, rcSt{fg: c.dimmer})
	}
	y++
	cv.text(4, y, "today", rcSt{fg: c.muted})
	cv.text(18, y, aiBudgetText(sp.today), rcSt{fg: c.secondary})
	if db > 0 {
		runsBar(cv, 26, y, 30, runsSpendParts(sp, db, c), c.dimmer)
		cv.text(58, y, "of "+aiBudgetText(db)+" a day", rcSt{fg: c.muted})
	} else {
		cv.textMax(26, y, "no day budget (ai_day_budget_usd in config.json)", w-30, rcSt{fg: c.dimmer})
	}
	return y + 1
}

// runsLogLine is one WHAT HAPPENED line.
type runsLogLine struct {
	at, text string
	kind     int // 0 dim, 1 normal, 2 live, 3 bad
}

func (l runsLogLine) col(c runsColours) lipgloss.Color {
	switch l.kind {
	case 1:
		return c.secondary
	case 2:
		return c.ok
	case 4:
		return c.warn
	case 3:
		return c.err
	}
	return c.dim
}

// runsLog is what happened in the run, from its steps' times and states.
func (m *Model) runsLog(it runsItem, now time.Time) []runsLogLine {
	type ev struct {
		t    time.Time
		line runsLogLine
	}
	var evs []ev
	add := func(t time.Time, text string, kind int) {
		at := "--:--:--"
		if !t.IsZero() {
			at = runsClockSecs(t)
		}
		evs = append(evs, ev{t, runsLogLine{at: at, text: text, kind: kind}})
	}
	where := "the chat"
	if !it.open {
		where = "the chat \"" + rcCut(aiCleanText(it.chatTitle), 30) + "\""
	}
	add(it.started, "started from "+where+" · profile "+it.run.Profile, 0)
	for k, st := range it.steps {
		if st.StartedAtMs == 0 {
			continue
		}
		start := time.UnixMilli(st.StartedAtMs)
		n := strconv.Itoa(k+1) + " " + runsStepTitle(st)
		add(start, n+" started · "+st.Model, 0)
		switch agentdag.State(st.State) {
		case agentdag.StateCompleted:
			text := n + " done"
			if first := aiOneLine(aiCleanText(ai.Redact(st.Text))); first != "" {
				text += " · " + rcCut(first, 60)
			}
			add(start.Add(time.Duration(st.ElapsedMs)*time.Millisecond), text, 0)
		case agentdag.StateFailed:
			add(start.Add(time.Duration(st.ElapsedMs)*time.Millisecond), n+" failed · "+aiOneLine(aiCleanText(st.Error)), 3)
		case agentdag.StateRunning:
			if live := m.ai.runsStepLive(it, k, now); live.live && live.now != "" {
				kind := 2
				if live.stalled > 0 {
					kind = 4
				}
				add(now, n+" "+live.now, kind)
			}
		}
		for _, note := range st.Notes {
			_ = note // notes have no time; the step's row shows the last one
		}
	}
	switch it.kind {
	case runsDone, runsFailed, runsCancelled, runsResumable:
		add(it.started.Add(it.elapsed), "run "+runsKindWord(it)+" after "+runsDur(it.elapsed), 1)
	case runsPaused:
		add(time.Time{}, it.why, 1)
	}
	// time order (lines without a time keep their place at the end)
	for i := 1; i < len(evs); i++ {
		for j := i; j > 0 && !evs[j].t.IsZero() && !evs[j-1].t.IsZero() && evs[j].t.Before(evs[j-1].t); j-- {
			evs[j], evs[j-1] = evs[j-1], evs[j]
		}
	}
	out := make([]runsLogLine, len(evs))
	for i, e := range evs {
		out[i] = e.line
	}
	if n := len(out); n > 0 && out[n-1].kind == 0 && it.kind.live() {
		out[n-1].kind = 1
	}
	return out
}

// drawRunPills draws the run page's verbs (the ones its state has).
func (m *Model) drawRunPills(cv *rcCanvas, y int, it runsItem, compact bool, c runsColours) []runsHit {
	var hits []runsHit
	x := 4
	if compact {
		x = 2
	}
	add := func(label, verb string, fg lipgloss.Color, bold bool) {
		x0 := x
		x = cv.pill(x, y, " "+label+" ", c.bgChip, fg, bold) + 1
		hits = append(hits, runsHit{x0: x0, x1: x - 1, y: y, id: "verb:" + verb + ":" + it.key, label: label})
	}
	switch it.kind {
	case runsRunning, runsStalled:
		add("Stop (x)", "stop", c.err, true)
		add("Pause (p)", "pause", c.primary, false)
	case runsPausing, runsPaused:
		add("Resume (r)", "resume", c.warn, true)
		add("Stop (x)", "stop", c.err, false)
	default:
		if it.resume > 0 {
			add("Resume (r)", "resume", c.warn, true)
		}
	}
	add("Open in chat (c)", "chat", c.primary, false)
	more := runsMoreGlyph() + " more (Enter): Profile in Itermoc · Copy run id · AGENTS · Usage by model"
	if compact {
		more = runsMoreGlyph() + " more"
	}
	cv.textMax(x+2, y, more, cv.w-x-4, rcSt{fg: c.muted})
	hits = append(hits, runsHit{x0: x + 2, x1: x + 2 + runeW(runsMoreGlyph()+" more"), y: y, id: "verb:more:" + it.key, label: runsMoreGlyph() + " more"})
	return hits
}

// ── The empty page ───────────────────────────────────────────────────────

func (m *Model) drawRunsEmpty(cv *rcCanvas, w, h int, now time.Time, c runsColours) (int, []runsHit) {
	var hits []runsHit
	cv.text(2, 0, runsIconLive.String()+" Runs", rcSt{fg: c.ok}.B())
	cv.text(9, 0, "what the AI is doing now", rcSt{fg: c.secondary})
	cv.textR(w-2, 0, "nothing running", rcSt{fg: c.dim})
	cv.hline(2, 1, w-4, runsDash(), rcSt{fg: c.subtle})
	m.drawSpendStrip(cv, 4, 3, w-8, now, c)
	cw := min(72, w-4)
	cx := (w - cw) / 2
	y := max(min(11, h-16), 7)
	cv.box(cx, y, cw, 14, rcSt{fg: c.border})
	cv.textC(cx, y+2, cw, runsIconLive.String()+"  Nothing is running", rcSt{fg: c.primary}.B())
	prof := m.ai.activeProfile()
	line1, line2 := "Send a message in the chat. The active profile ("+prof.Name+") starts a run.",
		"It shows up here with its steps, its model and its cost, live."
	if len(prof.Steps) < 2 && prof.Loop == nil {
		line1 = "The active profile (" + prof.Name + ") answers in one step: a plain chat turn."
		line2 = "Pick a profile with steps (review, plan) and its runs show up here, live."
	}
	cv.textC(cx, y+4, cw, rcCut(line1, cw-2), rcSt{fg: c.muted})
	cv.textC(cx, y+5, cw, rcCut(line2, cw-2), rcSt{fg: c.muted})
	var chips []string
	for i, st := range prof.Steps {
		t := st.Title
		if t == "" {
			t = st.ID
		}
		chips = append(chips, "▐ "+strconv.Itoa(i+1)+" "+aiCleanText(t)+" ▌")
	}
	if theme.CurrentIconMode == theme.IconModeASCII {
		for i := range chips {
			chips[i] = strings.NewReplacer("▐", "[", "▌", "]").Replace(chips[i])
		}
	}
	pipe := "you " + runsArrow(3) + " " + strings.Join(chips, " "+runsArrow(3)+" ") + " " + runsArrow(3) + " answer"
	cv.textC(cx, y+7, cw, rcCut(pipe, cw-2), rcSt{fg: c.dimmer})
	lbl, lbl2 := " Open the chat (c) ", " Profiles in Itermoc (i) "
	px := cx + (cw-rcPillW(lbl)-1-rcPillW(lbl2))/2
	px2 := px + rcPillW(lbl) + 1
	cv.pill(px, y+10, lbl, c.accent, c.onAccent, true)
	cv.pill(px2, y+10, lbl2, c.bgChip, c.secondary, false) // the quiet second way
	hits = append(hits, runsHit{x0: px, x1: px + rcPillW(lbl), y: y + 10, id: "verb:openchat:", label: "Open the chat (c)"},
		runsHit{x0: px2, x1: px2 + rcPillW(lbl2), y: y + 10, id: "verb:itermoc:", label: "Profiles in Itermoc (i)"})
	cv.textC(0, y+16, w, "finished runs stay in the list for 7 days", rcSt{fg: c.dim})
	return y + 17, hits
}

// ── Clicks ───────────────────────────────────────────────────────────────

// runsPageTargets are the page's click targets in screen cells.
func (m Model) runsPageTargets(pane editorPane) []hitTarget {
	if m.runs == nil {
		return nil
	}
	m.renderRunsPage(pane.W, pane.H) // the hits of what is drawn
	m.runs.cache.mu.Lock()
	defer m.runs.cache.mu.Unlock()
	var out []hitTarget
	for _, h := range m.runs.cache.pageHits {
		out = append(out, hitTarget{ID: "runs:pg:" + h.id, Label: h.label, X: pane.X + h.x0, Y: pane.Top + h.y, W: h.x1 - h.x0, H: 1})
	}
	return out
}

// runsSidebarTargets are the list's click targets in screen cells.
func (m Model) runsSidebarTargets(x0, w int) []hitTarget {
	if m.runs == nil {
		return nil
	}
	m.renderRunsSidebar(w, m.h)
	m.runs.cache.mu.Lock()
	defer m.runs.cache.mu.Unlock()
	var out []hitTarget
	for _, h := range m.runs.cache.sideHits {
		out = append(out, hitTarget{ID: "runs:side:" + h.id, Label: h.label, X: x0 + h.x0, Y: h.y, W: h.x1 - h.x0, H: 1})
	}
	return out
}

// runsClick runs the target id (a "run:", "older" or "verb:" hit).
func (m *Model) runsClick(id string, page bool) tea.Cmd {
	st := m.runsS()
	switch {
	case id == "older":
		st.sel = runsOlderKey
		st.older = !st.older
		return nil
	case strings.HasPrefix(id, "run:"):
		key := strings.TrimPrefix(id, "run:")
		if page && st.sel == key && st.detail == "" { // a second click opens it
			return m.runsShowPage(key, true)
		}
		st.sel = key
		if !page {
			return m.runsShowPage(key, false)
		}
		return nil
	case strings.HasPrefix(id, "verb:"):
		verb, key, _ := strings.Cut(strings.TrimPrefix(id, "verb:"), ":")
		switch verb {
		case "openchat":
			return m.openAIPanel(true)
		case "itermoc":
			return m.revealItermocView(true, imSecProfiles)
		}
		it, ok := m.runsFind(key)
		if !ok {
			return nil
		}
		st.sel = key
		return m.runsVerb(verb, it)
	}
	return nil
}

func (m Model) runsPageMouse(msg tea.MouseMsg) (Model, tea.Cmd, bool) {
	pane := m.editorPaneLayout()
	if msg.X < pane.X || msg.X >= pane.X+pane.W || msg.Y < pane.Top || msg.Y >= pane.Top+pane.H {
		return m, nil, false
	}
	st := m.runsS()
	switch {
	case msg.Button == tea.MouseButtonWheelDown && msg.Action == tea.MouseActionPress:
		st.pageScroll = min(st.pageScroll+3, max(st.pageM, 0))
		return m, nil, true
	case msg.Button == tea.MouseButtonWheelUp && msg.Action == tea.MouseActionPress:
		st.pageScroll = max(st.pageScroll-3, 0)
		return m, nil, true
	case msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress:
		return m, nil, true
	}
	for _, h := range m.runsPageTargets(pane) {
		if msg.X >= h.X && msg.X < h.X+h.W && msg.Y >= h.Y && msg.Y < h.Y+h.H {
			if m.probed(h.ID) {
				return m, nil, true
			}
			m.focus = FocusEditor
			return m, m.runsClick(strings.TrimPrefix(h.ID, "runs:pg:"), true), true
		}
	}
	if m.probed("runs:pg") {
		return m, nil, true
	}
	m.focus = FocusEditor
	return m, nil, true
}

// handleRunsSidebarMouse is a click in the list.
func (m Model) handleRunsSidebarMouse(x, y int, t tea.MouseEventType) (Model, tea.Cmd) {
	if t != tea.MouseLeft {
		return m, nil
	}
	w := m.explorerWidth - 1
	for _, h := range m.runsSidebarTargets(0, w) {
		if x >= h.X && x < h.X+h.W && y == h.Y {
			if m.probed(h.ID) {
				return m, nil
			}
			m.focus = FocusExplorer
			return m, m.runsClick(strings.TrimPrefix(h.ID, "runs:side:"), false)
		}
	}
	if m.probed("runs:side") {
		return m, nil
	}
	m.focus = FocusExplorer
	return m, nil
}
