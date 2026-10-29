package agentrun

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// G-WT: a node's context says whether another node that may write files
// is in its wave (they may run at the same time), so the app can give it
// its own worktree. A read-only sibling does not count, and a run that
// runs one node at a time has no shared waves.
func TestSharedWave(t *testing.T) {
	for _, tc := range []struct {
		name     string
		parallel int
		readOnly string
		want     map[string]bool
	}{
		{"parallel", 2, "", map[string]bool{"a": false, "b": true, "c": true, "d": false}},
		{"serial", 1, "", map[string]bool{"a": false, "b": false, "c": false, "d": false}},
		{"read-only sibling", 2, "c", map[string]bool{"a": false, "b": false, "c": false, "d": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			got := map[string]bool{}
			h := newHarness(map[string]*fakeProv{"a": {}, "b": {}, "c": {}, "d": {}})
			o := h.options(tc.parallel)
			build := o.Build
			o.Build = func(ctx context.Context, n agentdag.Node, input string) (*ai.Agent, []ai.Message, error) {
				mu.Lock()
				got[n.ID] = SharedWave(ctx)
				mu.Unlock()
				return build(ctx, n, input)
			}
			r := diamond()
			for i := range r.Nodes {
				if r.Nodes[i].ID == tc.readOnly {
					r.Nodes[i].Tools.Preset = agentdag.PresetReadOnly
				}
			}
			run, err := Start(context.Background(), r, o)
			if err != nil {
				t.Fatal(err)
			}
			if end := wait(t, run); end.Kind != RunDone {
				t.Fatalf("end %+v", end)
			}
			for id, w := range tc.want {
				if got[id] != w {
					t.Errorf("%s: SharedWave %v, want %v", id, got[id], w)
				}
			}
		})
	}
	if SharedWave(context.Background()) {
		t.Error("outside a run: want false")
	}
}

// A writing node of a later layer can start while a slow writing node
// of an earlier layer still runs (its own deps ended early): both share a
// wave, else both would write in the user's tree at once.
func TestSharedWaveAcrossLayers(t *testing.T) {
	slow := &fakeProv{gate: make(chan struct{})}
	h := newHarness(map[string]*fakeProv{"a": slow, "r": {}, "b": {}})
	o := h.options(2)
	var mu sync.Mutex
	got := map[string]bool{}
	build := o.Build
	o.Build = func(ctx context.Context, n agentdag.Node, input string) (*ai.Agent, []ai.Message, error) {
		mu.Lock()
		got[n.ID] = SharedWave(ctx)
		mu.Unlock()
		if n.ID == "b" {
			close(slow.gate) // a still runs while b starts
		}
		return build(ctx, n, input)
	}
	// a (writes, slow) and r (read-only) are the first layer; b (writes)
	// needs only r.
	r := agentdag.Run{ID: "r3", Nodes: []agentdag.Node{
		{ID: "a"}, {ID: "r", Tools: agentdag.ToolRule{Preset: agentdag.PresetReadOnly}}, {ID: "b"}},
		Edges: []agentdag.Edge{{From: "r", To: "b"}}}
	run, err := Start(context.Background(), r, o)
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
	mu.Lock()
	defer mu.Unlock()
	if !got["a"] || !got["b"] {
		t.Errorf("SharedWave a=%v b=%v, want both true", got["a"], got["b"])
	}
	if got["r"] {
		t.Error("read-only r: SharedWave true")
	}
}

func TestNodeWrites(t *testing.T) {
	for preset, want := range map[string]bool{"": true, agentdag.PresetAll: true, agentdag.PresetReadOnly: false, agentdag.PresetNone: false} {
		if got := agentdag.Writes(agentdag.Node{Tools: agentdag.ToolRule{Preset: preset}}); got != want {
			t.Errorf("preset %q: Writes %v", preset, got)
		}
	}
}

// Finished runs in each node's goroutine once its agent ended (done,
// failed or stopped), before the run sees the node's end, so the app can
// look at the node's worktree first.
func TestFinishedRunsBeforeNodeEnd(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"a": {}, "b": {replies: []ai.Chunk{{Err: errors.New("boom")}}}})
	o := h.options(1)
	var mu sync.Mutex
	var order []string
	o.Finished = func(_ context.Context, n agentdag.Node) {
		mu.Lock()
		defer mu.Unlock()
		h.mu.Lock()
		ended := 0
		for _, e := range h.events {
			if e.NodeID == n.ID && (e.Kind == NodeDone || e.Kind == NodeFailed) {
				ended++
			}
		}
		h.mu.Unlock()
		if ended > 0 {
			t.Errorf("%s: Finished after the node's end event", n.ID)
		}
		order = append(order, n.ID)
	}
	r := agentdag.Run{ID: "r", Nodes: []agentdag.Node{{ID: "a"}, {ID: "b"}}, Edges: agentdag.Chain("a", "b")}
	run, err := Start(context.Background(), r, o)
	if err != nil {
		t.Fatal(err)
	}
	wait(t, run)
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Errorf("Finished order %v", order)
	}
}
