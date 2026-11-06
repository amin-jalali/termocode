package webproto

import "slices"

// The Itermoc view of the web (WEBP-ITERMOC): the laptop's "Itermoc"
// sidebar view, read only. itermoc.state gives its profiles, model slots,
// AI providers, services and outside agents as the laptop's snapshot has
// them; that snapshot holds no secrets, and here a slot says only whether
// its key is there ("ok", "missing", "none": a local AI needs none,
// "unset": a server that may run without one). A change to the AI setup
// is made on the laptop: itermoc.ask asks the laptop to open one of its
// own setup forms, after a laptop card, and the user fills it there. A
// key is never typed, shown or sent on the web.

// Itermoc op names.
const (
	OpItermoc    = "itermoc.state"
	OpItermocAsk = "itermoc.ask"
)

// ItermocActions are the laptop setup forms itermoc.ask may open: the
// laptop palette ids of the same commands.
var ItermocActions = []string{"ai-add", "ai-model-slot-add", "ai-models-edit", "ai-profile-edit", "ai-profile-new", "ai-profile-form"}

// Limits of itermoc.state.
const (
	MaxItermocRows = 64
	MaxItermocText = 160
)

// ItermocAskArgs names the form to open.
type ItermocAskArgs struct {
	Action string `json:"action" enum:"ai-add|ai-model-slot-add|ai-models-edit|ai-profile-edit|ai-profile-new|ai-profile-form"`
}

func (a *ItermocAskArgs) validate(int) error {
	if !slices.Contains(ItermocActions, a.Action) {
		return invalid("bad action")
	}
	return nil
}

// Itermoc is the result of itermoc.state.
type Itermoc struct {
	Profiles  []ItermocProfile  `json:"profiles"`
	Slots     []ItermocSlot     `json:"slots"`
	Providers []ItermocProvider `json:"providers"`
	Services  []ItermocService  `json:"services"`
	// Listening: the laptop's server for outside agents runs; Agents are
	// the ones connected now.
	Listening bool           `json:"listening"`
	Agents    []ItermocAgent `json:"agents"`
	// Problems counts the profile files the laptop could not read.
	Problems int `json:"problems,omitempty"`
}

// ItermocProfile is one agent profile: its source and whether the
// laptop trusts it here (as AIProfile), its title, steps and model.
type ItermocProfile struct {
	Name   string `json:"name"`
	Title  string `json:"title,omitempty"`
	Source string `json:"source" enum:"builtin|user|project"`
	Trust  string `json:"trust" enum:"use|ask|refused"`
	Active bool   `json:"active,omitempty"`
	Steps  int    `json:"steps"`
	Model  string `json:"model,omitempty"`
}

// ItermocSlot is one model slot: names, and whether its key is there.
// User: an entry of the laptop's config.json (its own, not built in).
type ItermocSlot struct {
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	HasKey   string `json:"hasKey" enum:"ok|missing|none|unset"`
	User     bool   `json:"user,omitempty"`
}

// ItermocProvider is one connected AI: its name, how it is connected
// ("API key", "sign-in" …), what it costs, and how many models it has.
type ItermocProvider struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Label  string `json:"label,omitempty"`
	Cost   string `json:"cost,omitempty"`
	Models int    `json:"models"`
	// SignedOut: a sign-in AI whose tool is signed out.
	SignedOut bool `json:"signedOut,omitempty"`
}

// ItermocService is one MCP service and how many tools it gives.
type ItermocService struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Tools int    `json:"tools"`
}

// ItermocAgent is one connected outside agent.
type ItermocAgent struct {
	Name  string `json:"name"`
	Since string `json:"since"`
	Calls int    `json:"calls"`
}

var opItermocRows = []Policy{
	{Op: OpItermoc, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpItermocAsk, New: newOf[ItermocAskArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opItermocRows...); return true }()

func init() {
	registerResult(OpItermoc, Itermoc{})
	registerResult(OpItermocAsk, AskRef{})
}
