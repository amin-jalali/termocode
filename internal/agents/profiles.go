package agents

import (
	"errors"
	"regexp"
	"sort"
	"strings"
)

// ── Launch profiles (CMP-21) ────────────────────────────────────────────
//
// A launch profile is a named way to start an outside agent: the model,
// a permission mode (plan mode where the CLI has one) and extra
// environment variables. Only the CLIs' own documented flags are used;
// termocode never edits their config files. Secrets never go in a
// profile: a variable that looks like one is refused (keys belong in
// termocode's secret store).

// LaunchProfile is one saved profile of an agent (config.json
// agent_profiles, by agent id).
type LaunchProfile struct {
	Name  string            `json:"name"`
	Model string            `json:"model,omitempty"`
	Mode  string            `json:"mode,omitempty"` // a ProfileMode ID; "" = the CLI's default
	Env   map[string]string `json:"env,omitempty"`
}

// ProfileMode is one permission mode of an agent.
type ProfileMode struct {
	ID    string
	Title string
	Args  []string
	// Plan: the agent only reads and plans (no edits, no commands).
	Plan bool
	// Risky: the agent skips every permission check.
	Risky bool
}

type profileFlags struct {
	model string
	modes []ProfileMode
}

// approvalModes are the --approval-mode values of Gemini CLI and its
// fork Qwen Code.
var approvalModes = []ProfileMode{
	{ID: "edits", Title: "Accept edits", Args: []string{"--approval-mode", "auto_edit"}},
	{ID: "yolo", Title: "No permission checks", Args: []string{"--approval-mode", "yolo"}, Risky: true},
}

// flagTable is the documented flags of each CLI that has them.
var flagTable = map[string]profileFlags{
	"claude": {model: "--model", modes: []ProfileMode{
		{ID: "plan", Title: "Plan (read only)", Args: []string{"--permission-mode", "plan"}, Plan: true},
		{ID: "edits", Title: "Accept edits", Args: []string{"--permission-mode", "acceptEdits"}},
		{ID: "bypass", Title: "No permission checks", Args: []string{"--permission-mode", "bypassPermissions"}, Risky: true},
	}},
	"codex": {model: "--model", modes: []ProfileMode{
		{ID: "plan", Title: "Plan (read only)", Args: []string{"--sandbox", "read-only", "--ask-for-approval", "on-request"}, Plan: true},
		{ID: "auto", Title: "Edit the workspace, ask for more", Args: []string{"--sandbox", "workspace-write", "--ask-for-approval", "on-request"}},
		{ID: "full", Title: "No permission checks, no sandbox", Args: []string{"--dangerously-bypass-approvals-and-sandbox"}, Risky: true},
	}},
	"gemini":   {model: "--model", modes: approvalModes},
	"qwen":     {model: "--model", modes: approvalModes},
	"opencode": {model: "--model"},
	"aider": {model: "--model", modes: []ProfileMode{
		{ID: "yes", Title: "Say yes to every question", Args: []string{"--yes-always"}, Risky: true},
	}},
}

// ModelFlag is the agent's documented model flag ("" = none known).
func ModelFlag(id string) string { return flagTable[id].model }

// ProfileModes are the agent's permission modes (nil = none known).
func ProfileModes(id string) []ProfileMode {
	return append([]ProfileMode(nil), flagTable[id].modes...)
}

// ProfileModeOf finds mode id of agent. "" is the CLI's default mode.
func ProfileModeOf(agent, mode string) (ProfileMode, bool) {
	if mode == "" {
		return ProfileMode{Title: "CLI default"}, true
	}
	for _, md := range flagTable[agent].modes {
		if md.ID == mode {
			return md, true
		}
	}
	return ProfileMode{}, false
}

// HasProfileFlags says whether agent has a model flag or modes.
func HasProfileFlags(id string) bool { _, ok := flagTable[id]; return ok }

var (
	profileNameRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,23}$`)
	profileModelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,79}$`)
	envNameRe      = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
	// secretWords mark a variable that holds a secret.
	secretWords = []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "PASS", "CREDENTIAL", "AUTH", "COOKIE", "SESSION"}
	// loaderEnv change which programs or libraries run.
	loaderEnv = map[string]bool{"PATH": true, "HOME": true, "SHELL": true, "IFS": true, "ENV": true, "BASH_ENV": true,
		"LD_PRELOAD": true, "LD_LIBRARY_PATH": true, "LD_AUDIT": true, "NODE_OPTIONS": true, "PYTHONPATH": true, "PYTHONSTARTUP": true}
)

