package shotkit

import (
	"fmt"
	"time"
)

// agentPickerScene is Agent: Start… (AGX-1) with made-up programs on PATH
// and sign-in states (app agentPickerDemo; no CLI runs).
func agentPickerScene(name string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("agentpickerdemo"), Pause(600*time.Millisecond))}
}

// agentPickerCases are the agent picker at 150x42 and 80x24, and with
// ASCII icons.
func agentPickerCases() []goldenCase {
	var out []goldenCase
	sc := agentPickerScene("agent-picker")
	for _, sz := range [][2]int{{150, 42}, {80, 24}} {
		out = append(out, goldenCase{fmt.Sprintf("%s-%dx%d", sc.Name, sz[0], sz[1]), sc, sz[0], sz[1]})
	}
	ascii := agentPickerScene("agent-picker-ascii")
	ascii.Icons = "ascii"
	return append(out, goldenCase{"agent-picker-ascii-150x42", ascii, 150, 42})
}
