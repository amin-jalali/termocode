package app

import (
	"sort"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// aiGitGUIActions are the git and worktree actions of the GUI: the
// palette ids git-* and worktree-*, and the web's git.*, pr.*, ci.*,
// worktrees.* and board.* ops.
func aiGitGUIActions() []string {
	var out []string
	for _, d := range AIActionPolicyCatalog() {
		if strings.HasPrefix(d.ID, "git-") || strings.HasPrefix(d.ID, "worktree-") {
			out = append(out, d.ID)
		}
	}
	for _, p := range webproto.Ops() {
		for _, pre := range []string{"git.", "pr.", "ci.", "worktrees.", "board."} {
			if strings.HasPrefix(p.Op, pre) {
				out = append(out, p.Op)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Guard (git parity): every git and worktree action of the GUI maps to an
// AI tool that does it (aiGitParity) or sits on the commented "not for
// AI" list (aiGitNotForAI). A new palette id or web op of these areas
// with neither makes this test red. The tools named must exist in the
// chat; an execute_action row must be one execute_action may run.
func TestAIGitEveryGUIActionHasAnAITool(t *testing.T) {
	tools := map[string]bool{}
	for _, d := range AIToolCatalog() {
		tools[d.Name] = true
	}
	actions := aiGitGUIActions()
	if len(actions) < 200 {
		t.Fatalf("only %d git and worktree actions found; the lookup is broken", len(actions))
	}
	known := map[string]bool{}
	var missing []string
	for _, id := range actions {
		known[id] = true
		tool, mapped := aiGitParity[id]
		why, skipped := aiGitNotForAI[id]
		switch {
		case mapped && skipped:
			t.Errorf("%s is on both lists", id)
		case !mapped && !skipped:
			missing = append(missing, id)
		case skipped && strings.TrimSpace(why) == "":
			t.Errorf("%s is not for AI with no reason", id)
		case mapped:
			for _, part := range strings.Split(tool, " + ") {
				name, _, _ := strings.Cut(part, " ")
				if !tools[name] {
					t.Errorf("%s maps to %q: %s is not a chat tool", id, tool, name)
				}
				if name == "execute_action" {
					if p, ok := aiActionPolicyFor(id); !ok || p.Gate == aiGateNever {
						t.Errorf("%s maps to execute_action, which never runs it", id)
					}
				}
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d git and worktree actions map to no AI tool and are not on the not-for-AI list:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
	for _, m := range []map[string]string{aiGitParity, aiGitNotForAI} {
		for id := range m {
			if !known[id] {
				t.Errorf("%s is listed but is no git or worktree action of the GUI", id)
			}
		}
	}
}
