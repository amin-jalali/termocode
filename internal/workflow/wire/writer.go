package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

// GateDecision is the user's answer on the write gate card.
type GateDecision int

const (
	GateDeny GateDecision = iota
	GateAllowOnce
	// GateAllowWorkflow: the card has put the step on the allow-list
	// (bound to the file hash); this write goes out.
	GateAllowWorkflow
)

// GateAsk is what the gate card shows.
type GateAsk struct {
	Step, Item string
	Target     string // where it writes
	Preview    string // what it writes
	Undo       string // "" = cannot be undone
}

// Writer is the run.Writer: the gate (allow-list or card), the write
// ledger (at most once, marker on every comment) and the targets.
type Writer struct {
	// Root, Workflow and Hash bind the allow-list (ledger.Allowed).
	Root, Workflow, Hash string
	// Ledger is the run's write ledger.
	Ledger *ledger.Ledger
	// Workspace is the root file writes stay inside.
	Workspace string
	// Tools calls MCP tools for mcp / status writes.
	Tools source.ToolCaller
	// Ask shows the gate card and waits (nil: every write is denied).
	Ask func(ctx context.Context, a GateAsk) (GateDecision, error)
	// Notify sends a message write via os / web (nil: not available).
	Notify func(ctx context.Context, via, text string) error
	// GitHubComment comments on a PR (nil: not available).
	GitHubComment func(ctx context.Context, pr, body string) (ledger.Result, error)
	// Find looks a write up by its marker (a resumed run); nil: a write
	// that may have been sent is not sent again. Commits are looked up in
	// the item's branch without it.
	Find func(ctx context.Context, spec workflow.WriteSpec, marker string) (ledger.Result, bool, error)
	// PR pushes an item's branch and opens its pull request (the app:
	// git push + gh pr create); nil: pr writes are not available.
	PR func(ctx context.Context, p PRCall) (ledger.Result, error)
	// Dir is the workflow file's folder; body_file stays inside it.
	Dir string
	// Audit gets one line per gate answer (nil: none).
	Audit *ledger.Audit
}

// PRCall is one pull request a pr write opens: Branch (pushed by PR)
// from the item's worktree Dir into Base.
type PRCall struct {
	Dir, Branch, Base string
	Title, Body       string // Body ends with the run marker
	Draft             bool
}

// Write passes one write through the gate and the ledger.
func (w *Writer) Write(ctx context.Context, c run.WriteCall) (run.Output, error) {
	if c.Step == nil || w.Ledger == nil {
		return run.Output{}, run.Permanent(errors.New("the write is not set up"))
	}
	spec := c.Spec
	if spec.BodyFile != "" && (spec.To == "pr" || spec.To == "github.comment") {
		body, err := w.bodyFile(spec.BodyFile, c.Render)
		if err != nil {
			return run.Output{}, err
		}
		spec.Body = body
	}
	env := c.Env
	if (spec.To == "pr" || spec.To == "commit") && (env.ID == "" || env.Branch == "") {
		return run.Output{}, run.Permanent(fmt.Errorf(`a %s write needs the item's worktree: put it in an each with "worktree": true`, spec.To))
	}
	item := ""
	if c.Item != nil {
		item = c.Item.Key
	}
	target, preview, undo := describe(spec)
	if spec.To == "pr" || spec.To == "commit" {
		target, undo = describeGit(spec, env)
	}
	find := w.Find
	if spec.To == "commit" {
		find = func(_ context.Context, _ workflow.WriteSpec, marker string) (ledger.Result, bool, error) {
			return findCommit(w.Workspace, env, marker)
		}
	}
	if old, ok := w.Ledger.Get(c.Step.ID, item); ok {
		if old.State == ledger.Done {
			return output(spec, old.ExternalID, preview), nil
		}
		if find == nil {
			return run.Output{}, run.Permanent(ledger.ErrMaybeSent)
		}
		res, found, err := find(ctx, spec, old.Marker)
		if err != nil {
			return run.Output{}, err
		}
		if found {
			e, _, err := w.Ledger.Do(ctx, ledger.Write{Step: c.Step.ID, Item: item, Target: target, Args: spec.Args},
				func(context.Context, string) (ledger.Result, error) { return res, nil },
				func(context.Context, string) (ledger.Result, bool, error) { return res, true, nil })
			return output(spec, e.ExternalID, preview), err
		}
	}
	if err := w.gate(ctx, c, target, preview, undo, item); err != nil {
		return run.Output{}, err
	}
	send := func(ctx context.Context, marker string) (ledger.Result, error) { return w.send(ctx, spec, env, marker) }
	notFound := func(context.Context, string) (ledger.Result, bool, error) { return ledger.Result{}, false, nil }
	var u *ledger.Undo
	switch {
	case spec.Undo != nil:
		u = &ledger.Undo{Title: undo, Tool: spec.Undo.Tool, Args: textArgs(spec.Undo.Args)}
	case spec.To == "pr":
		u = &ledger.Undo{Title: undo, Tool: "github.pr.close", Args: map[string]string{"branch": env.Branch, "delete_branch": "true"}}
	case spec.To == "commit":
		u = &ledger.Undo{Title: undo, Tool: "git.reset", Args: map[string]string{"branch": env.Branch, "to": "HEAD~1", "mode": "soft"}}
	}
	e, _, err := w.Ledger.Do(ctx, ledger.Write{Step: c.Step.ID, Item: item, Target: target, Args: spec.Args, Undo: u}, send, notFound)
	if err != nil {
		return run.Output{}, err
	}
	return output(spec, e.ExternalID, preview), nil
}

