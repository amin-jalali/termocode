package app

import (
	"os"
	"strings"
	"testing"
)

// WFX-0: the workflows_* settings (plan §5.8) are Settings rows in a
// "Workflows" group with their defaults, saved in config.json, checked
// on set, and gated for the AI: the limits ask first, the on/off switch
// and the agent sandbox are never the AI's.
func TestWorkflowSettingsRows(t *testing.T) {
	withConfigDir(t)
	want := map[string]struct {
		def  string
		gate aiGate
	}{
		"workflows_enabled":          {"true", aiGateNever},
		"workflows_parallel_max":     {"2", aiGateConfirm},
		"workflows_day_budget_usd":   {"0", aiGateConfirm},
		"workflows_missed_grace_min": {"30", aiGateConfirm},
		"workflows_history_days":     {"30", aiGateConfirm},
		"workflows_wait_hours":       {"12", aiGateConfirm},
		"workflows_agent_sandbox":    {"worktree", aiGateNever},
	}
	cats := settingsCategories()
	found := 0
	for _, e := range SettingsCatalog() {
		w, ok := want[e.Key]
		if !ok {
			continue
		}
		found++
		if e.Default != w.def {
			t.Errorf("%s: default %q, want %q", e.Key, e.Default, w.def)
		}
		if cats[e.ID] != "Workflows" {
			t.Errorf("%s: category %q, want Workflows", e.Key, cats[e.ID])
		}
		if g := aiSettingPolicies[e.Key].Gate; g != w.gate {
			t.Errorf("%s: AI gate %q, want %q", e.Key, g, w.gate)
		}
		r, _ := findSettingsRow(e.ID)
		if v := currentSettingValue(r, loadSettings()); v != w.def {
			t.Errorf("%s: value with no config %q, want %q", e.Key, v, w.def)
		}
	}
	if found != len(want) {
		t.Fatalf("found %d of %d workflows rows", found, len(want))
	}
	if got := settingChoices("setting-workflows-agent-sandbox"); strings.Join(got, ",") != "worktree,read-only" {
		t.Errorf("sandbox choices %v", got)
	}
}

func TestWorkflowSettingsApply(t *testing.T) {
	withConfigDir(t)
	path, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	m := &Model{w: 120, h: 40}
	val := func(id string) string {
		r, _ := findSettingsRow(id)
		return currentSettingValue(r, loadSettings())
	}
	good := []struct{ id, in, want string }{
		{"setting-workflows-enabled", "off", "false"},
		{"setting-workflows-parallel-max", "4", "4"},
		{"setting-workflows-day-budget", "2.50", "2.5"},
		{"setting-workflows-missed-grace", "0", "0"},
		{"setting-workflows-history-days", "90", "90"},
		{"setting-workflows-wait-hours", "48", "48"},
		{"setting-workflows-agent-sandbox", "read-only", "read-only"},
	}
	for _, c := range good {
		m.applySetting(c.id, c.in)
		if got := val(c.id); got != c.want {
			t.Errorf("%s=%s: value %q, want %q", c.id, c.in, got, c.want)
		}
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"workflows_parallel_max": 4`) || !strings.Contains(string(b), `"workflows_agent_sandbox": "read-only"`) {
		t.Fatalf("config.json:\n%s", b)
	}
	bad := []struct{ id, in string }{
		{"setting-workflows-enabled", "maybe"},
		{"setting-workflows-parallel-max", "0"},
		{"setting-workflows-parallel-max", "17"},
		{"setting-workflows-day-budget", "-1"},
		{"setting-workflows-day-budget", "lots"},
		{"setting-workflows-history-days", "0"},
		{"setting-workflows-wait-hours", "x"},
		{"setting-workflows-agent-sandbox", "none"},
	}
	for _, c := range bad {
		before := val(c.id)
		m.applySetting(c.id, c.in)
		if got := val(c.id); got != before {
			t.Errorf("%s=%s: a bad value changed it to %q", c.id, c.in, got)
		}
	}
	// The AI may set a limit after its card, never the switch.
	if _, v, err := aiValidateSetting("workflows_parallel_max", "3"); err != nil || v != "3" {
		t.Errorf("AI parallel_max: %q %v", v, err)
	}
	if _, v, err := aiValidateSetting("workflows_day_budget_usd", "1.50"); err != nil || v != "1.5" {
		t.Errorf("AI day budget: %q %v", v, err)
	}
	if _, _, err := aiValidateSetting("workflows_parallel_max", "99"); err == nil {
		t.Error("AI parallel_max 99: want a range error")
	}
	for _, k := range []string{"workflows_enabled", "workflows_agent_sandbox"} {
		if _, _, err := aiValidateSetting(k, "true"); err == nil || !strings.Contains(err.Error(), "not for the AI") {
			t.Errorf("AI %s: %v", k, err)
		}
	}
}
