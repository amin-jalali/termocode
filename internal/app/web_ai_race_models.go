package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Compare models from the phone (MOB-63, mobile plan 2.4 and 3.9): the
// laptop's agent_race_models behind a laptop card. The same checks run
// before the card (an agent whose CLI takes --model, installed, 2–4
// different safe model ids, one verify line), and Confirm starts the race
// as the tool does. The lanes then show in the races topic like any race
// (MOB-61), each lane's title naming its model.

func init() {
	webAskOps[webproto.OpAIRaceModels] = (*Model).webAIRaceModels
	webOps[webproto.OpAIRaceModelList] = (*Model).webAIRaceModelList
}

// webAIRaceModels checks the compare and asks the laptop to start it.
func (m *Model) webAIRaceModels(call *webCall) *webAsk {
	a := call.args.(*webproto.AIRaceModelsArgs)
	if !git.IsRepo(aiWorkspaceRoot()) {
		call.fail(webproto.CodeBadRequest, "Compare models needs a git repository: each lane works in its own git worktree.")
		return nil
	}
	list := make([]any, len(a.Models))
	for i, md := range a.Models {
		list[i] = md
	}
	args := map[string]any{"task": a.Task, "agent": a.Agent, "models": list, "verify": a.Verify}
	task, verify, ag, models, err := aiRaceModelArgs(args)
	if err != nil {
		call.fail(webproto.CodeBadRequest, aiOneLine(ai.Redact(err.Error())))
		return nil
	}
	return &webAsk{
		summary: fmt.Sprintf("compare %d models of %s: %s", len(models), ag.ID, strings.Join(models, ", ")),
		title:   "Compare models in a race?",
		body:    describeRaceModels(args),
		action:  "Start race",
		run:     func(m *Model) tea.Cmd { return m.startRaceModels(task, verify, ag.ID, models) },
	}
}

// webAIRaceModelList answers with the picker's agents and models.
func (m *Model) webAIRaceModelList(call *webCall) tea.Cmd {
	call.reply(webproto.AIRaceModelList{Agents: raceModelChoices()})
	return nil
}
