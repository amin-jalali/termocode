package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// ── Workflows: what is drawn (plan §5.1–§5.5) ───────────────────────────
//
// Plain text rows, padded to the width; colour is state only (a whole
// row's tint). The sidebar is the list; the page is the overview, one
// workflow, or one run. Under wfxNarrowW the overview carries the list.

// wfxLine is one drawn row: text and its tint ("" none, ok, warn, err,
// dim, head, sel).
type wfxLine struct{ text, tint string }

func wfxPaint(lines []wfxLine, w, h int, scroll int) string {
	if scroll > 0 && scroll < len(lines) {
		lines = lines[scroll:]
	}
	var b strings.Builder
	for i := 0; i < h; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		if i >= len(lines) {
			b.WriteString(strings.Repeat(" ", max(w, 0)))
			continue
		}
		b.WriteString(wfxTint(lines[i].tint, padRight(rcCut(lines[i].text, w), w)))
	}
	return b.String()
}

func wfxTint(tint, s string) string {
	switch tint {
	case "ok":
		return lipgloss.NewStyle().Foreground(theme.LG(theme.AccentGreen)).Render(s)
	case "warn":
		return lipgloss.NewStyle().Foreground(theme.LG(theme.AccentAmber)).Render(s)
	case "err":
		return lipgloss.NewStyle().Foreground(theme.LG(theme.AccentRedCoral)).Render(s)
	case "dim":
		return lipgloss.NewStyle().Foreground(theme.LG(theme.TextDim)).Render(s)
	case "head":
		return lipgloss.NewStyle().Foreground(theme.LG(theme.TextDim)).Bold(true).Render(s)
	case "sel":
		return lipgloss.NewStyle().Foreground(theme.LG(theme.AccentGreen)).Bold(true).Render(s)
	}
	return s
}

// wfxDot is the state mark of a workflow: ◉ running, ✗ broken file,
// ◌ not trusted, ● on, ○ off.
func (m *Model) wfxDot(e wire.Entry) (mark, word, tint string) {
	switch {
	case m.wfxRunning(e.Name) != nil:
		return "◉", "running", "ok"
	case e.Broken():
		return "✗", "file error", "err"
	case !e.Trusted:
		return "◌", "not trusted", "warn"
	case e.Enabled:
		return "●", "on", ""
	}
	return "○", "off", "dim"
}

// wfxLastWord is the last result of a workflow: "✓ 14:02", "✗ Nov 5".
func (m *Model) wfxLastWord(name string, now time.Time) string {
	runs := m.wfxS().runs[name]
	if len(runs) == 0 {
		return "—"
	}
	rf := runs[0]
	mark := map[string]string{run.RunOK: "✓", run.RunFail: "✗", run.RunCancel: "■", run.RunPause: "‖", run.RunRunning: "◉", run.RunInterrupt: "↻"}[rf.State]
	if mark == "" {
		mark = "·"
	}
	s := mark + " " + wfxAgo(rf.StartedAt, now)
	if rf.Dry {
		s += " dry"
	}
	return s
}

func (m *Model) wfxListRows(w int, now time.Time) []wfxLine {
	st := m.wfxS()
	var out []wfxLine
	nameW := 4
	for _, n := range m.wfxKeys() {
		nameW = max(nameW, len(n))
	}
	nameW = min(nameW, max(w/2-2, 8))
	for _, name := range m.wfxKeys() {
		e, _ := wire.Find(st.list, name)
		mark, word, tint := m.wfxDot(e)
		cur := "  "
		if name == st.sel {
			cur = "▸ "
			if tint == "" || tint == "dim" {
				tint = "sel"
			}
		}
		out = append(out, wfxLine{fmt.Sprintf(" %s%s  %s %-11s %s", cur, padRight(rcCut(name, nameW), nameW), mark, word, m.wfxLastWord(name, now)), tint})
	}
	if len(out) == 0 {
		out = append(out, wfxLine{"   no workflows yet", "dim"},
			wfxLine{"   add .termocode/workflows/<name>.jsonc", "dim"})
	}
	return out
}