// output is a write's steps.<id>: url (a PR's link), json.number (a
// PR's number), path (a file), text (what was written).
func output(spec workflow.WriteSpec, external, preview string) run.Output {
	o := run.Output{URL: external, Text: preview, Path: filePath(spec)}
	switch spec.To {
	case "pr":
		if n := prNumber(external); n > 0 {
			o.JSON = map[string]any{"number": float64(n)}
		}
	case "commit":
		o.URL = ""
		if external != "" {
			o.Text = "committed " + external
		} else {
			o.Text = "nothing to commit"
		}
		o.JSON = map[string]any{"sha": external}
	}
	return o
}

// prNumber is the number at the end of a pull request link (0: none).
func prNumber(url string) int {
	i := strings.LastIndex(url, "/pull/")
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimRight(url[i+len("/pull/"):], "/"))
	if err != nil {
		return 0
	}
	return n
}

// bodyFile reads a body_file from the workflow folder and fills it.
func (w *Writer) bodyFile(rel string, render func(string) (string, error)) (string, error) {
	b, err := (&Steps{Dir: w.Dir}).readPrompt(rel)
	if err != nil {
		return "", err
	}
	body := string(b)
	if render != nil {
		if body, err = render(body); err != nil {
			return "", run.Permanent(fmt.Errorf("body_file: %w", err))
		}
	}
	return body, nil
}

// describeGit is the card's target and undo of a pr or commit write.
func describeGit(s workflow.WriteSpec, env run.ItemEnv) (target, undo string) {
	if s.To == "commit" {
		return "commit on " + env.Branch, "git reset --soft HEAD~1 on branch " + env.Branch
	}
	base := s.Base
	if base == "" {
		base = "the default branch"
	}
	target = "pull request " + env.Branch + " → " + base
	if s.Draft {
		target += " (draft)"
	}
	return target, "close the pull request and delete branch " + env.Branch
}

func (w *Writer) gate(ctx context.Context, c run.WriteCall, target, preview, undo, item string) error {
	answer := func(a string) {
		_ = w.Audit.Add(ledger.AuditLine{Kind: "gate", Step: c.Step.ID, Item: item, Target: target, Answer: a})
	}
	if c.Spec.Gate == workflow.GateAllow && w.Root != "" {
		ok, err := ledger.Allowed(w.Root, w.Workflow, w.Hash, c.Step.ID)
		if err != nil {
			return err
		}
		if ok {
			answer("allow-list")
			return nil
		}
	}
	if w.Ask == nil {
		return fmt.Errorf("no one can answer the write card: %w", run.ErrDenied)
	}
	if c.Wait != nil {
		c.Wait()
	}
	d, err := w.Ask(ctx, GateAsk{Step: c.Step.ID, Item: item, Target: target, Preview: preview, Undo: undo})
	if err != nil {
		answer("deny")
		return err
	}
	switch d {
	case GateAllowOnce:
		answer("allow once")
	case GateAllowWorkflow:
		answer("allow for this workflow")
	default:
		answer("deny")
		return fmt.Errorf("the write was denied: %w", run.ErrDenied)
	}
	return nil
}

var errNotYet = errors.New("this write target comes with a later version of workflows")

