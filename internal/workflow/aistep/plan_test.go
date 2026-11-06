package aistep

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
)

var planItems = []Item{
	{Key: "A-1", Source: "linear", Text: `{"title":"crash on quit"}`},
	{Key: "A-2", Source: "linear", Text: `{"title":"typo in help"}`},
	{Key: "A-3", Source: "linear", Text: `{"title":"needs a design"}`},
}

const goodPlan = "Here is the plan.\n```json\n" +
	`{"order": ["A-2", "A-1"], "skip": {"A-3": "needs a person"}, "labels": {"A-1": ["bug", "p1"]}}` +
	"\n```"

func userText(req ai.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		if m.Role == ai.RoleUser {
			b.WriteString(m.Content)
		}
	}
	return b.String()
}

// Spec: the plan step sends the whole item list once (every item marked
// as data, no tools) and turns the JSON answer into order, skip and
// labels; items the answer leaves out of order come after, in list order.
func TestPlanStepReadsTheAnswer(t *testing.T) {
	h := &harness{prov: &fakeProv{replies: []ai.Chunk{{Delta: goodPlan}}}}
	p, err := RunPlan(context.Background(), PlanStep{ID: "order"}, planItems, "run1", h.env())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"A-2", "A-1", "A-3"}; !slices.Equal(p.Order, want) {
		t.Errorf("order %v, want %v", p.Order, want)
	}
	if p.Rank("A-2") != 1 || p.Rank("A-1") != 2 || p.Rank("A-3") != 3 || p.Rank("nope") != 0 {
		t.Errorf("ranks %d %d %d", p.Rank("A-2"), p.Rank("A-1"), p.Rank("A-3"))
	}
	if p.Skip["A-3"] != "needs a person" || len(p.Skip) != 1 {
		t.Errorf("skip %v", p.Skip)
	}
	if !slices.Equal(p.Labels["A-1"], []string{"bug", "p1"}) {
		t.Errorf("labels %v", p.Labels)
	}
	reqs := h.prov.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	if len(reqs[0].Tools) != 0 {
		t.Errorf("plan step offered tools %v", toolNames(reqs[0]))
	}
	if !strings.Contains(reqs[0].Messages[0].Content, DataSentence) {
		t.Error("system prompt lacks the data sentence")
	}
	u := userText(reqs[0])
	for _, it := range planItems {
		if !strings.Contains(u, Block(it)) {
			t.Errorf("prompt lacks the block of %s", it.Key)
		}
	}
	if !strings.Contains(u, `"order"`) {
		t.Error("prompt lacks the answer shape")
	}
}

// Spec: a bad answer is repaired once (like NodeRepair): the step says
// what is wrong and asks again, and a good second answer is used.
func TestPlanStepRepairsABadAnswerOnce(t *testing.T) {
	h := &harness{prov: &fakeProv{replies: []ai.Chunk{{Delta: "```json\n{\"order\": [\"A-9\"]}\n```"}, {Delta: goodPlan}}}}
	p, err := RunPlan(context.Background(), PlanStep{ID: "order"}, planItems, "run1", h.env())
	if err != nil {
		t.Fatal(err)
	}
	if p.Rank("A-2") != 1 {
		t.Errorf("order %v", p.Order)
	}
	reqs := h.prov.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests, want 2", len(reqs))
	}
	u := userText(reqs[1])
	if !strings.Contains(u, "not valid") || !strings.Contains(u, "A-9") {
		t.Errorf("repair turn does not say what is wrong:\n%s", u)
	}
	found := false
	for _, k := range h.kinds() {
		found = found || k == agentrun.NodeRepair
	}
	if !found {
		t.Errorf("no NodeRepair event: %v", h.kinds())
	}
}

// Spec: a second bad answer fails the step; there is no third try.
func TestPlanStepFailsAfterTheRepair(t *testing.T) {
	h := &harness{prov: &fakeProv{replies: []ai.Chunk{{Delta: "no json here"}}}}
	if _, err := RunPlan(context.Background(), PlanStep{ID: "order"}, planItems, "run1", h.env()); err == nil {
		t.Fatal("a bad answer twice did not fail")
	}
	if got := len(h.prov.requests()); got != 2 {
		t.Errorf("%d requests, want 2", got)
	}
}

// Spec: the answer check names what to fix.
func TestParsePlanAnswer(t *testing.T) {
	keys := []string{"A-1", "A-2"}
	cases := []struct {
		name, text, err string
	}{
		{"ok bare", `{"order": ["A-1"]}`, ""},
		{"no json", "I would start with A-1.", "no plan JSON"},
		{"bad json", "```json\n{\"order\": [\n```", "does not parse"},
		{"unknown field", `{"order": [], "rank": {}}`, "unknown field"},
		{"unknown key in order", `{"order": ["A-7"]}`, `"A-7"`},
		{"duplicate key", `{"order": ["A-1", "A-1"]}`, "twice"},
		{"unknown key in skip", `{"order": [], "skip": {"B": "x"}}`, `"B"`},
		{"empty reason", `{"order": [], "skip": {"A-1": " "}}`, "reason"},
		{"unknown key in labels", `{"order": [], "labels": {"B": ["x"]}}`, `"B"`},
		{"empty label", `{"order": [], "labels": {"A-1": [""]}}`, "label"},
	}
	for _, c := range cases {
		_, err := ParsePlanAnswer(c.text, keys)
		switch {
		case c.err == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("%s: err %v, want %q", c.name, err, c.err)
		}
	}
}
