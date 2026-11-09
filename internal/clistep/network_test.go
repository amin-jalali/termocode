package clistep

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (owner, 2026-11-09): servers, containers and databases are reached
// only through termocode's tools. The CLI's own shell has no network in
// a termocode step: Codex read-only has none, workspace-write says so
// (network_access=false), and Claude Code gets no shell (Bash) and no web
// tool at all.
func TestArgsOwnShellHasNoNetwork(t *testing.T) {
	for _, write := range []bool{false, true} {
		codex := strings.Join(Args(Spec{CLI: ai.CLICodex, Dir: "/w", Write: write}), " ")
		if strings.Contains(codex, "danger-full-access") || strings.Contains(codex, "network_access=true") {
			t.Errorf("codex write=%v has network: %s", write, codex)
		}
		if write && !strings.Contains(codex, "sandbox_workspace_write.network_access=false") {
			t.Errorf("codex workspace-write does not turn network off: %s", codex)
		}
		claude := Args(Spec{CLI: ai.CLIClaude, Write: write})
		for i, a := range claude {
			if a == "--tools" {
				for _, tool := range strings.Split(claude[i+1], ",") {
					if tool == "Bash" || tool == "WebFetch" || tool == "WebSearch" {
						t.Errorf("claude write=%v has %s", write, tool)
					}
				}
			}
		}
	}
}
