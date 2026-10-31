package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── AGENTS task list (OMX-36) ─────────────────────────────────────────
//
// t shows the run as a dense list instead of cards: one row per step in
// wave order — wave, state, title, then model, time and tokens as room
// allows. The keys stay the same; t again shows the cards.

// agentsListBody fills out's body and hits with the list rows.
func (s *aiState) agentsListBody(out *aiAgentsCache, d agentsRun, sel, width int, now time.Time) {
	mark := "▸ "
	if theme.CurrentIconMode == theme.IconModeASCII {
		mark = "> "
	}
	out.body = append(out.body, theme.FgBg(theme.TextSecondary, theme.BgPanel).Bold(true).Render(
		agFit(" Tasks · "+strconv.Itoa(len(d.nodes))+" step"+plural(len(d.nodes))+" · "+strconv.Itoa(len(d.layers))+" wave"+plural(len(d.layers)), width)))
	for li, layer := range d.layers {
		for _, ni := range layer {
			n := d.nodes[ni]
			glyph, _, tok := agentsLook(n.state, d.live)
			lead := "  "
			if ni == sel {
				lead, tok = mark, theme.AIAccent
			}
			left := " " + lead + strconv.Itoa(li+1) + " " + glyph + " " + n.title
			var facts []string
			if st := n.st; st != nil && n.state != agentdag.StatePending && st.Model != "" {
				facts = append(facts, aiShortModel(st.Model))
			}
			dur, _, _ := s.agentsStatParts(d, n, now)
			facts = append(facts, dur)
			if st := n.st; st != nil && st.Usage != nil {
				facts = append(facts, aiTokText(aiUsageTokens(*st.Usage)))
			}
			right := strings.Join(nonEmpty(facts...), " · ")
			room := width - runewidth.StringWidth(left) - 2
			if room < 6 {
				right = ""
			} else {
				right = runewidth.Truncate(right, room, "…")
			}
			pad := max(width-runewidth.StringWidth(left)-runewidth.StringWidth(right)-1, 1)
			y := len(out.body)
			out.body = append(out.body, agText(tok, left+strings.Repeat(" ", pad)+right, width))
			out.hits = append(out.hits, agentsHit{y0: y, y1: y + 1, x0: 0, x1: width, id: n.id})
			if ni == sel {
				out.selTop, out.selBot = y, y
			}
		}
	}
}
