package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Runs: the look ───────────────────────────────────────────────────────
//
// Colour is state only: green = alive, amber = needs you (stalled,
// paused, stopped with steps left), red = failed; done is dim. The
// spend bars are the one exception: their parts are models (shades of
// one blue).

var (
	runsIconLive  = theme.Icon{NerdFont: "◉", Unicode: "◉", ASCII: "@"}
	runsIconDone  = theme.Icon{NerdFont: "✓", Unicode: "✓", ASCII: "v"}
	runsIconStep  = theme.Icon{NerdFont: "●", Unicode: "●", ASCII: "*"}
	runsIconTodo  = theme.Icon{NerdFont: "○", Unicode: "○", ASCII: "o"}
	runsIconFail  = theme.Icon{NerdFont: "✗", Unicode: "✗", ASCII: "x"}
	runsIconStall = theme.Icon{NerdFont: "⚠", Unicode: "⚠", ASCII: "!"}
	// runsIconPause: nf-fa-pause; ǁ (U+01C1, one cell) without the Nerd Font.
	runsIconPause = theme.Icon{NerdFont: "\uf04c", Unicode: "ǁ", ASCII: "|"}
)

func runsGlyphLive() theme.Icon { return runsIconLive }

// runsBarOn / runsBarOff draw bars (━ and ─; = and - in ASCII).
func runsBarOn() rune {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return '='
	}
	return '━'
}

func runsBarOff() rune {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return '-'
	}
	return '─'
}

func runsDash() rune {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return '-'
	}
	return '─'
}

// runsArrow is "───▶" (n cells; "-->" in ASCII).
func runsArrow(n int) string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return strings.Repeat("-", max(n-1, 0)) + ">"
	}
	return strings.Repeat("─", max(n-1, 0)) + "▶"
}

func runsSwatch() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "#"
	}
	return "■"
}

func runsMoreGlyph() string { return theme.GlyphMore.String() }

// runsColours are the page's colours for the current theme.
type runsColours struct {
	ok, warn, err, accent, blue                       lipgloss.Color
	primary, secondary, muted, dim, dimmer, subtle    lipgloss.Color
	bgEditor, bgSide, bgChip, bgSel, bgSelSide        lipgloss.Color
	liveBg, warnBg, border, onAccent, modelB, modelOt lipgloss.Color
}

func runsCol() runsColours {
	mix := func(a, b theme.Color256, t float64) lipgloss.Color {
		return theme.Lerp(theme.ToRGB(a), theme.ToRGB(b), t).LG()
	}
	return runsColours{
		ok: theme.LG(theme.AccentGreen), warn: theme.LG(theme.AccentAmber), err: theme.LG(theme.AccentRedCoral),
		accent: theme.LG(theme.AccentLavender), blue: theme.LG(theme.AccentBlue),
		primary: theme.LG(theme.TextPrimary), secondary: theme.LG(theme.TextSecondary), muted: theme.LG(theme.TextMuted),
		dim: theme.LG(theme.TextDim), dimmer: theme.LG(theme.TextDimmer), subtle: theme.LG(theme.BorderSubtle),
		bgEditor: theme.LG(theme.BgEditor), bgSide: theme.LG(theme.BgSidebar), bgChip: theme.LG(theme.BgSectionHdr),
		bgSel:     mix(theme.BgEditor, theme.BgSelection, 0.38),
		bgSelSide: mix(theme.BgSidebar, theme.BgSelection, 0.38),
		liveBg:    mix(theme.BgEditor, theme.AccentGreen, 0.22),
		warnBg:    mix(theme.BgEditor, theme.AccentAmber, 0.22),
		border:    theme.LG(theme.BorderDefault), onAccent: theme.LG(theme.BgEditor),
		modelB:  mix(theme.AccentBlue, theme.TextPrimary, 0.55),
		modelOt: theme.LG(theme.TextDim),
	}
}

// runsModelColour is the bar colour of the model ranked i by spend.
func (c runsColours) model(i int) lipgloss.Color {
	switch i {
	case 0:
		return c.blue
	case 1:
		return c.modelB
	}
	return c.modelOt
}

// look is a run's glyph, colour and the one word on its right.
func (it runsItem) look(c runsColours) (glyph string, col lipgloss.Color, word string) {
	cost := runsCostText(it)
	switch it.kind {
	case runsRunning:
		return runsIconLive.String(), c.ok, cost
	case runsStalled:
		return runsIconStall.String(), c.warn, "stalled"
	case runsPausing:
		return runsIconPause.String(), c.warn, "pausing"
	case runsPaused:
		return runsIconPause.String(), c.warn, "paused"
	case runsResumable:
		return runsIconPause.String(), c.warn, "resume"
	case runsFailed:
		return runsIconFail.String(), c.err, "failed"
	case runsCancelled:
		return runsIconTodo.String(), c.dim, "stopped"
	}
	return runsIconDone.String(), c.dim, cost
}

