package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
)

var testSlots = map[string]bool{SlotMain: true, SlotQuick: true, SlotLocal: true, "gpt": true}

func TestBuiltinProfiles(t *testing.T) {
	want := []string{ProfileDefault, ProfileReview, ProfileFast, ProfileLocalOnly, ProfilePlan}
	got := BuiltinProfiles()
	if len(got) != len(want) {
		t.Fatalf("builtins: %d", len(got))
	}
	for i, p := range got {
		if p.Name != want[i] || p.Source != ProfileSourceBuiltin {
			t.Errorf("builtin %d = %q (%s)", i, p.Name, p.Source)
		}
		if err := agentdag.Validate(p.Run("r")); err != nil {
			t.Errorf("%s: %v", p.Name, err)
		}
	}
	def := got[0]
	if len(def.Steps) != 1 || def.SlotFor(def.Steps[0]) != SlotMain {
		t.Fatalf("default must be one step on the main slot: %+v", def)
	}
	rev := got[1]
	r := rev.Run("r1")
	if len(r.Nodes) != 2 || len(r.Edges) != 1 || r.Edges[0] != (agentdag.Edge{From: "reviewer", To: "fixer"}) {
		t.Fatalf("review run: %+v", r)
	}
	if r.Nodes[0].Slot != SlotMain || r.Nodes[1].Slot != SlotQuick {
		t.Errorf("review slots: deep→%q quick→%q", r.Nodes[0].Slot, r.Nodes[1].Slot)
	}
	if r.Nodes[0].Tools.Preset != agentdag.PresetReadOnly || r.Profile != ProfileReview || r.ID != "r1" {
		t.Errorf("review node 0: %+v", r)
	}
	if got[3].SlotFor(got[3].Steps[0]) != SlotLocal {
		t.Errorf("local-only must use the local slot")
	}
}

