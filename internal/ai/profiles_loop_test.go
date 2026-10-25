package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
)

// OM-3: profiles may declare a verify/fix loop; the built-in until-done
// profile works until done.

func TestBuiltinUntilDone(t *testing.T) {
	var p Profile
	for _, b := range BuiltinProfiles() {
		if b.Name == ProfileUntilDone {
			p = b
		}
	}
	if p.Loop == nil || p.BudgetUSD != DefaultUntilDoneBudgetUSD || p.BudgetSet {
		t.Fatalf("until-done %+v", p)
	}
	r := p.Run("r")
	if err := agentdag.Validate(r); err != nil {
		t.Fatal(err)
	}
	if r.Loop == nil || r.Loop.Verify != "verify" || r.Loop.Fix.ID != "fix" || r.Loop.MaxRounds != DefaultUntilDoneRounds {
		t.Fatalf("loop %+v", r.Loop)
	}
	if !r.Nodes[0].Plan || r.Nodes[1].ID != "verify" || len(r.Edges) != 1 {
		t.Errorf("nodes %+v edges %+v", r.Nodes, r.Edges)
	}
	if r.Loop.Fix.Tools.Preset != agentdag.PresetAll || r.Loop.Fix.Slot != SlotQuick {
		t.Errorf("fix %+v", r.Loop.Fix)
	}
	for _, w := range []string{"tests_run", "run_command", "read_file"} {
		if !contains(r.Nodes[1].Tools.Allow, w) {
			t.Errorf("verify lacks %s: %v", w, r.Nodes[1].Tools.Allow)
		}
	}
	if contains(r.Nodes[1].Tools.Allow, "write_file") {
		t.Error("verify must not write files")
	}
}

func TestParseProfileLoop(t *testing.T) {
	data := `{"profiles": {
  "tdd": {
    "steps": [{"id": "do", "category": "quick"}, {"id": "check", "category": "deep"}],
    "loop": {"verify": "check", "fix": {"title": "Repair", "category": "quick", "system": "fix it"}, "max_rounds": 5},
    "budget_usd": 0
  },
  "plain": {
    "steps": [{"id": "do"}, {"id": "check"}],
    "loop": {"verify": "check", "fix": {"id": "mend"}}
  }
}}`
	ps, err := ParseProfiles([]byte(data), "profiles.json", ProfileSourceUser, testSlots, 1)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Profile{}
	for _, p := range ps {
		by[p.Name] = p
	}
	tdd := by["tdd"]
	if tdd.Loop == nil || tdd.Loop.Fix.ID != "fix" || tdd.Loop.MaxRounds != 5 || !tdd.BudgetSet {
		t.Fatalf("tdd %+v", tdd)
	}
	r := tdd.Run("r")
	if r.Loop.Fix.Title != "Repair" || r.Loop.Fix.System != "fix it" || r.Loop.MaxRounds != 5 {
		t.Errorf("tdd run loop %+v", r.Loop)
	}
	if plain := by["plain"]; plain.BudgetSet || plain.Loop.MaxRounds != 0 || plain.Loop.Fix.ID != "mend" {
		t.Errorf("plain %+v", plain)
	}
}

func TestParseProfileLoopErrors(t *testing.T) {
	steps := `"steps": [{"id": "plan", "plan": true}, {"id": "check"}]`
	for _, c := range []struct{ name, loop, want string }{
		{"unknown verify", `{"verify": "nope", "fix": {}}`, `loop.verify: unknown step "nope"`},
		{"planner verify", `{"verify": "plan", "fix": {}}`, `step "plan" is a planner`},
		{"fix id taken", `{"verify": "check", "fix": {"id": "check"}}`, `loop.fix id "check" is a step id`},
		{"bad fix id", `{"verify": "check", "fix": {"id": "a b"}}`, `loop.fix id "a b"`},
		{"fix plans", `{"verify": "check", "fix": {"plan": true}}`, "may not be a planner"},
		{"fix deps", `{"verify": "check", "fix": {"deps": ["check"]}}`, "may not have deps"},
		{"rounds 11", `{"verify": "check", "fix": {}, "max_rounds": 11}`, "loop.max_rounds is 11 (1 to 10"},
		{"rounds -1", `{"verify": "check", "fix": {}, "max_rounds": -1}`, "loop.max_rounds is -1"},
		{"fix slot", `{"verify": "check", "fix": {"slot": "nope"}}`, `loop.fix "fix": unknown slot "nope"`},
	} {
		data := "{\"profiles\": {\n\n\"x\": {" + steps + `, "loop": ` + c.loop + "}}}"
		_, err := ParseProfiles([]byte(data), "profiles.json", ProfileSourceUser, testSlots, 1)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "profiles.json line 3") {
			t.Errorf("%s: err %v, want %q at line 3", c.name, err, c.want)
		}
	}
}