// runsStepLook is a step's glyph, colour and word inside run it.
func runsStepLook(it runsItem, k int, c runsColours, stalled bool) (glyph string, col lipgloss.Color, word string) {
	st := agentdag.State(it.steps[k].State)
	switch {
	case st == agentdag.StateRunning && stalled:
		return runsIconStall.String(), c.warn, "stalled"
	case st == agentdag.StateRunning && it.kind.live():
		return runsIconLive.String(), c.ok, "running"
	case st == agentdag.StateCompleted:
		return runsIconStep.String(), c.dim, "done"
	case st == agentdag.StateFailed:
		return runsIconFail.String(), c.err, "failed"
	case k == it.cur && (it.kind == runsPaused || it.kind == runsPausing || it.kind == runsResumable):
		return runsIconPause.String(), c.warn, "paused"
	case st == agentdag.StateBlocked:
		return runsIconTodo.String(), c.dimmer, "blocked"
	case st == agentdag.StateCancelled && !it.kind.live() && it.kind != runsResumable:
		return runsIconTodo.String(), c.dimmer, "stopped"
	}
	return runsIconTodo.String(), c.dimmer, "waiting"
}

// runsStepTitle is a step's title (its id when it has none).
func runsStepTitle(st ai.StoredStep) string {
	t := st.Title
	if t == "" {
		t = st.ID
	}
	return agentdag.Clean(aiCleanText(t))
}

// runsDur is a duration as "41 s" or "1 m 32 s".
func runsDur(d time.Duration) string { return aiSecs(int(d / time.Second)) }

// runsTok is a token count "12.4k".
func runsTok(n int) string { return aiTokShort(n) }

// runsPct is a share as "29 %".
func runsPct(part, whole float64) string {
	if whole <= 0 {
		return "—"
	}
	return strconv.Itoa(int(part/whole*100+0.5)) + " %"
}

// runsBar draws a bar of w cells at (x, y): the parts in order, then off.
func runsBar(cv *rcCanvas, x, y, w int, parts []runsPart, off lipgloss.Color) {
	pos := 0
	for _, p := range parts {
		n := int(p.frac*float64(w) + 0.5)
		if p.frac > 0 && n == 0 {
			n = 1
		}
		for i := 0; i < n && pos < w; i++ {
			cv.put(x+pos, y, runsBarOn(), rcSt{fg: p.col})
			pos++
		}
	}
	for ; pos < w; pos++ {
		cv.put(x+pos, y, runsBarOff(), rcSt{fg: off})
	}
}

type runsPart struct {
	frac float64
	col  lipgloss.Color
}

// runsMiniPipe draws ●─◉─○ for the steps of it (at most maxW cells);
// returns the cell after it.
func runsMiniPipe(cv *rcCanvas, x, y, maxW int, it runsItem, c runsColours) int {
	n := len(it.steps)
	if n == 0 || maxW < 1 {
		return x
	}
	show := n
	if 2*n-1 > maxW {
		show = max((maxW-2)/2, 1) // room for "+k"
	}
	for k := 0; k < show; k++ {
		if k > 0 {
			cv.put(x, y, runsDash(), rcSt{fg: c.dimmer})
			x++
		}
		g, col, _ := runsStepLook(it, k, c, k == it.cur && it.kind == runsStalled)
		x = cv.text(x, y, g, rcSt{fg: col})
	}
	if show < n {
		x = cv.text(x, y, "+"+strconv.Itoa(n-show), rcSt{fg: c.dimmer})
	}
	return x
}

// runsChip draws one step as a pill of a one-line pipeline. detail: 0 =
// number, title and glyph · 1 = the live step also shows its time · 2 =
// every step that ran shows its time.
func (m *Model) runsChip(cv *rcCanvas, x, y int, it runsItem, k int, detail int, c runsColours, now time.Time) int {
	st := it.steps[k]
	live := m.ai.runsStepLive(it, k, now)
	g, _, word := runsStepLook(it, k, c, live.stalled > 0)
	label := " " + strconv.Itoa(k+1) + " " + rcCut(runsStepTitle(st), 18) + " " + g
	ran := word != "waiting" && word != "blocked"
	isLive := live.live || k == it.cur && it.kind.needsYou()
	if ran && (detail == 2 || (detail == 1 && isLive)) && live.elapsed > 0 {
		label += " " + runsDur(live.elapsed)
	}
	label += " "
	switch {
	case live.live && live.stalled == 0:
		return cv.pill(x, y, label, c.liveBg, c.primary, true)
	case isLive && (live.stalled > 0 || it.kind.needsYou()):
		return cv.pill(x, y, label, c.warnBg, c.primary, true)
	case word == "failed":
		return cv.pill(x, y, label, c.bgChip, c.err, false)
	case word == "done":
		return cv.pill(x, y, label, c.bgChip, c.muted, false)
	}
	return cv.pill(x, y, label, c.bgChip, c.dim, false)
}