// renderWorkflowsSidebar draws the list (w×h).
func (m Model) renderWorkflowsSidebar(w, h int) string {
	mm := &m
	now := clock.Now()
	st := mm.wfxS()
	lines := []wfxLine{{" WORKFLOWS", "head"}, {" " + strings.Repeat("─", max(w-2, 1)), "dim"}}
	lines = append(lines, mm.wfxListRows(w, now)...)
	foot := " / filter  r run  d dry  ? keys"
	if st.typing || st.filter != "" {
		foot = " / " + st.filter
		if st.typing {
			foot += "▏"
		}
	}
	for len(lines) < h-1 {
		lines = append(lines, wfxLine{})
	}
	lines = append(lines[:max(h-1, 0)], wfxLine{foot, "dim"})
	return wfxPaint(lines, w, h, 0)
}

// renderWorkflowsPage draws the page (w×h, the editor area).
func (m Model) renderWorkflowsPage(w, h int) string {
	mm := &m
	st := mm.wfxS()
	now := clock.Now()
	var lines []wfxLine
	switch {
	case strings.HasPrefix(st.detail, "run:"):
		lines = mm.wfxRunLines(strings.TrimPrefix(st.detail, "run:"), w, now)
	case st.detail != "":
		lines = mm.wfxFileLines(st.detail, w, now)
	default:
		lines = mm.wfxOverviewLines(w, now)
	}
	// the verbs stay on the last row
	body, verbs := lines[:len(lines)-1], lines[len(lines)-1]
	maxScroll := max(len(body)-(h-1), 0)
	scroll := min(st.pageScroll, maxScroll)
	if scroll > 0 {
		body = body[scroll:]
	}
	if len(body) > h-1 {
		body = body[:max(h-1, 0)]
	}
	for len(body) < h-1 {
		body = append(body, wfxLine{})
	}
	return wfxPaint(append(body, verbs), w, h, 0)
}

func wfxRule(w int) wfxLine { return wfxLine{" " + strings.Repeat("─", max(w-2, 1)), "dim"} }

// wfxHeader is "left … right" on one row.
func wfxHeader(left, right string, w int) wfxLine {
	gap := w - textWidth(left) - textWidth(right) - 1
	if gap < 2 {
		return wfxLine{rcCut(left, w), ""}
	}
	return wfxLine{left + strings.Repeat(" ", gap) + right, ""}
}

func (m *Model) wfxOverviewLines(w int, now time.Time) []wfxLine {
	st := m.wfxS()
	running := 0
	var live []*wfxLive
	for _, l := range st.live {
		select {
		case <-l.h.Done():
		default:
			running++
			live = append(live, l)
		}
	}
	sort.Slice(live, func(i, j int) bool { return live[i].id < live[j].id })
	out := []wfxLine{wfxHeader(" ◉ Workflows", fmt.Sprintf("%d workflows · %d running", len(st.list), running), w), wfxRule(w)}
	out = append(out, wfxLine{" NOW", "head"})
	if len(live) == 0 {
		out = append(out, wfxLine{"   nothing runs", "dim"})
	}
	for _, l := range live {
		rf := l.h.Snapshot()
		done, total := wfxStepCount(rf)
		kind := "run"
		if l.dry {
			kind = "dry run"
		}
		out = append(out, wfxLine{fmt.Sprintf("   ◉ %s  %s · %d/%d steps · %s", l.name, kind, done, total, wfxDur(now.Sub(rf.StartedAt))), "ok"})
	}
	out = append(out, wfxLine{" RECENT", "head"})
	var recent []*run.RunFile
	for _, list := range st.runs {
		recent = append(recent, list...)
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].StartedAt.After(recent[j].StartedAt) })
	if len(recent) == 0 {
		out = append(out, wfxLine{"   no runs yet", "dim"})
	}
	for i, rf := range recent {
		if i == 8 {
			break
		}
		out = append(out, wfxRunRow(rf, now))
	}
	out = append(out, wfxLine{" WORKFLOWS", "head"})
	out = append(out, m.wfxListRows(w, now)...)
	out = append(out, wfxRule(w), wfxLine{" [Open (Enter)] [Run (r)] [Dry run (d)] [On/Off (o)] [Edit (e)] [Keys (?)]", "dim"})
	return out
}

