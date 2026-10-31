// Package aihooks runs the user's commands after AI tool calls (OMX-12):
// a formatter after a file write, a linter after an edit. Hooks live in
// a JSON file:
//
//	{"post_tool": [
//	  {"tools": ["write_file", "create_file"], "run": "gofmt -w \"$TERMOCODE_TOOL_PATH\""}
//	]}
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
	"path/filepath"
	"runtime"
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

// DefaultTimeout and MaxTimeout bound one hook run.
const (
	DefaultTimeout = 30 * time.Second
	MaxTimeout     = 5 * time.Minute
)

// OutputMax caps the output kept of one run.
const OutputMax = 4 << 10

// Hook is one post-tool command.
type Hook struct {
	Tools    []string `json:"tools"` // tool names; "*" (or empty) is every tool
	Run      string   `json:"run"`
	TimeoutS int      `json:"timeout_s,omitempty"`
	Source   string   `json:"-"`
}

// Matches reports whether the hook runs after tool.
func (h Hook) Matches(tool string) bool {
	if len(h.Tools) == 0 {
		return true
	}
	for _, t := range h.Tools {
		if t == "*" || t == tool {
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
	PostTool    []Hook
	Errors      []string // broken files: "<file>: <why>"
	ProjectHash string   // sha256 of the project file ("" when it has no hooks)
}

// Project returns the project's hooks.
func (s Set) Project() []Hook {
	var out []Hook
	for _, h := range s.PostTool {
		if h.Source == SourceProject {
			out = append(out, h)
		}
	}
	return out
}

// Parse reads one hooks file.
func Parse(data []byte, source string) ([]Hook, error) {
	var doc struct {
		PostTool []Hook `json:"post_tool"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, errors.New(strings.TrimPrefix(err.Error(), "json: "))
	}
	for i := range doc.PostTool {
		h := &doc.PostTool[i]
		h.Run = strings.TrimSpace(h.Run)
		if h.Run == "" {
			return nil, fmt.Errorf("post_tool[%d]: no run command", i)
		}
		h.Source = source
	}
	return doc.PostTool, nil
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
		s.PostTool = append(s.PostTool, hs...)
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

// Call is the tool call a hook runs after.
type Call struct {
	Tool string
	Path string // the call's "path" argument, made absolute ("" for none)
	Root string // the workspace
}

// Result is one finished hook run.
type Result struct {
	Exit     int // -1: did not start or was stopped
	Output   string
	Duration time.Duration
	Err      error // start failure or timeout
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
	cmd.Dir = call.Root
	cmd.Env = append(os.Environ(), "TERMOCODE_TOOL="+call.Tool, "TERMOCODE_TOOL_PATH="+call.Path,
		"TERMOCODE_WORKSPACE="+call.Root, "GIT_TERMINAL_PROMPT=0", "CI=1")
	cmd.WaitDelay = 2 * time.Second // a child that keeps the pipe open
	var out capped
	cmd.Stdout, cmd.Stderr = &out, &out
	start := time.Now()
	err := cmd.Run()
	r := Result{Exit: 0, Duration: time.Since(start), Output: ai.Redact(strings.TrimSpace(out.String()))}
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
