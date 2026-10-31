// Package agenthooks installs small state hooks into outside agents'
// own config files (OMX-34): the agent then calls
// `termocode agent report <state> --agent <id>` when it starts working,
// waits for the user, or ends a turn, so termocode knows the exact state
// instead of guessing from the screen. Plan builds the change and a
// preview; Apply writes it (with a backup of the old file). Nothing is
// written without a Plan the user saw.
package agenthooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// reportArgs follow the termocode program in a hook's command.
const reportArgs = " agent report "

// Target is one agent whose config can hold the hooks.
type Target struct {
	Agent string // agents.Agent ID
	File  func(home string) string
}

// Targets are the agents with a known hook format.
var Targets = []Target{
	{Agent: "claude", File: func(home string) string { return filepath.Join(home, ".claude", "settings.json") }},
	{Agent: "codex", File: func(home string) string { return filepath.Join(home, ".codex", "config.toml") }},
}

// TargetFor finds the target of agent id.
func TargetFor(id string) (Target, bool) {
	for _, t := range Targets {
		if t.Agent == id {
			return t, true
		}
	}
	return Target{}, false
}

// claudeEvents maps the agent's hook events to states.
var claudeEvents = []struct{ event, state string }{
	{"UserPromptSubmit", "working"},
	{"PreToolUse", "working"},
	{"Notification", "waiting"},
	{"Stop", "done"},
}

// command is the hook command line of exe for agent and state.
func command(exe, agent, state string) string {
	return shellQuote(exe) + reportArgs + state + " --agent " + agent
}

// tail is how a hook command of ours ends (any termocode path).
func tail(agent, state string) string { return reportArgs + state + " --agent " + agent }

// shellQuote quotes a path for sh when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Plan is a change to one file.
type Plan struct {
	Agent     string
	Path      string
	Installed bool     // the hooks are there already (Apply does nothing)
	Lines     []string // what changes, for the card
	New       []byte   // the whole new file
	Remove    bool     // the plan takes the hooks out
	Exe       string   // the termocode program the hooks run
}

// PlanInstall builds the install of agent's hooks (running exe, the
// termocode program) in its file under home.
func PlanInstall(agent, home, exe string) (Plan, error) {
	return plan(agent, home, exe, false)
}

// PlanRemove builds the removal (hooks of any termocode path).
func PlanRemove(agent, home string) (Plan, error) {
	return plan(agent, home, "termocode", true)
}

func plan(agent, home, exe string, remove bool) (Plan, error) {
	if exe == "" {
		exe = "termocode"
	}
	t, ok := TargetFor(agent)
	if !ok {
		return Plan{}, fmt.Errorf("no state hooks for %q", agent)
	}
	p := Plan{Agent: agent, Path: t.File(home), Remove: remove, Exe: exe}
	old, err := os.ReadFile(p.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return p, err
	}
	switch agent {
	case "claude":
		err = planClaude(&p, old)
	case "codex":
		err = planCodex(&p, old)
	}
	return p, err
}

// planClaude adds or removes one command hook per event in the hooks
// object, keeping every other key and hook.
func planClaude(p *Plan, old []byte) error {
	doc := map[string]any{}
	if len(bytes.TrimSpace(old)) > 0 {
		if err := json.Unmarshal(old, &doc); err != nil {
			return fmt.Errorf("%s is not plain JSON (%v): add the hooks by hand", p.Path, err)
		}
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		if _, bad := doc["hooks"]; bad {
			return fmt.Errorf("%s: hooks is not an object", p.Path)
		}
		hooks = map[string]any{}
	}
	all := true
	for _, e := range claudeEvents {
		cmd := command(p.Exe, p.Agent, e.state)
		groups, _ := hooks[e.event].([]any)
		has := false
		var kept []any
		for _, g := range groups {
			if claudeGroupHas(g, tail(p.Agent, e.state)) {
				has = true
				if p.Remove {
					continue
				}
			}
			kept = append(kept, g)
		}
		all = all && has
		switch {
		case p.Remove && has:
			p.Lines = append(p.Lines, "- hooks."+e.event+": "+cmd)
			if len(kept) == 0 {
				delete(hooks, e.event)
			} else {
				hooks[e.event] = kept
			}
		case !p.Remove && !has:
			p.Lines = append(p.Lines, "+ hooks."+e.event+": "+cmd)
			hooks[e.event] = append(groups, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmd, "timeout": 5}}})
		}
	}
	p.Installed = all
	if len(p.Lines) == 0 {
		return nil
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	} else {
		doc["hooks"] = hooks
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	p.New = append(b, '\n')
	return nil
}

// claudeGroupHas: a hook of the group is ours (its command ends so).
func claudeGroupHas(g any, end string) bool {
	gm, _ := g.(map[string]any)
	hs, _ := gm["hooks"].([]any)
	for _, h := range hs {
		hm, _ := h.(map[string]any)
		if c, _ := hm["command"].(string); strings.HasSuffix(c, end) && strings.Contains(c, "termocode") {
			return true
		}
	}
	return false
}

// codexLine is the top-level notify setting (the agent calls it at the
// end of each turn, with a JSON argument the report ignores).
func codexLine(exe, agent string) string {
	q, _ := json.Marshal(exe) // a TOML basic string is a JSON string here
	return `notify = [` + string(q) + `, "agent", "report", "done", "--agent", "` + agent + `"]`
}

// codexOurs: a notify line of ours (any termocode path).
func codexOurs(line, agent string) bool {
	return strings.HasSuffix(strings.TrimSpace(line), `, "agent", "report", "done", "--agent", "`+agent+`"]`) && strings.Contains(line, "termocode")
}

// planCodex puts the notify line first in config.toml (top-level keys
// must come before any table). A notify of the user's own is kept: then
// the hook can not be added.
func planCodex(p *Plan, old []byte) error {
	line := codexLine(p.Exe, p.Agent)
	lines := strings.Split(string(old), "\n")
	idx := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			break // tables start: no more top-level keys
		}
		if strings.HasPrefix(t, "notify") && strings.Contains(strings.SplitN(t, "=", 2)[0], "notify") && strings.Contains(t, "=") {
			idx = i
			break
		}
	}
	switch {
	case idx >= 0 && codexOurs(lines[idx], p.Agent):
		p.Installed = true
		if p.Remove {
			p.Lines = []string{"- " + strings.TrimSpace(lines[idx])}
			p.New = []byte(strings.Join(append(lines[:idx:idx], lines[idx+1:]...), "\n"))
		}
	case idx >= 0:
		if !p.Remove {
			return fmt.Errorf("%s already has its own notify line: add the report to it by hand", p.Path)
		}
	case !p.Remove:
		p.Lines = []string{"+ " + line + "   (first line)"}
		p.New = []byte(line + "\n" + string(old))
	}
	return nil
}

// Apply writes the plan: the old file goes to <file>.termocode-backup
// first. A plan with no change writes nothing.
func Apply(p Plan) error {
	if p.New == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(p.Path); err == nil {
		mode = fi.Mode().Perm()
		old, err := os.ReadFile(p.Path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(p.Path+".termocode-backup", old, mode); err != nil {
			return err
		}
	}
	tmp := p.Path + ".termocode-tmp"
	if err := os.WriteFile(tmp, p.New, mode); err != nil {
		return err
	}
	return os.Rename(tmp, p.Path)
}

// Agents lists the agent ids with a hook format, sorted.
func Agents() []string {
	var out []string
	for _, t := range Targets {
		out = append(out, t.Agent)
	}
	sort.Strings(out)
	return out
}