func wfxRunRow(rf *run.RunFile, now time.Time) wfxLine {
	word, tint := wfxStateWord(rf.State)
	kind := ""
	if rf.Dry {
		kind = " · dry run"
	}
	dur := "—"
	if !rf.EndedAt.IsZero() {
		dur = wfxDur(rf.EndedAt.Sub(rf.StartedAt))
	}
	return wfxLine{fmt.Sprintf("   %-6s %-18s %-12s %s%s", wfxAgo(rf.StartedAt, now), rcCut(rf.Workflow, 18), word, dur, kind), tint}
}

func wfxStateWord(state string) (string, string) {
	switch state {
	case run.RunOK:
		return "✓ done", "ok"
	case run.RunFail:
		return "✗ failed", "err"
	case run.RunCancel:
		return "■ stopped", "dim"
	case run.RunPause:
		return "‖ paused", "warn"
	case run.RunInterrupt:
		return "↻ interrupted", "warn"
	case run.RunRunning:
		return "◉ running", "ok"
	}
	return state, ""
}

func wfxDur(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d m", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02d", int(d.Hours()), int(d.Minutes())%60)
}

// wfxStepKey is the run file key of a step: its id, or line<N>.
func wfxStepKey(s *workflow.Step) string {
	if s.ID != "" {
		return s.ID
	}
	return fmt.Sprintf("line%d", s.Line)
}

func wfxStepCount(rf *run.RunFile) (done, total int) {
	for _, s := range rf.Steps {
		total++
		if s != nil && s.State != run.StatePending && s.State != run.StateRunning {
			done++
		}
	}
	return done, total
}

