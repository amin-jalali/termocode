// Package agentroll shares a roll-up of each termocode window's agent
// states (OMX-35): every window writes one small file (its workspace,
// how many agents work and how many wait for the user) when the states
// change, and the others read them, so blocked agents in other
// workspaces show. A window that ended (its pid is gone) is skipped and
// its file removed.
package agentroll

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Item is one agent: an AI run or a coding CLI in a terminal tab.
type Item struct {
	Name  string `json:"name"`
	State string `json:"state"` // working | waiting | done
}

// Rollup is one window's agents.
type Rollup struct {
	Workspace string `json:"workspace"`
	PID       int    `json:"pid"`
	UpdatedMs int64  `json:"updated_ms"`
	Items     []Item `json:"items,omitempty"`
}

// Counts are how many items work and wait.
func (r Rollup) Counts() (working, waiting int) {
	for _, it := range r.Items {
		switch it.State {
		case "working":
			working++
		case "waiting":
			waiting++
		}
	}
	return working, waiting
}

// Dir is $XDG_STATE_HOME/termocode/agents (or ~/.local/state/…).
func Dir() (string, error) {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "termocode", "agents"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "termocode", "agents"), nil
}

func file(dir string, pid int) string { return filepath.Join(dir, strconv.Itoa(pid)+".json") }

// Write stores r (r.PID names the file); a rollup with no working or
// waiting item removes the file.
func Write(dir string, r Rollup) error {
	if w, x := r.Counts(); w == 0 && x == 0 {
		Remove(dir, r.PID)
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp := file(dir, r.PID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file(dir, r.PID))
}

// Remove drops pid's file.
func Remove(dir string, pid int) { _ = os.Remove(file(dir, pid)) }

// List reads the other windows' rollups (not self), by workspace; alive
// says whether a pid still runs (a dead one's file is removed).
func List(dir string, self int, alive func(int) bool) []Rollup {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Rollup
	for _, e := range ents {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		pid, err := strconv.Atoi(name)
		if !ok || err != nil || pid == self {
			continue
		}
		if alive != nil && !alive(pid) {
			Remove(dir, pid)
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r Rollup
		if json.Unmarshal(b, &r) != nil || r.PID != pid {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Workspace < out[j].Workspace })
	return out
}

// Line is a short summary: "2 working · 1 waiting for you".
func (r Rollup) Line() string {
	w, x := r.Counts()
	var parts []string
	if x > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting for you", x))
	}
	if w > 0 {
		parts = append(parts, fmt.Sprintf("%d working", w))
	}
	return strings.Join(parts, " · ")
}
