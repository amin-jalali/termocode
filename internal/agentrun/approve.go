package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// Plan approval (G-PLAN). With Options.Approval set, a step that may
// write files or run commands never starts before the user approved the
// plan: the run stops before the next step (like a pause), sends
// RunAwaitApproval with the steps left and waits for Approve, EditPlan
// or Deny. A planner's plan is shown once it is there (the planner
// itself only reads). Steps that join later (a new plan, the loop's fix
// step) ask again only when they were not approved and may write or run
// commands; otherwise the run goes on with RunPlanKept.

// Approval modes (Options.Approval).
const (
	ApprovalOff    = ""       // never ask (ai_plan_approval "auto")
	ApprovalAsk    = "ask"    // ask before steps that write files or run commands
	ApprovalAlways = "always" // ask before any step, read-only plans too
)

type decisionKind int

const (
	decideApprove decisionKind = iota + 1
	decideEdit
)

type decision struct {
	kind decisionKind
	text string
}

// Approve runs the plan the run waits on: every step it has now (and
// the loop's fix step) is approved. A no-op when it does not wait.
func (h *Handle) Approve() { h.decideAsync(decision{kind: decideApprove}) }

// EditPlan replaces the steps left of the plan the run waits on with
// text (PlanText's shape, see ApplyPlanEdit). The run then asks again
// (RunPlanEdited, then RunAwaitApproval); a text that does not fit
// leaves the plan as it was and the new RunAwaitApproval carries the
// error.
func (h *Handle) EditPlan(text string) { h.decideAsync(decision{kind: decideEdit, text: text}) }

// Deny stops the run before any step that waits for approval starts; it
// ends with RunCancelled and Err DeniedErr.
func (h *Handle) Deny() {
	h.denied.Store(true)
	h.cancel()
}

// DeniedErr is the Err of the final event of a run whose plan was denied.
const DeniedErr = "the plan was denied"

func (h *Handle) decideAsync(d decision) {
	select {
	case h.decide <- d:
	default: // one answer at a time; the card is shown again after it
	}
}

// acts reports whether n may write files or run commands.
func (h *Handle) acts(n agentdag.Node) (writes, exec bool) {
	if h.o.Acts != nil {
		return h.o.Acts(n)
	}
	w := agentdag.Writes(n)
	return w, false
}

// gated reports whether n may not start before the user approved it.
func (h *Handle) gated(n agentdag.Node) bool {
	if h.o.Approval == ApprovalOff || n.Plan || contains(h.run.Approved, n.ID) {
		return false
	}
	if h.o.Approval == ApprovalAlways {
		return true
	}
	w, x := h.acts(n)
	return w || x
}

// needsApproval reports whether the run must wait for the user before
// it starts any of ready: one of them is gated, or the plan is complete
// (no planner left to run) and a step still to run is gated, so the
// user sees the whole plan before its first step. Loop goroutine only.
func (h *Handle) needsApproval(ready []string) bool {
	if h.o.Approval == ApprovalOff {
		return false
	}
	for _, id := range ready {
		if h.gated(*h.node(id)) {
			return true
		}
	}
	for _, n := range h.run.Nodes {
		if n.Plan && !n.State.Done() && !n.State.Stops() {
			return false // the plan is not there yet
		}
	}
	for _, n := range h.run.Nodes {
		if !n.State.Done() && !n.State.Stops() && h.gated(n) {
			return true
		}
	}
	return false
}

// leftNodes are the steps that did not run yet, in run order, and the
// loop's fix step when it has not joined (it runs only if verify fails).
func (h *Handle) leftNodes() []agentdag.Node {
	var out []agentdag.Node
	for _, n := range h.run.Nodes {
		if !n.State.Done() && !n.State.Stops() {
			out = append(out, n)
		}
	}
	if l := h.run.Loop; l != nil && h.fix != "" {
		if _, ok := h.order[h.fix]; !ok {
			f := l.Fix
			if f.Title == "" {
				f.Title = f.ID
			}
			if f.Slot == "" {
				f.Slot = h.o.Categories[f.Category]
			}
			f.State = agentdag.StatePending
			out = append(out, f)
		}
	}
	return out
}