// wfxStepWhat is the short "what it does" of a step.
func wfxStepWhat(s *workflow.Step) string {
	switch {
	case s.Source != nil:
		sp := s.Source
		switch sp.From {
		case "mcp":
			return sp.Server + " · " + sp.Tool
		case "github":
			return "github " + sp.What
		case "shell":
			return strings.Join(sp.Argv, " ")
		case "files":
			return "files " + sp.Glob
		case "http":
			return "http " + sp.Request
		case "db":
			return "db " + sp.Connection
		}
	case s.Plan != nil:
		return "AI plan · " + firstNonEmpty(s.Plan.Model, s.Plan.Profile, "quick") + " model"
	case s.Filter != nil:
		f := s.When
		if s.Filter.Take > 0 {
			f += fmt.Sprintf(" · take %d", s.Filter.Take)
		}
		return f
	case s.Each != nil:
		par := max(s.Each.Parallel, 1)
		wt := ""
		if s.Each.Worktree {
			wt = " · worktree"
		}
		return fmt.Sprintf("%s · parallel %d%s", s.Each.Items, par, wt)
	case s.AI != nil:
		return "our AI · " + firstNonEmpty(s.AI.Profile, s.AI.Model)
	case s.Agent != nil:
		return s.Agent.Agent + " · sandbox " + firstNonEmpty(s.Agent.Sandbox, "worktree")
	case s.Pipeline != nil:
		return "pipeline " + s.Pipeline.Profile
	case s.Tests != nil:
		return "tests " + firstNonEmpty(s.Tests.Task, strings.Join(s.Tests.Argv, " "))
	case s.Shell != nil:
		return strings.Join(s.Shell.Argv, " ")
	case s.Write != nil:
		t, _, _ := wfxDescribe(*s.Write)
		return t
	case s.Loop != nil:
		w := fmt.Sprintf("up to %d rounds", s.Loop.Max)
		if s.Loop.Until != "" {
			w += " · until " + s.Loop.Until
		}
		if s.Loop.BudgetUSD > 0 {
			w += fmt.Sprintf(" · cap $%.2f", s.Loop.BudgetUSD)
		}
		return w
	case s.Branch != nil:
		names := make([]string, len(s.Branch.Branches))
		for i, b := range s.Branch.Branches {
			names[i] = b.Name
		}
		return strings.Join(names, " ‖ ") + " · join " + s.Branch.Join
	case s.Wait != nil:
		return "wait for " + s.Wait.For + " · PR " + s.Wait.PR + " · every " + s.Wait.Every.String()
	}
	return ""
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func wfxDescribe(s workflow.WriteSpec) (target, preview, undo string) {
	switch s.To {
	case "mcp", "status":
		target = s.Server + " " + s.Tool
	case "file":
		target = "file " + s.Path
	case "message":
		target, preview = "message · "+s.Via, s.Text
	case "github.comment":
		target, preview = "comment on PR "+s.PR, s.Body
	case "pr":
		target, preview = "PR → "+s.Base, s.Title
	default:
		target = s.To
	}
	if s.To == "file" {
		preview = s.Content
	}
	return target, preview, undo
}

func (m *Model) wfxGateWord(e wire.Entry, s *workflow.Step) string {
	if s.Write == nil {
		return ""
	}
	switch s.Write.Gate {
	case workflow.GateDry:
		return "dry only"
	case workflow.GateAllow:
		if ok, _ := ledger.Allowed(m.wfxS().root, e.Name, e.Hash, s.ID); ok {
			return "✓ allowed"
		}
	}
	return "asks"
}

func (m *Model) wfxFileLines(name string, w int, now time.Time) []wfxLine {
	st := m.wfxS()
	e, ok := wire.Find(st.list, name)
	if !ok {
		return []wfxLine{{" no workflow " + name, "dim"}, {" [Back (Esc)]", "dim"}}
	}
	mark, word, tint := m.wfxDot(e)
	when := "by hand"
	if e.File != nil && len(e.File.Triggers) > 0 {
		when = fmt.Sprintf("on %d trigger(s)", len(e.File.Triggers))
	}
	if e.File != nil && e.File.Schedule != nil {
		sc := e.File.Schedule
		when = strings.TrimSpace(firstNonEmpty(sc.Cron, sc.Preset+" "+sc.Time) + " " + sc.TZ)
		if next, ok := m.wfxNextRun(e.Name); ok && e.Enabled {
			when += " · next " + wfxAgo(next, now)
		}
	}
	title := wfxTitle(e)
	left := " ◉ " + e.Name
	if title != "" {
		left += " · " + title
	}
	head := wfxHeader(left, mark+" "+word+" · "+when, w)
	head.tint = tint
	out := []wfxLine{head, wfxRule(w)}
	if e.Broken() {
		out = append(out, wfxLine{" PROBLEMS  (e opens the file at the first one)", "head"})
		for _, p := range e.Problems {
			out = append(out, wfxLine{"   " + p.String(), "err"})
		}
		out = append(out, wfxLine{" FILE      " + wfxRel(e), "dim"})
		return append(out, wfxRule(w), wfxLine{" [Edit file (e)] [Back (Esc)]", "dim"})
	}
	if len(e.File.Triggers) > 0 {
		out = append(out, wfxLine{" TRIGGERS  (Workflows: Trigger Events… lists what came in)", "head"})
		for _, t := range e.File.Triggers {
			out = append(out, wfxLine{"   " + wfxTrigWords(t), ""})
		}
	}
	out = append(out, wfxLine{" STEPS", "head"})
	var add func(steps []workflow.Step, depth int)
	add = func(steps []workflow.Step, depth int) {
		for i := range steps {
			s := &steps[i]
			ind := strings.Repeat("  ", depth)
			row := fmt.Sprintf("   %s%-8s %-7s %s", ind, rcCut(wfxStepKey(s), 8), s.Type, wfxStepWhat(s))
			if g := m.wfxGateWord(e, s); g != "" {
				row = padRight(row, max(w-14, textWidth(row)+2)) + g
			}
			if len(s.Else) > 0 {
				row += "  else → " + wfxStepKey(&s.Else[0])
			}
			if s.Handoff != "" {
				row += "  ← handoff " + s.Handoff
			}
			out = append(out, wfxLine{row, ""})
			if s.Branch != nil {
				for _, b := range s.Branch.Branches {
					out = append(out, wfxLine{"   " + ind + "  ‖ " + b.Name, "dim"})
					add(b.Steps, depth+2)
				}
				continue
			}
			for _, l := range s.Lists() {
				add(l, depth+1)
			}
		}
	}
	add(e.File.Steps, 0)
	trust := "trusted for this file version"
	if e.Private {
		trust = "your own file (" + wfxRel(e) + ")"
	} else if !e.Trusted {
		trust = "not trusted yet — r or d shows the file and asks"
	}
	out = append(out, wfxLine{" TRUST            " + trust, map[bool]string{true: "", false: "warn"}[e.Trusted]})
	allowed := "none (every write asks)"
	if s, err := ledger.LoadState(st.root, e.Name, e.Hash); err == nil && len(s.Allow) > 0 {
		allowed = strings.Join(s.Allow, ", ") + "  (for this file version)"
	}
	out = append(out, wfxLine{" ALLOWED WRITES   " + allowed, ""})
	out = append(out, wfxLine{" LIMITS           " + wfxLimits(e.File.Limits), ""})
	var last []string
	for i, rf := range st.runs[e.Name] {
		if i == 3 {
			break
		}
		word, _ := wfxStateWord(rf.State)
		s := wfxAgo(rf.StartedAt, now) + " " + word
		if rf.Dry {
			s += " (dry)"
		}
		last = append(last, s)
	}
	if len(last) == 0 {
		last = []string{"none yet"}
	}
	out = append(out, wfxLine{" LAST RUNS        " + strings.Join(last, " · "), ""})
	if len(e.Warns) > 0 {
		out = append(out, wfxLine{" WARNINGS", "head"})
		for _, p := range e.Warns {
			out = append(out, wfxLine{"   " + p.String(), "warn"})
		}
	}
	return append(out, wfxRule(w), wfxLine{" [Run now (r)] [Dry run (d)] [On/Off (o)] [Edit file (e)] [History (h)] [Back (Esc)]", "dim"})
}

func wfxLimits(l workflow.Limits) string {
	var parts []string
	if l.BudgetUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f run", l.BudgetUSD))
	}
	if l.ItemBudgetUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f item", l.ItemBudgetUSD))
	}
	if l.Timeout > 0 {
		parts = append(parts, wfxDur(l.Timeout))
	}
	parts = append(parts, fmt.Sprintf("%d items max", l.MaxItems))
	if l.Parallel > 1 {
		parts = append(parts, fmt.Sprintf("%d parallel", l.Parallel))
	}
	return strings.Join(parts, " · ")
}

