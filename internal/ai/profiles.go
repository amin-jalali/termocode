package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Agent profiles (U20 AP-2). A profile is a named pipeline of agent
// steps; each step runs on a model slot. Slots are named provider
// configs: "main" is always ai_provider / ai_model (today's chat),
// "quick" is the main provider with ai_inline_model, "local" is Ollama;
// config.json "ai_models" adds slots or replaces quick / local. Profiles
// come from the built-ins, ~/.config/termocode/ai/profiles.json and the
// project's .termocode/ai-profiles.json (later wins by name). No file
// holds a secret: a slot names a provider kind and the key comes from
// the credentials by kind.

// Built-in slot names.
const (
	SlotMain  = "main"
	SlotQuick = "quick"
	SlotLocal = "local"
)

// Built-in profile names.
const (
	ProfileDefault   = "default"
	ProfileReview    = "review"
	ProfileFast      = "fast"
	ProfileLocalOnly = "local-only"
	ProfilePlan      = "plan"
	ProfileUntilDone = "until-done"
)

// "Work until done" defaults (ai_until_done_rounds,
// ai_until_done_budget_usd).
const (
	DefaultUntilDoneRounds    = 3
	DefaultUntilDoneBudgetUSD = 2.0
)

// Where a profile comes from (Profile.Source).
const (
	ProfileSourceBuiltin = "builtin"
	ProfileSourceUser    = "user"
	ProfileSourceProject = "project"
)

// Profile limits.
const (
	MaxProfileSteps = agentdag.MaxNodes
	MaxPromptBytes  = 8 << 10
)

// ModelSlot is one entry of config.json "ai_models".
type ModelSlot struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	// Reasoning is the slot's reasoning level (reasoning.go; "": the
	// model's default). Edited by hand; SaveModelSlot keeps it.
	Reasoning string `json:"reasoning,omitempty"`
}

