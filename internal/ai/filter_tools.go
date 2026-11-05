package ai

import (
	"slices"

	"github.com/amin-jalali/termocode/internal/agentdag"
)

// FilterTools narrows tools by a step's rule. It only removes tools:
// none = no tools; read-only = tools that change nothing (no mutating,
// gated or reviewed tool); Allow keeps only the named ones, Deny drops
// the named ones.
func FilterTools(tools []Tool, r agentdag.ToolRule) []Tool {
	if r.Preset == "" || r.Preset == agentdag.PresetAll {
		if len(r.Allow) == 0 && len(r.Deny) == 0 {
			return tools
		}
	}
	if r.Preset == agentdag.PresetNone {
		return nil
	}
	var out []Tool
	for _, t := range tools {
		if r.Preset == agentdag.PresetReadOnly && (t.Mutating || t.Gate != nil || t.Class == ClassReview) {
			continue
		}
		if len(r.Allow) > 0 && !slices.Contains(r.Allow, t.Name) {
			continue
		}
		if slices.Contains(r.Deny, t.Name) {
			continue
		}
		out = append(out, t)
	}
	return out
}