func wfxStepMark(state string) string {
	switch state {
	case run.StateDone:
		return "✓"
	case run.StateFailed:
		return "✗"
	case run.StateRunning:
		return "◉"
	case run.StateDenied:
		return "⊘"
	case run.StateWouldRun:
		return "would run"
	case run.StateRecorded:
		return "would write"
	case run.StateSkipped:
		return "skipped"
	case run.StatePaused:
		return "‖"
	case run.StateCancelled:
		return "■"
	case run.StateInterrupted:
		return "↻"
	}
	return "○"
}

func (m *Model) wfxRunLines(id string, w int, now time.Time) []wfxLine {
	st := m.wfxS()
	rf, ok := m.wfxRunByID(id)
	if !ok {
		return []wfxLine{{" no run " + id, "dim"}, {" [Back (Esc)]", "dim"}}
	}
	e, _ := wire.Find(st.list, rf.Workflow)
	l := st.live[id]
	reads, writes := 0, 0
	for _, wr := range rf.Writes {
		if wr.State == run.StateRecorded || wr.State == "done" {
			writes++
		}
	}
	for _, s := range rf.Steps {
		if s != nil {
			reads += len(s.Items)
		}
	}
	word, tint := wfxStateWord(rf.State)
	var left, right string
	end := rf.EndedAt
	if end.IsZero() {
		end = now
	}
	if rf.Dry {
		left = fmt.Sprintf(" ◉ Dry run · %s · %s", rf.Workflow, wfxAgo(rf.StartedAt, now))
		right = fmt.Sprintf("%s · read %d · would write %d", word, reads, writes)
	} else {
		left = fmt.Sprintf(" ◉ %s · run %s · %s", rf.Workflow, wfxAgo(rf.StartedAt, now), wfxDur(end.Sub(rf.StartedAt)))
		right = fmt.Sprintf("%s · wrote %d", word, writes)
	}
	head := wfxHeader(left, right, w)
	head.tint = tint
	out := []wfxLine{head, wfxRule(w)}
	if rf.Error != "" {
		out = append(out, wfxLine{" ERROR  " + rf.Error, "err"})
	}
	if rf.Resumed > 0 {
		out = append(out, wfxLine{fmt.Sprintf(" ↻ Resumed after termocode ended mid-run (%d×): finished steps kept, the rest ran again", rf.Resumed), "warn"})
	}
	if e.File != nil {
		var chain []string
		for i := range e.File.Steps {
			s := &e.File.Steps[i]
			k := wfxStepKey(s)
			ss := rf.Steps[k]
			mark := "○"
			if ss != nil {
				mark = wfxStepMark(ss.State)
				if len(ss.Items) > 0 {
					mark += fmt.Sprintf(" %d", len(ss.Items))
				}
			}
			if st.stepTab == i+1 {
				chain = append(chain, "‹"+k+" "+mark+"›")
				continue
			}
			chain = append(chain, k+" "+mark)
		}
		out = append(out, wfxLine{" " + strings.Join(chain, "  →  "), ""})
		if i := st.stepTab - 1; i >= 0 && i < len(e.File.Steps) {
			out = append(out, wfxStepTabLines(&e.File.Steps[i], rf.Steps[wfxStepKey(&e.File.Steps[i])])...)
		}
		out = append(out, wfxLine{" STEPS", "head"})
		for i := range e.File.Steps {
			s := &e.File.Steps[i]
			k := wfxStepKey(s)
			ss := rf.Steps[k]
			if ss == nil {
				out = append(out, wfxLine{fmt.Sprintf("   %-8s %-7s ○ not yet", rcCut(k, 8), s.Type), "dim"})
				continue
			}
			row := fmt.Sprintf("   %-8s %-7s %s", rcCut(k, 8), s.Type, wfxStepMark(ss.State))
			if len(ss.Items) > 0 {
				row += fmt.Sprintf(" → %d items", len(ss.Items))
			}
			if ss.Attempts > 1 {
				row += fmt.Sprintf(" · try %d", ss.Attempts)
			}
			tint := ""
			if ss.State == run.StateFailed || ss.State == run.StateDenied {
				tint = "err"
			}
			if ss.Error != "" {
				row += " · " + ss.Error
			}
			if ss.Wait != "" && ss.State == run.StateRunning {
				row += " · " + ss.Wait
			}
			out = append(out, wfxLine{row, tint})
			out = append(out, wfxGraphRows(s, ss)...)
			if ss.Each != nil && len(ss.Each.Items) > 0 {
				out = append(out, wfxLine{" ITEMS", "head"})
				for _, it := range ss.Each.Items {
					out = append(out, wfxItemRow(it))
				}
			}
		}
	}
	if len(rf.Writes) > 0 {
		label := " WRITES"
		if rf.Dry {
			label = " WOULD WRITE  (a dry run never sends)"
		}
		out = append(out, wfxLine{label, "head"})
		for _, wr := range rf.Writes {
			target, preview, _ := wfxDescribe(wr.Spec)
			row := fmt.Sprintf("   %-8s %-11s %s", rcCut(wr.Step, 8), wfxStepMark(wr.State), target)
			if wr.Item != "" {
				row += " · " + wr.Item
			}
			if p := strings.TrimSpace(firstLine(preview)); p != "" {
				row += "  \"" + rcCut(p, 40) + "\""
			}
			if wr.State != run.StateRecorded {
				row += "  " + wr.State
			}
			out = append(out, wfxLine{row, ""})
		}
	}
	if !rf.Dry && st.root != "" {
		if undo := wire.Undoable(st.root, rf); len(undo) > 0 {
			out = append(out, wfxLine{" UNDO  (u takes back the first one not undone; it asks first)", "head"})
			for _, u := range undo {
				row := fmt.Sprintf("   %-8s %s", rcCut(u.Step, 8), u.Title)
				if u.Item != "" {
					row = fmt.Sprintf("   %-8s %s · %s", rcCut(u.Step, 8), u.Item, u.Title)
				}
				tint := ""
				if u.Undone {
					row, tint = row+"  ↶ undone", "dim"
				} else if u.Blocked != "" {
					row, tint = row+"  ✗ undo by hand (a secret it needs was not kept)", "warn"
				}
				out = append(out, wfxLine{row, tint})
			}
		}
	}
	if l != nil {
		if l.budget != "" {
			out = append(out, wfxLine{" PAUSED", "head"}, wfxLine{"   the " + wfxBudgetName(l.budget) + " is reached: the card asks to stop or go on", "warn"})
		} else if l.h.Held() {
			out = append(out, wfxLine{" PAUSED", "head"}, wfxLine{"   by you: the working step finishes, the next one waits (p continues)", "warn"})
		}
		if l.waiting != "" {
			out = append(out, wfxLine{" NEEDS YOU", "head"}, wfxLine{"   " + l.waiting + " waits for your answer on the card (Deny is first)", "warn"})
		}
		if tail := l.tail(8); len(tail) > 0 {
			out = append(out, wfxLine{" LIVE", "head"})
			for _, t := range tail {
				out = append(out, wfxLine{"   " + t, "dim"})
			}
		}
	}
	verbs := " [Back (Esc)] [Logs (l)]"
	if !rf.Dry && st.root != "" && len(wire.Undoable(st.root, rf)) > 0 {
		verbs = " [Undo… (u)] [Back (Esc)] [Logs (l)]"
	}
	if l != nil {
		verbs = " [Stop (x)] [Pause (p)] [Logs (l)] [Back (Esc)]"
		if l.h.Held() {
			verbs = " [Stop (x)] [Continue (p)] [Logs (l)] [Back (Esc)]"
		}
	} else if rf.Dry {
		verbs = " [Run for real (r)] [Dry run again (d)] [Back (Esc)]"
	} else if wfxRetryable(e, rf) {
		verbs = " [Retry (t)]" + verbs
	}
	return append(out, wfxRule(w), wfxLine{verbs, "dim"})
}

