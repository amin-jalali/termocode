package ai

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func abilityTools() []Tool {
	mk := func(name string) Tool {
		return Tool{Name: name, Description: name + " does a thing. More.", Run: func(context.Context, map[string]any) (string, error) { return "ran " + name, nil }}
	}
	return []Tool{mk("read_file"), mk("db_query"), mk("db_tables"), mk("kube_pods"), mk("git_log")}
}

var abilityGroups = []ToolGroup{
	{Name: "db", About: "databases", Names: []string{"db_"}},
	{Name: "kube", About: "Kubernetes", Names: []string{"kube_"}},
	{Name: "git", About: "git", Names: []string{"git_"}},
}

func shownNames(tools []Tool) []string {
	var out []string
	for _, t := range shownTools(tools) {
		out = append(out, t.Name)
	}
	return out
}

func toolNamed(tools []Tool, name string) *Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

// Spec (owner 2026-11-15, abilities): each tool group has 3 states. on:
// its tools are in every request; on demand: deferred behind
// tool_search (the default); off: never sent, tool_search and a call by
// name say to ask the user, and ability_request asks (a mutating tool:
// the Deny-first card). Allowed, the group turns on and loads.
func TestAbilityStates(t *testing.T) {
	states := map[string]string{"db": AbilityOn, "kube": AbilityOff}
	var enabled []string
	pol := GroupPolicy{
		State: func(g string) string {
			if s := states[g]; s != "" {
				return s
			}
			return AbilityDemand
		},
		Enable: func(_ context.Context, g string) error {
			enabled = append(enabled, g)
			states[g] = AbilityDemand
			return nil
		},
	}
	loaded := &LoadedGroups{}
	tools := DeferToolGroupsWith(abilityTools(), []string{"read_file"}, abilityGroups, loaded, pol)
	shown := shownNames(tools)
	for _, want := range []string{"read_file", "db_query", "db_tables", ToolSearchTool, AbilityRequestTool} {
		if !slices.Contains(shown, want) {
			t.Errorf("%s not sent: %v", want, shown)
		}
	}
	for _, not := range []string{"kube_pods", "git_log"} {
		if slices.Contains(shown, not) {
			t.Errorf("%s sent: %v", not, shown)
		}
	}
	search := toolNamed(tools, ToolSearchTool)
	if !strings.Contains(search.Description, "Off (") || !strings.Contains(search.Description, "kube") || strings.Contains(search.Description, "- kube") {
		t.Errorf("tool_search does not list kube as off:\n%s", search.Description)
	}
	res, _ := search.Run(context.Background(), map[string]any{"query": "kube"})
	if !strings.Contains(res, "kube group is off") || loaded.Has("kube") {
		t.Errorf("tool_search loaded an off group: %q", res)
	}
	if _, err := toolNamed(tools, "kube_pods").Run(context.Background(), nil); err == nil || !strings.Contains(err.Error(), AbilityRequestTool) {
		t.Errorf("an off tool ran by name: %v", err)
	}

	req := toolNamed(tools, AbilityRequestTool)
	if !req.Mutating || req.Class != "" {
		t.Error("ability_request must ask every time (mutating, no class)")
	}
	if err := req.Validate(map[string]any{"group": "git", "why": "x"}); err == nil {
		t.Error("ability_request for a group that is not off")
	}
	if d := req.Describe(map[string]any{"group": "kube", "why": "read pod logs"}); !strings.Contains(d, "kube") || !strings.Contains(d, "read pod logs") {
		t.Errorf("card text %q", d)
	}
	out, err := req.Run(context.Background(), map[string]any{"group": "kube", "why": "logs"})
	if err != nil || !strings.Contains(out, "kube_pods") || strings.Join(enabled, ",") != "kube" {
		t.Fatalf("enable: %q %v %v", out, err, enabled)
	}
	if !slices.Contains(shownNames(tools), "kube_pods") {
		t.Error("the turned-on group is not sent from the next step")
	}
}

// Without a policy nothing changes: every group is on demand and there
// is no ability_request.
func TestAbilityNoPolicyIsOnDemand(t *testing.T) {
	tools := DeferToolGroups(abilityTools(), []string{"read_file"}, abilityGroups, &LoadedGroups{})
	shown := shownNames(tools)
	if strings.Join(shown, ",") != "read_file,"+ToolSearchTool {
		t.Fatalf("shown %v", shown)
	}
}
