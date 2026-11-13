//go:build unix

package app

import (
	"strconv"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/perfbudget"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/profileform"
)

// Spec (AGX-1): Agent: Start… with 30 presets (half installed) and 8
// profiles: rows built, picker made and drawn at 150x42, median of 15,
// within agent_picker.
func TestPerfAgentPicker(t *testing.T) {
	perfbudget.Require(t)
	list := append([]agents.Agent(nil), agents.Registry...)
	for i := len(list); i < 30; i++ {
		id := "fake" + strconv.Itoa(i)
		list = append(list, agents.Agent{ID: id, Title: "Fake Agent " + strconv.Itoa(i), Bin: id})
	}
	var profs []agentPickProfile
	for i := 0; i < 8; i++ {
		profs = append(profs, agentPickProfile{name: "profile-" + strconv.Itoa(i), steps: i + 1})
	}
	in := agentPickInput{agents: list, profiles: profs,
		installed: func(a agents.Agent) bool { return len(a.ID)%2 == 0 },
		cli:       map[string]profileform.CLIState{ai.CLIClaude: profileform.CLIReady}}
	var ds []time.Duration
	for i := 0; i < 15; i++ {
		start := time.Now()
		p := picker.NewItems(" Start an agent ", agentPickerItems(in))
		p.SetSize(150, 42)
		_ = p.View()
		ds = append(ds, time.Since(start))
	}
	perfbudget.Check(t, "agent_picker", perfMs(perfMedian(ds)))
}
