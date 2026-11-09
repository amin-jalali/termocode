package app

import (
	"sort"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The GUI actions the other parity pieces own: databases (ai_db_parity.go),
// containers / servers / kube, and git / worktrees.
var (
	aiParityOtherPalette = []string{"git-", "db-", "redis-", "containers-", "kubernetes-", "worktree"}
	aiParityOtherOps     = []string{"db.", "redis.", "containers.", "kube.", "git.", "pr.", "ci.", "worktrees."}
	aiParityAgentPalette = []string{"acp-", "agent-", "ai-", "bugfix-", "canvas-", "codebase-", "itermoc-", "mcp-", "preview-",
		"prompt-actions", "secrets-", "spec-", "split-", "tracker-", "workflow"}
	aiParityAgentOps = []string{"ai.", "chat.", "runs.", "workflows.", "view.", "cards.", "itermoc.", "mcp.", "board."}
)

func aiHasPrefix(s string, pfx []string) bool {
	for _, p := range pfx {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// aiParityActions are the GUI actions of one area: its palette ids (all
// of them; those execute_action may run need no row) and its web ops.
func aiParityActions(agents bool) (palette, ops []string) {
	for _, d := range AIActionPolicyCatalog() {
		if aiHasPrefix(d.ID, aiParityOtherPalette) || aiHasPrefix(d.ID, aiParityAgentPalette) != agents {
			continue
		}
		palette = append(palette, d.ID)
	}
	for _, p := range webproto.Ops() {
		if aiHasPrefix(p.Op, aiParityOtherOps) || aiHasPrefix(p.Op, aiParityAgentOps) != agents {
			continue
		}
		ops = append(ops, p.Op)
	}
	sort.Strings(palette)
	sort.Strings(ops)
	return palette, ops
}

func aiParityCheck(t *testing.T, agents bool, parity, notForAI map[string]string, min int) {
	t.Helper()
	tools := map[string]bool{}
	for _, d := range AIToolCatalog() {
		tools[d.Name] = true
	}
	palette, ops := aiParityActions(agents)
	if len(palette)+len(ops) < min {
		t.Fatalf("only %d actions found; the lookup is broken", len(palette)+len(ops))
	}
	known := map[string]bool{}
	var missing []string
	check := func(id string, needRow bool) {
		known[id] = true
		tool, mapped := parity[id]
		why, skipped := notForAI[id]
		switch {
		case mapped && skipped:
			t.Errorf("%s is on both lists", id)
		case !mapped && !skipped:
			if needRow {
				missing = append(missing, id)
			}
		case skipped && strings.TrimSpace(why) == "":
			t.Errorf("%s is not for AI with no reason", id)
		case mapped:
			name, _, _ := strings.Cut(tool, " ")
			if !tools[name] {
				t.Errorf("%s maps to %q, which is not a chat tool", id, tool)
			}
			if name == "execute_action" && !strings.Contains(id, ".") { // a web op runs a palette id of its own
				if p, ok := aiActionPolicyFor(id); !ok || p.Gate == aiGateNever {
					t.Errorf("%s maps to execute_action, which never runs it", id)
				}
			}
			if name == "ide_op" && strings.Contains(id, ".") {
				if _, ok := aiIDEOps[id]; !ok {
					t.Errorf("%s maps to ide_op, which does not offer it", id)
				}
			}
		}
	}
	for _, id := range palette {
		p, _ := aiActionPolicyFor(id)
		check(id, p.Gate == aiGateNever) // auto / confirm: execute_action runs it
	}
	for _, op := range ops {
		check(op, true)
	}
	if len(missing) > 0 {
		t.Errorf("%d GUI actions map to no AI tool and are not on the not-for-AI list:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
	for _, m := range []map[string]string{parity, notForAI} {
		for id := range m {
			if !known[id] {
				t.Errorf("%s is listed but is no action of this area", id)
			}
		}
	}
}

// Guard (agents parity): every action of the agents, runs, workflows,
// races, todos, chats, views, specs and prompt actions GUI maps to an AI
// tool or sits on the commented not-for-AI list.
func TestAIAgentsEveryGUIActionHasAnAITool(t *testing.T) {
	aiParityCheck(t, true, aiAgentsParity, aiAgentsNotForAI, 150)
}

// Guard (the rest): every action of editor and files, explorer, terminal,
// services / tasks / tests, debug, LSP, HTTP, settings and web devices
// maps to an AI tool or sits on the commented not-for-AI list.
func TestAIRestEveryGUIActionHasAnAITool(t *testing.T) {
	aiParityCheck(t, false, aiRestParity, aiRestNotForAI, 300)
}

// Every web op the AI may run through ide_op is on the rest list (the
// ide_op table and the guard agree).
func TestAIIDEOpsAreInTheParityTable(t *testing.T) {
	for op := range aiIDEOps {
		if tool, ok := aiRestParity[op]; !ok || !strings.HasPrefix(tool, "ide_op") {
			t.Errorf("%s: ide_op offers it, the parity table says %q", op, tool)
		}
	}
}