// wfxGraphRows are the rounds of a loop step and the branches of a
// branch step under its row in the run view (CMP-42).
func wfxGraphRows(s *workflow.Step, ss *run.StepState) []wfxLine {
	var out []wfxLine
	if ss.Loop != nil {
		for i, rs := range ss.Loop.Rounds {
			row := fmt.Sprintf("     round %-2d %s %s", i+1, wfxStepMark(rs.State), wfxStepMarks(rs.Steps))
			if rs.Error != "" {
				row += " · " + rs.Error
			}
			out = append(out, wfxLine{row, "dim"})
		}
	}
	if s.Branch != nil && ss.Branches != nil {
		for _, b := range s.Branch.Branches {
			bs := ss.Branches[b.Name]
			if bs == nil {
				continue
			}
			row := fmt.Sprintf("     ‖ %-8s %s %s", rcCut(b.Name, 8), wfxStepMark(bs.State), wfxStepMarks(bs.Steps))
			tint := "dim"
			if bs.State == run.StateFailed {
				row, tint = row+" · "+bs.Error, "err"
			}
			out = append(out, wfxLine{row, tint})
		}
	}
	return out
}

// wfxStepMarks is "id mark · id mark" for a list's step states, by id.
func wfxStepMarks(steps map[string]*run.StepState) string {
	keys := make([]string, 0, len(steps))
	for k := range steps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		if s := steps[k]; s != nil {
			parts = append(parts, k+" "+wfxStepMark(s.State))
		}
	}
	return strings.Join(parts, " · ")
}

