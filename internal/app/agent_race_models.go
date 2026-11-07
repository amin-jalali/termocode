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
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// ── Compare models (MOB-63, mobile plan 3.9) ────────────────────────────
//
// A race of one agent CLI on 2–4 different models: every lane runs the
// same agent in its own git worktree, started with --model <id>, and gets
// the same task; the compare page, the summary, verify, merge and discard
// work as for any race. Only agents whose CLI takes --model can compare
// (Claude Code, Codex); their built-in model lists are the picker's (any
// other safe id is fine too). The AI asks with agent_race_models (always
// a card, Deny first); the phone asks with ai.race_models (a laptop card).

// raceModelAgents are the agents whose CLI takes --model, in picker order,
// with the subscription CLI whose model list they use.
var raceModelAgents = []struct{ id, cli string }{
	{"claude", ai.CLIClaude},
	{"codex", ai.CLICodex},
}

func raceModelCLI(id string) (string, bool) {
	for _, a := range raceModelAgents {
		if a.id == id {
			return a.cli, true
		}
	}
	return "", false
}

// aiRaceModelsHook replaces the start in a test.
var aiRaceModelsHook func(m *Model, task, verify, agent string, models []string)

// raceModelBin is the command line typed into a lane's tab: the agent's
// program, with --model when the lane has one (ModelWord ids only, so
// nothing in it is read by the shell).
func raceModelBin(bin, model string) string {
	if model == "" || !agentrace.ModelWord(model) {
		return bin
	}
	return bin + " --model " + model
}

// aiRaceModelArgs reads and checks a compare: the same rules for the AI
// tool and the phone op, before any card.
func aiRaceModelArgs(a map[string]any) (task, verify string, ag agents.Agent, models []string, err error) {
	if err := aiArgsSecretError(a); err != nil {
		return "", "", agents.Agent{}, nil, err
	}
	task = strings.Join(strings.Fields(ai.ArgString(a, "task")), " ")
	verify = strings.TrimSpace(ai.ArgString(a, "verify"))
	switch {
	case task == "":
		return "", "", agents.Agent{}, nil, errors.New("task is empty: say what every lane should do")
	case len(task) > aiRaceTaskMax:
		return "", "", agents.Agent{}, nil, fmt.Errorf("task is longer than %d characters", aiRaceTaskMax)
	case len(verify) > aiRaceVerifyMax || strings.ContainsAny(verify, "\n\r"):
		return "", "", agents.Agent{}, nil, fmt.Errorf("verify must be one command line of at most %d characters", aiRaceVerifyMax)
	}
	id := strings.TrimSpace(ai.ArgString(a, "agent"))
	var ids []string
	for _, x := range raceModelAgents {
		ids = append(ids, x.id)
	}
	if _, ok := raceModelCLI(id); !ok {
		return "", "", agents.Agent{}, nil, fmt.Errorf("agent must be one of %s: only their CLI takes a model", strings.Join(ids, ", "))
	}
	ag, ok := agents.ByID(id)
	switch {
	case !ok:
		return "", "", agents.Agent{}, nil, fmt.Errorf("agent %s is not known", id)
	case !agents.HasScreenRules(ag.ID):
		return "", "", agents.Agent{}, nil, fmt.Errorf("%s cannot race: termocode cannot read its screen, so the task cannot be typed safely", ag.Title)
	}
	if _, err := agentLookPath(ag.Bin); err != nil {
		return "", "", agents.Agent{}, nil, fmt.Errorf("%s is not installed (%s is not on PATH); the user installs it first (Agent: Start…)", ag.Title, ag.Bin)
	}
	list, _ := a["models"].([]any)
	if len(list) < agentrace.MinLanes || len(list) > agentrace.MaxLanes {
		return "", "", agents.Agent{}, nil, fmt.Errorf("models must list %d to %d models", agentrace.MinLanes, agentrace.MaxLanes)
	}
	seen := map[string]bool{}
	for _, v := range list {
		s, _ := v.(string)
		s = strings.TrimSpace(s)
		switch {
		case !agentrace.ModelWord(s):
			return "", "", agents.Agent{}, nil, errors.New("a model id is letters, digits and . _ : / @ + - (no space, no leading dash)")
		case seen[s]:
			return "", "", agents.Agent{}, nil, fmt.Errorf("model %s is listed twice: each lane runs another model", s)
		}
		seen[s] = true
		models = append(models, s)
	}
	return task, verify, ag, models, nil
}

