package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Step cards and the run row of a pipeline run in the chat transcript
// (U20 AP-4, B5 fold rules). A running card is expanded (its tool rows
// and the end of its text), a finished one folds to one line. A click on
// the first row, or Tab to select a card and Enter, toggles a card; the
// choice is kept per card id in the chat (Conversation.Folded).
//
//	▸ 1 · Reviewer · claude-sonnet-4-5 · 14 s · 3 tools · 4.1k tok · $0.013
//	▾ 2 · Fixer · gpt-5-mini (quick) · running · 1 tool
//	    ⚙ Write quit.go → written
//	    ✎ now · "…so I will also update the test"
//	✦ AI · …            (the last step's answer)
//	─ run · 36 s · 2 steps · 13.9k tok · $0.017 · context 31 % ──────────

// aiCardID is the id of a step card (also its button id).
func aiCardID(run, step string) string { return "step:" + run + ":" + step }

// aiCardExpanded reports whether a card is drawn expanded: the user's
// choice, else expanded only while the step runs.
func (s *aiState) aiCardExpanded(id string, st ai.StoredStep) bool {
	if v, ok := s.conv.Folded[id]; ok {
		return !v
	}
	return agentdag.State(st.State) == agentdag.StateRunning
}

// aiCardIDs lists every card of the chat in order.
func (s *aiState) aiCardIDs() []string {
	var out []string
	for _, msg := range s.conv.Messages {
		if msg.Run == nil {
			continue
		}
		for _, st := range msg.Steps {
			out = append(out, aiCardID(msg.Run.ID, st.ID))
		}
	}
	return out
}

// aiToggleCard folds or unfolds a card and remembers the choice.
func (m *Model) aiToggleCard(id string) {
	s := m.aiS()
	for _, msg := range s.conv.Messages {
		if msg.Run == nil {
			continue
		}
		for _, st := range msg.Steps {
			if aiCardID(msg.Run.ID, st.ID) != id {
				continue
			}
			if s.conv.Folded == nil {
				s.conv.Folded = map[string]bool{}
			}
			s.conv.Folded[id] = s.aiCardExpanded(id, st)
			s.runGen++
			if !s.streaming {
				_ = s.store.Save(&s.conv)
			}
			return
		}
	}
}

// aiCycleCard moves the keyboard card selection (Tab / Shift+Tab); it
// reports false when the chat has no cards.
func (m *Model) aiCycleCard(dir int) bool {
	s := m.aiS()
	ids := s.aiCardIDs()
	if len(ids) == 0 {
		return false
	}
	cur := -1
	for i, id := range ids {
		if id == s.cardSel {
			cur = i
		}
	}
	switch {
	case cur < 0 && dir > 0:
		cur = 0
	case cur < 0:
		cur = len(ids) - 1
	default:
		cur = (cur + dir + len(ids)) % len(ids)
	}
	s.cardSel = ids[cur]
	s.runGen++
	return true
}

// aiFoldGlyph is ▾ (expanded) / ▸ (folded), v / > in ASCII.
func aiFoldGlyph(expanded bool) string {
	ascii := theme.CurrentIconMode == theme.IconModeASCII
	switch {
	case expanded && ascii:
		return "v"
	case ascii:
		return ">"
	case expanded:
		return "▾"
	}
	return "▸"
}