// awaitApproval shows the plan and waits for the user's answer. It
// returns false when the run stopped while waiting. Loop goroutine only.
func (h *Handle) awaitApproval(ctx context.Context) bool {
	errText := ""
	for {
		h.emit(Event{Kind: RunAwaitApproval, Nodes: h.leftNodes(), Err: errText, Round: h.round()})
		select {
		case <-ctx.Done():
			return false
		case d := <-h.decide:
			switch d.kind {
			case decideApprove:
				ids := append([]string(nil), h.run.Approved...)
				for _, n := range h.leftNodes() {
					if !contains(ids, n.ID) {
						ids = append(ids, n.ID)
					}
				}
				h.mu.Lock()
				h.run.Approved = ids
				h.mu.Unlock()
				h.emit(Event{Kind: RunApproved})
				return true
			case decideEdit:
				errText = ""
				edited, err := ApplyPlanEdit(h.run, d.text, h.o.Categories)
				if err != nil {
					errText = err.Error()
					continue
				}
				ls, _ := agentdag.Layers(edited)
				before := len(h.run.Nodes)
				h.mu.Lock()
				h.run = edited
				h.index(ls)
				h.mu.Unlock()
				added := append([]agentdag.Node(nil), edited.Nodes[before:]...)
				h.emit(Event{Kind: RunPlanEdited, Nodes: added})
			}
		}
	}
}

// planKept tells the UI that steps joined an approved run without
// asking (none of them needs approval). Loop goroutine only.
func (h *Handle) planKept(added []agentdag.Node) {
	if h.o.Approval == ApprovalOff || len(h.run.Approved) == 0 {
		return
	}
	for _, n := range added {
		if h.gated(n) {
			return // the run asks before it starts them
		}
	}
	h.emit(Event{Kind: RunPlanKept, Nodes: added, Round: h.round()})
}

// planEditDoc is the text the user edits: PlanText writes it,
// ApplyPlanEdit reads it.
type planEditDoc struct {
	Nodes []planEditNode `json:"nodes"`
}

type planEditNode struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Prompt   string   `json:"prompt"`
	Category string   `json:"category,omitempty"`
	Deps     []string `json:"deps"`
}

