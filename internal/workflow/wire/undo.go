package wire

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

// UndoWindow is how long a write can be undone (plan §5.6).
const UndoWindow = 7 * 24 * time.Hour

// UndoItem is one write of a run that has an undo recipe.
type UndoItem struct {
	Step, Item string
	Target     string
	Title      string // what the undo does
	Undone     bool
	DoneAt     time.Time
	// Blocked says why the undo cannot run here ("" = it can): its
	// recipe needs a value that held a secret and was not kept.
	Blocked string
}

// Undoable lists the writes of rf that were sent and have a recipe,
// oldest first.
func Undoable(root string, rf *run.RunFile) []UndoItem {
	l, err := ledger.Open(root, rf.Workflow, rf.ID)
	if err != nil {
		return nil
	}
	var out []UndoItem
	for _, e := range l.Entries() {
		if e.Undo == nil || (e.State != ledger.Done && e.State != ledger.Undone) {
			continue
		}
		out = append(out, UndoItem{Step: e.Step, Item: e.Item, Target: e.Target, Title: e.Undo.Title,
			Undone: e.State == ledger.Undone, DoneAt: time.UnixMilli(e.DoneMs), Blocked: redactedWhy(e.Undo)})
	}
	return out
}

// Undoer runs undo recipes.
type Undoer struct {
	Root, Workspace string
	Tools           source.ToolCaller
	Exec            func(ctx context.Context, argv []string, dir string) (string, int, error)
}

// Undo runs the recipe of the write of step for item in rf once and
// marks it undone.
func (u Undoer) Undo(ctx context.Context, rf *run.RunFile, step, item string) error {
	l, err := ledger.Open(u.Root, rf.Workflow, rf.ID)
	if err != nil {
		return err
	}
	e, ok := l.Get(step, item)
	switch {
	case !ok:
		return errors.New("no such write in this run")
	case e.State == ledger.Undone:
		return errors.New("this write was undone already")
	case e.Undo == nil || e.Undo.Tool == "":
		return errors.New("this write cannot be undone")
	case e.State != ledger.Done:
		return errors.New("this write was not sent")
	case clock.Now().Sub(time.UnixMilli(e.DoneMs)) > UndoWindow:
		return errors.New("this write is older than 7 days; undo it by hand")
	}
	if why := redactedWhy(e.Undo); why != "" {
		return errors.New(why) // never guess a secret
	}
	args := e.Undo.Args
	switch e.Undo.Tool {
	case "github.pr.close":
		if args["branch"] == "" {
			return errors.New("the pull request's branch is not known")
		}
		argv := []string{"gh", "pr", "close", args["branch"]}
		if args["delete_branch"] == "true" {
			argv = append(argv, "--delete-branch")
		}
		err = u.run(ctx, argv, u.Workspace)
	case "git.reset":
		dir := itemWorktree(rf, item)
		if dir == "" {
			return errors.New("the item's worktree is not known")
		}
		mode := "--soft"
		if args["mode"] == "mixed" {
			mode = "--mixed"
		}
		err = u.run(ctx, []string{"git", "reset", mode, "HEAD~1"}, dir)
	default:
		server := writeServer(rf, step, item)
		if server == "" || u.Tools == nil {
			return errors.New("the write's MCP server is not known or not connected")
		}
		call := make(map[string]any, len(args))
		for k, v := range args {
			call[k] = v
		}
		_, _, err = u.Tools.CallTool(ctx, server, e.Undo.Tool, call)
	}
	if err != nil {
		return err
	}
	return l.MarkUndone(step, item)
}

// redactedWhy explains why recipe u cannot run: the ledger dropped the
// values of these arguments because they held a secret ("" = none).
func redactedWhy(u *ledger.Undo) string {
	var keys []string
	for k, v := range u.Args {
		if strings.Contains(v, ledger.Redacted) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	return "the undo needs " + strings.Join(keys, ", ") + ", which held a secret that was not kept; undo it by hand"
}

func (u Undoer) run(ctx context.Context, argv []string, dir string) error {
	if u.Exec == nil {
		return errors.New("no way to run commands here")
	}
	out, code, err := u.Exec(ctx, argv, dir)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("%s exited %d: %s", strings.Join(argv[:2], " "), code, strings.TrimSpace(out))
	}
	return nil
}

func writeServer(rf *run.RunFile, step, item string) string {
	for _, w := range rf.Writes {
		if w.Step == step && w.Item == item {
			return w.Spec.Server
		}
	}
	return ""
}

func itemWorktree(rf *run.RunFile, item string) string {
	for _, st := range rf.Steps {
		if st == nil || st.Each == nil {
			continue
		}
		for _, it := range st.Each.Items {
			if it.Key == item {
				return it.Env.Worktree
			}
		}
	}
	return ""
}
