// Package aihooks runs the user's commands on AI events (OMX-12,
// CMP-59): a formatter after a file write, a guard before a shell call,
// a check when the agent stops. Hooks live in a JSON file:
//
//	{"post_tool": [
//	  {"tools": ["write_file", "create_file"], "run": "gofmt -w \"$TERMOCODE_TOOL_PATH\""}
//	],
//	 "pre_tool": [
//	  {"tools": ["run_command", "mcp__*"], "run": "./guard.sh"}
//	 ],
//	 "file_save": [{"files": ["*.go"], "run": "go vet ./..."}],
//	 "stop": [{"run": "./check-done.sh"}],
//	 "pre_compact": [{"run": "./save-notes.sh"}], "post_compact": [{"run": "..."}]}
//
// Events: post_tool runs after a tool call worked; pre_tool runs before
// the call (before any approval card) and can block it, change its
// arguments or (user hooks only) allow it without the card; file_save
// runs after the user saves a file in the editor; stop runs when the
// agent ends a turn and can ask it to go on; pre_compact runs before the
// older chat is summarised (and can stop that), post_compact after it.
// tools entries are glob patterns ("mcp__github__*" matches every tool
// of that MCP server); files entries match the saved file's workspace
// path or its base name.
//
// A hook gets the event as JSON on stdin ({"event", "tool", "path",
// "args", "workspace", "summary"}). Its answer: exit 2 blocks (the
// output is the reason); exit 0 with a JSON object on stdout
// ({"decision": "deny"|"allow", "reason": "…", "args": {…}}) decides;
// any other exit is a failed hook that decides nothing.
//
// The user's file (<config>/termocode/ai/hooks.json) is the user's own;
// a project's file (.termocode/hooks.json) comes from the repository and
// runs only after the user trusts that exact content (the app's card).
// A hook runs in the workspace with sh -c (cmd /C on Windows), gets the
// call in TERMOCODE_TOOL / TERMOCODE_TOOL_PATH / TERMOCODE_WORKSPACE, and
// is stopped after its timeout. Its output is capped and redacted.
package aihooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// ProjectFile is the project hooks file, relative to the workspace root.
const ProjectFile = ".termocode/hooks.json"

// Hook sources.
const (
	SourceUser    = "user"
	SourceProject = "project"
)

// Hook events: the keys of a hooks file.
const (
	EventPostTool    = "post_tool"
	EventPreTool     = "pre_tool"
	EventFileSave    = "file_save"
	EventStop        = "stop"
	EventPreCompact  = "pre_compact"
	EventPostCompact = "post_compact"
)

// Events are every hook event, in file order.
var Events = []string{EventPreTool, EventPostTool, EventFileSave, EventStop, EventPreCompact, EventPostCompact}

// DefaultTimeout and MaxTimeout bound one hook run.
const (
	DefaultTimeout = 30 * time.Second
	MaxTimeout     = 5 * time.Minute
)

// OutputMax caps the output kept of one run.
const OutputMax = 4 << 10

// Hook is one command on one event.
type Hook struct {
	Tools    []string `json:"tools"`           // tool name globs; "*" (or empty) is every tool
	Files    []string `json:"files,omitempty"` // file_save: path globs; empty is every file
	Run      string   `json:"run"`
	TimeoutS int      `json:"timeout_s,omitempty"`
	Source   string   `json:"-"`
	Event    string   `json:"-"`
}

// Matches reports whether the hook runs for tool ("mcp__*" style globs
// match MCP tools).
func (h Hook) Matches(tool string) bool {
	return globAny(h.Tools, tool)
}

// MatchesFile reports whether a file_save hook runs for rel, the saved
// file's slash path in the workspace (or its absolute path outside it).
func (h Hook) MatchesFile(rel string) bool {
	if len(h.Files) == 0 {
		return true
	}
	return globAny(h.Files, rel) || globAny(h.Files, path.Base(rel))
}

// globAny reports whether name matches one of pats (none: every name).
func globAny(pats []string, name string) bool {
	if len(pats) == 0 {
		return true
	}
	for _, p := range pats {
		if p == "*" || p == name {
			return true
		}
		if ok, err := path.Match(p, name); err == nil && ok {
			return true
		}
	}
	return false
}