// PlanText is the editable plan of r: its steps that did not run yet, in
// the planner's JSON shape (id, title, prompt = the task, category,
// deps).
func PlanText(r agentdag.Run) string {
	doc := planEditDoc{Nodes: []planEditNode{}}
	for _, n := range r.Nodes {
		if n.State.Done() || n.State.Stops() {
			continue
		}
		deps := agentdag.Deps(r, n.ID)
		if deps == nil {
			deps = []string{}
		}
		doc.Nodes = append(doc.Nodes, planEditNode{ID: n.ID, Title: n.Title, Prompt: n.Prompt, Category: n.Category, Deps: deps})
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return string(b) + "\n"
}

// ApplyPlanEdit applies an edited plan (PlanText's shape) to the steps of
// r that did not run yet:
//   - a step that is listed gets the title, task, category and deps
//     given (a profile step keeps its model; an empty task keeps none);
//   - a step that is left out is skipped (the loop's verify step must
//     stay);
//   - a new id is a new step (it needs a task), on the category's model
//     slot from cats.
//
// Steps that ran already cannot be listed, but deps may name them. The
// result is checked as a whole; errors say what to fix.
func ApplyPlanEdit(r agentdag.Run, text string, cats map[string]string) (agentdag.Run, error) {
	body, ok := ai.FirstCodeBlock(text)
	if !ok {
		body = strings.TrimSpace(text)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "{") {
		return agentdag.Run{}, errors.New("no plan JSON found: keep the {\"nodes\": [...]} shape")
	}
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var doc planEditDoc
	if err := dec.Decode(&doc); err != nil {
		return agentdag.Run{}, fmt.Errorf("the plan JSON does not parse: %s", strings.TrimPrefix(err.Error(), "json: "))
	}
	out := r.Clone()
	idx := map[string]int{}
	for i, n := range out.Nodes {
		idx[n.ID] = i
	}
	verify := ""
	if out.Loop != nil {
		verify = strings.TrimSpace(out.Loop.Verify)
	}
	listed := map[string]bool{}
	deps := map[string][]string{}
	for _, p := range doc.Nodes {
		id := strings.TrimSpace(p.ID)
		switch {
		case !planIDRe.MatchString(id):
			return agentdag.Run{}, fmt.Errorf("step id %q: use letters, digits, '-', '_' or '.'", p.ID)
		case listed[id]:
			return agentdag.Run{}, fmt.Errorf("duplicate step id %q", id)
		case len(p.Prompt) > PlanPromptMaxBytes:
			return agentdag.Run{}, fmt.Errorf("step %q: the prompt is longer than 8 KB", id)
		case out.Loop != nil && id == strings.TrimSpace(out.Loop.Fix.ID):
			return agentdag.Run{}, fmt.Errorf("step id %q belongs to the loop's fix step; pick another", id)
		}
		listed[id] = true
		cat := p.Category
		switch cat {
		case "", agentdag.CategoryQuick, agentdag.CategoryDeep:
		default:
			return agentdag.Run{}, fmt.Errorf("step %q: unknown category %q (quick or deep)", id, p.Category)
		}
		var ds []string
		for _, d := range p.Deps {
			ds = append(ds, strings.TrimSpace(d))
		}
		deps[id] = ds
		title := cutRunes(agentdag.Clean(p.Title), 80)
		prompt := strings.TrimSpace(p.Prompt)
		if i, ok := idx[id]; ok {
			n := &out.Nodes[i]
			if n.State.Done() || n.State.Stops() {
				return agentdag.Run{}, fmt.Errorf("step %q already ran; leave it out", id)
			}
			if title != "" {
				n.Title = title
			}
			planned := n.Prompt != ""
			n.Prompt = prompt
			if cat != "" && cat != n.Category && n.CLI == "" {
				n.Category = cat
				if planned {
					n.Slot = cats[cat]
				}
			}
			if n.Input.Deps != nil {
				n.Input.Deps = inputDeps(out, ds)
			}
			continue
		}
		if prompt == "" {
			return agentdag.Run{}, fmt.Errorf("new step %q has an empty prompt", id)
		}
		if cat == "" {
			cat = agentdag.CategoryQuick
		}
		if title == "" {
			title = id
		}
		out.Nodes = append(out.Nodes, agentdag.Node{ID: id, Title: title, Category: cat, Slot: cats[cat], Prompt: prompt,
			State: agentdag.StatePending, Input: agentdag.InputRule{Deps: inputDeps(out, ds)}})
		idx[id] = len(out.Nodes) - 1
	}
	left := 0
	for i := range out.Nodes {
		n := &out.Nodes[i]
		if n.State.Done() || n.State.Stops() || listed[n.ID] {
			if listed[n.ID] {
				left++
			}
			continue
		}
		if n.ID == verify {
			return agentdag.Run{}, fmt.Errorf("step %q checks the work of the loop; keep it", n.ID)
		}
		n.State = agentdag.StateSkipped
	}
	if left == 0 {
		return agentdag.Run{}, errors.New("the plan has no steps left; use Deny to stop the run")
	}
	var edges []agentdag.Edge
	for _, e := range out.Edges {
		if _, ok := deps[e.To]; !ok {
			edges = append(edges, e) // the edges into steps that were not listed stay
		}
	}
	for _, p := range doc.Nodes {
		id := strings.TrimSpace(p.ID)
		for _, d := range deps[id] {
			if _, ok := idx[d]; !ok {
				return agentdag.Run{}, fmt.Errorf("step %q: unknown dep %q", id, d)
			}
			edges = append(edges, agentdag.Edge{From: d, To: id})
		}
	}
	out.Edges = edges
	if err := agentdag.Validate(out); err != nil {
		return agentdag.Run{}, errors.New(strings.TrimPrefix(err.Error(), "agentdag: "))
	}
	n, _ := agentdag.Normalize(out)
	return n, nil
}

// inputDeps are the deps whose output a planned step gets: a planner's
// answer (the plan JSON) is never passed on.
func inputDeps(r agentdag.Run, deps []string) []string {
	out := []string{}
	for _, d := range deps {
		planner := false
		for _, n := range r.Nodes {
			if n.ID == d && n.Plan {
				planner = true
			}
		}
		if !planner {
			out = append(out, d)
		}
	}
	return out
}
