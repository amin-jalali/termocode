package shotkit

import (
	"fmt"
	"time"
)

// smallCases are the screens of SCN-8 at the smallest windows people
// really use: 60x20 for the start page, Source Control, the databases,
// the containers, the AI panel and the palette, and 80x24 for the two of
// them that had no frame there (Source Control, the AI panel).
func smallCases() []goldenCase {
	var out []goldenCase
	for _, sc := range []Scene{Dashboard(), SCM(), Databases(), Containers(), {Name: "ai-chat", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Act("aidemo"), Pause(600*time.Millisecond))}, Palette()} {
		out = append(out, goldenCase{fmt.Sprintf("%s-60x20", sc.Name), sc, 60, 20})
	}
	for _, sc := range []Scene{SCM(), {Name: "ai-chat", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Act("aidemo"), Pause(600*time.Millisecond))}} {
		out = append(out, goldenCase{fmt.Sprintf("%s-80x24", sc.Name), sc, 80, 24})
	}
	return out
}