// Timeout is the hook's time limit.
func (h Hook) Timeout() time.Duration {
	if h.TimeoutS <= 0 {
		return DefaultTimeout
	}
	return min(time.Duration(h.TimeoutS)*time.Second, MaxTimeout)
}

// Set is the hooks of the user and of a project.
type Set struct {
	PostTool    []Hook   // post_tool hooks (kept apart: the oldest event)
	Other       []Hook   // the hooks of every other event, in file order
	Errors      []string // broken files: "<file>: <why>"
	ProjectHash string   // sha256 of the project file ("" when it has no hooks)
}

// All returns every hook of s.
func (s Set) All() []Hook { return append(append([]Hook(nil), s.PostTool...), s.Other...) }

// Project returns the project's hooks.
func (s Set) Project() []Hook {
	var out []Hook
	for _, h := range s.All() {
		if h.Source == SourceProject {
			out = append(out, h)
		}
	}
	return out
}

// Of returns the hooks of event in hs.
func Of(hs []Hook, event string) []Hook {
	var out []Hook
	for _, h := range hs {
		if h.Event == event {
			out = append(out, h)
		}
	}
	return out
}

// Parse reads one hooks file: its hooks of every event, in Events order.
func Parse(data []byte, source string) ([]Hook, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, errors.New(strings.TrimPrefix(err.Error(), "json: "))
	}
	for k := range doc {
		if !slices.Contains(Events, k) && !strings.HasPrefix(k, "$") && !strings.HasPrefix(k, "_") { // "$schema", "_comment"
			return nil, fmt.Errorf("unknown event %q (one of %s)", k, strings.Join(Events, ", "))
		}
	}
	var out []Hook
	for _, ev := range Events {
		raw, ok := doc[ev]
		if !ok {
			continue
		}
		var hs []Hook
		if err := json.Unmarshal(raw, &hs); err != nil {
			return nil, fmt.Errorf("%s: %s", ev, strings.TrimPrefix(err.Error(), "json: "))
		}
		for i := range hs {
			h := &hs[i]
			h.Run = strings.TrimSpace(h.Run)
			if h.Run == "" {
				return nil, fmt.Errorf("%s[%d]: no run command", ev, i)
			}
			for _, p := range append(append([]string(nil), h.Tools...), h.Files...) {
				if _, err := path.Match(p, ""); err != nil {
					return nil, fmt.Errorf("%s[%d]: bad pattern %q", ev, i, p)
				}
			}
			h.Source, h.Event = source, ev
		}
		out = append(out, hs...)
	}
	return out, nil
}

// Load reads the user file ("" for none) and the project file of root.
// A missing file is no hooks; a broken one is named in Errors.
func Load(root, userFile string) Set {
	var s Set
	read := func(path, source string) {
		if path == "" {
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				s.Errors = append(s.Errors, path+": "+err.Error())
			}
			return
		}
		hs, err := Parse(b, source)
		if err != nil {
			s.Errors = append(s.Errors, path+": "+err.Error())
			return
		}
		for _, h := range hs {
			if h.Event == EventPostTool {
				s.PostTool = append(s.PostTool, h)
			} else {
				s.Other = append(s.Other, h)
			}
		}
		if source == SourceProject && len(hs) > 0 {
			sum := sha256.Sum256(b)
			s.ProjectHash = hex.EncodeToString(sum[:])
		}
	}
	read(userFile, SourceUser)
	if root != "" {
		read(filepath.Join(root, filepath.FromSlash(ProjectFile)), SourceProject)
	}
	return s
}

// Call is the event a hook runs on.
type Call struct {
	Event   string         // "" is the hook's own event
	Tool    string         // tool events
	Path    string         // the call's "path" argument or the saved file, made absolute ("" for none)
	Root    string         // the workspace
	Args    map[string]any // pre_tool / post_tool: the call's arguments
	Summary string         // post_compact: the new summary
}

// Result is one finished hook run.
type Result struct {
	Exit     int // -1: did not start or was stopped
	Output   string
	Duration time.Duration
	Err      error  // start failure or timeout
	raw      string // the output before redaction (the decision JSON)
}

