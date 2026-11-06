package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	// that may have been sent is not sent again.
	Find func(ctx context.Context, spec workflow.WriteSpec, marker string) (ledger.Result, bool, error)
}

// Write passes one write through the gate and the ledger.
func (w *Writer) Write(ctx context.Context, c run.WriteCall) (run.Output, error) {
	if c.Step == nil || w.Ledger == nil {
		return run.Output{}, run.Permanent(errors.New("the write is not set up"))
	}
	spec := c.Spec
	item := ""
	if c.Item != nil {
		item = c.Item.Key
	}
	target, preview, undo := describe(spec)
	if old, ok := w.Ledger.Get(c.Step.ID, item); ok {
		if old.State == ledger.Done {
			return run.Output{URL: old.ExternalID, Text: preview}, nil
		}
		if w.Find == nil {
			return run.Output{}, run.Permanent(ledger.ErrMaybeSent)
		}
		res, found, err := w.Find(ctx, spec, old.Marker)
		if err != nil {
			return run.Output{}, err
		}
		if found {
			e, _, err := w.Ledger.Do(ctx, ledger.Write{Step: c.Step.ID, Item: item, Target: target, Args: spec.Args},
				func(context.Context, string) (ledger.Result, error) { return res, nil },
				func(context.Context, string) (ledger.Result, bool, error) { return res, true, nil })
			return run.Output{URL: e.ExternalID, Text: preview}, err
		}
	}
	if err := w.gate(ctx, c, target, preview, undo, item); err != nil {
		return run.Output{}, err
	}
	send := func(ctx context.Context, marker string) (ledger.Result, error) { return w.send(ctx, spec, marker) }
	notFound := func(context.Context, string) (ledger.Result, bool, error) { return ledger.Result{}, false, nil }
	var u *ledger.Undo
	if spec.Undo != nil {
		u = &ledger.Undo{Title: undo, Tool: spec.Undo.Tool, Args: textArgs(spec.Undo.Args)}
	}
	e, _, err := w.Ledger.Do(ctx, ledger.Write{Step: c.Step.ID, Item: item, Target: target, Args: spec.Args, Undo: u}, send, notFound)
	if err != nil {
		return run.Output{}, err
	}
	return run.Output{URL: e.ExternalID, Text: preview, Path: filePath(spec)}, nil
}

func (w *Writer) gate(ctx context.Context, c run.WriteCall, target, preview, undo, item string) error {
	if c.Spec.Gate == workflow.GateAllow && w.Root != "" {
		ok, err := ledger.Allowed(w.Root, w.Workflow, w.Hash, c.Step.ID)
		if err != nil {
			return err
		}
		if ok {
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
		return err
	}
	if d == GateDeny {
		return fmt.Errorf("the write was denied: %w", run.ErrDenied)
	}
	return nil
}

var errNotYet = errors.New("this write target comes with a later version of workflows")

func (w *Writer) send(ctx context.Context, spec workflow.WriteSpec, marker string) (ledger.Result, error) {
	switch spec.To {
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
