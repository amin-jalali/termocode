package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/search"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// Tool is one capability the agent can call. Port of mobocode's AiTool:
// read tools run freely; Mutating tools pass the confirm gate first, unless
// the user auto-approved their Class (fileWrite / git / run). Run returns
// a string for the model; errors are reported to the model as text.
type Tool struct {
	Name        string
	Description string
	Params      map[string]any // JSON-Schema properties
	Required    []string
	// Schema (optional) is a whole JSON Schema object sent as is by Spec
	// (MCP tools bring one with $defs, anyOf …). Params stays the list the
	// text protocol shows.
	Schema map[string]any
	// Display (optional) is the name people see in the confirm dialog
	// ("mcp.linear.create_issue"); Name is the wire name the model calls.
	Display  string
	Mutating bool
	Class    string
	Describe func(args map[string]any) string
	// Validate (optional) rejects bad args before the confirm gate, so
	// the user is never asked about a call that cannot run.
	Validate func(args map[string]any) error
	// Gate (optional) decides the confirm rule per call and replaces
	// Mutating / Class for it: one tool (execute_action) runs a safe
	// command at once and asks for a risky one. Class "" with mutating
	// true always asks, with no "Always allow" button.
	Gate func(args map[string]any) (mutating bool, class string)
	// Release (optional) drops whatever the tool cached for this args map
	// (Describe, Validate, Gate and Run share one parsed request, keyed
	// by the map). The agent calls it before a call and when the call is
	// over, whether it was run, refused, declined or panicked, so a later
	// map can never meet an old entry.
	Release func(args map[string]any)
	Run     func(ctx context.Context, args map[string]any) (string, error)
}

// Spec is the native tool-calling description of t.
func (t Tool) Spec() ToolSpec {
	if t.Schema != nil {
		return ToolSpec{Name: t.Name, Description: t.Description, Schema: t.Schema}
	}
	props := t.Params
	if props == nil {
		props = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(t.Required) > 0 {
		schema["required"] = t.Required
	}
	return ToolSpec{Name: t.Name, Description: t.Description, Schema: schema}
}

// Label is the name shown to people: Display, or Name.
func (t Tool) Label() string {
	if t.Display != "" {
		return t.Display
	}
	return t.Name
}

// Human returns the one-line description of a specific call (shown in the
// confirm dialog and the chat transcript).
func (t Tool) Human(args map[string]any) string {
	if t.Describe != nil {
		return t.Describe(args)
	}
	return t.Name
}

// Auto-approve classes for mutating tools.
const (
	ClassFileWrite = "fileWrite"
	ClassGit       = "git"
	ClassRun       = "run"
	ClassIDE       = "ide"      // IDE commands the agent runs (execute_action)
	ClassSettings  = "settings" // settings_set: change a config.json setting
	ClassServices  = "services" // services_start / stop / restart (the Services view)
	// ClassContainers is the card class of host container actions
	// (containers_control, images_pull, compose_up). Its "Always allow"
	// lasts for the session only: it is not in ToolClasses and is never
	// saved in ai_auto_approve (a host action can touch any project).
	ClassContainers = "containers"
	// ClassReview marks a file-changing tool that shows its change as a
	// diff and waits for the user's accept / reject itself (Group R): no
	// confirm dialog, never auto-approved. Not in ToolClasses.
	ClassReview = "review"
)

// FindToolClass reports whether any tool has class c.
func FindToolClass(tools []Tool, c string) bool {
	for _, t := range tools {
		if t.Class == c {
			return true
		}
	}
	return false
}

// ToolClasses lists the auto-approve classes in UI order.
var ToolClasses = []string{ClassFileWrite, ClassGit, ClassRun, ClassIDE, ClassSettings, ClassServices}

// FindTool looks a tool up by name.
func FindTool(tools []Tool, name string) (Tool, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// ToolsPromptSpec renders the catalog for the text (non-native) protocol.
func ToolsPromptSpec(tools []Tool) string {
	var b strings.Builder
	for _, t := range tools {
		var args []string
		keys := make([]string, 0, len(t.Params))
		for k := range t.Params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			typ := "string"
			if p, ok := t.Params[k].(map[string]any); ok {
				if s, ok := p["type"].(string); ok {
					typ = s
				}
			}
			opt := "?"
			for _, r := range t.Required {
				if r == k {
					opt = ""
				}
			}
			args = append(args, fmt.Sprintf("%q%s: %s", k, opt, typ))
		}
		gate := ""
		if t.Class == ClassReview {
			gate = " (the user reviews the change as a diff)"
		} else if t.Gate != nil {
			gate = " (some calls need user confirmation)"
		} else if t.Mutating {
			gate = " (mutating — needs user confirmation)"
		}
		fmt.Fprintf(&b, "- %s {%s} — %s%s\n", t.Name, strings.Join(args, ", "), t.Description, gate)
	}
	return b.String()
}

// ArgString returns args[key] as a string.
func ArgString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// ArgInt returns args[key] as an int (JSON numbers decode as float64).
func ArgInt(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		var n int
		fmt.Sscan(v, &n)
		return n
	}
	return 0
}

