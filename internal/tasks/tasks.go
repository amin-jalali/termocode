// Package tasks loads workspace tasks for termocode's "Run Task…" flow.
//
// Tasks come from three places, in this order (first label wins):
//
//  1. .termocode/tasks.json in the workspace root (Load),
//  2. the user's commands.json entries (the app converts them),
//  3. auto-detection from project files — go.mod, package.json scripts,
//     Makefile targets, Cargo.toml, pytest config, justfile recipes
//     (Detect).
//
// The package is pure: it reads files but never runs anything. The app
// runs a task in a PTY terminal tab and feeds the output to Match
// (matchers.go) to turn compiler errors into diagnostics.
package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Task groups. A task with no group is plain "run anything".
const (
	GroupBuild = "build"
	GroupTest  = "test"
)

// Task is one runnable command.
type Task struct {
	// Label is the unique display name ("go: build", "npm: dev", …).
	Label string
	// Command is a shell command line. Args (optional) are appended,
	// shell-quoted.
	Command string
	Args    []string
	// Cwd is the working directory. Empty means the workspace root; a
	// relative path is resolved against the root.
	Cwd string
	// Env holds extra environment variables.
	Env map[string]string
	// Group is GroupBuild, GroupTest or "".
	Group string
	// IsDefault marks the default task of its group (Build / Test).
	IsDefault bool
	// ProblemMatcher names the matchers to run on the output ("$go",
	// "$tsc", …). Empty means no matching. See matchers.go.
	ProblemMatcher []string
	// Source tells where the task came from: "tasks.json", "user", "go",
	// "npm", "make", "cargo", "pytest", "just".
	Source string
}

// ConfigPath returns the tasks.json path for a workspace root.
func ConfigPath(root string) string {
	return filepath.Join(root, ".termocode", "tasks.json")
}

// rawTask is the on-disk shape. group and problemMatcher accept either a
// string or an object / list, like VS Code.
type rawTask struct {
	Label          string            `json:"label"`
	Command        string            `json:"command"`
	Args           []string          `json:"args"`
	Cwd            string            `json:"cwd"`
	Env            map[string]string `json:"env"`
	Group          json.RawMessage   `json:"group"`
	ProblemMatcher json.RawMessage   `json:"problemMatcher"`
}

type rawFile struct {
	Version any       `json:"version"`
	Tasks   []rawTask `json:"tasks"`
}

// Load reads <root>/.termocode/tasks.json. A missing file returns
// (nil, nil). A malformed file returns an error and no tasks, so the
// caller can show it — it never blocks startup.
func Load(root string) ([]Task, error) {
	data, err := os.ReadFile(ConfigPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return Parse(data)
}

// Parse decodes tasks.json content. It accepts {"tasks": [...]} or a bare
// array. Entries without a label or command are skipped.
func Parse(data []byte) ([]Task, error) {
	var raws []rawTask
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(data, &raws); err != nil {
			return nil, fmt.Errorf("tasks.json: %w", err)
		}
	} else {
		var f rawFile
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("tasks.json: %w", err)
		}
		raws = f.Tasks
	}
	out := make([]Task, 0, len(raws))
	for _, r := range raws {
		if strings.TrimSpace(r.Label) == "" || strings.TrimSpace(r.Command) == "" {
			continue
		}
		t := Task{
			Label:   r.Label,
			Command: r.Command,
			Args:    r.Args,
			Cwd:     r.Cwd,
			Env:     r.Env,
			Source:  "tasks.json",
		}
		t.Group, t.IsDefault = parseGroup(r.Group)
		t.ProblemMatcher = parseMatchers(r.ProblemMatcher)
		out = append(out, t)
	}
	return out, nil
}

