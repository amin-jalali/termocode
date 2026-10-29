package agentrun

import (
	"context"
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

func TestNodeWrites(t *testing.T) {
	for preset, want := range map[string]bool{"": true, agentdag.PresetAll: true, agentdag.PresetReadOnly: false, agentdag.PresetNone: false} {
		if got := agentdag.Writes(agentdag.Node{Tools: agentdag.ToolRule{Preset: preset}}); got != want {
			t.Errorf("preset %q: Writes %v", preset, got)
		}
	}
}