// ProfileStep is one agent step of a profile.
type ProfileStep struct {
	ID       string            `json:"id"`
	Title    string            `json:"title,omitempty"`
	Slot     string            `json:"slot,omitempty"`
	Category string            `json:"category,omitempty"`
	System   string            `json:"system,omitempty"`
	Tools    agentdag.ToolRule `json:"tools,omitempty"`
	Output   string            `json:"output,omitempty"`
	MaxSteps int               `json:"max_steps,omitempty"`
	// Deps turn the step list into a graph (needs ai_parallel_max > 1).
	// Without deps anywhere the steps run one after the other.
	Deps []string `json:"deps,omitempty"`
	// Plan makes the step a planner: it answers with a plan and the run
	// grows by the plan's nodes (at most one per profile).
	Plan bool `json:"plan,omitempty"`
	// CLI runs the step with an official CLI and the user's own
	// subscription login (CLIClaude, CLICodex) instead of a model slot
	// (CS-1). Such a step has no slot or category; it reads only unless
	// tools.preset is "all".
	CLI string `json:"cli,omitempty"`
	// Provider + Model run the step on a connected provider's model
	// instead of a named slot (the synthetic slot "<provider>/<model>").
	// With CLI, Model is the model the CLI is asked for ("" = its
	// default). Model is ignored on a slot or category step.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// The CLIs a step can run (ProfileStep.CLI).
const (
	CLIClaude = "claude"
	CLICodex  = "codex"
)

// CLIs lists the step CLIs in picker order.
var CLIs = []string{CLIClaude, CLICodex}

// CLITitle is the product name of a step CLI ("" for an unknown one).
func CLITitle(cli string) string {
	switch cli {
	case CLIClaude:
		return "Claude Code"
	case CLICodex:
		return "Codex"
	}
	return ""
}

// CLILabel is how the form and the cards name a CLI step's runner.
func CLILabel(cli string) string {
	if t := CLITitle(cli); t != "" {
		return t + " (subscription)"
	}
	return cli
}

// HasCLI reports whether a step of p (or its loop's fix step) runs a CLI.
func (p Profile) HasCLI() bool {
	for _, st := range p.Steps {
		if st.CLI != "" {
			return true
		}
	}
	return p.Loop != nil && p.Loop.Fix.CLI != ""
}

// StepTools is the tool preset step st really gets: its own, else "all"
// for a model step and "read-only" for a CLI step (a CLI step changes
// files only with preset "all").
func StepTools(st ProfileStep) string {
	switch {
	case st.Tools.Preset != "":
		return st.Tools.Preset
	case st.CLI != "":
		return agentdag.PresetReadOnly
	}
	return agentdag.PresetAll
}

// StepLabel is what step st runs on, for people: its CLI's label (and
// model), its model and provider, else its model slot.
func (p Profile) StepLabel(st ProfileStep) string {
	switch {
	case st.CLI != "" && st.Model != "":
		return CLILabel(st.CLI) + " · " + st.Model
	case st.CLI != "":
		return CLILabel(st.CLI)
	case st.Provider != "":
		return st.Model + " · " + ProviderName(st.Provider)
	}
	return p.SlotFor(st)
}

// ProviderName is the short name of a provider kind ("Anthropic").
func ProviderName(kind string) string {
	switch kind {
	case KindAnthropic:
		return "Anthropic"
	case KindClaudeOAuth:
		return "Claude"
	case KindOpenAI:
		return "OpenAI"
	case KindOpenRouter:
		return "OpenRouter"
	case KindOllama:
		return "Ollama"
	case KindLMStudio:
		return "LM Studio"
	case KindCompatible:
		return "OpenAI-compatible"
	}
	return kind
}

// modelIDRe is a model id a step may name: it goes into a CLI's argv,
// so it never starts with '-' and has no space.
var modelIDRe = lazyre.New(`^[A-Za-z0-9][A-Za-z0-9_.:/@+-]{0,127}$`)

// ValidModelID reports whether id is a model id a step may name.
func ValidModelID(id string) bool { return modelIDRe.MatchString(id) }

// SyntheticSlot is the slot name of a provider step: "<kind>/<model>".
// It never collides with a config.json slot (a slot name has no '/').
func SyntheticSlot(kind, model string) string { return kind + "/" + model }

// SplitSyntheticSlot splits a synthetic slot name into its provider kind
// and model; ok=false for any other name.
func SplitSyntheticSlot(name string) (kind, model string, ok bool) {
	kind, model, found := strings.Cut(name, "/")
	if !found || !knownKind(kind) || model == "" {
		return "", "", false
	}
	return kind, model, true
}

// Profile is a named pipeline.
type Profile struct {
	Name  string `json:"-"`
	Title string `json:"title,omitempty"`
	// Categories maps quick / deep to slot names for this profile.
	Categories map[string]string `json:"categories,omitempty"`
	Steps      []ProfileStep     `json:"steps"`
	// BudgetUSD (0: none) pauses a run of this profile before its next
	// step once the steps with a known price have cost this much.
	BudgetUSD float64 `json:"budget_usd,omitempty"`
	// BudgetSet: the profile file names budget_usd (0 then means no
	// budget); a loop profile without it takes ai_until_done_budget_usd.
	BudgetSet bool `json:"-"`
	// Loop makes the profile work until done (verify, then fix and
	// verify again while verify fails).
	Loop   *ProfileLoop `json:"loop,omitempty"`
	Source string       `json:"-"`
}

// ProfileLoop is a profile's verify/fix loop: Verify is a step id, Fix
// the step that runs (after Verify failed) before Verify runs again.
// MaxRounds 0 takes ai_until_done_rounds.
type ProfileLoop struct {
	Verify    string      `json:"verify"`
	Fix       ProfileStep `json:"fix"`
	MaxRounds int         `json:"max_rounds,omitempty"`
}

// SetLoopDefaults fills the loop profiles that leave them out with the
// settings: max_rounds (rounds) and budget_usd (budget; 0 = none).
func (s *ProfileSet) SetLoopDefaults(rounds int, budget float64) {
	for name, p := range s.byName {
		if p.Loop == nil {
			continue
		}
		if p.Loop.MaxRounds == 0 {
			l := *p.Loop
			l.MaxRounds = rounds
			p.Loop = &l
		}
		if !p.BudgetSet {
			p.BudgetUSD = budget
		}
		s.byName[name] = p
	}
}

// Planned reports whether p has a planner step.
func (p Profile) Planned() bool {
	for _, st := range p.Steps {
		if st.Plan {
			return true
		}
	}
	return false
}

// CategorySlots maps quick and deep to their slots in p (the slots of
// the steps a planner adds): the profile's categories, else quick →
// "quick" and deep → "main".
func (p Profile) CategorySlots() map[string]string {
	return map[string]string{
		agentdag.CategoryQuick: p.SlotFor(ProfileStep{Category: agentdag.CategoryQuick}),
		agentdag.CategoryDeep:  p.SlotFor(ProfileStep{Category: agentdag.CategoryDeep}),
	}
}

// SlotFor is the slot a step runs on: its provider's synthetic slot,
// else its own slot, else its category's slot in this profile, else
// quick → "quick" and everything else → "main".
func (p Profile) SlotFor(st ProfileStep) string {
	if st.Provider != "" && st.CLI == "" {
		return SyntheticSlot(st.Provider, st.Model)
	}
	if st.Slot != "" {
		return st.Slot
	}
	if s := p.Categories[st.Category]; s != "" {
		return s
	}
	if st.Category == agentdag.CategoryQuick {
		return SlotQuick
	}
	return SlotMain
}

// Run builds the agent run of p: one node per step (with its slot
// resolved), edges from the steps' deps or, without any, a chain in step
// order. A loop profile's run gets its Loop (max rounds 0 =
// DefaultUntilDoneRounds).
func (p Profile) Run(id string) agentdag.Run {
	r := agentdag.Run{ID: id, Profile: p.Name}
	ids := make([]string, 0, len(p.Steps))
	graph := false
	for _, st := range p.Steps {
		r.Nodes = append(r.Nodes, p.node(st))
		ids = append(ids, st.ID)
		graph = graph || len(st.Deps) > 0
	}
	if l := p.Loop; l != nil {
		rounds := l.MaxRounds
		if rounds == 0 {
			rounds = DefaultUntilDoneRounds
		}
		r.Loop = &agentdag.Loop{Verify: l.Verify, Fix: p.node(l.Fix), MaxRounds: rounds}
	}
	if !graph {
		r.Edges = agentdag.Chain(ids...)
		return r
	}
	for _, st := range p.Steps {
		for _, d := range st.Deps {
			r.Edges = append(r.Edges, agentdag.Edge{From: d, To: st.ID})
		}
	}
	return r
}

// node is the run node of step st.
func (p Profile) node(st ProfileStep) agentdag.Node {
	title := st.Title
	if title == "" {
		title = st.ID
	}
	slot, model := p.SlotFor(st), ""
	if st.CLI != "" {
		slot, model = "", st.Model // the CLI's own login, no model slot
	}
	return agentdag.Node{
		ID: st.ID, Title: title, Slot: slot, Category: st.Category, CLI: st.CLI, Model: model,
		System: st.System, Tools: st.Tools, Output: st.Output, Plan: st.Plan,
		Input: agentdag.InputRule{MaxSteps: st.MaxSteps},
	}
}

// UntilDoneVerifyTools are the tools of the until-done profile's verify
// step: it reads, checks and runs tests, and edits nothing.
var UntilDoneVerifyTools = []string{"read_file", "list_dir", "search", "git_status", "git_diff",
	"diagnostics", "run_command", "tests_run", "tests_results", "tasks_list", "tasks_run", "tasks_status"}

// BuiltinProfiles are the profiles that always exist, in picker order.
// default is one step on the main slot: exactly today's chat.
func BuiltinProfiles() []Profile {
	return []Profile{
		{Name: ProfileDefault, Title: "Default", Source: ProfileSourceBuiltin,
			Steps: []ProfileStep{{ID: "main", Title: "Assistant"}}},
		{Name: ProfileReview, Title: "Review then fix", Source: ProfileSourceBuiltin,
			Steps: []ProfileStep{
				{ID: "reviewer", Title: "Reviewer", Category: agentdag.CategoryDeep,
					System: "You review code. List concrete problems with file:line. Do not edit files.",
					Tools:  agentdag.ToolRule{Preset: agentdag.PresetReadOnly}},
				{ID: "fixer", Title: "Fixer", Category: agentdag.CategoryQuick,
					System: "Fix the problems the reviewer listed. Keep the changes small.",
					Tools:  agentdag.ToolRule{Preset: agentdag.PresetAll}, Output: agentdag.OutputShown},
			}},
		{Name: ProfileFast, Title: "Fast answers", Source: ProfileSourceBuiltin,
			Steps: []ProfileStep{{ID: "answer", Title: "Answer", Category: agentdag.CategoryQuick,
				Tools: agentdag.ToolRule{Preset: agentdag.PresetReadOnly}}}},
		{Name: ProfileLocalOnly, Title: "Local only", Source: ProfileSourceBuiltin,
			Steps: []ProfileStep{{ID: "answer", Title: "Answer", Slot: SlotLocal}}},
		{Name: ProfilePlan, Title: "Plan, then run in waves", Source: ProfileSourceBuiltin,
			Steps: []ProfileStep{{ID: "planner", Title: "Planner", Category: agentdag.CategoryDeep, Plan: true,
				Tools: agentdag.ToolRule{Preset: agentdag.PresetReadOnly}}}},
		{Name: ProfileUntilDone, Title: "Work until done", Source: ProfileSourceBuiltin, BudgetUSD: DefaultUntilDoneBudgetUSD,
			Steps: []ProfileStep{
				{ID: "planner", Title: "Planner", Category: agentdag.CategoryDeep, Plan: true,
					Tools: agentdag.ToolRule{Preset: agentdag.PresetReadOnly},
					System: `After your plan a verify step (id "verify") checks the work, and a fix step (id "fix") repairs what it finds. ` +
						`Do not plan a final check of your own and do not use the ids verify or fix.`},
				{ID: "verify", Title: "Verify", Category: agentdag.CategoryDeep,
					Tools:  agentdag.ToolRule{Allow: UntilDoneVerifyTools},
					System: "Run the project's tests and checks (tests_run, tasks_run or run_command) and read the changes. Do not edit files."},
			},
			Loop: &ProfileLoop{Verify: "verify", Fix: ProfileStep{ID: "fix", Title: "Fix", Category: agentdag.CategoryQuick,
				Tools:  agentdag.ToolRule{Preset: agentdag.PresetAll},
				System: "Fix exactly the problems in the verify report. Keep the changes small."}}},
	}
}

// ProfileSet is the merged profiles plus the load errors (one per broken
// file or slot).
type ProfileSet struct {
	byName map[string]Profile
	names  []string
	Errors []string
}

// Get returns the profile called name.
func (s ProfileSet) Get(name string) (Profile, bool) {
	p, ok := s.byName[name]
	return p, ok
}

// Names lists the profiles: the built-ins first, then the others by name.
func (s ProfileSet) Names() []string { return append([]string(nil), s.names...) }

// ProfilesPath is the user's ai/profiles.json.
func ProfilesPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ai", "profiles.json"), nil
}

