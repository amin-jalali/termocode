package agentdag

import (
	"reflect"
	"strings"
	"testing"
)

func nodes(ids ...string) []Node {
	out := make([]Node, len(ids))
	for i, id := range ids {
		out[i] = Node{ID: id}
	}
	return out
}

func TestNormalizeErrors(t *testing.T) {
	cases := []struct {
		name string
		run  Run
		want string // substring of the error
	}{
		{"no nodes", Run{}, "no nodes"},
		{"empty id", Run{Nodes: []Node{{ID: " "}}}, "empty id"},
		{"duplicate id", Run{Nodes: nodes("a", "a")}, "duplicate node id"},
		{"unknown state", Run{Nodes: []Node{{ID: "a", State: "dancing"}}}, "unknown state"},
		{"unknown category", Run{Nodes: []Node{{ID: "a", Category: "slow"}}}, "unknown category"},
		{"unknown output", Run{Nodes: []Node{{ID: "a", Output: "loud"}}}, "unknown output"},
		{"unknown preset", Run{Nodes: []Node{{ID: "a", Tools: ToolRule{Preset: "some"}}}}, "unknown tool preset"},
		{"negative steps", Run{Nodes: []Node{{ID: "a", Input: InputRule{MaxSteps: -1}}}}, "max steps"},
		{"dangling from", Run{Nodes: nodes("a"), Edges: []Edge{{From: "x", To: "a"}}}, "unknown node"},
		{"dangling to", Run{Nodes: nodes("a"), Edges: []Edge{{From: "a", To: "x"}}}, "unknown node"},
		{"self edge", Run{Nodes: nodes("a"), Edges: []Edge{{From: "a", To: "a"}}}, "self edge"},
		{"cycle", Run{Nodes: nodes("a", "b", "c"), Edges: []Edge{{"a", "b"}, {"b", "c"}, {"c", "a"}}}, "cycle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Normalize(c.run)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestNormalizeDefaultsAndCleans(t *testing.T) {
	in := Run{
		Nodes: []Node{
			{ID: " a ", Title: "Plan\x1b[31m red\x1b[0m‮", Error: strings.Repeat("x", 600)},
			{ID: "b", State: StateRunning, Output: "shown"},
		},
		Edges: []Edge{{" a", "b "}, {"a", "b"}}, // duplicate edge collapses
	}
	got, err := Normalize(in)
	if err != nil {
		t.Fatal(err)
	}
	a := got.Nodes[0]
	if a.ID != "a" || a.State != StatePending || a.Output != OutputFolded {
		t.Errorf("node a = %+v", a)
	}
	if a.Title != "Plan red" {
		t.Errorf("title = %q", a.Title)
	}
	if n := len([]rune(a.Error)); n > MaxErrorLen {
		t.Errorf("error len %d", n)
	}
	if got.Nodes[1].Output != OutputShown {
		t.Errorf("output = %q", got.Nodes[1].Output)
	}
	if !reflect.DeepEqual(got.Edges, []Edge{{"a", "b"}}) {
		t.Errorf("edges = %+v", got.Edges)
	}
	// The input is not changed.
	if in.Nodes[0].ID != " a " {
		t.Error("Normalize changed its input")
	}
}

func TestValidateLimits(t *testing.T) {
	big := Run{Nodes: nodes("1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13")}
	if err := Validate(big); err == nil || !strings.Contains(err.Error(), "too many nodes") {
		t.Errorf("13 nodes: %v", err)
	}
	ids := []string{"a", "b", "c", "d", "e", "f", "g"}
	deep := Run{Nodes: nodes(ids...), Edges: Chain(ids...)}
	if err := Validate(deep); err == nil || !strings.Contains(err.Error(), "too many layers") {
		t.Errorf("7 layers: %v", err)
	}
	ok := Run{Nodes: nodes(ids[:6]...), Edges: Chain(ids[:6]...)}
	if err := Validate(ok); err != nil {
		t.Errorf("6 layers: %v", err)
	}
}

func TestLayers(t *testing.T) {
	cases := []struct {
		name  string
		run   Run
		want  [][]string
		error string
	}{
		{"single", Run{Nodes: nodes("a")}, [][]string{{"a"}}, ""},
		{"chain", Run{Nodes: nodes("a", "b", "c"), Edges: Chain("a", "b", "c")}, [][]string{{"a"}, {"b"}, {"c"}}, ""},
		{"diamond", Run{Nodes: nodes("a", "b", "c", "d"),
			Edges: []Edge{{"a", "b"}, {"a", "c"}, {"b", "d"}, {"c", "d"}}},
			[][]string{{"a"}, {"b", "c"}, {"d"}}, ""},
		{"wide", Run{Nodes: nodes("e", "d", "c", "b", "a")}, [][]string{{"e", "d", "c", "b", "a"}}, ""},
		{"wide then join", Run{Nodes: nodes("a", "b", "c", "z"),
			Edges: []Edge{{"a", "z"}, {"b", "z"}, {"c", "z"}}},
			[][]string{{"a", "b", "c"}, {"z"}}, ""},
		{"uneven", Run{Nodes: nodes("a", "b", "c"), Edges: []Edge{{"a", "b"}, {"b", "c"}, {"a", "c"}}},
			[][]string{{"a"}, {"b"}, {"c"}}, ""},
		{"cycle", Run{Nodes: nodes("a", "b", "c"), Edges: []Edge{{"a", "b"}, {"b", "c"}, {"c", "b"}}}, nil, "cycle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Layers(c.run)
			if c.error != "" {
				if err == nil || !strings.Contains(err.Error(), c.error) {
					t.Fatalf("err = %v, want %q", err, c.error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("layers = %v, want %v", got, c.want)
			}
		})
	}
}

func TestCycleErrorNamesNodes(t *testing.T) {
	_, err := Layers(Run{Nodes: nodes("a", "b", "c"), Edges: []Edge{{"a", "b"}, {"b", "c"}, {"c", "b"}}})
	if err == nil || !strings.Contains(err.Error(), "b") || !strings.Contains(err.Error(), "c") || strings.Contains(err.Error(), "a,") {
		t.Errorf("err = %v", err)
	}
}

func diamond(states map[string]State) Run {
	r := Run{Nodes: nodes("a", "b", "c", "d"),
		Edges: []Edge{{"a", "b"}, {"a", "c"}, {"b", "d"}, {"c", "d"}}}
	for i := range r.Nodes {
		if s, ok := states[r.Nodes[i].ID]; ok {
			r.Nodes[i].State = s
		}
	}
	return r
}

func TestReady(t *testing.T) {
	cases := []struct {
		name   string
		states map[string]State
		want   []string
	}{
		{"start", nil, []string{"a"}},
		{"a running", map[string]State{"a": StateRunning}, nil},
		{"a done", map[string]State{"a": StateCompleted}, []string{"b", "c"}},
		{"b done only", map[string]State{"a": StateCompleted, "b": StateCompleted, "c": StateRunning}, nil},
		{"both done", map[string]State{"a": StateCompleted, "b": StateCompleted, "c": StateCompleted}, []string{"d"}},
		{"skipped counts as done", map[string]State{"a": StateCompleted, "b": StateSkipped, "c": StateCompleted}, []string{"d"}},
		{"b failed", map[string]State{"a": StateCompleted, "b": StateFailed, "c": StateCompleted}, nil},
		{"all done", map[string]State{"a": StateCompleted, "b": StateCompleted, "c": StateCompleted, "d": StateCompleted}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Ready(diamond(c.states))
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ready = %v, want %v", got, c.want)
			}
		})
	}
}

func TestBlockAfterFailure(t *testing.T) {
	r := Run{Nodes: nodes("a", "b", "c", "d", "e"),
		Edges: []Edge{{"a", "b"}, {"b", "c"}, {"a", "d"}}}
	r.Nodes[0].State = StateCompleted
	r.Nodes[1].State = StateFailed
	got, blocked := Block(r)
	if !reflect.DeepEqual(blocked, []string{"c"}) {
		t.Errorf("blocked = %v", blocked)
	}
	want := map[string]State{"a": StateCompleted, "b": StateFailed, "c": StateBlocked, "d": StatePending, "e": StatePending}
	for _, n := range got.Nodes {
		if n.State != want[n.ID] {
			t.Errorf("%s = %s, want %s", n.ID, n.State, want[n.ID])
		}
	}
	if !reflect.DeepEqual(Ready(got), []string{"d", "e"}) {
		t.Errorf("ready = %v", Ready(got))
	}
	// Transitive: a cancelled root blocks the whole chain.
	ch := Run{Nodes: nodes("x", "y", "z"), Edges: Chain("x", "y", "z")}
	ch.Nodes[0].State = StateCancelled
	got, blocked = Block(ch)
	if !reflect.DeepEqual(blocked, []string{"y", "z"}) || got.Nodes[2].State != StateBlocked {
		t.Errorf("chain blocked = %v %+v", blocked, got.Nodes)
	}
	if r.Nodes[2].State != "" {
		t.Error("Block changed its input")
	}
}

func TestChain(t *testing.T) {
	if got := Chain("a"); len(got) != 0 {
		t.Errorf("one id = %v", got)
	}
	if got := Chain("a", "b", "c"); !reflect.DeepEqual(got, []Edge{{"a", "b"}, {"b", "c"}}) {
		t.Errorf("chain = %v", got)
	}
}

func TestDeps(t *testing.T) {
	r := diamond(nil)
	if got := Deps(r, "d"); !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("deps d = %v", got)
	}
	if got := Deps(r, "a"); len(got) != 0 {
		t.Errorf("deps a = %v", got)
	}
}

func TestClean(t *testing.T) {
	cases := map[string]string{
		"plain":                "plain",
		"a\x1b[1mbold\x1b[0m":  "abold",
		"bell\x07 and nul\x00": "bell and nul",
		"line\nbreak\ttab":     "line break tab",
		"evil‮gnp.exe":         "evilgnp.exe",
		"iso⁦late⁩ mark‏":      "isolate mark",
		"c1\u009bx":            "c1x",
		"  spaces  ":           "spaces",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}
