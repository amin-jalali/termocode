package aistep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
)

// PlanStep is a workflow `plan` step (plan §3.3) after its templates
// ran:
//
//	{ "id": "order", "type": "plan", "profile": "quick",
//	  "prompt_file": "prompts/plan.md" }
type PlanStep struct {
	// ID is the step id (the run's one node).
	ID string
	// Model is the category (quick | deep) or model slot that answers
	// (the file's "profile"; "": quick).
	Model string
	// Prompt is the step's own prompt ("": DefaultPlanPrompt). The
	// answer shape (PlanFormat) always follows it.
	Prompt string
}

// DefaultPlanPrompt is the plan step's prompt when the file has none.
const DefaultPlanPrompt = "Read all the items below and plan the work on them: which come first, which to skip and why, and short labels where they help."

// PlanFormat tells the model the answer shape of a plan step.
const PlanFormat = "Answer with one fenced json block and nothing else that matters, in this exact shape:\n" +
	"```json\n" +
	`{"order": ["KEY-1", "KEY-2"], "skip": {"KEY-3": "why it is skipped"}, "labels": {"KEY-1": ["bug"]}}` +
	"\n```\n" +
	"Rules:\n" +
	"- Use only the keys from the item marks (key=…).\n" +
	"- order: the keys to work on, the most important first.\n" +
	"- skip: the keys not to work on, each with a short reason.\n" +
	"- labels: optional short labels per key.\n" +
	"- Only the keys order, skip and labels are allowed."

// Plan is a plan step's result: it sets item.plan.rank, item.plan.skip
// and item.plan.labels.
type Plan struct {
	// Order is every item key, the planned ones first in the answer's
	// order, then the rest in list order.
	Order []string
	// Skip maps a key to why it is skipped.
	Skip map[string]string
	// Labels maps a key to its labels.
	Labels map[string][]string
}

// Rank is key's place in Order (1: first; 0: not an item).
func (p Plan) Rank(key string) int {
	return slices.Index(p.Order, key) + 1
}

// Caps of one plan answer.
const (
	maxLabels   = 16
	maxLabelLen = 64
	maxReason   = 512
)

type planAnswer struct {
	Order  []string            `json:"order"`
	Skip   map[string]string   `json:"skip"`
	Labels map[string][]string `json:"labels"`
}

// ParsePlanAnswer reads a plan step's answer: the first fenced code
// block (or the whole answer when it is a bare JSON object). keys are
// the items' keys in list order. Errors are short and say what to fix:
// they go back to the model once.
func ParsePlanAnswer(text string, keys []string) (Plan, error) {
	body, ok := ai.FirstCodeBlock(text)
	if !ok {
		body = strings.TrimSpace(text)
		if !strings.HasPrefix(body, "{") {
			return Plan{}, errors.New("no plan JSON found: answer with one ```json block")
		}
	}
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var a planAnswer
	if err := dec.Decode(&a); err != nil {
		return Plan{}, fmt.Errorf("the plan JSON does not parse: %s", strings.TrimPrefix(err.Error(), "json: "))
	}
	known := func(k string) bool { return slices.Contains(keys, k) }
	p := Plan{Skip: map[string]string{}, Labels: map[string][]string{}}
	for _, k := range a.Order {
		switch {
		case !known(k):
			return Plan{}, fmt.Errorf("order names %q, which is not an item key", k)
		case slices.Contains(p.Order, k):
			return Plan{}, fmt.Errorf("order names %q twice", k)
		}
		p.Order = append(p.Order, k)
	}
	for _, k := range keys {
		if !slices.Contains(p.Order, k) {
			p.Order = append(p.Order, k)
		}
	}
	for k, why := range a.Skip {
		why = strings.TrimSpace(why)
		switch {
		case !known(k):
			return Plan{}, fmt.Errorf("skip names %q, which is not an item key", k)
		case why == "":
			return Plan{}, fmt.Errorf("skip %q has no reason", k)
		case utf8.RuneCountInString(why) > maxReason:
			return Plan{}, fmt.Errorf("the skip reason of %q is longer than %d characters", k, maxReason)
		}
		p.Skip[k] = why
	}
	for k, ls := range a.Labels {
		if !known(k) {
			return Plan{}, fmt.Errorf("labels names %q, which is not an item key", k)
		}
		if len(ls) > maxLabels {
			return Plan{}, fmt.Errorf("%q has more than %d labels", k, maxLabels)
		}
		for _, l := range ls {
			l = strings.TrimSpace(l)
			if l == "" || utf8.RuneCountInString(l) > maxLabelLen {
				return Plan{}, fmt.Errorf("a label of %q is empty or longer than %d characters", k, maxLabelLen)
			}
			p.Labels[k] = append(p.Labels[k], l)
		}
	}
	return p, nil
}

// RunPlan runs plan step st over the whole item list: one agentrun run
// of one node without tools reads every item (as marked data blocks)
// and answers with the plan. A bad answer is sent back once with what
// is wrong (a NodeRepair event); a second bad answer fails the step.
func RunPlan(ctx context.Context, st PlanStep, items []Item, runID string, env Env) (Plan, error) {
	if env.Base.Build == nil {
		return Plan{}, errors.New("aistep: Env.Base.Build is required")
	}
	model := st.Model
	if model == "" {
		model = agentdag.CategoryQuick
	}
	n := agentdag.Node{ID: st.ID, Title: st.ID, Tools: agentdag.ToolRule{Preset: agentdag.PresetNone}}
	if model == agentdag.CategoryQuick || model == agentdag.CategoryDeep {
		n.Category, n.Slot = model, env.Categories[model]
	} else {
		n.Slot = model
	}
	prompt := strings.TrimSpace(st.Prompt)
	if prompt == "" {
		prompt = DefaultPlanPrompt
	}
	keys := make([]string, 0, len(items))
	var b strings.Builder
	b.WriteString(prompt + "\n\n" + PlanFormat)
	for _, it := range items {
		keys = append(keys, it.Key)
		b.WriteString("\n\n" + Block(it))
	}
	user := b.String()

	o := env.Base
	o.Categories = env.Categories
	o.Approval = agentrun.ApprovalOff
	o.Budget = 0
	o.Build = guardBuild(o.Build, n.Tools)
	ask := func(user string) (string, error) {
		o.User = user
		h, err := agentrun.Start(ctx, agentdag.Run{ID: runID, Nodes: []agentdag.Node{n}}, o)
		if err != nil {
			return "", err
		}
		final := h.Wait()
		if final.Kind != agentrun.RunDone {
			return "", fmt.Errorf("plan step %q: %s", st.ID, final.Err)
		}
		return final.Output, nil
	}
	answer, err := ask(user)
	if err != nil {
		return Plan{}, err
	}
	p, perr := ParsePlanAnswer(answer, keys)
	if perr == nil {
		return p, nil
	}
	if o.Emit != nil {
		o.Emit(agentrun.Event{RunID: runID, NodeID: st.ID, Kind: agentrun.NodeRepair, Err: perr.Error()})
	}
	answer, err = ask(user + "\n\n### Your last answer\n" + strings.ReplaceAll(answer, "<<<", "<< <") +
		"\n\n" + agentrun.RepairPrompt(perr))
	if err != nil {
		return Plan{}, err
	}
	if p, perr = ParsePlanAnswer(answer, keys); perr != nil {
		return Plan{}, fmt.Errorf("plan step %q: the answer is not valid after one repair: %w", st.ID, perr)
	}
	return p, nil
}
