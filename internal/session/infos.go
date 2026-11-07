package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Info describes a running session (`termocode sessions`).
type Info struct {
	Workspace string    `json:"workspace"`
	PID       int       `json:"pid"`
	UIPID     int       `json:"ui_pid,omitempty"`
	Started   time.Time `json:"started"`
	Attached  bool      `json:"attached"`
	// Headless: the UI was started with no terminal (MOB-51, a project
	// the phone opened); a later attach shows it as usual.
	Headless bool `json:"headless,omitempty"`
}

// ReadInfos reads the Info files in dir (Dir) of the sessions whose
// server still runs (alive says whether a pid runs), by workspace. Unlike
// List it dials no socket, so it never waits on a busy server; a file
// that is not a session's Info (caps, web status) or does not parse is
// skipped.
func ReadInfos(dir string, alive func(int) bool) []Info {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil
	}
	var out []Info
	for _, f := range files {
		if key := strings.TrimSuffix(filepath.Base(f), ".json"); key == "" || strings.Contains(key, ".") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var in Info
		if json.Unmarshal(b, &in) != nil || in.Workspace == "" || in.PID <= 0 {
			continue
		}
		if alive != nil && !alive(in.PID) {
			continue
		}
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Workspace < out[j].Workspace })
	return out
}
