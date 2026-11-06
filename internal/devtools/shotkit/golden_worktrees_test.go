package shotkit

import (
	"fmt"
	"time"
)

// worktreesScene is the WORKTREES view with a canned repository (app
// worktreesDemo): the cursor on a working, a waiting, an asleep and an
// external worktree, the ? card, the filter line (WTX-4) and Worktree:
// Jump… (WTX-3).
func worktreesScene(name, variant string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("worktreesdemo:"+variant), Pause(400*time.Millisecond))}
}

// worktreesCases are the WORKTREES view (WTX-2) at 150x42 and 80x24 (the
// compact one-line list), and the list with ASCII icons.
func worktreesCases() []goldenCase {
	var out []goldenCase
	for _, v := range []string{"running", "waiting", "asleep", "external", "keys", "filter", "jump", "new", "new-error", "setup", "setup-running"} {
		sc := worktreesScene("worktrees-"+v, v)
		for _, sz := range [][2]int{{150, 42}, {80, 24}} {
			out = append(out, goldenCase{fmt.Sprintf("%s-%dx%d", sc.Name, sz[0], sz[1]), sc, sz[0], sz[1]})
		}
	}
	ascii := worktreesScene("worktrees-ascii", "running")
	ascii.Icons = "ascii"
	return append(out, goldenCase{"worktrees-ascii-150x42", ascii, 150, 42}, goldenCase{"worktrees-ascii-80x24", ascii, 80, 24})
}
