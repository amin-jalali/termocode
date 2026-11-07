// Package promptactions keeps the user's saved prompts ("prompt
// actions", CMP-74): a name, an optional group, the prompt text and where
// it runs (the AI chat or the active terminal tab, where a coding agent
// may wait). They live in <config>/termocode/prompt-actions.json, which
// the user may also edit by hand, export and import.
//
// The file holds only what the user typed; it is never sent anywhere
// until the user picks an action.
package promptactions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/cfgfile"
)

// FileName is the file in the termocode config folder.
const FileName = "prompt-actions.json"

// Version is the file version this build writes.
const Version = 1

// Limits of one file.
const (
	MaxActions   = 200
	MaxName      = 80
	MaxPrompt    = 16 * 1024
	MaxFileBytes = 1 << 20
)

// Targets: where an action's prompt goes.
const (
	TargetChat     = "chat"     // the AI chat (the default)
	TargetTerminal = "terminal" // typed into the active terminal tab, then Enter
)

// Action is one saved prompt.
type Action struct {
	Name   string `json:"name"`
	Group  string `json:"group,omitempty"`
	Prompt string `json:"prompt"`
	Target string `json:"target,omitempty"` // TargetChat ("" too) or TargetTerminal
}

// Key names an action in its file: "group/name" in lower case ("name"
// without a group). Two actions with one key are the same action.
func (a Action) Key() string {
	n := strings.ToLower(a.Name)
	if a.Group == "" {
		return n
	}
	return strings.ToLower(a.Group) + "/" + n
}

// TargetOrChat is the action's target, TargetChat when not set.
func (a Action) TargetOrChat() string {
	if a.Target == "" {
		return TargetChat
	}
	return a.Target
}

// Title is "Group · Name", or the name alone.
func (a Action) Title() string {
	if a.Group == "" {
		return a.Name
	}
	return a.Group + " · " + a.Name
}

type file struct {
	Version int      `json:"version"`
	Actions []Action `json:"actions"`
}

// check says what is wrong with a ("" when nothing).
func (a Action) check() string {
	switch {
	case strings.TrimSpace(a.Name) == "":
		return "no name"
	case len(a.Name) > MaxName || len(a.Group) > MaxName:
		return "the name is too long"
	case strings.TrimSpace(a.Prompt) == "":
		return "no prompt"
	case len(a.Prompt) > MaxPrompt:
		return "the prompt is over 16 KB"
	case a.Target != "" && a.Target != TargetChat && a.Target != TargetTerminal:
		return fmt.Sprintf("target %q is not chat or terminal", a.Target)
	}
	return ""
}

// Parse reads a file's bytes. Bad rows are left out, each with a note;
// broken JSON or a newer version is an error. An empty file is empty.
func Parse(b []byte) (list []Action, notes []string, err error) {
	if strings.TrimSpace(string(b)) == "" {
		return nil, nil, nil
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, nil, fmt.Errorf("not valid JSON: %w", err)
	}
	if f.Version > Version {
		return nil, nil, fmt.Errorf("version %d is newer than this termocode reads (%d)", f.Version, Version)
	}
	seen := map[string]bool{}
	for i, a := range f.Actions {
		a.Name, a.Group = strings.TrimSpace(a.Name), strings.TrimSpace(a.Group)
		if why := a.check(); why != "" {
			notes = append(notes, fmt.Sprintf("action %d: %s", i+1, why))
			continue
		}
		if seen[a.Key()] {
			notes = append(notes, fmt.Sprintf("action %d: %q is there twice", i+1, a.Title()))
			continue
		}
		if len(list) == MaxActions {
			notes = append(notes, fmt.Sprintf("more than %d actions: the rest is left out", MaxActions))
			break
		}
		seen[a.Key()] = true
		list = append(list, a)
	}
	return list, notes, nil
}

// Marshal is the file for list (sorted, indented).
func Marshal(list []Action) ([]byte, error) {
	if len(list) > MaxActions {
		return nil, fmt.Errorf("more than %d prompt actions", MaxActions)
	}
	for _, a := range list {
		if why := a.check(); why != "" {
			return nil, fmt.Errorf("%q: %s", a.Title(), why)
		}
	}
	b, err := json.MarshalIndent(file{Version: Version, Actions: Sorted(list)}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Load reads the file at path; a missing file is empty.
func Load(path string) ([]Action, []string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(b) > MaxFileBytes {
		return nil, nil, fmt.Errorf("the file is over %d KB", MaxFileBytes>>10)
	}
	return Parse(b)
}

// Save writes list to path (through a temp file and a rename).
func Save(path string, list []Action) error {
	b, err := Marshal(list)
	if err != nil {
		return err
	}
	return cfgfile.WriteFile(path, b)
}

// Merge adds add to have: an action with a key that is there replaces
// it, the others are added.
func Merge(have, add []Action) (out []Action, added, replaced int) {
	out = append([]Action(nil), have...)
	at := make(map[string]int, len(out))
	for i, a := range out {
		at[a.Key()] = i
	}
	for _, a := range add {
		if i, ok := at[a.Key()]; ok {
			out[i] = a
			replaced++
			continue
		}
		at[a.Key()] = len(out)
		out = append(out, a)
		added++
	}
	return out, added, replaced
}

// Sorted is a copy of list: actions with no group first, then by group,
// then by name (case does not count).
func Sorted(list []Action) []Action {
	out := append([]Action(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		gi, gj := strings.ToLower(out[i].Group), strings.ToLower(out[j].Group)
		if gi != gj {
			return gi < gj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Find is the action with key.
func Find(list []Action, key string) (Action, bool) {
	for _, a := range list {
		if a.Key() == key {
			return a, true
		}
	}
	return Action{}, false
}

// ParseRef splits "Group/Name" (the first slash; no slash: no group).
func ParseRef(s string) (group, name string, err error) {
	s = strings.TrimSpace(s)
	if g, n, ok := strings.Cut(s, "/"); ok {
		group, name = strings.TrimSpace(g), strings.TrimSpace(n)
	} else {
		name = s
	}
	switch {
	case name == "":
		return "", "", errors.New("the action needs a name")
	case len(name) > MaxName || len(group) > MaxName:
		return "", "", fmt.Errorf("the name is longer than %d characters", MaxName)
	}
	return group, name, nil
}