// aiRunLines draws the assistant message i of a run: the step cards, the
// answer, the run row and, for a stopped run, the retry button.
func (m Model) aiRunLines(i int, msg ai.StoredMessage, width, inner int) []aiLine {
	s := m.ai
	ind := strings.Repeat(" ", aiIndent)
	out := []aiLine{{sel: aiSel{ok: true, join: "\n"}}}
	for k, st := range msg.Steps {
		out = append(out, m.aiStepCard(msg, k, st, width, inner)...)
	}
	if text := aiCleanText(msg.Text); strings.TrimSpace(text) != "" {
		out = append(out, aiLine{sel: aiSel{ok: true, join: "\n"}})
		model := "" // the model of the step that answered
		if n := len(msg.Steps); n > 0 {
			model = msg.Steps[n-1].Model
			for k := n - 1; k >= 0; k-- { // a loop's answer is verify's, not the last card's
				if msg.Steps[k].Text == msg.Text {
					model = msg.Steps[k].Model
					break
				}
			}
		}
		out = append(out, m.aiAnswerHeaderModel(width, i, true, model))
		out = append(out, s.renderAnswer(i, text, width, false, m.dragKind == dragActionsLeft)...)
	}
	dash := "─"
	if theme.CurrentIconMode == theme.IconModeASCII {
		dash = "-"
	}
	// A planned (graph) run's row opens it in the AGENTS view on a click.
	waves := s.aiRunWaveText(i)
	var agentsBtn []aiLineButton
	if waves != "" {
		agentsBtn = []aiLineButton{{0, width, "agents:" + msg.Run.ID}}
	}
	if msg.Run.State == ai.RunStateRunning {
		if waves != "" { // where the run is: "wave 2/4 · done 1/5 · 2 running"
			if msg.Run.Rounds > 1 {
				waves = aiRoundText(msg.Run.Rounds, msg.Run.MaxRounds) + " · " + waves
			}
			row := dash + " run · " + waves + " "
			if pad := inner - runewidth.StringWidth(row); pad > 0 {
				row += strings.Repeat(dash, pad)
			}
			out = append(out, aiLine{text: ind + row, style: theme.Fg(theme.TextDim), buttons: agentsBtn})
		}
		return out
	}
	if msg.Run.State == ai.RunStatePaused { // the budget pause (or Pause on the Runs page): ask in the chat too
		text := "Budget " + aiBudgetText(msg.Run.Budget) + " reached · " + aiDollars(msg.Run.Spent) + " so far · the run is paused"
		if msg.Run.Budget == 0 {
			text = "Paused by you · " + aiDollars(msg.Run.Spent) + " so far · Continue runs the next step"
		}
		for _, l := range wrapPlain(text, max(inner, 8)) {
			out = append(out, aiLine{text: ind + l, style: theme.Fg(theme.DiagWarning)})
		}
		out = append(out, aiChip("Continue", "run-continue:"+strconv.Itoa(i), width), aiChip("Stop", "run-stop:"+strconv.Itoa(i), width))
		return out
	}
	summary := aiRunSummaryText(msg.Run, msg.Steps)
	row := dash + " " + summary + " "
	if pad := inner - runewidth.StringWidth(row); pad > 0 {
		row += strings.Repeat(dash, pad)
	}
	// The tokens and the cost open AI: Usage (before the whole-row link).
	buttons := agentsBtn
	if x0, x1, ok := aiRunUsageSpan(summary); ok {
		off := aiIndent + runewidth.StringWidth(dash+" ")
		buttons = append([]aiLineButton{{off + x0, off + x1, "usage:" + msg.Run.ID}}, agentsBtn...)
	}
	out = append(out, aiLine{text: ind + row, style: theme.Fg(theme.TextDim), buttons: buttons})
	if msg.Run.State == ai.RunStateInterrupted {
		out = append(out, aiLine{text: ind + "✘ run interrupted (termocode restarted)", style: theme.Fg(theme.DiagError)})
	}
	if msg.Run.Verdict == "fail" {
		text := fmt.Sprintf("✘ Not done after %d round%s: verify still fails. The last verify report is above.", msg.Run.Rounds, plural(msg.Run.Rounds))
		for _, l := range wrapPlain(text, max(inner, 8)) {
			out = append(out, aiLine{text: ind + l, style: theme.Fg(theme.DiagError)})
		}
	}
	if n := s.aiRetryStep(i); n > 0 {
		label := "↻ Retry from step " + strconv.Itoa(n)
		if left, total, planned := aiResumeLeft(msg); planned {
			label = "↻ Resume (" + strconv.Itoa(left) + " of " + strconv.Itoa(total) + " left)"
		}
		out = append(out, aiChip(label, "run-retry:"+strconv.Itoa(i), width))
	}
	return out
}