// ProjectProfilesPath is a workspace's .termocode/ai-profiles.json.
func ProjectProfilesPath(root string) string {
	return filepath.Join(root, ".termocode", "ai-profiles.json")
}

// LoadProfiles merges the built-ins, the user file and the project file
// (later wins by name; "" or a missing file is skipped). slots are the
// known slot names; parallelMax is ai_parallel_max. A broken file never
// fails: its profiles are left out and Errors names the file and line.
func LoadProfiles(userPath, projectPath string, slots map[string]bool, parallelMax int) ProfileSet {
	set := ProfileSet{byName: map[string]Profile{}}
	for _, p := range BuiltinProfiles() {
		set.byName[p.Name] = p
		set.names = append(set.names, p.Name)
	}
	builtin := len(set.names)
	for _, f := range []struct{ path, source string }{
		{userPath, ProfileSourceUser}, {projectPath, ProfileSourceProject},
	} {
		if f.path == "" {
			continue
		}
		b, err := os.ReadFile(f.path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				set.Errors = append(set.Errors, f.path+": "+err.Error())
			}
			continue
		}
		ps, err := ParseProfiles(b, f.path, f.source, slots, parallelMax)
		if err != nil {
			set.Errors = append(set.Errors, err.Error())
			continue
		}
		for _, p := range ps {
			if _, ok := set.byName[p.Name]; !ok {
				set.names = append(set.names, p.Name)
			}
			set.byName[p.Name] = p
		}
	}
	sort.Strings(set.names[builtin:])
	return set
}