func TestParseProfilesValid(t *testing.T) {
	data := `{
  "profiles": {
    "mine": {
      "title": "Mine",
      "categories": {"quick": "gpt"},
      "steps": [
        {"id": "a", "category": "quick", "system": "be short"},
        {"id": "b", "slot": "local", "tools": {"preset": "none"}, "output": "shown"}
      ],
      "future_key": 1
    }
  }
}`
	ps, err := ParseProfiles([]byte(data), "profiles.json", ProfileSourceUser, testSlots, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Name != "mine" || ps[0].Source != ProfileSourceUser {
		t.Fatalf("got %+v", ps)
	}
	r := ps[0].Run("x")
	if r.Nodes[0].Slot != "gpt" || r.Nodes[1].Slot != SlotLocal || r.Nodes[0].System != "be short" {
		t.Errorf("slots: %+v", r.Nodes)
	}
	if len(r.Edges) != 1 {
		t.Errorf("a list of steps is a chain: %+v", r.Edges)
	}
}

func TestParseProfilesErrors(t *testing.T) {
	long := strings.Repeat("x", MaxPromptBytes+1)
	var many []string
	for i := 0; i < MaxProfileSteps+1; i++ {
		many = append(many, `{"id":"s`+string(rune('a'+i))+`"}`)
	}
	cases := []struct {
		name, data, want string
	}{
		{"broken json", "{\n  \"profiles\": {\n    \"x\": { \"steps\": [ }\n  }\n}", "line 3"},
		{"wrong type", "{\n\"profiles\": {\n\"x\": {\"steps\": \"nope\"}}}", "line 3"},
		{"duplicate ids", "{\"profiles\": {\n\"x\": {\"steps\": [{\"id\":\"a\"},{\"id\":\"a\"}]}}}", `duplicate step id "a"`},
		{"unknown slot", "{\"profiles\": {\n\n\"x\": {\"steps\": [{\"id\":\"a\",\"slot\":\"nope\"}]}}}", `line 3: profile "x": step "a": unknown slot "nope"`},
		{"unknown category slot", `{"profiles": {"x": {"categories": {"quick": "nope"}, "steps": [{"id":"a"}]}}}`, `unknown slot "nope"`},
		{"unknown category key", `{"profiles": {"x": {"categories": {"medium": "gpt"}, "steps": [{"id":"a"}]}}}`, `unknown category "medium"`},
		{"unknown category", `{"profiles": {"x": {"steps": [{"id":"a","category":"slow"}]}}}`, `unknown category "slow"`},
		{"unknown preset", `{"profiles": {"x": {"steps": [{"id":"a","tools":{"preset":"some"}}]}}}`, `unknown tool preset "some"`},
		{"unknown output", `{"profiles": {"x": {"steps": [{"id":"a","output":"loud"}]}}}`, `unknown output "loud"`},
		{"no steps", `{"profiles": {"x": {"steps": []}}}`, "no steps"},
		{"too many steps", `{"profiles": {"x": {"steps": [` + strings.Join(many, ",") + `]}}}`, "too many steps"},
		{"long prompt", `{"profiles": {"x": {"steps": [{"id":"a","system":"` + long + `"}]}}}`, "system prompt is longer than 8 KB"},
		{"default reserved", `{"profiles": {"default": {"steps": [{"id":"a"}]}}}`, `"default" is built in`},
		{"deps need parallel", `{"profiles": {"x": {"steps": [{"id":"a"},{"id":"b","deps":["a"]}]}}}`, "deps need ai_parallel_max"},
		{"bad id", `{"profiles": {"x": {"steps": [{"id":"a b"}]}}}`, `step id "a b"`},
	}
	for _, c := range cases {
		ps, err := ParseProfiles([]byte(c.data), "profiles.json", ProfileSourceUser, testSlots, 1)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
		if ps != nil {
			t.Errorf("%s: a broken file must give no profiles, got %d", c.name, len(ps))
		}
		if err != nil && !strings.HasPrefix(err.Error(), "profiles.json") {
			t.Errorf("%s: error must name the file: %v", c.name, err)
		}
	}
}

func TestParseProfilesDepsWithParallel(t *testing.T) {
	diamond := `{"profiles": {"d": {"steps": [{"id":"a"},{"id":"b","deps":["a"]},{"id":"c","deps":["a"]},{"id":"d","deps":["b","c"]}]}}}`
	ps, err := ParseProfiles([]byte(diamond), "p.json", ProfileSourceUser, testSlots, 2)
	if err != nil {
		t.Fatal(err)
	}
	ls, err := agentdag.Layers(ps[0].Run("r"))
	if err != nil || len(ls) != 3 || len(ls[1]) != 2 {
		t.Fatalf("diamond layers %v %v", ls, err)
	}
	cycle := `{"profiles": {"c": {"steps": [{"id":"a","deps":["b"]},{"id":"b","deps":["a"]}]}}}`
	if _, err := ParseProfiles([]byte(cycle), "p.json", ProfileSourceUser, testSlots, 2); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("cycle: %v", err)
	}
	dangling := `{"profiles": {"c": {"steps": [{"id":"a","deps":["zz"]}]}}}`
	if _, err := ParseProfiles([]byte(dangling), "p.json", ProfileSourceUser, testSlots, 2); err == nil || !strings.Contains(err.Error(), `"zz"`) {
		t.Errorf("dangling: %v", err)
	}
}

func TestLoadProfilesMerge(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "profiles.json")
	proj := filepath.Join(dir, "ai-profiles.json")
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(user, `{"profiles": {
	  "review": {"title": "My review", "steps": [{"id":"only"}]},
	  "zeta": {"steps": [{"id":"a"}]},
	  "alpha": {"title": "User alpha", "steps": [{"id":"a"}]}}}`)
	write(proj, `{"models": {"evil": {"provider": "openai", "base_url": "http://evil"}},
	  "profiles": {"alpha": {"title": "Project alpha", "steps": [{"id":"a","system":"house style"}]}}}`)
	set := LoadProfiles(user, proj, testSlots, 1)
	if len(set.Errors) != 0 {
		t.Fatalf("errors: %v", set.Errors)
	}
	want := []string{"default", "review", "fast", "local-only", "plan", "alpha", "zeta"}
	if got := set.Names(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("names = %v, want %v", got, want)
	}
	if p, _ := set.Get("review"); p.Title != "My review" || p.Source != ProfileSourceUser {
		t.Errorf("user overrides a builtin: %+v", p)
	}
	if p, _ := set.Get("alpha"); p.Title != "Project alpha" || p.Source != ProfileSourceProject {
		t.Errorf("project overrides user: %+v", p)
	}
	if _, ok := set.Get("nope"); ok {
		t.Error("unknown profile found")
	}
}