// aiStepCard draws one card: its first row and, expanded, its tool rows
// and text.
func (m Model) aiStepCard(msg ai.StoredMessage, k int, st ai.StoredStep, width, inner int) []aiLine {
	s := m.ai
	id := aiCardID(msg.Run.ID, st.ID)
	expanded := s.aiCardExpanded(id, st)
	state := agentdag.State(st.State)
	tok := theme.TextSecondary
	switch state {
	case agentdag.StateRunning:
		tok = theme.AIAccent
	case agentdag.StateFailed:
		tok = theme.DiagError
	case agentdag.StatePending, agentdag.StateCancelled, agentdag.StateBlocked, agentdag.StateSkipped:
		tok = theme.TextDim
	}
	stallSecs, stalled := m.ai.agentsStalled(msg.Run.ID, state, st.ID, clock.Now())
	if stalled {
		tok = theme.DiagWarning
	}
	base := theme.FgBg(tok, theme.BgPanel)
	title := base.Bold(true)
	if s.cardSel == id {
		title = title.Underline(true)
	}
	model := st.Model
	if model == "" {
		model = st.Slot
	}
	rest := ""
	if model != "" {
		rest = " · " + model
	}
	if st.Category != "" && !state.Done() && state != agentdag.StateFailed {
		rest += " (" + st.Category + ")"
	}
	if st.Round > 1 {
		rest += " · " + aiRoundText(st.Round, msg.Run.MaxRounds)
	}
	if status := aiStepStatus(msg.Steps, k); status != "" {
		rest += " · " + status
	}
	if stalled {
		rest += " · " + theme.GlyphAIStall.String() + " possibly stalled · " + agSecs(stallSecs)
	}
	head := aiFoldGlyph(expanded) + " " + strconv.Itoa(k+1) + " · "
	row := theme.Bg(theme.BgPanel).Render(strings.Repeat(" ", aiIndent)) +
		ansi.Truncate(base.Render(head)+title.Render(aiCleanText(st.Title))+base.Render(aiCleanText(rest)), inner, "…")
	out := []aiLine{{styled: row, buttons: []aiLineButton{{0, width, id}}}}
	ind := strings.Repeat(" ", aiIndent+2)
	dim := theme.Fg(theme.TextDim)
	if wt := st.Worktree; wt != nil { // G-WT: shown folded too
		out = append(out, aiLine{text: ind + ansi.Truncate(aiWorktreeStatus(wt), max(inner-2, 4), "…"), style: theme.Fg(theme.TextSecondary)})
		out = append(out, m.aiWorktreeButtons(msg, st, width)...)
	}
	if !expanded {
		return out
	}
	for _, t := range st.Notes {
		out = append(out, aiLine{text: ind + aiOneLine(aiCleanText(t)), style: dim})
	}
	for _, t := range st.Tools {
		out = append(out, aiLine{text: ind + aiOneLine(aiCleanText(t)), style: dim})
	}
	switch {
	case state == agentdag.StateRunning || (state == agentdag.StatePending && st.Round == 0):
		if t := aiOneLine(aiCleanText(st.Text)); t != "" {
			room := max(inner-2-len("✎ now · \"…\""), 8)
			if r := []rune(t); len(r) > room {
				t = string(r[len(r)-room:])
			}
			out = append(out, aiLine{text: ind + "✎ now · \"…" + strings.TrimLeft(t, " ") + "\"", style: dim})
		}
	case state == agentdag.StateFailed && st.Error != "":
		for _, l := range wrapPlain("✘ "+aiCleanText(st.Error), max(inner-2, 4)) {
			out = append(out, aiLine{text: ind + l, style: theme.Fg(theme.DiagError)})
		}
	default:
		for _, l := range wrapPlain(aiCleanText(st.Text), max(inner-2, 4)) {
			out = append(out, aiLine{text: ind + l, style: theme.Fg(theme.TextSecondary), sel: aiSel{ok: true, x: aiIndent + 2, text: l, join: "\n"}})
		}
	}
	return out
}

// aiOneLine folds whitespace runs (newlines too) into single spaces.
func aiOneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// aiStepStatus is the end of the first row of card k: what the step
// does or what it did. A waiting step names the steps it waits for (its
// Deps; without them, the step before it).
func aiStepStatus(steps []ai.StoredStep, k int) string {
	st := steps[k]
	tools := ""
	if n := len(st.Tools); n > 0 {
		tools = fmt.Sprintf("%d tool%s", n, plural(n))
	}
	switch agentdag.State(st.State) {
	case agentdag.StatePending, "":
		if len(st.Deps) > 0 {
			var nums []string
			for _, d := range st.Deps {
				for j, o := range steps {
					if o.ID == d {
						nums = append(nums, strconv.Itoa(j+1))
					}
				}
			}
			if len(nums) == 1 {
				return "waiting for step " + nums[0]
			}
			if len(nums) > 1 {
				return "waiting for steps " + strings.Join(nums, ", ")
			}
		}
		if k > 0 {
			return "waiting for step " + strconv.Itoa(k)
		}
		return "waiting"
	case agentdag.StateRunning:
		return strings.Join(nonEmpty("running", tools), " · ")
	case agentdag.StateFailed:
		head := aiOneLine(st.Error)
		if r := []rune(head); len(r) > 60 {
			head = string(r[:59]) + "…"
		}
		return "✘ " + head
	case agentdag.StateCancelled, agentdag.StateBlocked, agentdag.StateSkipped:
		return st.State
	}
	parts := []string{aiDurText(st.ElapsedMs), tools}
	if st.Usage != nil {
		parts = append(parts, aiTokText(aiUsageTokens(*st.Usage)))
	}
	if c := aiStepCost(st); c != "" {
		parts = append(parts, c)
	}
	if st.Files > 0 {
		parts = append(parts, fmt.Sprintf("%d file%s", st.Files, plural(st.Files)))
	}
	return strings.Join(nonEmpty(parts...), " · ")
}