func wfxItemRow(it *run.ItemState) wfxLine {
	keys := make([]string, 0, len(it.Steps))
	for k := range it.Steps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		if s := it.Steps[k]; s != nil {
			parts = append(parts, k+" "+wfxStepMark(s.State))
		}
	}
	word, tint := wfxStateWord(it.State)
	if it.State == run.StateFailed {
		word, tint = "✗ failed", "err"
	}
	row := fmt.Sprintf("   %-10s %-10s %s", rcCut(it.Key, 10), word, strings.Join(parts, " · "))
	if it.Error != "" {
		row += " · " + it.Error
	}
	return wfxLine{row, tint}
}

// ── The page tab ─────────────────────────────────────────────────────────

func wfxPageTmp() string { return filepath.Join(os.TempDir(), "termocode-workflows-page") }

// isWfxPagePath says whether a buffer name is the Workflows page.
func isWfxPagePath(p string) bool {
	return p == "termocode-workflows-page" || strings.HasSuffix(p, "/termocode-workflows-page")
}

// wfxPageShown reports whether the editor area shows the page now.
func (m Model) wfxPageShown() bool {
	return m.wfx != nil && m.wfx.pageOn && isWfxPagePath(m.editor.Path())
}

// wfxPageTitle is the tab name.
func (m Model) wfxPageTitle() string {
	name := "Workflows"
	if m.wfx != nil && m.wfx.detail != "" {
		name = strings.TrimPrefix(m.wfx.detail, "run:")
		if rf, ok := (&m).wfxRunByID(name); ok && strings.HasPrefix(m.wfx.detail, "run:") {
			name = rf.Workflow
			if rf.Dry {
				name = "Dry run · " + name
			}
		}
	}
	return "⇶ " + rcCut(name, 28)
}