// ProfileNameOK checks a name: 1–24 of a-z 0-9 _ -, not first - or _.
func ProfileNameOK(name string) error {
	if !profileNameRe.MatchString(name) {
		return errors.New("use a short name: lower-case letters, digits, - and _ (up to 24)")
	}
	return nil
}

// CheckEnvName refuses a variable a profile must not set.
func CheckEnvName(a Agent, k string) error {
	if !envNameRe.MatchString(k) {
		return errors.New(k + ": use upper-case letters, digits and _")
	}
	for _, w := range strings.Split(k, "_") {
		for _, s := range secretWords {
			if w == s || (len(w) > len(s) && strings.HasSuffix(w, s)) {
				return errors.New(k + " looks like a secret: keep keys in termocode's secret store (AI: Secret Store…), never in a profile")
			}
		}
	}
	if loaderEnv[k] || strings.HasPrefix(k, "DYLD_") || strings.HasPrefix(k, "LD_") {
		return errors.New(k + " changes which programs run; a profile cannot set it")
	}
	if a.ConfigEnv != "" && k == a.ConfigEnv {
		return errors.New(k + " is the account folder: use Agent: Accounts… for it")
	}
	return nil
}

// CheckProfile checks every field of p for agent a.
func CheckProfile(a Agent, p LaunchProfile) error {
	if err := ProfileNameOK(p.Name); err != nil {
		return err
	}
	if p.Model != "" {
		if ModelFlag(a.ID) == "" {
			return errors.New(a.Title + " has no known model flag: leave the model empty")
		}
		if !profileModelRe.MatchString(p.Model) {
			return errors.New("the model name has a character the shell line cannot take")
		}
	}
	if _, ok := ProfileModeOf(a.ID, p.Mode); !ok {
		return errors.New(a.Title + " has no mode " + p.Mode)
	}
	for k, v := range p.Env {
		if err := CheckEnvName(a, k); err != nil {
			return err
		}
		if len(v) > 200 {
			return errors.New(k + ": the value is too long (200 at most)")
		}
		if _, ok := ShellWord(v); !ok {
			return errors.New(k + ": the value has a character the shell line cannot quote")
		}
	}
	return nil
}

// envKeys are p's variable names, sorted.
func envKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ProfileLine is base (the agent's own command line, with its MCP
// flags) run with profile p: "env K=V … base --model M <mode flags>".
func ProfileLine(a Agent, p LaunchProfile, base string) (string, error) {
	if err := CheckProfile(a, p); err != nil {
		return "", err
	}
	line := base
	if p.Model != "" {
		line += " " + ModelFlag(a.ID) + " " + p.Model
	}
	md, _ := ProfileModeOf(a.ID, p.Mode)
	for _, w := range md.Args {
		line += " " + w
	}
	if len(p.Env) == 0 {
		return line, nil
	}
	var b strings.Builder
	b.WriteString("env")
	for _, k := range envKeys(p.Env) {
		v, _ := ShellWord(p.Env[k]) // checked above; "" is ''
		b.WriteString(" " + k + "=" + v)
	}
	return b.String() + " " + line, nil
}

// ParseEnv reads "K=V K2=V2" (blank = none).
func ParseEnv(s string) (map[string]string, error) {
	env := map[string]string{}
	for _, w := range strings.Fields(s) {
		k, v, ok := strings.Cut(w, "=")
		if !ok || k == "" {
			return nil, errors.New(w + ": write NAME=value")
		}
		env[k] = v
	}
	return env, nil
}

// ProfileSummary is p in one line: model · mode · env names.
func ProfileSummary(agent string, p LaunchProfile) string {
	var parts []string
	if p.Model != "" {
		parts = append(parts, p.Model)
	}
	if p.Mode != "" {
		if md, ok := ProfileModeOf(agent, p.Mode); ok {
			parts = append(parts, md.Title)
		}
	}
	if len(p.Env) > 0 {
		parts = append(parts, "env "+strings.Join(envKeys(p.Env), ", "))
	}
	if len(parts) == 0 {
		return "CLI defaults"
	}
	return strings.Join(parts, " · ")
}