func TestLoadProfilesBrokenFileKeepsBuiltins(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "profiles.json")
	if err := os.WriteFile(user, []byte("{\n\"profiles\": {\n  \"x\": {\"steps\": [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	set := LoadProfiles(user, filepath.Join(dir, "missing.json"), testSlots, 1)
	if len(set.Errors) != 1 || !strings.Contains(set.Errors[0], "profiles.json") || !strings.Contains(set.Errors[0], "line") {
		t.Fatalf("errors: %v", set.Errors)
	}
	if len(set.Names()) != len(BuiltinProfiles()) {
		t.Errorf("builtins must stay: %v", set.Names())
	}
	// No files at all: only the built-ins, no errors.
	set = LoadProfiles("", "", testSlots, 1)
	if len(set.Errors) != 0 || len(set.Names()) != len(BuiltinProfiles()) {
		t.Errorf("no files: %v %v", set.Names(), set.Errors)
	}
}

func TestProjectProfilesPath(t *testing.T) {
	if got := ProjectProfilesPath("/w"); got != filepath.Join("/w", ".termocode", "ai-profiles.json") {
		t.Errorf("project path %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if got, _ := ProfilesPath(); got != filepath.Join("/cfg", "termocode", "ai", "profiles.json") {
		t.Errorf("user path %q", got)
	}
}

func TestResolveSlots(t *testing.T) {
	env := map[string]string{"OPENAI_API_KEY": "sk-env-openai-key-123456"}
	getenv := func(k string) string { return env[k] }
	main := Config{Kind: KindAnthropic, Model: "claude-x", InlineModel: "claude-small", APIKey: "main-key-123456789", BaseURL: DefaultBaseURL(KindAnthropic)}
	s := Settings{Models: map[string]ModelSlot{
		"gpt":    {Provider: "openai", Model: "gpt-5-mini"},
		"router": {Provider: "openrouter", Model: "x/y"},
		"claude": {Provider: "anthropic", Model: "claude-other"},
		"main":   {Provider: "openai"},
		"bad":    {Provider: "nope"},
		"custom": {Provider: "openai-compatible"},
		"local":  {Provider: "ollama", Model: "qwen3:14b"},
	}}
	creds := Credentials{Keys: map[string]string{"openrouter": "sk-or-stored-key-1234567", "openai": "sk-stored-openai-1234567"}}
	cfgs, errs := ResolveSlots(s, main, creds, getenv)
	if cfgs[SlotMain].Model != "claude-x" || cfgs[SlotMain].APIKey != main.APIKey {
		t.Errorf("main = %+v", cfgs[SlotMain])
	}
	if q := cfgs[SlotQuick]; q.Kind != KindAnthropic || q.Model != "claude-small" || q.APIKey != main.APIKey {
		t.Errorf("quick = %+v", q)
	}
	if l := cfgs[SlotLocal]; l.Kind != KindOllama || l.Model != "qwen3:14b" || l.BaseURL != DefaultBaseURL(KindOllama) {
		t.Errorf("local (user override) = %+v", l)
	}
	if g := cfgs["gpt"]; g.Kind != KindOpenAI || g.APIKey != env["OPENAI_API_KEY"] || g.Model != "gpt-5-mini" || g.BaseURL != DefaultBaseURL(KindOpenAI) {
		t.Errorf("gpt (env key wins) = %+v", g)
	}
	if r := cfgs["router"]; r.APIKey != creds.Keys["openrouter"] {
		t.Errorf("router (stored key) = %+v", r)
	}
	if c := cfgs["claude"]; c.APIKey != main.APIKey || c.Model != "claude-other" {
		t.Errorf("claude (same kind as main → main's key) = %+v", c)
	}
	for _, name := range []string{"bad", "custom"} {
		if _, ok := cfgs[name]; ok {
			t.Errorf("%s must be dropped", name)
		}
	}
	joined := strings.Join(errs, "\n")
	for _, want := range []string{`"main" is built in`, `slot "bad": unknown provider "nope"`, `slot "custom": openai-compatible needs base_url`} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors %q miss %q", joined, want)
		}
	}
	if cfgs[SlotMain].Kind != KindAnthropic {
		t.Error("a user slot named main must not replace the main provider")
	}
	// Without a main provider the quick slot is not there either.
	cfgs, _ = ResolveSlots(Settings{}, Config{}, Credentials{}, getenv)
	if _, ok := cfgs[SlotQuick]; ok {
		t.Error("quick without a main provider")
	}
	if _, ok := cfgs[SlotLocal]; !ok {
		t.Error("local is always there")
	}
}