// runsChipPipeW is the width of the chip pipeline of it.
func (m *Model) runsChipPipe(cv *rcCanvas, x, y int, it runsItem, detail int, c runsColours, now time.Time) int {
	x = cv.text(x, y, "you", rcSt{fg: c.muted})
	for k := range it.steps {
		x = cv.text(x+1, y, runsArrow(3), rcSt{fg: c.dimmer}) + 1
		x = m.runsChip(cv, x, y, it, k, detail, c, now)
	}
	x = cv.text(x+1, y, runsArrow(3), rcSt{fg: c.dimmer}) + 1
	col := c.dimmer
	if it.kind == runsDone {
		col = c.muted
	}
	return cv.text(x, y, "answer", rcSt{fg: col})
}

// runsChipPipeWidth measures runsChipPipe without drawing.
func (m *Model) runsChipPipeWidth(it runsItem, detail int, now time.Time) int {
	cv := newRCCanvas(4096, 1, "")
	return m.runsChipPipe(cv, 0, 0, it, detail, runsCol(), now)
}

// runsSummary is "41 s · 12.4k tok · $0.031" (the parts that are known).
func runsSummary(it runsItem) string {
	parts := []string{runsDur(it.elapsed)}
	if it.anyTok {
		parts = append(parts, runsTok(it.tokens)+" tok")
	}
	if c := runsCostText(it); c != "" {
		parts = append(parts, c)
	}
	return strings.Join(parts, " · ")
}

// runsStepOn is what step st runs on: "main · claude-sonnet-4-5", or
// "Claude Code · subscription" for a CLI step (CS-3).
func runsStepOn(st ai.StoredStep) string {
	if st.CLI != "" {
		return ai.CLITitle(st.CLI) + " · subscription"
	}
	slot := st.Slot
	if slot == "" {
		slot = ai.SlotMain
	}
	return slot + " · " + st.Model
}

// runsStepModelShort is the model column of the narrow step table.
func runsStepModelShort(st ai.StoredStep) string {
	if st.CLI != "" {
		return runsStepOn(st)
	}
	return strings.TrimPrefix(st.Model, "claude-")
}

// runsCostText is a run's cost: dollars, "subscription" when only
// subscription CLIs ran (CS-3), "$0.031 + subscription" when both did;
// "" when it is not known.
func runsCostText(it runsItem) string {
	switch {
	case !it.anyTok || !it.costOK:
		return ""
	case it.sub && it.cost == 0:
		return "subscription"
	case it.sub:
		return aiDollars(it.cost) + " + subscription"
	}
	return aiDollars(it.cost)
}

// runsTail is the second line's end in the list: where the run is.
func (m *Model) runsTail(it runsItem, c runsColours, now time.Time) (string, lipgloss.Color) {
	cur := ""
	if it.cur >= 0 && it.cur < len(it.steps) {
		cur = runsStepTitle(it.steps[it.cur])
	}
	switch it.kind {
	case runsRunning:
		return cur + " · " + runsDur(it.elapsed), c.muted
	case runsStalled:
		return runsDur(m.ai.runsStepLive(it, it.cur, now).elapsed), c.warn
	case runsPausing:
		return "pausing", c.warn
	case runsPaused:
		return "paused", c.warn
	case runsResumable:
		return strconv.Itoa(it.left) + " left", c.warn
	case runsFailed:
		return it.why, c.err
	}
	return runsDur(it.elapsed), c.muted
}

// runsClock is a wall time "12:04" (local).
func runsClock(t time.Time) string {
	if t.IsZero() {
		return "--:--"
	}
	return t.In(clock.Now().Location()).Format("15:04")
}

func runsClockSecs(t time.Time) string {
	if t.IsZero() {
		return "--:--:--"
	}
	return t.In(clock.Now().Location()).Format("15:04:05")
}

// runsCount is "1 running" style counts of the NOW group.
func runsCounts(items []runsItem, c runsColours) [][2]any {
	var run, stall, pause int
	for _, it := range items {
		switch it.kind {
		case runsRunning:
			run++
		case runsStalled:
			stall++
		case runsPaused, runsPausing:
			pause++
		case runsResumable:
			if it.group == runsGroupNow {
				pause++
			}
		}
	}
	var out [][2]any
	if run > 0 {
		out = append(out, [2]any{runsIconLive.String() + " " + strconv.Itoa(run) + " running", c.ok})
	}
	if stall > 0 {
		out = append(out, [2]any{runsIconStall.String() + " " + strconv.Itoa(stall) + " stalled", c.warn})
	}
	if pause > 0 {
		out = append(out, [2]any{runsIconPause.String() + " " + strconv.Itoa(pause) + " paused", c.warn})
	}
	return out
}

// runsOlderText is "12 this week" / "12 runs this week · $3.80".
func runsOlderText(items []runsItem, long bool) string {
	n, usd := 0, 0.0
	for _, it := range items {
		if it.group == runsGroupOlder {
			n++
			usd += it.cost
		}
	}
	if !long {
		return fmt.Sprintf("%d this week", n)
	}
	s := fmt.Sprintf("%d run%s this week", n, plural(n))
	if usd > 0 {
		s += " · " + aiBudgetText(usd)
	}
	return s
}
