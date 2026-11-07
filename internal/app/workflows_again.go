package app

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// ── Workflows: step tabs, retry, plan approval (CMP-45) ─────────────────
//
// A run page has a tab per step: [ and ] pick one, and its detail (state,
// tries, error, output, the list it made, its items) shows above the
// step rows. t runs an ended run again from its first part that did not
// end well (run.Retry): the steps before it keep their answers, done
// items are kept, writes that went out are never sent twice. It asks
// first; Cancel is first. A plan step with "approve": true waits on the
// write gate card (its own words: Stop here first, then Approve plan).

// wfxStepTabRows caps the rows of a step tab's lists and output.
const wfxStepTabRows = 12

// wfxStepTabMove moves the run page's step tab: 0 is "all steps", 1..n
// the file's steps.
func (m *Model) wfxStepTabMove(d int) {
	st := m.wfxS()
	id, ok := strings.CutPrefix(st.detail, "run:")
	if !ok {
		return
	}
	rf, ok := m.wfxRunByID(id)
	if !ok {
		return
	}
	e, ok := wire.Find(st.list, rf.Workflow)
	if !ok || e.File == nil {
		return
	}
	st.stepTab = min(max(st.stepTab+d, 0), len(e.File.Steps))
}

// wfxStepTabLines is the detail of one step of a run.
func wfxStepTabLines(s *workflow.Step, ss *run.StepState) []wfxLine {
	k := wfxStepKey(s)
	head := " STEP " + k + " · " + s.Type
	if what := wfxStepWhat(s); what != "" {
		head += " · " + what
	}
	out := []wfxLine{{head, "head"}}
	if ss == nil {
		return append(out, wfxLine{"   ○ not started yet", "dim"})
	}
	word, tint := wfxStateWord(ss.State)
	if word == ss.State { // a step state the run words do not know
		word = wfxStepMark(ss.State) + " " + word
	}
	if ss.State == run.StateDenied {
		tint = "err"
	}
	row := "   " + word
	if ss.Attempts > 0 {
		row += fmt.Sprintf(" · try %d", ss.Attempts)
	}
	out = append(out, wfxLine{row, tint})
	if ss.Error != "" {
		out = append(out, wfxLine{"   ERROR  " + ss.Error, "err"})
	}
	if s.Plan != nil && s.Plan.Approve {
		out = append(out, wfxLine{"   asks you to approve its plan before it goes on", "dim"})
	}
	if o := ss.Output; o != nil {
		text := strings.TrimRight(firstNonEmpty(o.Text, o.URL), "\n")
		if text != "" {
			out = append(out, wfxLine{"   OUTPUT", "dim"})
			lines := strings.Split(text, "\n")
			for i, ln := range lines {
				if i == wfxStepTabRows {
					out = append(out, wfxLine{fmt.Sprintf("     … %d more lines", len(lines)-i), "dim"})
					break
				}
				out = append(out, wfxLine{"     " + ln, ""})
			}
		}
	}
	if len(ss.Items) > 0 {
		out = append(out, wfxLine{fmt.Sprintf("   LIST  %d items", len(ss.Items)), "dim"})
		for i, it := range ss.Items {
			if i == wfxStepTabRows {
				out = append(out, wfxLine{fmt.Sprintf("     … %d more", len(ss.Items)-i), "dim"})
				break
			}
			out = append(out, wfxLine{"     " + it.Key + wfxPlanWord(it.Plan), ""})
		}
	}
	if ss.Each != nil {
		for i, it := range ss.Each.Items {
			if i == wfxStepTabRows {
				out = append(out, wfxLine{fmt.Sprintf("     … %d more items", len(ss.Each.Items)-i), "dim"})
				break
			}
			out = append(out, wfxItemRow(it))
		}
	}
	return out
}

