package webproto

// The ai data (WEBP-3): what the chat header's profile and model picker
// and the editor's AI need to know of the laptop's AI: is it set up, the
// provider's name (never a key or an endpoint), the model and the models
// the laptop offers for that provider, the active agent profile and the
// profiles (a project profile says whether the laptop trusts it), and
// the laptop's ghost text, next edit and agent worktree settings. The
// page may ask to use another profile or model; that runs only after a
// laptop card (ai.use_profile, ai.use_model), and a project profile the
// laptop has not trusted is never used from the web. The delta is the
// whole snapshot again.

func init() {
	registerTopic("ai", AISnap{}, AISnap{})
	registerResult(OpAIUseProfile, AskRef{})
	registerResult(OpAIUseModel, AskRef{})
}

// AI picker op names.
const (
	OpAIUseProfile = "ai.use_profile"
	OpAIUseModel   = "ai.use_model"
)

// Limits of the ai topic.
const (
	MaxAIProfiles = 64
	MaxAIModels   = 32
	MaxAIName     = 128
)

// AISnap is the ai topic.
type AISnap struct {
	Configured bool        `json:"configured"`
	Provider   string      `json:"provider,omitempty"` // "Anthropic", "OpenAI", "Ollama" …
	Model      string      `json:"model,omitempty"`
	Models     []string    `json:"models"` // the laptop's suggestions for the provider (the model first when it is not one)
	Profile    string      `json:"profile"`
	Profiles   []AIProfile `json:"profiles"`
	Inline     bool        `json:"inline"`    // ai_inline: ghost text while typing
	NextEdit   bool        `json:"nextEdit"`  // ai_next_edit
	Worktrees  bool        `json:"worktrees"` // ai_worktrees: parallel steps in their own worktree
}

// AIProfile is one agent profile: where it comes from and, for a
// project's, whether the laptop trusts it here ("use"), was never asked
// ("ask") or said no ("refused"). Hint: its steps and their models.
type AIProfile struct {
	Name   string `json:"name"`
	Source string `json:"source" enum:"builtin|user|project"`
	Trust  string `json:"trust" enum:"use|ask|refused"`
	Hint   string `json:"hint,omitempty"`
}

// AIUseArgs names a profile or a model the page asks to use.
type AIUseArgs struct {
	Name string `json:"name"`
}

func (a *AIUseArgs) validate(int) error {
	if !validName(a.Name, MaxAIName) {
		return invalid("bad name")
	}
	return nil
}

var opAIUseRows = []Policy{
	{Op: OpAIUseProfile, New: newOf[AIUseArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpAIUseModel, New: newOf[AIUseArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opAIUseRows...); return true }()
