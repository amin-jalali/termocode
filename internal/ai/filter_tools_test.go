package ai

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
)

func filterNames(ts []Tool) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}

// Spec: a step's tool rule only removes tools (WFX-5 reuses it for
// workflow ai steps): none keeps nothing, read-only drops every tool
// that changes things, Allow keeps only the named ones, Deny drops the
// named ones, and an empty rule keeps all.
func TestFilterToolsOnlyRemoves(t *testing.T) {
	all := []Tool{
		{Name: "read_file"},
		{Name: "write_file", Mutating: true},
		{Name: "review_edit", Class: ClassReview},
		{Name: "execute_action", Gate: func(map[string]any) (bool, string) { return false, "" }},
		{Name: "git_push", Mutating: true},
	}
	cases := []struct {
		name string
		rule agentdag.ToolRule
		want []string
	}{
		{"empty keeps all", agentdag.ToolRule{}, []string{"read_file", "write_file", "review_edit", "execute_action", "git_push"}},
		{"none", agentdag.ToolRule{Preset: agentdag.PresetNone}, nil},
		{"read-only", agentdag.ToolRule{Preset: agentdag.PresetReadOnly}, []string{"read_file"}},
		{"deny", agentdag.ToolRule{Preset: agentdag.PresetAll, Deny: []string{"git_push"}}, []string{"read_file", "write_file", "review_edit", "execute_action"}},
		{"allow then deny", agentdag.ToolRule{Allow: []string{"read_file", "write_file"}, Deny: []string{"write_file"}}, []string{"read_file"}},
		{"allow never adds", agentdag.ToolRule{Allow: []string{"read_file", "no_such_tool"}}, []string{"read_file"}},
	}
	for _, c := range cases {
		got := filterNames(FilterTools(all, c.rule))
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}
