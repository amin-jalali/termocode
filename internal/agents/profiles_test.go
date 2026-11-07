package agents

import (
	"strings"
	"testing"
)

// Spec (CMP-21): the agents with documented flags have a model flag and
// their permission modes; plan mode is one of the modes where the CLI
// has one; a mode that skips every permission check is Risky.
func TestProfileModes(t *testing.T) {
	for id, want := range map[string][]string{
		"claude": {"plan", "edits", "bypass"},
		"codex":  {"plan", "auto", "full"},
		"gemini": {"edits", "yolo"},
		"aider":  {"yes"},
	} {
		got := map[string]ProfileMode{}
		for _, md := range ProfileModes(id) {
			got[md.ID] = md
		}
		for _, w := range want {
			if _, ok := got[w]; !ok {
				t.Errorf("%s has no mode %q (has %v)", id, w, got)
			}
		}
	}
	for _, risky := range []string{"claude/bypass", "codex/full", "gemini/yolo", "aider/yes"} {
		id, mode, _ := strings.Cut(risky, "/")
		md, ok := ProfileModeOf(id, mode)
		if !ok || !md.Risky {
			t.Errorf("%s not risky: %+v %v", risky, md, ok)
		}
	}
	if md, _ := ProfileModeOf("claude", "plan"); md.Risky || !md.Plan {
		t.Errorf("claude plan %+v", md)
	}
	if ModelFlag("claude") != "--model" || ModelFlag("codex") != "--model" || ModelFlag("droid") != "" {
		t.Errorf("model flags %q %q %q", ModelFlag("claude"), ModelFlag("codex"), ModelFlag("droid"))
	}
	if len(ProfileModes("droid")) != 0 {
		t.Error("droid has modes")
	}
}

// Spec (CMP-21): a profile's line is env with its variables, the base
// line, then the model flag and the mode's flags; the default profile
// (no model, mode or env) leaves the line as it is.
func TestProfileLine(t *testing.T) {
	claude, _ := ByID("claude")
	codex, _ := ByID("codex")
	cases := []struct {
		a    Agent
		p    LaunchProfile
		base string
		want string
	}{
		{claude, LaunchProfile{Name: "x"}, "claude", "claude"},
		{claude, LaunchProfile{Name: "plan", Model: "opus", Mode: "plan"}, "claude --mcp-config /t/c.json",
			"claude --mcp-config /t/c.json --model opus --permission-mode plan"},
		{claude, LaunchProfile{Name: "e", Env: map[string]string{"MAX_THINKING": "8000", "A_FLAG": "a b"}}, "claude",
			"env A_FLAG='a b' MAX_THINKING=8000 claude"},
		{codex, LaunchProfile{Name: "ro", Mode: "plan", Model: "gpt-5"}, "codex",
			"codex --model gpt-5 --sandbox read-only --ask-for-approval on-request"},
	}
	for _, c := range cases {
		got, err := ProfileLine(c.a, c.p, c.base)
		if err != nil || got != c.want {
			t.Errorf("%s %+v: %q %v, want %q", c.a.ID, c.p, got, err, c.want)
		}
	}
}

// Spec (CMP-21): bad values are refused: a model that could break the
// shell line, an unknown mode, a model for a CLI without a model flag,
// env names that look like secrets (keys go in termocode's secret store,
// never in config.json), names that change how programs load, the
// accounts' own config variable, and values with control characters.
func TestProfileCheck(t *testing.T) {
	claude, _ := ByID("claude")
	droid, _ := ByID("droid")
	bad := []struct {
		a Agent
		p LaunchProfile
	}{
		{claude, LaunchProfile{Name: "Bad Name"}},
		{claude, LaunchProfile{Name: "x", Model: "opus; rm -rf ~"}},
		{claude, LaunchProfile{Name: "x", Model: "$(id)"}},
		{claude, LaunchProfile{Name: "x", Mode: "nope"}},
		{droid, LaunchProfile{Name: "x", Model: "m"}},
		{claude, LaunchProfile{Name: "x", Env: map[string]string{"ANTHROPIC_API_KEY": "k"}}},
		{claude, LaunchProfile{Name: "x", Env: map[string]string{"GH_TOKEN": "k"}}},
		{claude, LaunchProfile{Name: "x", Env: map[string]string{"DB_PASSWORD": "k"}}},
		{claude, LaunchProfile{Name: "x", Env: map[string]string{"LD_PRELOAD": "/x.so"}}},
		{claude, LaunchProfile{Name: "x", Env: map[string]string{"PATH": "/x"}}},
		{claude, LaunchProfile{Name: "x", Env: map[string]string{"CLAUDE_CONFIG_DIR": "/x"}}},
		{claude, LaunchProfile{Name: "x", Env: map[string]string{"lower": "1"}}},
		{claude, LaunchProfile{Name: "x", Env: map[string]string{"A": "1\n2"}}},
		{claude, LaunchProfile{Name: "x", Env: map[string]string{"A": "it's"}}},
	}
	for _, b := range bad {
		if err := CheckProfile(b.a, b.p); err == nil {
			t.Errorf("%s %+v accepted", b.a.ID, b.p)
		}
		if _, err := ProfileLine(b.a, b.p, b.a.Bin); err == nil {
			t.Errorf("%s %+v: a line was made", b.a.ID, b.p)
		}
	}
	if err := CheckProfile(claude, LaunchProfile{Name: "ok-1", Model: "claude-opus-4@2026", Mode: "edits", Env: map[string]string{"DISABLE_TELEMETRY": "1"}}); err != nil {
		t.Errorf("good profile refused: %v", err)
	}
}

// Spec (CMP-21): ParseEnv reads "K=V K2=V2"; blank is no env; a word
// without = is refused.
func TestParseEnv(t *testing.T) {
	env, err := ParseEnv("  A=1   B_2=x/y ")
	if err != nil || len(env) != 2 || env["A"] != "1" || env["B_2"] != "x/y" {
		t.Errorf("env %v %v", env, err)
	}
	if env, err := ParseEnv(" "); err != nil || len(env) != 0 {
		t.Errorf("blank %v %v", env, err)
	}
	if _, err := ParseEnv("A=1 oops"); err == nil {
		t.Error("a word without = accepted")
	}
}

// Spec (CMP-21): Summary is the one-line hint of a profile.
func TestProfileSummary(t *testing.T) {
	p := LaunchProfile{Name: "p", Model: "opus", Mode: "plan", Env: map[string]string{"B": "1", "A": "2"}}
	if got := ProfileSummary("claude", p); got != "opus · Plan (read only) · env A, B" {
		t.Errorf("summary %q", got)
	}
	if got := ProfileSummary("claude", LaunchProfile{Name: "d"}); got != "CLI defaults" {
		t.Errorf("default summary %q", got)
	}
}
