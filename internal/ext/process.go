package ext

import "strings"

// RunSpec is extension.json "run": the program of a process extension.
// Command is looked up on PATH unless it contains a path separator; a
// relative path ("./bin/tool", "main.py" as an arg) is relative to the
// extension folder, which is also the working directory.
type RunSpec struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Contributes lists what a process extension offers before it runs, so
// it can start lazily: its commands are in the palette at once and the
// first run starts the process.
type Contributes struct {
	Commands []Command `json:"commands,omitempty"`
}

// StartsAtStartup reports whether a process extension starts when
// termocode starts. It does with "onStartup" or "*", and when it declares
// neither activation events nor commands (nothing else could start it).
func (m Manifest) StartsAtStartup() bool {
	for _, e := range m.ActivationEvents {
		if e == "*" || e == "onStartup" {
			return true
		}
	}
	return len(m.ActivationEvents) == 0 && len(m.Contributes.Commands) == 0
}

// ActivatesOn reports whether event (an editor event name such as "save"
// or "open") starts the extension: activation event "on" + Event.
func (m Manifest) ActivatesOn(event string) bool {
	for _, e := range m.ActivationEvents {
		if strings.EqualFold(e, "on"+event) {
			return true
		}
	}
	return false
}