// Spec (owner 2026-10-25): ai_until_done_rounds and
// ai_until_done_budget_usd default to 3 and $2; 0 dollars set on purpose
// is no budget; values out of range fall back and are named; both keys
// survive a save.
func TestSettingsUntilDone(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for _, c := range []struct {
		name    string
		s       Settings
		rounds  int
		budget  float64
		problem string
	}{
		{"defaults", Settings{}, 3, 2, ""},
		{"set", Settings{UntilDoneRounds: 5, UntilDoneBudgetUSD: f(0.5)}, 5, 0.5, ""},
		{"no budget", Settings{UntilDoneBudgetUSD: f(0)}, 3, 0, ""},
		{"rounds too high", Settings{UntilDoneRounds: 11}, 3, 2, "ai_until_done_rounds must be 1 to 10 (it is 11); using 3"},
		{"rounds negative", Settings{UntilDoneRounds: -1}, 3, 2, "must be 1 to 10"},
		{"budget negative", Settings{UntilDoneBudgetUSD: f(-1)}, 3, 2, "ai_until_done_budget_usd must be a dollar amount above 0, or 0 for no budget"},
	} {
		r, b, problems := c.s.UntilDone()
		if r != c.rounds || b != c.budget {
			t.Errorf("%s: %d %v, want %d %v", c.name, r, b, c.rounds, c.budget)
		}
		if got := strings.Join(problems, "\n"); (c.problem == "") != (got == "") || !strings.Contains(got, c.problem) {
			t.Errorf("%s: problems %q, want %q", c.name, got, c.problem)
		}
	}
	if CheckUntilDoneRounds(0) == nil || CheckUntilDoneRounds(10) != nil || CheckUntilDoneBudget(-0.01) == nil || CheckUntilDoneBudget(0) != nil {
		t.Error("range checks")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"theme": "x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(path, Settings{UntilDoneRounds: 4, UntilDoneBudgetUSD: f(0)}); err != nil {
		t.Fatal(err)
	}
	s := LoadSettings(path)
	if s.UntilDoneRounds != 4 || s.UntilDoneBudgetUSD == nil || *s.UntilDoneBudgetUSD != 0 {
		t.Errorf("after save %+v", s)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"theme"`) {
		t.Errorf("other keys lost: %s", b)
	}
}

// Spec (owner 2026-10-25): the settings give the defaults; a profile's
// own loop.max_rounds and budget_usd win.
func TestSetLoopDefaults(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "profiles.json")
	if err := os.WriteFile(user, []byte(`{"profiles": {
  "own": {"steps": [{"id": "check"}], "loop": {"verify": "check", "fix": {}, "max_rounds": 2}, "budget_usd": 5},
  "free": {"steps": [{"id": "check"}], "loop": {"verify": "check", "fix": {}}, "budget_usd": 0},
  "bare": {"steps": [{"id": "check"}], "loop": {"verify": "check", "fix": {}}}
}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	set := LoadProfiles(user, "", testSlots, 1)
	if len(set.Errors) > 0 {
		t.Fatal(set.Errors)
	}
	set.SetLoopDefaults(7, 4.5)
	for _, c := range []struct {
		name   string
		rounds int
		budget float64
	}{
		{ProfileUntilDone, 7, 4.5}, {"own", 2, 5}, {"free", 7, 0}, {"bare", 7, 4.5},
	} {
		p, _ := set.Get(c.name)
		if p.Loop.MaxRounds != c.rounds || p.BudgetUSD != c.budget || p.Run("r").Loop.MaxRounds != c.rounds {
			t.Errorf("%s: rounds %d budget %v, want %d %v", c.name, p.Loop.MaxRounds, p.BudgetUSD, c.rounds, c.budget)
		}
	}
	if p, _ := set.Get(ProfilePlan); p.BudgetUSD != 0 || p.Loop != nil {
		t.Errorf("a profile without a loop keeps its budget: %+v", p)
	}
	if b := BuiltinProfiles(); b[len(b)-1].Loop.MaxRounds != 0 {
		t.Error("SetLoopDefaults changed the built-in table")
	}
}