// wfxPlanWord is an item's plan mark ("" = none).
func wfxPlanWord(p *run.PlanMark) string {
	if p == nil {
		return ""
	}
	var parts []string
	if p.Rank > 0 {
		parts = append(parts, fmt.Sprintf("#%d", p.Rank))
	}
	if p.Skip != "" {
		parts = append(parts, "skip: "+p.Skip)
	}
	if len(p.Labels) > 0 {
		parts = append(parts, strings.Join(p.Labels, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  (" + strings.Join(parts, " · ") + ")"
}

// wfxRetryable: the run has ended (not dry) and run.Retry has something
// to run again.
func wfxRetryable(e wire.Entry, rf *run.RunFile) bool {
	return e.File != nil && run.CanRetry(rf, e.File)
}

// wfxRetryRun is the run a retry is about: the run page's, else the
// selected workflow's newest real run ("" = none).
func (m *Model) wfxRetryRun() string {
	st := m.wfxS()
	if id, ok := strings.CutPrefix(st.detail, "run:"); ok && st.pageOn {
		return id
	}
	e, ok := m.wfxTarget()
	if !ok {
		return ""
	}
	for _, rf := range st.runs[e.Name] {
		if !rf.Dry {
			return rf.ID
		}
	}
	return ""
}

// wfxAskRetry asks before an ended run runs again from its failed part.
func (m *Model) wfxAskRetry() tea.Cmd {
	id := m.wfxRetryRun()
	if id == "" {
		return m.aiInfo("No run to retry")
	}
	rf, ok := m.wfxRunByID(id)
	if !ok {
		return m.aiInfo("No run to retry")
	}
	st := m.wfxS()
	e, ok := wire.Find(st.list, rf.Workflow)
	switch {
	case !ok || e.File == nil:
		return m.aiInfo(rf.Workflow + " has no good file now; fix it first")
	case st.live[id] != nil || m.wfxRunning(e.Name) != nil:
		return m.aiInfo(e.Name + " is running; stop it or wait")
	case !workflowsEnabled():
		return m.aiInfo("Workflows are turned off (Settings → Workflows → workflows_enabled)")
	}
	again, err := run.Retry(rf, e.File)
	if err != nil {
		return m.aiInfo(e.Name + ": " + err.Error())
	}
	st.pendingRun = id
	m.wfxCard(confirmKindWorkflowRetry, "Retry the run of "+e.Name+"?", wfxRetryBody(e.File, again),
		confirm.Button{ID: "cancel", Title: "Cancel"}, confirm.Button{ID: "retry", Title: "Retry"})
	return nil
}

// wfxRetryBody tells what runs again and what is kept.
func wfxRetryBody(f *workflow.File, after *run.RunFile) string {
	var again, kept []string
	for i := range f.Steps {
		k := wfxStepKey(&f.Steps[i])
		if now := after.Steps[k]; now == nil || now.State == run.StatePending {
			again = append(again, k)
		} else {
			kept = append(kept, k)
		}
	}
	items := 0
	for _, ss := range after.Steps {
		if ss != nil && ss.Each != nil {
			for _, it := range ss.Each.Items {
				if it.State == run.StatePending {
					items++
				}
			}
		}
	}
	var b strings.Builder
	b.WriteString("Run " + after.ID + " goes on in the same run.\n")
	b.WriteString("Runs again: " + strings.Join(again, ", "))
	if items > 0 {
		b.WriteString(fmt.Sprintf(" (%d item(s); done items are kept)", items))
	}
	if len(kept) > 0 {
		b.WriteString("\nSteps kept with their answers: " + strings.Join(kept, ", "))
	}
	b.WriteString("\nWrites that went out are never sent twice; every new write asks again unless you allowed it.")
	return b.String()
}

// answerWorkflowRetryCard handles the retry card; ok=false: not ours.
func (m *Model) answerWorkflowRetryCard(kind confirmKindEnum, id string) (tea.Cmd, bool) {
	if kind != confirmKindWorkflowRetry {
		return nil, false
	}
	st := m.wfxS()
	runID := st.pendingRun
	st.pendingRun = ""
	if id != "retry" {
		return nil, true
	}
	rf, ok := m.wfxRunByID(runID)
	if !ok {
		return m.aiInfo("That run is gone"), true
	}
	e, ok := wire.Find(st.list, rf.Workflow)
	if !ok || e.File == nil || m.wfxRunning(e.Name) != nil {
		return m.aiInfo(rf.Workflow + " cannot run again now"), true
	}
	again, err := run.Retry(rf, e.File)
	if err != nil {
		return m.aiInfo(e.Name + ": " + err.Error()), true
	}
	_, _, cmd, err := m.wfxStartRunFrom(e, false, true, again)
	if err != nil {
		return m.aiInfo(e.Name + ": " + agentCtlShort(err.Error())), true
	}
	st.stepTab = 0
	return tea.Batch(cmd, m.aiInfo(e.Name+": retrying run "+runID)), true
}

// wfxPlanAsk is the gate ask of a plan that waits for approval: the
// order (rank) and the items it skips.
func wfxPlanAsk(c run.ApproveCall) wire.GateAsk {
	key := ""
	if c.Step != nil {
		key = wfxStepKey(c.Step)
	}
	ranked := append([]run.Item(nil), c.Items...)
	sort.SliceStable(ranked, func(i, j int) bool { return wfxRank(ranked[i]) < wfxRank(ranked[j]) })
	var b strings.Builder
	n := 0
	for _, it := range ranked {
		if it.Plan != nil && it.Plan.Skip != "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "%d. %s%s\n", n, it.Key, wfxPlanWord(it.Plan))
	}
	for _, it := range ranked {
		if it.Plan != nil && it.Plan.Skip != "" {
			fmt.Fprintf(&b, "skip %s: %s\n", it.Key, it.Plan.Skip)
		}
	}
	return wire.GateAsk{Step: key, Item: c.Item, Target: fmt.Sprintf("plan step %s · %d items", key, len(c.Items)), Preview: b.String()}
}

// wfxRank sorts unranked items last.
func wfxRank(it run.Item) int {
	if it.Plan == nil || it.Plan.Rank <= 0 {
		return 1 << 30
	}
	return it.Plan.Rank
}
