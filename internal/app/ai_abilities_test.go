package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiAbilityRealFound uses the real "anything found?" check for one test.
func aiAbilityRealFound(t *testing.T) {
	t.Helper()
	aiAbilityFound = aiAbilityFoundReal
	t.Cleanup(func() { aiAbilityFound = func(string, string) bool { return true } })
	t.Setenv("KUBECONFIG", "")
}

// Spec (owner 2026-11-15, abilities): a group the user did not set is on
// demand; db, containers and kube are off while termocode finds nothing
// for them; a compose file, a saved connection or a kubeconfig turns
// them back to on demand; a database attachment counts as found; the
// user's setting always wins.
func TestAIAbilityDefaults(t *testing.T) {
	aiTestModel(t) // a fake HOME and config folder
	aiAbilityRealFound(t)
	root := t.TempDir()
	a := aiAbilityStates(nil, root, nil)
	for g, want := range map[string]string{"db": ai.AbilityOff, "containers": ai.AbilityOff, "kube": ai.AbilityOff, "git": ai.AbilityDemand, "files": ai.AbilityDemand} {
		if got := a.state(g); got != want {
			t.Errorf("empty project: %s is %s, want %s", g, got, want)
		}
	}
	if got := aiAbilityStates(nil, root, []string{"db"}).state("db"); got != ai.AbilityDemand {
		t.Errorf("a database attachment: db is %s", got)
	}
	if err := os.WriteFile(filepath.Join(root, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a = aiAbilityStates(nil, root, nil)
	if a.state("db") != ai.AbilityDemand || a.state("containers") != ai.AbilityDemand || a.state("kube") != ai.AbilityOff {
		t.Errorf("compose file: db %s containers %s kube %s", a.state("db"), a.state("containers"), a.state("kube"))
	}
	home, _ := os.UserHomeDir()
	if err := os.MkdirAll(filepath.Join(home, ".kube"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".kube", "config"), []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := aiAbilityStates(nil, root, nil).state("kube"); got != ai.AbilityDemand {
		t.Errorf("kubeconfig: kube is %s", got)
	}
	set := map[string]string{"kube": ai.AbilityOff, "git": ai.AbilityOn, "db": "nonsense"}
	a = aiAbilityStates(set, root, nil)
	if a.state("kube") != ai.AbilityOff || a.state("git") != ai.AbilityOn || a.state("db") != ai.AbilityDemand {
		t.Errorf("user setting: kube %s git %s db %s", a.state("kube"), a.state("git"), a.state("db"))
	}
}

// Spec (abilities): a chat turn follows the states: an off group is not
// sent and ability_request is; an "on" group is sent from the first
// request. Allowing ability_request saves the group as on demand.
func TestAIAbilityChatTurn(t *testing.T) {
	p := &recordingAIProvider{}
	m, _ := aiHeadTestModel(t, p)
	m.ai.conv.KeepGoingOff = true
	m.ai.settings.Abilities = map[string]string{"kube": ai.AbilityOff, "git": ai.AbilityOn}
	aiHeadSend(t, m, "hello")
	p.mu.Lock()
	names := aiReqToolNames(p.reqs[0])
	var search, req *ai.ToolSpec
	for i, tl := range p.reqs[0].Tools {
		switch tl.Name {
		case ai.ToolSearchTool:
			search = &p.reqs[0].Tools[i]
		case ai.AbilityRequestTool:
			req = &p.reqs[0].Tools[i]
		}
	}
	p.mu.Unlock()
	if slices.Contains(names, "kube_logs") || !slices.Contains(names, "git_log") || req == nil {
		t.Fatalf("tools: kube_logs %v git_log %v ability_request %v", slices.Contains(names, "kube_logs"), slices.Contains(names, "git_log"), req != nil)
	}
	if search == nil || !strings.Contains(search.Description, "Off (") {
		t.Fatal("tool_search does not name the off groups")
	}
	// Allowed on the card: the group is on demand in config.json.
	fake := []ai.Tool{{Name: "kube_logs", Run: func(context.Context, map[string]any) (string, error) { return "", nil }}}
	tools := m.ai.aiDeferTurnTools(fake, true, nil)
	for _, tl := range tools {
		if tl.Name == ai.AbilityRequestTool {
			if _, err := tl.Run(t.Context(), map[string]any{"group": "kube", "why": "pod logs"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	path, _ := ai.ConfigPath()
	if got := ai.LoadSettings(path).Abilities["kube"]; got != ai.AbilityDemand {
		t.Errorf("saved kube = %q, want demand", got)
	}
}

// Spec (abilities): AI: Abilities… lists every group with its state;
// Enter moves on → on demand → off → on and saves it.
func TestAIAbilitiesPicker(t *testing.T) {
	m := aiTestModel(t)
	m.openAIAbilitiesPicker()
	if !m.pickerOpen || m.pickerKind != pickerKindAIAbilities {
		t.Fatal("no picker")
	}
	rows := m.aiAbilityItems()
	if len(rows) != len(aiDeferGroups) || rows[0].Hint != "on demand" {
		t.Fatalf("rows %+v", rows)
	}
	path, _ := ai.ConfigPath()
	for _, want := range []string{ai.AbilityOff, ai.AbilityOn, ai.AbilityDemand} {
		m.onAIAbilityPicked("git")
		if got := ai.LoadSettings(path).Abilities["git"]; got != want {
			t.Fatalf("git saved %q, want %q", got, want)
		}
	}
}