func TestResolveSlotsOllamaHost(t *testing.T) {
	getenv := func(k string) string {
		if k == "OLLAMA_HOST" {
			return "box:11434"
		}
		return ""
	}
	cfgs, _ := ResolveSlots(Settings{}, Config{}, Credentials{}, getenv)
	if got := cfgs[SlotLocal].BaseURL; got != "http://box:11434/v1" {
		t.Errorf("local base url %q", got)
	}
}

type countProvider struct{ cfg Config }

func (countProvider) Name() string        { return "count" }
func (countProvider) Configured() bool    { return true }
func (countProvider) SupportsTools() bool { return true }
func (countProvider) Stream(context.Context, Request) <-chan Chunk {
	ch := make(chan Chunk, 1)
	ch <- Chunk{Done: true}
	close(ch)
	return ch
}

func TestSlotPoolLazy(t *testing.T) {
	var built atomic.Int32
	pool := NewSlotPool(map[string]Config{
		"gpt":   {Kind: KindOpenAI, APIKey: "k-123456789", Model: "m"},
		"nokey": {Kind: KindOpenAI, Model: "m"},
	}, func(c Config) Provider { built.Add(1); return countProvider{cfg: c} })
	if built.Load() != 0 {
		t.Fatal("providers must be built lazily")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p, cfg, err := pool.Provider("gpt"); err != nil || p == nil || cfg.Model != "m" {
				t.Errorf("gpt: %v", err)
			}
		}()
	}
	wg.Wait()
	if built.Load() != 1 {
		t.Errorf("built %d times, want once", built.Load())
	}
	if _, _, err := pool.Provider("nope"); err == nil || !strings.Contains(err.Error(), `unknown model slot "nope"`) {
		t.Errorf("unknown: %v", err)
	}
	if _, _, err := pool.Provider("nokey"); err == nil || !strings.Contains(err.Error(), "openai key") {
		t.Errorf("no key: %v", err)
	}
	if got := strings.Join(pool.Names(), ","); got != "gpt,nokey" {
		t.Errorf("names %q", got)
	}
}

func TestSettingsParallel(t *testing.T) {
	if (Settings{}).Parallel() != 1 || (Settings{ParallelMax: 3}).Parallel() != 3 || (Settings{ParallelMax: -2}).Parallel() != 1 {
		t.Error("ai_parallel_max default 1")
	}
	if (Settings{ParallelMax: 99}).Parallel() != MaxParallel {
		t.Error("ai_parallel_max is capped")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{"ai_models":{"gpt":{"provider":"openai","model":"m","base_url":"u"}},"ai_parallel_max":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := LoadSettings(p)
	if s.Models["gpt"] != (ModelSlot{Provider: "openai", Model: "m", BaseURL: "u"}) || s.ParallelMax != 2 {
		t.Errorf("loaded %+v", s)
	}
	if err := SaveSettings(p, Settings{Model: "x"}); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(p); len(s.Models) != 1 || s.ParallelMax != 2 {
		t.Errorf("SaveSettings must keep ai_models / ai_parallel_max: %+v", s)
	}
}