func (w *Writer) send(ctx context.Context, spec workflow.WriteSpec, env run.ItemEnv, marker string) (ledger.Result, error) {
	switch spec.To {
	case "commit":
		ok, err := git.CommitAgentWorktree(w.Workspace, env.ID, spec.Message+"\n\n"+marker)
		if err != nil || !ok {
			return ledger.Result{}, err
		}
		sha, err := git.HeadSHA(env.Worktree)
		return ledger.Result{ExternalID: sha}, err
	case "pr":
		if w.PR == nil {
			return ledger.Result{}, run.Permanent(errors.New("pull requests: gh is not available"))
		}
		// The agent's work goes on the item's branch first.
		if _, err := git.CommitAgentWorktree(w.Workspace, env.ID, spec.Title+"\n\n"+marker); err != nil {
			return ledger.Result{}, err
		}
		return w.PR(ctx, PRCall{Dir: env.Worktree, Branch: env.Branch, Base: spec.Base, Title: spec.Title,
			Body: strings.TrimRight(spec.Body, "\n") + "\n\n<!-- " + marker + " -->", Draft: spec.Draft})
	case "mcp", "status":
		if w.Tools == nil {
			return ledger.Result{}, fmt.Errorf("mcp: %w", source.ErrNotConnected)
		}
		args := map[string]any{}
		for k, v := range spec.Args {
			args[k] = v
		}
		for _, k := range []string{"body", "text", "comment"} {
			if s, ok := args[k].(string); ok && marker != "" {
				args[k] = s + "\n\n" + marker
				break
			}
		}
		_, _, err := w.Tools.CallTool(source.WithWriteAllowed(ctx), spec.Server, spec.Tool, args)
		return ledger.Result{}, err
	case "file":
		return ledger.Result{}, w.writeFile(spec)
	case "message":
		if w.Notify == nil {
			return ledger.Result{}, run.Permanent(fmt.Errorf("message via %q is not available here", spec.Via))
		}
		return ledger.Result{}, w.Notify(ctx, spec.Via, spec.Text)
	case "github.comment":
		if w.GitHubComment == nil {
			return ledger.Result{}, run.Permanent(errors.New("github: gh is not available"))
		}
		return w.GitHubComment(ctx, spec.PR, spec.Body+"\n\n<!-- "+marker+" -->")
	}
	return ledger.Result{}, run.Permanent(fmt.Errorf("write to %q: %w", spec.To, errNotYet))
}

func (w *Writer) writeFile(spec workflow.WriteSpec) error {
	if w.Workspace == "" {
		return run.Permanent(errors.New("file: no workspace"))
	}
	rel := filepath.Clean(filepath.FromSlash(spec.Path))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return run.Permanent(fmt.Errorf("file: %q is not inside the workspace", spec.Path))
	}
	p := filepath.Join(w.Workspace, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		ws, _ := filepath.EvalSymlinks(w.Workspace)
		if ws != "" && real != ws && !strings.HasPrefix(real, ws+string(filepath.Separator)) {
			return run.Permanent(fmt.Errorf("file: %q leaves the workspace", spec.Path))
		}
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	switch spec.Mode {
	case "overwrite":
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case "append":
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(p, flag, 0o644)
	if err != nil {
		return run.Permanent(fmt.Errorf("file: %w", err))
	}
	_, err = f.WriteString(spec.Content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// describe is the card text of a write: target, preview, undo.
func describe(s workflow.WriteSpec) (target, preview, undo string) {
	switch s.To {
	case "mcp", "status":
		target = s.Server + " · " + s.Tool
		preview = argsText(s.Args)
		if s.Undo != nil {
			undo = s.Undo.Tool + " " + argsText(s.Undo.Args)
		}
	case "file":
		mode := s.Mode
		if mode == "" {
			mode = "create"
		}
		target, preview = "file "+s.Path+" ("+mode+")", s.Content
	case "message":
		target, preview = "message · "+s.Via, s.Text
	case "github.comment":
		target, preview = "GitHub comment on PR #"+s.PR, s.Body
	case "pr":
		target, preview = "pull request → "+s.Base, s.Title+"\n"+s.Body
	default:
		target = s.To
	}
	return strings.TrimSpace(target), strings.TrimSpace(preview), strings.TrimSpace(undo)
}

func argsText(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := args[k]
		s, ok := v.(string)
		if !ok {
			bs, _ := json.Marshal(v)
			s = string(bs)
		}
		fmt.Fprintf(&b, "%s: %s\n", k, s)
	}
	return b.String()
}

func textArgs(args map[string]any) map[string]string {
	if len(args) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range args {
		if s, ok := v.(string); ok {
			out[k] = s
		} else {
			bs, _ := json.Marshal(v)
			out[k] = string(bs)
		}
	}
	return out
}

func filePath(s workflow.WriteSpec) string {
	if s.To == "file" {
		return s.Path
	}
	return ""
}

// Describe is what the gate card shows for a write spec: where it
// writes, what it writes and how to undo it ("" when it cannot be).
func Describe(s workflow.WriteSpec) (target, preview, undo string) { return describe(s) }

// findCommit looks for a commit with marker in its message on the item's
// branch (a resumed commit write).
func findCommit(repo string, env run.ItemEnv, marker string) (ledger.Result, bool, error) {
	if env.Branch == "" || marker == "" {
		return ledger.Result{}, false, nil
	}
	cmd := spawn.Command("git", "log", "-n1", "--format=%H", "-F", "--grep="+marker, "refs/heads/"+env.Branch, "--")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return ledger.Result{}, false, fmt.Errorf("git log: %w", err)
	}
	sha := strings.TrimSpace(string(out))
	return ledger.Result{ExternalID: sha}, sha != "", nil
}