// parseGroup accepts "build" or {"kind": "build", "isDefault": true}.
func parseGroup(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return normGroup(s), false
	}
	var obj struct {
		Kind      string `json:"kind"`
		IsDefault bool   `json:"isDefault"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return normGroup(obj.Kind), obj.IsDefault
	}
	return "", false
}

func normGroup(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case GroupBuild:
		return GroupBuild
	case GroupTest:
		return GroupTest
	}
	return ""
}

// parseMatchers accepts "$go" or ["$go", "$tsc"].
func parseMatchers(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s = strings.TrimSpace(s); s != "" {
			return []string{s}
		}
		return nil
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		var out []string
		for _, v := range list {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	return nil
}

// Template is the starter tasks.json written by "Tasks: Configure Tasks".
const Template = `{
  "version": 1,
  "tasks": [
    {
      "label": "build",
      "command": "echo",
      "args": ["edit .termocode/tasks.json"],
      "group": { "kind": "build", "isDefault": true },
      "problemMatcher": "$generic"
    }
  ]
}
`

// Marshal renders tasks as a tasks.json document (used to seed the file
// with the detected tasks).
func Marshal(list []Task) string {
	type outTask struct {
		Label          string            `json:"label"`
		Command        string            `json:"command"`
		Args           []string          `json:"args,omitempty"`
		Cwd            string            `json:"cwd,omitempty"`
		Env            map[string]string `json:"env,omitempty"`
		Group          any               `json:"group,omitempty"`
		ProblemMatcher any               `json:"problemMatcher,omitempty"`
	}
	f := struct {
		Version int       `json:"version"`
		Tasks   []outTask `json:"tasks"`
	}{Version: 1}
	for _, t := range list {
		o := outTask{Label: t.Label, Command: t.Command, Args: t.Args, Cwd: t.Cwd, Env: t.Env}
		if t.Group != "" {
			if t.IsDefault {
				o.Group = map[string]any{"kind": t.Group, "isDefault": true}
			} else {
				o.Group = t.Group
			}
		}
		switch len(t.ProblemMatcher) {
		case 0:
		case 1:
			o.ProblemMatcher = t.ProblemMatcher[0]
		default:
			o.ProblemMatcher = t.ProblemMatcher
		}
		f.Tasks = append(f.Tasks, o)
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	return string(b) + "\n"
}

// Merge joins task lists in priority order. A later task whose label is
// already taken is dropped.
func Merge(lists ...[]Task) []Task {
	seen := map[string]bool{}
	var out []Task
	for _, l := range lists {
		for _, t := range l {
			if seen[t.Label] {
				continue
			}
			seen[t.Label] = true
			out = append(out, t)
		}
	}
	return out
}

// DefaultFor returns the task for a group: the first one marked
// IsDefault, else the first one in the group. ok=false when none.
func DefaultFor(list []Task, group string) (Task, bool) {
	for _, t := range list {
		if t.Group == group && t.IsDefault {
			return t, true
		}
	}
	for _, t := range list {
		if t.Group == group {
			return t, true
		}
	}
	return Task{}, false
}

// Find returns the task with the given label.
func Find(list []Task, label string) (Task, bool) {
	for _, t := range list {
		if t.Label == label {
			return t, true
		}
	}
	return Task{}, false
}

// Vars are the values substituted into ${…} variables.
type Vars struct {
	WorkspaceFolder string
	File            string // absolute path of the active file ("" when none)
}

// Expand replaces ${workspaceFolder}, ${workspaceFolderBasename},
// ${file}, ${fileDirname}, ${fileBasename}, ${fileBasenameNoExtension},
// ${relativeFile} and ${env:NAME}. Unknown variables stay as they are.
func Expand(s string, v Vars) string {
	if !strings.Contains(s, "${") {
		return s
	}
	// File variables are empty when no file is open.
	var rel, dir, base string
	if v.File != "" {
		rel = v.File
		if r, err := filepath.Rel(v.WorkspaceFolder, v.File); err == nil {
			rel = r
		}
		dir = filepath.Dir(v.File)
		base = filepath.Base(v.File)
	}
	repl := map[string]string{
		"workspaceFolder":         v.WorkspaceFolder,
		"workspaceRoot":           v.WorkspaceFolder,
		"workspaceFolderBasename": filepath.Base(v.WorkspaceFolder),
		"file":                    v.File,
		"fileDirname":             dir,
		"fileBasename":            base,
		"fileBasenameNoExtension": strings.TrimSuffix(base, filepath.Ext(base)),
		"relativeFile":            rel,
		"cwd":                     v.WorkspaceFolder,
	}
	var b strings.Builder
	for {
		i := strings.Index(s, "${")
		if i < 0 {
			b.WriteString(s)
			break
		}
		j := strings.IndexByte(s[i:], '}')
		if j < 0 {
			b.WriteString(s)
			break
		}
		name := s[i+2 : i+j]
		b.WriteString(s[:i])
		if strings.HasPrefix(name, "env:") {
			b.WriteString(os.Getenv(strings.TrimPrefix(name, "env:")))
		} else if val, ok := repl[name]; ok {
			b.WriteString(val)
		} else {
			b.WriteString(s[i : i+j+1])
		}
		s = s[i+j+1:]
	}
	return b.String()
}

// CommandLine returns the full shell command: Command plus shell-quoted
// Args, with variables expanded.
func (t Task) CommandLine(v Vars) string {
	parts := []string{Expand(t.Command, v)}
	for _, a := range t.Args {
		parts = append(parts, ShellQuote(Expand(a, v)))
	}
	return strings.Join(parts, " ")
}

// Dir returns the absolute working directory for the task.
func (t Task) Dir(v Vars) string {
	cwd := Expand(t.Cwd, v)
	if cwd == "" {
		return v.WorkspaceFolder
	}
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(v.WorkspaceFolder, cwd)
	}
	return cwd
}

// ShellQuote quotes s for a POSIX shell when it holds special characters.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("-_./=:,+@%", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
