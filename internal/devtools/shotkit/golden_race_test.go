package shotkit

import (
	"fmt"
	"time"
)

// raceScene is the agent race card or compare page (AGX-4…6) with a
// made-up race (app raceDemo; no git, no tab, no CLI runs).
func raceScene(name, variant string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("racedemo:"+variant), Pause(600*time.Millisecond))}
}

// raceCases are the race card and the compare page running, done and
// after the merge (with the AI summary), at 150x42 and 80x24.
func raceCases() []goldenCase {
	var out []goldenCase
	for _, v := range []string{"card", "running", "done", "merged"} {
		sc := raceScene("race-"+v, v)
		for _, sz := range [][2]int{{150, 42}, {80, 24}} {
			out = append(out, goldenCase{fmt.Sprintf("%s-%dx%d", sc.Name, sz[0], sz[1]), sc, sz[0], sz[1]})
		}
	}
	return out
}
