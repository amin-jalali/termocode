package webproto

import "strings"

// Compare models from the phone (MOB-63, mobile plan 2.4 and 3.9): a
// race of one agent CLI on 2–4 different models. Each lane works in its
// own git worktree and starts the agent with --model <id>, so it starts
// only after a laptop card (Cancel first), like ai.race_start (MOB-62).
// ai.race_model_list gives the picker the agents whose CLI takes a model
// and their built-in model lists; the laptop checks everything again.

// Compare models op names.
const (
	OpAIRaceModels    = "ai.race_models"
	OpAIRaceModelList = "ai.race_model_list"
)

// Compare models limits (the laptop's agent_race_models tool has the same).
const (
	MaxAIRaceModelsTask   = 2000
	MaxAIRaceModelsVerify = 300
	MaxAIRaceModelsAgent  = 32
	MaxAIRaceModelID      = 128
	MinAIRaceModels       = 2
	MaxAIRaceModels       = 4
)

// AIRaceModelsArgs asks the laptop to compare models: the task every lane
// gets, the agent every lane runs, one model per lane (all different)
// and an optional one-line verify command.
type AIRaceModelsArgs struct {
	Task   string   `json:"task"`
	Agent  string   `json:"agent"`
	Models []string `json:"models"`
	Verify string   `json:"verify,omitempty"`
}

func (a *AIRaceModelsArgs) validate(int) error {
	if strings.TrimSpace(a.Task) == "" || len(a.Task) > MaxAIRaceModelsTask {
		return invalid("bad task")
	}
	if len(a.Verify) > MaxAIRaceModelsVerify || hasControl(a.Verify) {
		return invalid("bad verify command")
	}
	if !raceModelsAgentID(a.Agent) {
		return invalid("bad agent id")
	}
	if len(a.Models) < MinAIRaceModels || len(a.Models) > MaxAIRaceModels {
		return invalid("compare %d to %d models", MinAIRaceModels, MaxAIRaceModels)
	}
	seen := map[string]bool{}
	for _, m := range a.Models {
		if !raceModelID(m) {
			return invalid("bad model id")
		}
		if seen[m] {
			return invalid("each model once")
		}
		seen[m] = true
	}
	return nil
}

// raceModelsAgentID: lower case letters, digits and dashes, not first a
// dash (an agent id, never an option or a path).
func raceModelsAgentID(id string) bool {
	if id == "" || len(id) > MaxAIRaceModelsAgent || id[0] == '-' {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// raceModelID: a model id that is safe after --model: first a letter or
// digit, then letters, digits and . _ : / @ + - (agentrace.ModelWord).
func raceModelID(s string) bool {
	if s == "" || len(s) > MaxAIRaceModelID {
		return false
	}
	for i, r := range s {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if i == 0 && !alnum {
			return false
		}
		if !alnum && !strings.ContainsRune("._:/@+-", r) {
			return false
		}
	}
	return true
}

// AIRaceModelList is the result of ai.race_model_list: the agents whose
// CLI takes a model, in picker order.
type AIRaceModelList struct {
	Agents []AIRaceModelAgent `json:"agents"`
}

// AIRaceModelAgent is one agent of the picker: Installed when its program
// is on the laptop's PATH; Models is its built-in list, best first (the
// phone may also type another id).
type AIRaceModelAgent struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Installed bool               `json:"installed,omitempty"`
	Models    []AIRaceModelEntry `json:"models"`
}

// AIRaceModelEntry is one model of an agent's list.
type AIRaceModelEntry struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

var opAIRaceModelRows = []Policy{
	{Op: OpAIRaceModels, New: newOf[AIRaceModelsArgs](), Role: RoleController, Rate: slow, MaxBytes: 4 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpAIRaceModelList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opAIRaceModelRows...); return true }()

func init() {
	registerResult(OpAIRaceModels, AskRef{})
	registerResult(OpAIRaceModelList, AIRaceModelList{})
}