// Decision is what a hook's run asks for (pre_tool, stop, pre_compact).
type Decision struct {
	Block  bool           // do not run the call / do not stop / do not compact
	Allow  bool           // pre_tool: run without the approval card
	Reason string         // why (redacted)
	Args   map[string]any // pre_tool: run the call with these arguments
}

// Decide reads r as an answer: exit 2 blocks with the output as the
// reason; exit 0 with a JSON object decides; anything else decides
// nothing (a failed hook never blocks).
func Decide(r Result) Decision {
	if r.Err != nil {
		return Decision{}
	}
	switch r.Exit {
	case 2:
		why := r.Output
		if why == "" {
			why = "blocked by a hook"
		}
		return Decision{Block: true, Reason: why}
	case 0:
	default:
		return Decision{}
	}
	t := strings.TrimSpace(r.raw)
	if !strings.HasPrefix(t, "{") {
		return Decision{}
	}
	var doc struct {
		Decision string         `json:"decision"`
		Reason   string         `json:"reason"`
		Args     map[string]any `json:"args"`
	}
	if json.Unmarshal([]byte(t), &doc) != nil {
		return Decision{}
	}
	d := Decision{Reason: ai.Redact(strings.TrimSpace(doc.Reason)), Args: doc.Args}
	switch strings.ToLower(doc.Decision) {
	case "deny", "block":
		d.Block, d.Args = true, nil
		if d.Reason == "" {
			d.Reason = "blocked by a hook"
		}
	case "allow", "approve":
		d.Allow = true
	}
	return d
}

// input is the JSON a hook reads on stdin.
func (c Call) input(event string) []byte {
	b, _ := json.Marshal(struct {
		Event     string         `json:"event"`
		Tool      string         `json:"tool,omitempty"`
		Path      string         `json:"path,omitempty"`
		Args      map[string]any `json:"args,omitempty"`
		Workspace string         `json:"workspace"`
		Summary   string         `json:"summary,omitempty"`
	}{event, c.Tool, c.Path, c.Args, c.Root, c.Summary})
	return b
}

// Run runs h after call and waits for it.
func Run(ctx context.Context, h Hook, call Call) Result {
	ctx, cancel := context.WithTimeout(ctx, h.Timeout())
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = spawn.CommandContext(ctx, "cmd", "/C", h.Run)
	} else {
		cmd = spawn.CommandContext(ctx, "sh", "-c", h.Run)
	}
	event := call.Event
	if event == "" {
		event = h.Event
	}
	if event == "" {
		event = EventPostTool
	}
	cmd.Dir = call.Root
	cmd.Env = append(os.Environ(), "TERMOCODE_HOOK_EVENT="+event, "TERMOCODE_TOOL="+call.Tool, "TERMOCODE_TOOL_PATH="+call.Path,
		"TERMOCODE_WORKSPACE="+call.Root, "GIT_TERMINAL_PROMPT=0", "CI=1")
	cmd.Stdin = bytes.NewReader(call.input(event))
	cmd.WaitDelay = 2 * time.Second // a child that keeps the pipe open
	var out capped
	cmd.Stdout, cmd.Stderr = &out, &out
	start := time.Now()
	err := cmd.Run()
	raw := strings.TrimSpace(out.String())
	r := Result{Exit: 0, Duration: time.Since(start), Output: ai.Redact(raw), raw: raw}
	var ee *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		r.Exit, r.Err = -1, fmt.Errorf("stopped after %s", h.Timeout())
	case errors.As(err, &ee):
		r.Exit = ee.ExitCode()
	case err != nil:
		r.Exit, r.Err = -1, err
	}
	return r
}

// capped keeps the first OutputMax bytes written to it.
type capped struct {
	b    bytes.Buffer
	more bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := OutputMax - c.b.Len(); room > 0 {
		c.b.Write(p[:min(room, len(p))])
		c.more = c.more || len(p) > room
	} else {
		c.more = true
	}
	return len(p), nil
}

func (c *capped) String() string {
	if c.more {
		return c.b.String() + "\n…"
	}
	return c.b.String()
}