func nonEmpty(xs ...string) []string {
	var out []string
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

// aiRunSummaryText is the run row's text: state, time, steps, tokens,
// cost and the biggest context use. Unknown parts are left out: no $
// when a step that used tokens has no price, no context without a
// window.
func aiRunSummaryText(run *ai.StoredRun, steps []ai.StoredStep) string {
	label := "run"
	switch run.State {
	case ai.RunStateFailed:
		label = "run failed"
	case ai.RunStateCancelled:
		label = "run cancelled"
	case ai.RunStateInterrupted:
		label = "run interrupted"
	}
	done := 0
	tokens, anyUsage := 0, false
	cost, costOK, anyCost := 0.0, true, false
	pct, anyPct := 0.0, false
	for _, st := range steps {
		if agentdag.State(st.State).Done() {
			done++
		}
		if st.Usage != nil {
			anyUsage = true
			tokens += aiUsageTokens(*st.Usage)
			if st.Cost == nil {
				costOK = false
			}
		}
		if st.Cost != nil {
			anyCost = true
			cost += *st.Cost
		}
		if st.ContextPct != nil {
			anyPct = true
			pct = max(pct, *st.ContextPct)
		}
	}
	n := strconv.Itoa(len(steps))
	stepsText := n + " step" + plural(len(steps))
	if run.State != ai.RunStateDone {
		stepsText = strconv.Itoa(done) + "/" + n + " steps"
	}
	parts := []string{label, aiDurText(run.ElapsedMs), stepsText}
	switch {
	case run.Verdict == "pass" && run.Rounds > 1:
		parts = append(parts, fmt.Sprintf("%d rounds", run.Rounds), "verify ✓")
	case run.Verdict == "pass":
		parts = append(parts, "verify ✓")
	case run.Verdict == "fail":
		parts = append(parts, fmt.Sprintf("verify ✘ (%d round%s)", run.Rounds, plural(run.Rounds)))
	case run.Rounds > 1: // stopped in a later round
		parts = append(parts, aiRoundText(run.Rounds, run.MaxRounds))
	}
	if anyUsage {
		parts = append(parts, aiTokText(tokens))
	}
	if anyCost && costOK {
		parts = append(parts, aiDollars(cost))
	}
	if anyPct {
		parts = append(parts, fmt.Sprintf("context %.0f %%", pct))
	}
	return strings.Join(parts, " · ")
}

// aiRunUsageSpan is the cells of the tokens and the cost in a run row
// summary ("run · 36 s · 2 steps · 13.9k tok · $0.017 · context 31 %").
func aiRunUsageSpan(summary string) (x0, x1 int, ok bool) {
	i := strings.Index(summary, " tok")
	if i < 0 {
		return 0, 0, false
	}
	start := strings.LastIndex(summary[:i], " · ") + len(" · ")
	end := i + len(" tok")
	if rest := summary[end:]; strings.HasPrefix(rest, " · $") || strings.HasPrefix(rest, " · <$") {
		end += len(" · ")
		if j := strings.Index(summary[end:], " · "); j >= 0 {
			end += j
		} else {
			end = len(summary)
		}
	}
	return runewidth.StringWidth(summary[:start]), runewidth.StringWidth(summary[:end]), true
}

// aiRoundText is a loop round: "round 2/3" ("round 2" without a limit).
func aiRoundText(round, maxRounds int) string {
	if maxRounds > 0 {
		return fmt.Sprintf("round %d/%d", round, maxRounds)
	}
	return fmt.Sprintf("round %d", round)
}

// aiBudgetText is a budget in dollars with cents: $0.50, $2.00.
func aiBudgetText(usd float64) string { return fmt.Sprintf("$%.2f", usd) }

// aiUsageTokens is every token of u (prompt, cache and output).
func aiUsageTokens(u ai.Usage) int { return u.Input + u.Output + u.CacheRead + u.CacheWrite }

// aiTokText is a token count: 812 tok, 13.9k tok, 1.2M tok.
func aiTokText(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d tok", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk tok", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM tok", float64(n)/1_000_000)
}

// aiDurText is a duration: 36 s, 1m 05s.
func aiDurText(ms int64) string {
	sec := ms / 1000
	if sec < 60 {
		return fmt.Sprintf("%d s", sec)
	}
	return fmt.Sprintf("%dm %02ds", sec/60, sec%60)
}