// ArgBool returns args[key] as a bool.
func ArgBool(args map[string]any, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

func strParam(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// Workspace is what the built-in tools operate on.
type Workspace struct {
	Root       string
	RunTimeout time.Duration
}

// ErrOutsideWorkspace is returned for paths escaping the root.
var ErrOutsideWorkspace = errors.New("path is outside the workspace")

// Resolve maps a tool path (relative or absolute) to an absolute path
// inside the workspace root.
func (w Workspace) Resolve(p string) (string, error) {
	root, err := filepath.Abs(w.Root)
	if err != nil {
		return "", err
	}
	if p == "" || p == "." {
		return root, nil
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, p)
	}
	abs = filepath.Clean(abs)
	if !within(root, abs) {
		return "", ErrOutsideWorkspace
	}
	// A symlink in the workspace may point outside it (a cloned repo can
	// ship one): the real place of the path must be inside the real root.
	real, realRoot := realPath(abs, 0), realPath(root, 0)
	if real == "" || realRoot == "" || !within(realRoot, real) {
		return "", ErrOutsideWorkspace
	}
	return abs, nil
}

// argsInside is a Validate for file tools: each named path argument must
// resolve inside the workspace, so a call that could never run is
// refused before the user is asked.
func (w Workspace) argsInside(keys ...string) func(map[string]any) error {
	return func(a map[string]any) error {
		for _, k := range keys {
			if p := ArgString(a, k); p != "" {
				if _, err := w.Resolve(p); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

// within reports whether p is root or lies under it.
func within(root, p string) bool {
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
}

// realPath is p with every symlink of its existing part followed, the
// missing rest kept as it is. A dangling link is followed by its text, so
// writing through it is judged by where the write would land. "" means
// it can not be told (a link loop).
func realPath(p string, depth int) string {
	if depth > 40 {
		return ""
	}
	cur, rest := p, ""
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		if fi, err := os.Lstat(cur); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(cur)
			if err != nil {
				return ""
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(cur), target)
			}
			return realPath(filepath.Join(target, rest), depth+1)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Join(cur, rest)
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

func (w Workspace) rel(abs string) string {
	if r, err := filepath.Rel(w.Root, abs); err == nil {
		return r
	}
	return abs
}

const (
	maxReadBytes = 120 * 1024
	maxOutBytes  = 16 * 1024
)

func capOut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("\n… [truncated, %d more bytes]", len(s)-n)
}

// WorkspaceTools returns the filesystem / search / git / run tools — the
// app adds editor-backed (LSP) tools on top.
func WorkspaceTools(w Workspace) []Tool {
	path := func(args map[string]any) string { return ArgString(args, "path") }
	inside := w.argsInside("path")
	return []Tool{
		{
			Name: "read_file", Description: "Read a file and return its contents.",
			Params: map[string]any{"path": strParam("file path, relative to the workspace")}, Required: []string{"path"},
			Describe: func(a map[string]any) string { return "Read " + path(a) },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				abs, err := w.Resolve(path(a))
				if err != nil {
					return "", err
				}
				b, err := os.ReadFile(abs)
				if err != nil {
					return "", err
				}
				return capOut(string(b), maxReadBytes), nil
			},
		},
		{
			Name: "list_dir", Description: "List the entries of a directory.",
			Params:   map[string]any{"path": strParam("directory, relative to the workspace (default: root)")},
			Describe: func(a map[string]any) string { return "List " + orDot(path(a)) },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				abs, err := w.Resolve(path(a))
				if err != nil {
					return "", err
				}
				ents, err := os.ReadDir(abs)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				for _, e := range ents {
					name := e.Name()
					if e.IsDir() {
						name += "/"
					}
					b.WriteString(name + "\n")
				}
				return capOut(b.String(), maxOutBytes), nil
			},
		},
		{
			Name: "search", Description: "Search the workspace for a literal text pattern (max 50 matches).",
			Params: map[string]any{"query": strParam("text to find")}, Required: []string{"query"},
			Describe: func(a map[string]any) string { return fmt.Sprintf("Search: %q", ArgString(a, "query")) },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				q := ArgString(a, "query")
				if q == "" {
					return "", errors.New("query is empty")
				}
				res, sum, err := search.Collect(ctx, []string{w.Root}, q, search.Options{MaxResults: 50})
				if err != nil {
					return "", err
				}
				if len(res) == 0 {
					return "no matches", nil
				}
				var b strings.Builder
				for _, r := range res {
					fmt.Fprintf(&b, "%s:%d: %s\n", r.Path, r.Line, strings.TrimSpace(r.Preview))
				}
				if sum.Truncated() {
					fmt.Fprintf(&b, "(showing %d of %d)\n", sum.Shown, sum.Total)
				}
				return capOut(b.String(), maxOutBytes), nil
			},
		},
		{
			Name: "git_status", Description: "Show the working-tree status (changed / staged files).",
			Describe: func(map[string]any) string { return "Git status" },
			Run: func(context.Context, map[string]any) (string, error) {
				files, err := git.Status(w.Root)
				if err != nil {
					return "", err
				}
				if len(files) == 0 {
					return "clean", nil
				}
				var b strings.Builder
				for _, f := range files {
					fmt.Fprintf(&b, "%s %s\n", f.Code, f.Path)
				}
				return b.String(), nil
			},
		},
		{
			Name: "git_diff", Description: "Show the diff of one file vs HEAD, or of all changes when path is empty.",
			Params: map[string]any{"path": strParam("file path (optional)")},
			Describe: func(a map[string]any) string {
				if path(a) == "" {
					return "Git diff (all)"
				}
				return "Git diff " + path(a)
			},
			Run: func(_ context.Context, a map[string]any) (string, error) {
				var out string
				var err error
				if p := path(a); p != "" {
					out, err = git.Diff(w.Root, p)
				} else {
					out, err = git.DiffAgainst(w.Root, "HEAD")
				}
				if err != nil {
					return "", err
				}
				if strings.TrimSpace(out) == "" {
					return "no changes", nil
				}
				return capOut(out, maxReadBytes), nil
			},
		},
		{
			Name: "git_branch", Description: "List git branches (current marked with *).",
			Describe: func(map[string]any) string { return "Git branches" },
			Run: func(context.Context, map[string]any) (string, error) {
				return runCmd(context.Background(), w.Root, 15*time.Second, "git", "branch", "--list")
			},
		},
		// ── Mutating ────────────────────────────────────────────────
		{
			Name: "write_file", Description: "Overwrite a file with new content (creates it if missing).",
			Params:   map[string]any{"path": strParam("file path"), "content": strParam("the complete new file content")},
			Required: []string{"path", "content"}, Mutating: true, Class: ClassFileWrite,
			Validate: inside,
			Describe: func(a map[string]any) string {
				return fmt.Sprintf("Write %s (%d lines)", path(a), strings.Count(ArgString(a, "content"), "\n")+1)
			},
			Run: func(_ context.Context, a map[string]any) (string, error) {
				abs, err := w.Resolve(path(a))
				if err != nil {
					return "", err
				}
				if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
					return "", err
				}
				if err := os.WriteFile(abs, []byte(ArgString(a, "content")), 0o644); err != nil {
					return "", err
				}
				return "wrote " + w.rel(abs), nil
			},
		},
		{
			Name: "create_file", Description: "Create a new file, optionally with content. Fails if it exists.",
			Params:   map[string]any{"path": strParam("file path"), "content": strParam("initial content (optional)")},
			Required: []string{"path"}, Mutating: true, Class: ClassFileWrite,
			Validate: inside,
			Describe: func(a map[string]any) string { return "Create " + path(a) },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				abs, err := w.Resolve(path(a))
				if err != nil {
					return "", err
				}
				if _, err := os.Stat(abs); err == nil {
					return "", errors.New("file already exists")
				}
				if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
					return "", err
				}
				if err := os.WriteFile(abs, []byte(ArgString(a, "content")), 0o644); err != nil {
					return "", err
				}
				return "created " + w.rel(abs), nil
			},
		},
		{
			Name: "create_dir", Description: "Create a directory (and missing parents).",
			Params: map[string]any{"path": strParam("directory path")}, Required: []string{"path"},
			Validate: inside,
			Mutating: true, Class: ClassFileWrite,
			Describe: func(a map[string]any) string { return "Create folder " + path(a) },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				abs, err := w.Resolve(path(a))
				if err != nil {
					return "", err
				}
				return "created " + w.rel(abs), os.MkdirAll(abs, 0o755)
			},
		},
		{
			Name: "delete", Description: "Delete a file or directory.",
			Params: map[string]any{"path": strParam("path to delete")}, Required: []string{"path"},
			Validate: inside,
			Mutating: true, Class: ClassFileWrite,
			Describe: func(a map[string]any) string { return "Delete " + path(a) },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				abs, err := w.Resolve(path(a))
				if err != nil {
					return "", err
				}
				if root, _ := w.Resolve(""); abs == root {
					return "", errors.New("refusing to delete the workspace root")
				}
				return "deleted " + w.rel(abs), os.RemoveAll(abs)
			},
		},
		{
			Name: "rename", Description: "Rename or move a file / directory.",
			Params:   map[string]any{"from": strParam("current path"), "to": strParam("new path")},
			Required: []string{"from", "to"}, Mutating: true, Class: ClassFileWrite,
			Validate: w.argsInside("from", "to"),
			Describe: func(a map[string]any) string { return "Rename " + ArgString(a, "from") + " → " + ArgString(a, "to") },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				from, err := w.Resolve(ArgString(a, "from"))
				if err != nil {
					return "", err
				}
				to, err := w.Resolve(ArgString(a, "to"))
				if err != nil {
					return "", err
				}
				if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
					return "", err
				}
				return "renamed to " + w.rel(to), os.Rename(from, to)
			},
		},
		{
			Name: "git_stage", Description: "Stage a file for the next commit.",
			Params: map[string]any{"path": strParam("file path")}, Required: []string{"path"},
			Validate: inside,
			Mutating: true, Class: ClassGit,
			Describe: func(a map[string]any) string { return "Stage " + path(a) },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				if _, err := w.Resolve(path(a)); err != nil {
					return "", err
				}
				return "staged " + path(a), git.Stage(w.Root, path(a))
			},
		},
		{
			Name: "git_commit", Description: "Commit the staged changes with a message.",
			Params: map[string]any{"message": strParam("commit message")}, Required: []string{"message"},
			Mutating: true, Class: ClassGit,
			Describe: func(a map[string]any) string { return fmt.Sprintf("Commit: %q", firstLine(ArgString(a, "message"))) },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				return "committed", git.Commit(w.Root, ArgString(a, "message"))
			},
		},
		{
			Name: "git_checkout", Description: "Switch to a branch, optionally creating it.",
			Params:   map[string]any{"branch": strParam("branch name"), "create": map[string]any{"type": "boolean", "description": "create the branch first"}},
			Required: []string{"branch"}, Mutating: true, Class: ClassGit,
			Describe: func(a map[string]any) string {
				if ArgBool(a, "create") {
					return "Create branch " + ArgString(a, "branch")
				}
				return "Checkout " + ArgString(a, "branch")
			},
			Run: func(_ context.Context, a map[string]any) (string, error) {
				br := ArgString(a, "branch")
				if ArgBool(a, "create") {
					return "created and switched to " + br, git.CreateBranch(w.Root, br)
				}
				return "switched to " + br, git.Checkout(w.Root, br)
			},
		},
		{
			Name: "git_push", Description: "Push the current branch to its remote.",
			Mutating: true, Class: ClassGit,
			Describe: func(map[string]any) string { return "Git push" },
			Run:      func(context.Context, map[string]any) (string, error) { return "pushed", git.Push(w.Root) },
		},
		{
			Name: "git_pull", Description: "Pull the current branch from its remote.",
			Mutating: true, Class: ClassGit,
			Describe: func(map[string]any) string { return "Git pull" },
			Run:      func(context.Context, map[string]any) (string, error) { return "pulled", git.Pull(w.Root) },
		},
		{
			Name: "run_command", Description: "Run a shell command HIDDEN in the workspace root and return its captured output " +
				"(one-shot: build, test, grep, …; 2 min limit). For a command the user wants to see, an interactive one or " +
				"a long-running one (dev server, watcher), use terminal_send instead.",
			Params: map[string]any{"command": strParam("shell command")}, Required: []string{"command"},
			Mutating: true, Class: ClassRun,
			Describe: func(a map[string]any) string { return "Run: " + ArgString(a, "command") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				t := w.RunTimeout
				if t <= 0 {
					t = 2 * time.Minute
				}
				return runCmd(ctx, w.Root, t, "sh", "-c", ArgString(a, "command"))
			},
		},
	}
}

func orDot(s string) string {
	if s == "" {
		return "."
	}
	return s
}

func firstLine(s string) string { return strings.SplitN(strings.TrimSpace(s), "\n", 2)[0] }

// runCmd runs a command with a timeout and returns combined output plus
// the exit status (as text, not an error, so the model can read it).
func runCmd(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := spawn.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	s := capOut(string(out), maxOutBytes)
	if ctx.Err() == context.DeadlineExceeded {
		return s + "\n[timed out]", nil
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Sprintf("%s\n[exit status %d]", s, ee.ExitCode()), nil
		}
		return "", err
	}
	if strings.TrimSpace(s) == "" {
		return "[no output, exit 0]", nil
	}
	return s, nil
}
