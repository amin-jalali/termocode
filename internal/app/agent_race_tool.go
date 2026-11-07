package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
)

// ── agent_race_start (AGX-4, plan 2.8) ──────────────────────────────────
//
// The AI's way to start a race (the palette's Agent: Race… is a card only
// the user answers). It always asks (no Always allow class; Deny first),
// is mutating (a read-only step never gets it) and is not offered to
// outside agents. Its arguments are checked before the card.

const aiRaceTaskMax, aiRaceVerifyMax = 2000, 300

// aiRaceStartHook replaces the start in a test.
var aiRaceStartHook func(m *Model, task, verify string, lanes []string)

// aiRaceArgs reads and checks the tool's arguments.
func aiRaceArgs(a map[string]any) (task, verify string, lanes []agents.Agent, err error) {
	if err := aiArgsSecretError(a); err != nil {
		return "", "", nil, err
	}
	task = strings.Join(strings.Fields(ai.ArgString(a, "task")), " ")
	verify = strings.TrimSpace(ai.ArgString(a, "verify"))
	switch {
	case task == "":
		return "", "", nil, errors.New("task is empty: say what every agent should do")
	case len(task) > aiRaceTaskMax:
		return "", "", nil, fmt.Errorf("task is longer than %d characters", aiRaceTaskMax)
	case len(verify) > aiRaceVerifyMax || strings.ContainsAny(verify, "\n\r"):
		return "", "", nil, fmt.Errorf("verify must be one command line of at most %d characters", aiRaceVerifyMax)
	}
	list, _ := a["agents"].([]any)
	// CMP-18: a fan-out count ("claude:3,codex:2") instead of the list.
	if spec := strings.TrimSpace(ai.ArgString(a, "fanout")); spec != "" {
		if len(list) > 0 {
			return "", "", nil, errors.New("give agents or fanout, not both")
		}
		ids, err := agentrace.ParseFanout(spec)
		if err != nil {
			return "", "", nil, fmt.Errorf("fanout: %w", err)
		}
		for _, id := range ids {
			list = append(list, id)
		}
	}
	if len(list) < agentrace.MinLanes || len(list) > agentrace.MaxLanes {
		return "", "", nil, fmt.Errorf("agents (or fanout) must give %d to %d lanes", agentrace.MinLanes, agentrace.MaxLanes)
	}
	for _, v := range list {
		id, _ := v.(string)
		ag, ok := agents.ByID(strings.TrimSpace(id))
		switch {
		case !ok:
			return "", "", nil, fmt.Errorf("agent must be one of %s", strings.Join(aiAgentIDs(), ", "))
		case !agents.HasScreenRules(ag.ID):
			return "", "", nil, fmt.Errorf("%s cannot race: termocode cannot read its screen, so the task cannot be typed safely", ag.Title)
		}
		if _, err := agentLookPath(ag.Bin); err != nil {
			return "", "", nil, fmt.Errorf("%s is not installed (%s is not on PATH); the user installs it first (Agent: Start…)", ag.Title, ag.Bin)
		}
		lanes = append(lanes, ag)
	}
	return task, verify, lanes, nil
}

func checkRaceStart(a map[string]any) error {
	_, _, _, err := aiRaceArgs(a)
	return err
}

func describeRaceStart(a map[string]any) string {
	task, verify, lanes, err := aiRaceArgs(a)
	if err != nil {
		return "Start an agent race (invalid)"
	}
	var ids []string
	for _, l := range lanes {
		ids = append(ids, l.ID)
	}
	var names []string
	for _, c := range agentrace.Counts(ids) {
		ag, _ := agents.ByID(c.Agent)
		name := ag.Title
		if c.N > 1 {
			name += fmt.Sprintf(" ×%d", c.N)
		}
		names = append(names, name)
	}
	out := fmt.Sprintf("Start a race of %d lanes (%s) on the task %q? Each lane runs its agent in a terminal tab in its own git worktree "+
		"from your last commit; the task is typed into each once its input box shows. Your files change only when you merge a lane.",
		len(lanes), strings.Join(names, ", "), task)
	if verify != "" {
		out += " Verify command (runs only when you press v on the compare page): " + verify
	}
	return out
}

// aiRaceTools is agent_race_start.
func (m *Model) aiRaceTools(call aiIDECaller) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	var ids []string
	for _, a := range agents.Registry {
		if agents.HasScreenRules(a.ID) {
			ids = append(ids, a.ID)
		}
	}
	return []ai.Tool{{
		Name: "agent_race_start",
		Description: "Race one task across 2-8 lanes of outside coding agents (" + strings.Join(ids, ", ") + "): each works in its own git " +
			"worktree and terminal tab; the user compares the lanes and merges the winner (Agent: Race Compare). The user confirms first.",
		Params: map[string]any{
			"task":   str("what every agent should do (one paragraph)"),
			"agents": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": ids}, "description": "2 to 8 agents, one lane each (an agent may repeat); or use fanout"},
			"fanout": str("instead of agents: lanes per agent, e.g. claude:3,codex:2 (2 to 8 lanes in all)"),
			"verify": str("optional command run in each finished lane when the user asks, e.g. go test ./..."),
		},
		Required: []string{"task"},
		// No Always allow class: every call asks.
		Mutating: true,
		Validate: checkRaceStart,
		Describe: describeRaceStart,
		View:     raceStartView,
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			task, verify, lanes, err := aiRaceArgs(a)
			if err != nil {
				return "", err
			}
			var ids []string
			for _, l := range lanes {
				ids = append(ids, l.ID)
			}
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				if aiRaceStartHook != nil {
					aiRaceStartHook(m, task, verify, ids)
					return "Race started.", nil, nil
				}
				return fmt.Sprintf("Race started with %d lanes. The user follows it on the compare page (Agent: Race Compare).", len(ids)),
					m.startRace(task, verify, ids), nil
			})
		},
	}}
}

func init() {
	aiItermocWriteAllow["agent_race_start"] = "Start race"
}
