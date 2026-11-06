package shotkit

import (
	"fmt"
	"time"
)

// webScene is the web panel's start card or status chip with made-up
// state (app webDemo): nothing listens, no process starts.
func webScene(name, variant string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("webdemo:"+variant), Pause(400*time.Millisecond))}
}

// webCases are the start card (WEB-10) at 150x42 and 80x24, and the
// chip's two shown states at 150x42 (a narrow bar drops the chips), and
// the laptop card of a page's worktree archive.
func webCases() []goldenCase {
	card := webScene("web-start-card", "card")
	out := []goldenCase{{card.Name + "-150x42", card, 150, 42}, {card.Name + "-80x24", card, 80, 24}}
	for _, sc := range []Scene{webScene("web-chip-on", "on"), webScene("web-chip-connected", "connected")} {
		out = append(out, goldenCase{fmt.Sprintf("%s-150x42", sc.Name), sc, 150, 42})
	}
	ask := webScene("web-worktrees-ask", "worktrees") // a page asks to archive a worktree (WTX-16)
	return append(out, goldenCase{ask.Name + "-150x42", ask, 150, 42}, goldenCase{ask.Name + "-80x24", ask, 80, 24})
}