// wfxShowPage shows detail ("" overview, a name, "run:<id>") in the page
// tab, opening the tab when it is not there.
func (m *Model) wfxShowPage(detail string, focus bool) tea.Cmd {
	st := m.wfxS()
	if st.detail != detail {
		st.pageScroll, st.stepTab = 0, 0
	}
	st.pageOn, st.detail = true, detail
	if detail != "" && !strings.HasPrefix(detail, "run:") {
		st.sel = detail
	}
	if focus {
		m.focus = FocusEditor
	}
	if m.nvim == nil {
		return nil
	}
	if !m.wfxPageShown() {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`local name = ...
local b = vim.g.termocode_workflows_buf
if not (b and vim.api.nvim_buf_is_valid(b)) then
  b = vim.api.nvim_create_buf(true, false)
  vim.bo[b].buftype = 'nofile'
  vim.bo[b].swapfile = false
  vim.bo[b].bufhidden = 'hide'
  pcall(vim.api.nvim_buf_set_name, b, name)
  vim.bo[b].modifiable = false
  vim.g.termocode_workflows_buf = b
end
if vim.api.nvim_get_current_buf() ~= b then vim.cmd('keepalt buffer ' .. b) end
vim.cmd('stopinsert')`, wfxPageTmp())
	}
	return m.fetchStateCmd()
}
