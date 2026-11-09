package app

import (
	"context"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// AI parity (gates): a tool asks exactly where the GUI asks. Where the
// GUI asks every time (a web laptop card, or the view's own card), the
// AI's tool asks every time too, with no "Always allow" class:
//
//   - delete: the explorer's tree.delete asks every time.
//   - services_stop: services.stop and services.stop_all ask every time.
//   - workspace_folders add / remove: workspace.add_folder and
//     workspace.remove_folder ask every time.
//   - terminal_tabs close: term.close asks every time, idle or not.
//   - the palette's workflow-run, -dry-run, -stop and -retry open the
//     view's own card every time; workflow-pause has no card.

// aiEffGate is the confirm rule a call gets: the Gate, else Mutating and
// Class.
func aiEffGate(tl ai.Tool, a map[string]any) (bool, string) {
	if tl.Gate != nil {
		return tl.Gate(a)
	}
	return tl.Mutating, tl.Class
}

func TestAIGatesMatchTheGUICards(t *testing.T) {
	m := aiTestModel(t)
	idle := func(context.Context, aiIDEFunc) (string, error) { return "0", nil } // a tab with no command running
	tools := m.aiChatTools(t.TempDir(), idle)
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"delete", map[string]any{"path": "a.txt"}},
		{"services_stop", map[string]any{"name": "db"}},
		{"services_stop", map[string]any{"all": true}},
		{"workspace_folders", map[string]any{"op": "add", "path": "/tmp/x"}},
		{"workspace_folders", map[string]any{"op": "remove", "path": "/tmp/x"}},
		{"terminal_tabs", map[string]any{"op": "close", "tab": "1"}},
	} {
		tl, ok := ai.FindTool(tools, tc.tool)
		if !ok {
			t.Errorf("no tool %s", tc.tool)
			continue
		}
		if ask, class := aiEffGate(tl, tc.args); !ask || class != "" {
			t.Errorf("%s %v: ask=%v class=%q; the GUI asks every time, so no Always allow", tc.tool, tc.args, ask, class)
		}
	}
	for _, id := range []string{"workflow-run", "workflow-dry-run", "workflow-stop", "workflow-retry", "workflow-enable", "workflow-undo-write"} {
		if p, ok := aiActionPolicyFor(id); !ok || p.Gate != aiGateConfirm || p.Class != "" {
			t.Errorf("%s: %+v; its card asks every time", id, p)
		}
	}
	if p, _ := aiActionPolicyFor("workflow-pause"); p.Gate != aiGateAuto {
		t.Errorf("workflow-pause: %+v; a pause has no card", p)
	}
}