var profileIDRe = lazyre.New(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ParseProfiles reads one profiles file ({"profiles": {name: profile}};
// other keys, "models" included, are ignored). file names the file in
// errors. Any error drops the whole file: the result is then nil.
func ParseProfiles(data []byte, file, source string, slots map[string]bool, parallelMax int) ([]Profile, error) {
	var doc struct {
		Profiles map[string]Profile `json:"profiles"`
	}
	var keys struct { // which keys each profile names (budget_usd: 0 is a choice)
		Profiles map[string]map[string]json.RawMessage `json:"profiles"`
	}
	err := json.Unmarshal(data, &doc)
	if err == nil {
		_ = json.Unmarshal(data, &keys)
	}
	if err != nil {
		off := int64(len(data))
		var se *json.SyntaxError
		var te *json.UnmarshalTypeError
		switch {
		case errors.As(err, &se):
			off = se.Offset
		case errors.As(err, &te):
			off = te.Offset
		}
		return nil, fmt.Errorf("%s line %d: %s", file, lineAt(data, off), strings.TrimPrefix(err.Error(), "json: "))
	}
	names := make([]string, 0, len(doc.Profiles))
	for n := range doc.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Profile, 0, len(names))
	for _, name := range names {
		p := doc.Profiles[name]
		p.Name, p.Source = name, source
		_, p.BudgetSet = keys.Profiles[name]["budget_usd"]
		if err := validateProfile(&p, slots, parallelMax); err != nil {
			return nil, fmt.Errorf("%s line %d: profile %q: %s", file, keyLine(data, name), name, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// ProfileError is a profile check that failed: Msg is the whole text
// (what ParseProfiles shows), Step (a step id, "" for the profile) and
// Field (name, title, budget, steps, plan, categories, id, slot,
// category, cli, provider, model, tools, output, system, max_steps,
// deps) say where, so a form
// can show it next to the field.
type ProfileError struct {
	Step, Field, Msg string
}

func (e *ProfileError) Error() string { return e.Msg }

// ValidateProfile checks p with the rules of profiles.json (the same
// ones ParseProfiles uses) and returns a *ProfileError or nil. Empty
// step ids are not filled in p.
func ValidateProfile(p Profile, slots map[string]bool, parallelMax int) error {
	return validateProfile(&p, slots, parallelMax)
}

// checkStep checks the fields of one step (what names it in errors:
// "step" or "loop.fix").
func checkStep(st ProfileStep, what string, slots map[string]bool) error {
	bad := func(field, format string, a ...any) error {
		return &ProfileError{Step: st.ID, Field: field, Msg: fmt.Sprintf("%s %q: %s", what, st.ID, fmt.Sprintf(format, a...))}
	}
	switch {
	case st.CLI != "" && CLITitle(st.CLI) == "":
		return bad("cli", "unknown cli %q (claude or codex)", st.CLI)
	case st.CLI != "" && (st.Slot != "" || st.Category != ""):
		return bad("cli", "runs on its CLI; leave out slot and category")
	case st.CLI != "" && st.Plan:
		return bad("cli", "a CLI step cannot be the planner")
	case st.Provider != "" && st.CLI != "":
		return bad("provider", "names a provider and a cli; set one, not both")
	case st.Provider != "" && !knownKind(st.Provider):
		return bad("provider", "unknown provider %q (%s)", st.Provider, strings.Join(Kinds, ", "))
	case st.Provider != "" && (st.Slot != "" || st.Category != ""):
		return bad("provider", "runs on its provider's model; leave out slot and category")
	case st.Provider != "" && st.Model == "":
		return bad("model", "provider %q needs a model", st.Provider)
	case (st.Provider != "" || st.CLI != "") && st.Model != "" && !ValidModelID(st.Model):
		return bad("model", "model id %q: letters, digits and . _ : / @ + -, not starting with '-'", st.Model)
	}
	if st.Slot != "" && !slots[st.Slot] {
		return bad("slot", "unknown slot %q", st.Slot)
	}
	switch st.Category {
	case "", agentdag.CategoryQuick, agentdag.CategoryDeep:
	default:
		return bad("category", "unknown category %q (quick or deep)", st.Category)
	}
	switch st.Tools.Preset {
	case "", agentdag.PresetNone, agentdag.PresetReadOnly, agentdag.PresetAll:
	default:
		return bad("tools", "unknown tool preset %q (none, read-only or all)", st.Tools.Preset)
	}
	switch st.Output {
	case "", agentdag.OutputShown, agentdag.OutputFolded, agentdag.OutputHidden:
	default:
		return bad("output", "unknown output %q (shown, folded or hidden)", st.Output)
	}
	if len(st.System) > MaxPromptBytes {
		return bad("system", "system prompt is longer than 8 KB")
	}
	if st.MaxSteps < 0 {
		return bad("max_steps", "max_steps is negative")
	}
	return nil
}

// checkLoop checks p's loop (steps are the profile's step ids) and fills
// an empty fix id ("fix").
func checkLoop(p *Profile, steps map[string]bool, slots map[string]bool) error {
	l := p.Loop
	if l == nil {
		return nil
	}
	c := *l
	if c.Fix.ID == "" {
		c.Fix.ID = "fix"
	}
	p.Loop = &c
	switch {
	case !steps[c.Verify]:
		return fmt.Errorf("loop.verify: unknown step %q", c.Verify)
	case !profileIDRe.MatchString(c.Fix.ID):
		return fmt.Errorf("loop.fix id %q: use letters, digits, '-', '_' and '.'", c.Fix.ID)
	case steps[c.Fix.ID]:
		return fmt.Errorf("loop.fix id %q is a step id already; pick another", c.Fix.ID)
	case c.Fix.Plan:
		return errors.New("loop.fix may not be a planner (plan: true)")
	case len(c.Fix.Deps) > 0:
		return errors.New("loop.fix may not have deps (it runs before verify)")
	case c.MaxRounds != 0 && (c.MaxRounds < 1 || c.MaxRounds > agentdag.MaxRounds):
		return fmt.Errorf("loop.max_rounds is %d (1 to %d, or leave it out for ai_until_done_rounds)", c.MaxRounds, agentdag.MaxRounds)
	}
	for _, st := range p.Steps {
		if st.ID == c.Verify && st.Plan {
			return fmt.Errorf("loop.verify: step %q is a planner", c.Verify)
		}
	}
	return checkStep(c.Fix, "loop.fix", slots)
}

// validateProfile checks p and fills empty step ids (step-1, step-2 …).
func validateProfile(p *Profile, slots map[string]bool, parallelMax int) error {
	pe := func(field, format string, a ...any) error {
		return &ProfileError{Field: field, Msg: fmt.Sprintf(format, a...)}
	}
	switch {
	case p.Name == ProfileDefault:
		return pe("name", `"default" is built in (one step on ai_provider / ai_model); pick another name`)
	case !profileIDRe.MatchString(p.Name):
		return pe("name", "a profile name may use letters, digits, '-', '_' and '.'")
	case len(p.Steps) == 0:
		return pe("steps", "has no steps")
	case len(p.Steps) > MaxProfileSteps:
		return pe("steps", "too many steps (%d, at most %d)", len(p.Steps), MaxProfileSteps)
	case p.BudgetUSD < 0:
		return pe("budget", "budget_usd is negative")
	}
	planners := 0
	for _, st := range p.Steps {
		if st.Plan {
			planners++
		}
	}
	if planners > 1 {
		return pe("plan", "at most one planner step (plan: true)")
	}
	for cat, slot := range p.Categories {
		if cat != agentdag.CategoryQuick && cat != agentdag.CategoryDeep {
			return pe("categories", "unknown category %q in categories (quick or deep)", cat)
		}
		if !slots[slot] {
			return &ProfileError{Field: "categories:" + cat, Msg: fmt.Sprintf("categories: unknown slot %q", slot)}
		}
	}
	p.Steps = append([]ProfileStep(nil), p.Steps...)
	seen := map[string]bool{}
	for i := range p.Steps {
		st := &p.Steps[i]
		if st.ID == "" {
			st.ID = fmt.Sprintf("step-%d", i+1)
		}
		if !profileIDRe.MatchString(st.ID) {
			return &ProfileError{Step: st.ID, Field: "id", Msg: fmt.Sprintf("step id %q: use letters, digits, '-', '_' and '.'", st.ID)}
		}
		if seen[st.ID] {
			return &ProfileError{Step: st.ID, Field: "id", Msg: fmt.Sprintf("duplicate step id %q", st.ID)}
		}
		seen[st.ID] = true
		if err := checkStep(*st, "step", slots); err != nil {
			return err
		}
		if len(st.Deps) > 0 && parallelMax <= 1 {
			return &ProfileError{Step: st.ID, Field: "deps", Msg: fmt.Sprintf("step %q: deps need ai_parallel_max 2 or more (steps run one after the other)", st.ID)}
		}
	}
	for _, st := range p.Steps {
		for _, d := range st.Deps {
			if !seen[d] {
				return &ProfileError{Step: st.ID, Field: "deps", Msg: fmt.Sprintf("step %q: unknown dep %q", st.ID, d)}
			}
		}
	}
	if err := checkLoop(p, seen, slots); err != nil {
		var pe *ProfileError
		if errors.As(err, &pe) {
			return err
		}
		return &ProfileError{Field: "loop", Msg: err.Error()}
	}
	if err := agentdag.Validate(p.Run("check")); err != nil {
		return &ProfileError{Field: "steps", Msg: strings.TrimPrefix(err.Error(), "agentdag: ")}
	}
	return nil
}

// lineAt is the 1-based line of byte offset off in data.
func lineAt(data []byte, off int64) int {
	if off > int64(len(data)) {
		off = int64(len(data))
	}
	if off < 0 {
		off = 0
	}
	return 1 + strings.Count(string(data[:off]), "\n")
}

// keyLine is the line of the first `"key":` in data (1 when not found).
func keyLine(data []byte, key string) int {
	q, _ := json.Marshal(key)
	s := string(data)
	from := 0
	for {
		i := strings.Index(s[from:], string(q))
		if i < 0 {
			return 1
		}
		i += from
		rest := strings.TrimLeft(s[i+len(q):], " \t\r\n")
		if strings.HasPrefix(rest, ":") {
			return lineAt(data, int64(i))
		}
		from = i + len(q)
	}
}

// ── Model slots ───────────────────────────────────────────────────────

// ResolveSlots turns the main provider config and config.json ai_models
// into one provider Config per slot name. main is the resolved
// ai_provider (see Resolve); "main" and "quick" exist only when it is
// set. A slot's key comes from the vendor environment variable, then the
// stored key of its kind, then main's key when it is the same kind. Bad
// ai_models entries are left out and reported.
func ResolveSlots(s Settings, main Config, c Credentials, getenv func(string) string) (map[string]Config, []string) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if main.PromptCache == "" {
		main.PromptCache = promptCacheMode(s.PromptCache)
	}
	out := map[string]Config{}
	if main.Kind != KindNone {
		m := main
		m.Reasoning = NormalizeReasoning(s.Reasoning)
		out[SlotMain] = m
		q := main
		q.Reasoning = "" // quick: cheap and fast
		if q.InlineModel != "" {
			q.Model = q.InlineModel
		}
		out[SlotQuick] = q
	}
	out[SlotLocal] = slotConfig(ModelSlot{Provider: KindOllama}, main, c, getenv)
	names := make([]string, 0, len(s.Models))
	for n := range s.Models {
		names = append(names, n)
	}
	sort.Strings(names)
	var errs []string
	for _, name := range names {
		sl := s.Models[name]
		sl.Provider = strings.ToLower(strings.TrimSpace(sl.Provider))
		fail := func(msg string) { errs = append(errs, fmt.Sprintf("ai_models: slot %q: %s", name, msg)) }
		if err := CheckModelSlot(name, sl); err != nil {
			fail(err.Error())
			continue
		}
		out[name] = slotConfig(sl, main, c, getenv)
	}
	return out, errs
}

func knownKind(k string) bool {
	for _, x := range Kinds {
		if x == k {
			return true
		}
	}
	return false
}

func slotConfig(sl ModelSlot, main Config, c Credentials, getenv func(string) string) Config {
	kind := sl.Provider
	cfg := Config{Kind: kind, Model: sl.Model, BaseURL: sl.BaseURL, PromptCache: main.PromptCache, Reasoning: NormalizeReasoning(sl.Reasoning)}
	switch kind {
	case KindOllama, KindLMStudio:
	case KindClaudeOAuth:
		if main.Kind == KindClaudeOAuth {
			cfg.Token = main.Token
		}
	default:
		if e := envKeyFor(kind); e != "" {
			cfg.APIKey = getenv(e)
		}
		if cfg.APIKey == "" {
			cfg.APIKey = c.Keys[kind]
		}
		if cfg.APIKey == "" && kind == main.Kind {
			cfg.APIKey = main.APIKey
		}
	}
	if cfg.BaseURL == "" && kind == KindOllama && getenv("OLLAMA_HOST") != "" {
		cfg.BaseURL = ollamaHostURL(getenv("OLLAMA_HOST"))
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL(kind)
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel(kind)
	}
	cfg.InlineModel = cfg.Model
	RegisterSecret(cfg.APIKey)
	return cfg
}

// SyntheticResolver resolves a synthetic slot name ("<kind>/<model>",
// the slot of a provider step) the way ResolveSlots resolves a config
// slot: the kind's key from the environment, the store or main, and the
// base URL of the connected provider in provs (none: the kind's
// default). A provider that is not in provs still resolves: profiles
// travel between machines, and a key may come from the environment.
func SyntheticResolver(provs map[string]ProviderEntry, main Config, c Credentials, getenv func(string) string) func(string) (Config, bool) {
	if getenv == nil {
		getenv = os.Getenv
	}
	return func(name string) (Config, bool) {
		kind, model, ok := SplitSyntheticSlot(name)
		if !ok {
			return Config{}, false
		}
		return slotConfig(ModelSlot{Provider: kind, Model: model, BaseURL: provs[kind].BaseURL}, main, c, getenv), true
	}
}

// SlotPool holds one provider per slot, built on first use. It is safe
// for concurrent use (pipeline steps ask for their provider from their
// own goroutines).
type SlotPool struct {
	mu    sync.Mutex
	cfgs  map[string]Config
	provs map[string]Provider
	build func(Config) Provider
	// synth resolves a name that is not in cfgs (provider steps); nil:
	// none. synCfgs keeps what it resolved (Names leaves them out).
	synth   func(string) (Config, bool)
	synCfgs map[string]Config
}

// WithSynthetic makes the pool resolve names it does not hold with
// resolve (SyntheticResolver), on first use. It returns p.
func (p *SlotPool) WithSynthetic(resolve func(string) (Config, bool)) *SlotPool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.synth = resolve
	return p
}

// lookup is the config of name, resolving a synthetic name once. The
// caller holds p.mu.
func (p *SlotPool) lookup(name string) (Config, bool) {
	if cfg, ok := p.cfgs[name]; ok {
		return cfg, true
	}
	if p.synth == nil {
		return Config{}, false
	}
	if cfg, ok := p.synCfgs[name]; ok {
		return cfg, true
	}
	cfg, ok := p.synth(name)
	if ok {
		if p.synCfgs == nil {
			p.synCfgs = map[string]Config{}
		}
		p.synCfgs[name] = cfg
	}
	return cfg, ok
}

// NewSlotPool makes a pool over cfgs. build makes a provider (nil:
// NewProvider).
func NewSlotPool(cfgs map[string]Config, build func(Config) Provider) *SlotPool {
	if build == nil {
		build = NewProvider
	}
	return &SlotPool{cfgs: cfgs, provs: map[string]Provider{}, build: build}
}

// Names lists the slot names, sorted.
func (p *SlotPool) Names() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.cfgs))
	for n := range p.cfgs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Has reports whether name is a slot.
func (p *SlotPool) Has(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.lookup(name)
	return ok
}

// Config returns the resolved config of slot name (no provider is
// built); ok=false for a name that is not a slot.
func (p *SlotPool) Config(name string) (Config, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lookup(name)
}

// Provider returns the provider of slot name, building it the first
// time. A slot that is not configured (no key) is an error that says
// what is missing.
func (p *SlotPool) Provider(name string) (Provider, Config, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cfg, ok := p.lookup(name)
	if !ok {
		return nil, Config{}, fmt.Errorf("unknown model slot %q", name)
	}
	if !cfg.Configured() {
		what := cfg.Kind + " key"
		if cfg.Kind == KindClaudeOAuth {
			what = "Claude sign-in"
		}
		return nil, cfg, fmt.Errorf("model slot %q needs an %s (AI: Configure Provider…)", name, what)
	}
	if pr, ok := p.provs[name]; ok {
		return pr, cfg, nil
	}
	pr := p.build(cfg)
	p.provs[name] = pr
	return pr, cfg, nil
}
