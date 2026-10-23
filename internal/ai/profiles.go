package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/agentdag"
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
	Source    string  `json:"-"`
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

// SlotFor is the slot a step runs on: its own slot, else its category's
// slot in this profile, else quick → "quick" and everything else →
// "main".
func (p Profile) SlotFor(st ProfileStep) string {
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
// order.
func (p Profile) Run(id string) agentdag.Run {
	r := agentdag.Run{ID: id, Profile: p.Name}
	ids := make([]string, 0, len(p.Steps))
	graph := false
	for _, st := range p.Steps {
		title := st.Title
		if title == "" {
			title = st.ID
		}
		r.Nodes = append(r.Nodes, agentdag.Node{
			ID: st.ID, Title: title, Slot: p.SlotFor(st), Category: st.Category,
			System: st.System, Tools: st.Tools, Output: st.Output, Plan: st.Plan,
			Input: agentdag.InputRule{MaxSteps: st.MaxSteps},
		})
		ids = append(ids, st.ID)
		graph = graph || len(st.Deps) > 0
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

var profileIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ParseProfiles reads one profiles file ({"profiles": {name: profile}};
// other keys, "models" included, are ignored). file names the file in
// errors. Any error drops the whole file: the result is then nil.
func ParseProfiles(data []byte, file, source string, slots map[string]bool, parallelMax int) ([]Profile, error) {
	var doc struct {
		Profiles map[string]Profile `json:"profiles"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
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
		if err := validateProfile(&p, slots, parallelMax); err != nil {
			return nil, fmt.Errorf("%s line %d: profile %q: %s", file, keyLine(data, name), name, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// validateProfile checks p and fills empty step ids (step-1, step-2 …).
func validateProfile(p *Profile, slots map[string]bool, parallelMax int) error {
	switch {
	case p.Name == ProfileDefault:
		return errors.New(`"default" is built in (one step on ai_provider / ai_model); pick another name`)
	case !profileIDRe.MatchString(p.Name):
		return errors.New("a profile name may use letters, digits, '-', '_' and '.'")
	case len(p.Steps) == 0:
		return errors.New("has no steps")
	case len(p.Steps) > MaxProfileSteps:
		return fmt.Errorf("too many steps (%d, at most %d)", len(p.Steps), MaxProfileSteps)
	case p.BudgetUSD < 0:
		return errors.New("budget_usd is negative")
	}
	planners := 0
	for _, st := range p.Steps {
		if st.Plan {
			planners++
		}
	}
	if planners > 1 {
		return errors.New("at most one planner step (plan: true)")
	}
	for cat, slot := range p.Categories {
		if cat != agentdag.CategoryQuick && cat != agentdag.CategoryDeep {
			return fmt.Errorf("unknown category %q in categories (quick or deep)", cat)
		}
		if !slots[slot] {
			return fmt.Errorf("categories: unknown slot %q", slot)
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
			return fmt.Errorf("step id %q: use letters, digits, '-', '_' and '.'", st.ID)
		}
		if seen[st.ID] {
			return fmt.Errorf("duplicate step id %q", st.ID)
		}
		seen[st.ID] = true
		bad := func(format string, a ...any) error {
			return fmt.Errorf("step %q: %s", st.ID, fmt.Sprintf(format, a...))
		}
		if st.Slot != "" && !slots[st.Slot] {
			return bad("unknown slot %q", st.Slot)
		}
		switch st.Category {
		case "", agentdag.CategoryQuick, agentdag.CategoryDeep:
		default:
			return bad("unknown category %q (quick or deep)", st.Category)
		}
		switch st.Tools.Preset {
		case "", agentdag.PresetNone, agentdag.PresetReadOnly, agentdag.PresetAll:
		default:
			return bad("unknown tool preset %q (none, read-only or all)", st.Tools.Preset)
		}
		switch st.Output {
		case "", agentdag.OutputShown, agentdag.OutputFolded, agentdag.OutputHidden:
		default:
			return bad("unknown output %q (shown, folded or hidden)", st.Output)
		}
		if len(st.System) > MaxPromptBytes {
			return bad("system prompt is longer than 8 KB")
		}
		if st.MaxSteps < 0 {
			return bad("max_steps is negative")
		}
		if len(st.Deps) > 0 && parallelMax <= 1 {
			return bad("deps need ai_parallel_max 2 or more (steps run one after the other)")
		}
	}
	for _, st := range p.Steps {
		for _, d := range st.Deps {
			if !seen[d] {
				return fmt.Errorf("step %q: unknown dep %q", st.ID, d)
			}
		}
	}
	if err := agentdag.Validate(p.Run("check")); err != nil {
		return errors.New(strings.TrimPrefix(err.Error(), "agentdag: "))
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
	out := map[string]Config{}
	if main.Kind != KindNone {
		out[SlotMain] = main
		q := main
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
		switch {
		case name == SlotMain:
			fail(`"main" is built in (it is ai_provider / ai_model)`)
		case !profileIDRe.MatchString(name):
			fail("a slot name may use letters, digits, '-', '_' and '.'")
		case !knownKind(sl.Provider):
			fail(fmt.Sprintf("unknown provider %q (%s)", sl.Provider, strings.Join(Kinds, ", ")))
		case sl.Provider == KindCompatible && sl.BaseURL == "":
			fail("openai-compatible needs base_url")
		default:
			out[name] = slotConfig(sl, main, c, getenv)
		}
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
	cfg := Config{Kind: kind, Model: sl.Model, BaseURL: sl.BaseURL}
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

// SlotPool holds one provider per slot, built on first use. It is safe
// for concurrent use (pipeline steps ask for their provider from their
// own goroutines).
type SlotPool struct {
	mu    sync.Mutex
	cfgs  map[string]Config
	provs map[string]Provider
	build func(Config) Provider
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
	_, ok := p.cfgs[name]
	return ok
}

// Config returns the resolved config of slot name (no provider is
// built); ok=false for a name that is not a slot.
func (p *SlotPool) Config(name string) (Config, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cfg, ok := p.cfgs[name]
	return cfg, ok
}

// Provider returns the provider of slot name, building it the first
// time. A slot that is not configured (no key) is an error that says
// what is missing.
func (p *SlotPool) Provider(name string) (Provider, Config, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cfg, ok := p.cfgs[name]
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