func checkRaceModels(a map[string]any) error {
	_, _, _, _, err := aiRaceModelArgs(a)
	return err
}

func describeRaceModels(a map[string]any) string {
	task, verify, ag, models, err := aiRaceModelArgs(a)
	if err != nil {
		return "Compare models in a race (invalid)"
	}
	out := fmt.Sprintf("Compare %d models of %s (%s) on the task %q? Each lane runs %s on its model in a terminal tab in its own git worktree "+
		"from your last commit; the task is typed into each once its input box shows. Each lane uses your %s plan or key. Your files change only when you merge a lane.",
		len(models), ag.Title, strings.Join(models, ", "), task, ag.Title, ag.Title)
	if verify != "" {
		out += " Verify command (runs only when you press v on the compare page): " + verify
	}
	return out
}

// startRaceModels starts the compare the card asked for.
func (m *Model) startRaceModels(task, verify, agent string, models []string) tea.Cmd {
	a, ok := agents.ByID(agent)
	if !ok {
		return m.aiInfo("Unknown agent " + agent)
	}
	var specs []agentrace.LaneSpec
	for _, md := range models {
		specs = append(specs, agentrace.LaneSpec{Agent: a.ID, Title: a.Title + " · " + md, Model: md})
	}
	s := m.raceS()
	return raceStartCmd(aiWorkspaceRoot(), s.dir != "", agentrace.NextNumber(s.races)-1, task, verify, specs, clock.Now().UnixMilli())
}

// raceModelChoices is the phone's picker: the agents that can compare,
// whether each is installed, and its built-in model list.
func raceModelChoices() []webproto.AIRaceModelAgent {
	out := []webproto.AIRaceModelAgent{}
	for _, x := range raceModelAgents {
		a, ok := agents.ByID(x.id)
		if !ok {
			continue
		}
		_, err := agentLookPath(a.Bin)
		c := webproto.AIRaceModelAgent{ID: a.ID, Title: a.Title, Installed: err == nil && agents.HasScreenRules(a.ID), Models: []webproto.AIRaceModelEntry{}}
		for _, md := range ai.CLIModels(x.cli) {
			if agentrace.ModelWord(md.ID) {
				c.Models = append(c.Models, webproto.AIRaceModelEntry{ID: md.ID, Title: md.Title})
			}
		}
		out = append(out, c)
	}
	return out
}

// aiRaceModelTools is agent_race_models.
func (m *Model) aiRaceModelTools(call aiIDECaller) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	var ids []string
	for _, x := range raceModelAgents {
		ids = append(ids, x.id)
	}
	return []ai.Tool{{
		Name: "agent_race_models",
		Description: "Compare 2-4 models on one task: a race where every lane runs the same outside coding agent (" + strings.Join(ids, ", ") +
			") on another model, each in its own git worktree and terminal tab; the user compares the lanes and merges the winner " +
			"(Agent: Race Compare). Claude Code takes opus, sonnet, haiku or a full model id; Codex takes its model ids. The user confirms first.",
		Params: map[string]any{
			"task":   str("what every lane should do (one paragraph)"),
			"agent":  map[string]any{"type": "string", "enum": ids, "description": "the agent every lane runs"},
			"models": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "2 to 4 different model ids, one lane each"},
			"verify": str("optional command run in each finished lane when the user asks, e.g. go test ./..."),
		},
		Required: []string{"task", "agent", "models"},
		// No Always allow class: every call asks.
		Mutating: true,
		Validate: checkRaceModels,
		Describe: describeRaceModels,
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			task, verify, ag, models, err := aiRaceModelArgs(a)
			if err != nil {
				return "", err
			}
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				if aiRaceModelsHook != nil {
					aiRaceModelsHook(m, task, verify, ag.ID, models)
					return "Race started.", nil, nil
				}
				return fmt.Sprintf("Race started with %d lanes (%s on %s). The user follows it on the compare page (Agent: Race Compare).",
						len(models), ag.Title, strings.Join(models, ", ")),
					m.startRaceModels(task, verify, ag.ID, models), nil
			})
		},
	}}
}

func init() {
	aiItermocWriteAllow["agent_race_models"] = "Start race"
}
