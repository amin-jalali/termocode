package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
)

// History and rebase for the AI (git parity): git_history and git_rebase
// do what GRAPH, the commit view, the reflog, Undo / Redo and the
// interactive rebase card do, and ask where the GUI asks: every change of
// history on a Cancel-first card (the GUI's revert, cherry-pick, undo,
// reset and rebase cards), the uncommit with Always allow git (the
// palette runs it at once). A reset --hard first keeps the old HEAD as a
// backup ref and the changes in a stash, as the GUI's reset does.

const (
	aiGitShowMax  = 64 << 10  // diff bytes of one show
	aiGitFileMax  = 256 << 10 // bytes of one file_at
	aiGitPatchMax = 200 << 10 // bytes of one patch
)

// aiArgList reads key as a list of strings (one string is a list of one).
func aiArgList(a map[string]any, key string) []string {
	return aiArgPaths(map[string]any{"paths": a[key]})
}

// aiCap cuts s to max bytes and says so.
func aiGitCap(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n[cut at %d KB]", max>>10)
}

// aiRev is the rev argument (HEAD when empty), resolved to a full hash.
func aiRev(root string, a map[string]any) (string, error) {
	rev := strings.TrimSpace(ai.ArgString(a, "rev"))
	if rev == "" {
		rev = "HEAD"
	}
	return git.ResolveCommit(root, rev)
}

// aiPickResult says how a cherry-pick or revert went.
func aiPickResult(verb string, conflicts []string, err error) (string, error) {
	if len(conflicts) > 0 {
		return "", fmt.Errorf("the %s stopped on conflicts in %s: resolve them with git_conflicts, then git_conflicts op continue (or op abort)",
			verb, strings.Join(conflicts, ", "))
	}
	return "", err
}

// aiUndoError says why an undo or redo did not run.
func aiUndoError(err error) error {
	var busy *git.BusyError
	switch {
	case errors.As(err, &busy):
		return fmt.Errorf("a %s is in progress: finish it (git_conflicts / git_rebase op continue) or abort it first; nothing moved", busy.Op)
	case errors.Is(err, git.ErrUndoLocalChanges):
		return errors.New("local changes are in the way; nothing moved. Commit or stash them, or call again with stash=true")
	case errors.Is(err, git.ErrUndoStale):
		return errors.New("HEAD moved since; nothing moved (op reflog shows where it was)")
	}
	return err
}

func aiGitHistoryTool(root string, call aiIDECaller) ai.Tool {
	ws := ai.Workspace{Root: root}
	rel := aiRel(root)
	relPath := func(a map[string]any) (string, error) {
		abs, err := ws.Resolve(ai.ArgString(a, "path"))
		if err != nil {
			return "", err
		}
		return rel(abs), nil
	}
	// undo runs plan p and keeps it for redo (as the GUI's toast does).
	undo := func(ctx context.Context, p git.UndoPlan, move, stash bool) (string, error) {
		if p.Noop() {
			return "nothing to undo: HEAD is there already", nil
		}
		res, err := git.RunUndo(root, p, git.UndoOptions{Stash: stash})
		if err != nil {
			return "", aiUndoError(err)
		}
		if call != nil && !res.StashKept {
			_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
				if m.gitUndo == nil {
					m.gitUndo = &gitUndoState{}
				}
				m.gitUndo.redo = &undoRedo{plan: p, after: res.After, move: move}
				return "", nil, nil
			})
		}
		out := fmt.Sprintf("HEAD is at %s %q now", p.TargetShort(), p.TargetSubject)
		if p.Switch != "" {
			out = "back on " + p.Switch
		}
		if res.StashKept {
			out += "; your changes did not go back cleanly: they are kept in " + res.StashRef
		}
		return out + " (op redo takes it back)", nil
	}
	reflogAt := func(a map[string]any) (git.ReflogEntry, error) {
		i := ai.ArgInt(a, "index")
		es, err := git.Reflog(root, i+1)
		if err != nil {
			return git.ReflogEntry{}, err
		}
		if i < 0 || i >= len(es) {
			return git.ReflogEntry{}, fmt.Errorf("the reflog has no line %d (op reflog lists them)", i)
		}
		return es[i], nil
	}
	return aiGitOpTool(root, call, aiGitOpSpec{
		name: "git_history",
		desc: "Commit history. At once: show (rev, default HEAD: the commit and its diff; path limits it), file_at (rev, path: the file then), " +
			"search (query in the Search Log words: text, author:, since:, until:, path:, branch:, changes:, all), line_history (path, start, end), " +
			"reflog (n; index 0 is the newest), patch (rev: a commit as a mail patch; no rev: the uncommitted changes). " +
			"uncommit (undo the last commit, keep its changes staged) asks with Always allow git. Asking every time: cherry_pick (revs, in order), " +
			"revert (rev; parent for a merge commit), reset (rev, mode soft|mixed|keep|hard; hard keeps a backup ref and stashes the changes), " +
			"undo (the last git action; stash=true when local changes are in the way), redo, move_to (index: move HEAD to that reflog line), " +
			"restore_file (path, rev: the file as it was; its changes go to a stash first), move_commit (branch, create: the last commit to another branch), " +
			"apply_patch (patch text, nothing is committed).",
		ops: map[string]aiAsk{"show": aiAskNo, "file_at": aiAskNo, "search": aiAskNo, "line_history": aiAskNo, "reflog": aiAskNo, "patch": aiAskNo,
			"uncommit": aiAskGit, "cherry_pick": aiAskEvery, "revert": aiAskEvery, "reset": aiAskEvery, "undo": aiAskEvery, "redo": aiAskEvery,
			"move_to": aiAskEvery, "restore_file": aiAskEvery, "move_commit": aiAskEvery, "apply_patch": aiAskEvery},
		params: map[string]any{
			"rev": aiStr("a commit, branch or tag"), "revs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "cherry_pick: the commits, applied in this order"},
			"path": aiStr("a file"), "query": aiStr("search: the Search Log words"), "n": aiInt("search / reflog: how many (default 30)"),
			"start": aiInt("line_history: first line"), "end": aiInt("line_history: last line"),
			"mode":   map[string]any{"type": "string", "enum": []string{git.ResetSoft, git.ResetMixed, git.ResetKeep, git.ResetHard}, "description": "reset: how (default mixed)"},
			"parent": aiInt("revert of a merge commit: the parent to keep (1 = the branch it was merged into)"),
			"index":  aiInt("move_to: the reflog line"), "stash": aiBool("undo / move_to: stash local changes first, put them back after"),
			"branch": aiStr("move_commit: the target branch"), "create": aiBool("move_commit: make the branch"),
			"keep": aiBool("move_commit: keep the commit on the current branch too"), "patch": aiStr("apply_patch: the patch text"),
		},
		disk: func(op string, a map[string]any) bool {
			switch op {
			case "cherry_pick", "revert", "undo", "redo", "move_to", "restore_file", "apply_patch":
				return true
			case "reset":
				m := ai.ArgString(a, "mode")
				return m == git.ResetHard || m == git.ResetKeep
			}
			return false
		},
		check: func(op string, a map[string]any) error {
			if _, err := aiName(a, "rev"); err != nil {
				return err
			}
			switch op {
			case "file_at", "line_history", "restore_file":
				if strings.TrimSpace(ai.ArgString(a, "path")) == "" {
					return aiNeed(op, "path")
				}
				_, err := relPath(a)
				return err
			case "cherry_pick":
				if len(aiArgList(a, "revs")) == 0 && strings.TrimSpace(ai.ArgString(a, "rev")) == "" {
					return aiNeed(op, "revs")
				}
			case "reset", "revert":
				if strings.TrimSpace(ai.ArgString(a, "rev")) == "" {
					return aiNeed(op, "rev")
				}
				if m := ai.ArgString(a, "mode"); op == "reset" && m != "" {
					return aiOneOf("mode", m, []string{git.ResetSoft, git.ResetMixed, git.ResetKeep, git.ResetHard})
				}
			case "move_commit":
				_, err := aiNeedName(op, a, "branch")
				return err
			case "apply_patch":
				if strings.TrimSpace(ai.ArgString(a, "patch")) == "" {
					return aiNeed(op, "patch")
				}
			}
			return nil
		},
		describe: func(op string, a map[string]any) string {
			rev := ai.ArgString(a, "rev")
			switch op {
			case "cherry_pick":
				revs := aiArgList(a, "revs")
				if len(revs) == 0 {
					revs = []string{rev}
				}
				return "Cherry-pick " + strings.Join(revs, ", ") + " on top of HEAD? This makes new commits."
			case "revert":
				return "Revert " + rev + "? This makes a new commit that undoes it."
			case "reset":
				mode := ai.ArgString(a, "mode")
				if mode == "" {
					mode = git.ResetMixed
				}
				d := "Reset the current branch to " + rev + " (" + mode + ")?"
				if n := gitRevCount(root, rev+"..HEAD"); n > 0 {
					d += fmt.Sprintf(" %d commit%s leave the branch.", n, plural(n))
				}
				if mode == git.ResetHard {
					d += " Uncommitted changes go to a stash first; the old HEAD is kept as a backup ref."
				}
				return d
			case "undo":
				p, err := git.PlanUndo(root)
				if err != nil {
					return "Undo the last git action?"
				}
				return fmt.Sprintf("Undo the last git action (%s)? HEAD goes back to %s %q.", p.Entry.Summary(), p.TargetShort(), p.TargetSubject)
			case "redo":
				return "Redo the last undo?"
			case "move_to":
				if e, err := reflogAt(a); err == nil {
					return fmt.Sprintf("Move HEAD back to %s %q (reflog line %d)?", e.Short(), e.Subject, ai.ArgInt(a, "index"))
				}
				return "Move HEAD to a reflog line?"
			case "restore_file":
				return "Restore " + ai.ArgString(a, "path") + " as it was in " + rev + "? Its uncommitted changes go to a stash first."
			case "move_commit":
				return "Move the last commit to branch " + ai.ArgString(a, "branch") + "?"
			case "apply_patch":
				return fmt.Sprintf("Apply a patch (%d lines) to the working tree?", strings.Count(ai.ArgString(a, "patch"), "\n"))
			case "uncommit":
				return "Uncommit the last commit (its changes stay staged)"
			}
			return "Git history: " + op
		},
		run: func(ctx context.Context, op string, a map[string]any) (string, error) {
			n := ai.ArgInt(a, "n")
			if n <= 0 {
				n = 30
			}
			n = min(n, aiGitLogMax)
			switch op {
			case "show":
				rev, err := aiRev(root, a)
				if err != nil {
					return "", err
				}
				var paths []string
				if strings.TrimSpace(ai.ArgString(a, "path")) != "" {
					p, err := relPath(a)
					if err != nil {
						return "", err
					}
					paths = append(paths, p)
				}
				h, diff, cut, err := git.ShowCapped(root, rev, aiGitShowMax, paths...)
				if err != nil {
					return "", err
				}
				out := fmt.Sprintf("%s %s %s\n%s", h.Hash, h.Author, git.Ago(h.When), h.Subject)
				if b := strings.TrimSpace(h.Body); b != "" {
					out += "\n\n" + b
				}
				out += "\n\n" + diff
				if cut {
					out += fmt.Sprintf("\n[diff cut at %d KB; give path for one file]", aiGitShowMax>>10)
				}
				return out, nil
			case "file_at":
				p, err := relPath(a)
				if err != nil {
					return "", err
				}
				rev := strings.TrimSpace(ai.ArgString(a, "rev"))
				if rev == "" {
					rev = "HEAD"
				}
				text, found, cut, err := git.FileAt(root, rev, p, aiGitFileMax)
				switch {
				case err != nil:
					return "", err
				case !found:
					return "", fmt.Errorf("%s is not in %s", p, rev)
				case cut:
					text += fmt.Sprintf("\n[cut at %d KB]", aiGitFileMax>>10)
				}
				return text, nil
			case "search":
				es, err := git.SearchLog(root, git.ParseLogQuery(ai.ArgString(a, "query")), n)
				return aiLogLines(es), err
			case "line_history":
				p, err := relPath(a)
				if err != nil {
					return "", err
				}
				start := max(ai.ArgInt(a, "start"), 1)
				end := max(ai.ArgInt(a, "end"), start)
				cs, err := git.LineHistory(root, p, start, end)
				if err != nil {
					return "", err
				}
				if len(cs) == 0 {
					return "no commits touched those lines", nil
				}
				lines := make([]string, len(cs))
				for i, c := range cs {
					lines[i] = fmt.Sprintf("%s %s | %s | %s", c.Hash, git.Ago(c.When), c.Author, c.Subject)
				}
				return strings.Join(lines, "\n"), nil
			case "reflog":
				es, err := git.Reflog(root, n)
				if err != nil {
					return "", err
				}
				lines := make([]string, len(es))
				for i, e := range es {
					lines[i] = fmt.Sprintf("%d %s %s %s", i, e.Short(), e.Ago(), e.Summary())
				}
				return strings.Join(lines, "\n"), nil
			case "patch":
				if strings.TrimSpace(ai.ArgString(a, "rev")) == "" {
					p, err := git.ChangesPatch(root)
					if err == nil && strings.TrimSpace(p) == "" {
						return "no uncommitted changes", nil
					}
					return aiGitCap(p, aiGitPatchMax), err
				}
				rev, err := aiRev(root, a)
				if err != nil {
					return "", err
				}
				p, err := git.CommitPatch(root, rev)
				return aiGitCap(p, aiGitPatchMax), err
			case "uncommit":
				subject := git.StashAt(root, nil).Subject()
				if err := git.Uncommit(root); err != nil {
					return "", err
				}
				return fmt.Sprintf("uncommitted %q: its changes are staged", subject), nil
			case "cherry_pick":
				revs := aiArgList(a, "revs")
				if len(revs) == 0 {
					revs = []string{ai.ArgString(a, "rev")}
				}
				hashes := make([]string, 0, len(revs))
				for _, r := range revs {
					h, err := git.ResolveCommit(root, strings.TrimSpace(r))
					if err != nil {
						return "", err
					}
					hashes = append(hashes, h)
				}
				conflicts, err := git.CherryPickMany(root, hashes)
				if len(conflicts) > 0 || err != nil {
					return aiPickResult("cherry-pick", conflicts, err)
				}
				return fmt.Sprintf("cherry-picked %d commit%s", len(hashes), plural(len(hashes))), nil
			case "revert":
				rev, err := aiRev(root, a)
				if err != nil {
					return "", err
				}
				var conflicts []string
				if p := ai.ArgInt(a, "parent"); p > 0 {
					conflicts, err = git.RevertMerge(root, rev, p)
				} else {
					conflicts, err = git.Revert(root, rev)
				}
				if len(conflicts) > 0 || err != nil {
					if err != nil && strings.Contains(err.Error(), "-m option") {
						err = errors.New(shortHashOf(rev) + " is a merge commit: give parent (1 = the branch it was merged into)")
					}
					return aiPickResult("revert", conflicts, err)
				}
				return "reverted " + shortHashOf(rev) + " with a new commit", nil
			case "reset":
				rev := strings.TrimSpace(ai.ArgString(a, "rev"))
				mode := ai.ArgString(a, "mode")
				if mode == "" {
					mode = git.ResetMixed
				}
				if mode != git.ResetHard {
					if err := git.Reset(root, mode, rev); err != nil {
						return "", err
					}
					return "reset the branch to " + rev + " (" + mode + ")", nil
				}
				b, err := git.ResetHardSafe(root, rev)
				if err != nil {
					return "", err
				}
				out := "reset the branch to " + rev + " (hard); the old HEAD " + b.Old + " is kept as " + b.Ref
				if b.Stashed {
					out += "; the uncommitted changes are in the stash (stash@{0})"
				}
				return out, nil
			case "undo":
				p, err := git.PlanUndo(root)
				if err != nil {
					return "", aiUndoError(err)
				}
				return undo(ctx, p, false, ai.ArgBool(a, "stash"))
			case "move_to":
				e, err := reflogAt(a)
				if err != nil {
					return "", err
				}
				p, err := git.PlanMoveTo(root, e)
				if err != nil {
					return "", aiUndoError(err)
				}
				return undo(ctx, p, true, ai.ArgBool(a, "stash"))
			case "redo":
				if call == nil {
					return "", errors.New("the IDE is not available")
				}
				var r *undoRedo
				if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
					if m.gitUndo != nil {
						r, m.gitUndo.redo = m.gitUndo.redo, nil
					}
					return "", nil, nil
				}); err != nil {
					return "", err
				}
				if r == nil {
					return "", errors.New("nothing to redo: redo works right after an undo")
				}
				if err := git.RunRedo(root, r.plan, r.after); err != nil {
					return "", aiUndoError(err)
				}
				return "redone: HEAD is back at " + shortHashOf(r.plan.From), nil
			case "restore_file":
				p, err := relPath(a)
				if err != nil {
					return "", err
				}
				rev, err := aiRev(root, a)
				if err != nil {
					return "", err
				}
				backup, err := git.RestoreFileFrom(root, rev, p)
				if err != nil {
					return "", err
				}
				out := "restored " + p + " as it was in " + shortHashOf(rev)
				if backup != "" {
					out += "; its changes before are in " + backup
				}
				return out, nil
			case "move_commit":
				in, err := git.LastCommitForMove(root)
				if err != nil {
					return "", err
				}
				target := strings.TrimSpace(ai.ArgString(a, "branch"))
				keep := ai.ArgBool(a, "keep") || in.Pushed
				if err := git.MoveLastCommit(root, in.Hash, target, ai.ArgBool(a, "create"), keep); err != nil {
					return "", err
				}
				if keep {
					return fmt.Sprintf("copied %s %q to %s (it stays on %s: it is pushed already)", in.Short, in.Subject, target, in.Branch), nil
				}
				return fmt.Sprintf("moved %s %q to %s", in.Short, in.Subject, target), nil
			case "apply_patch":
				patch := ai.ArgString(a, "patch")
				stat, err := git.CheckPatch(root, patch)
				if err != nil {
					return "", fmt.Errorf("the patch does not apply: %w", err)
				}
				if err := git.ApplyPatch(root, patch); err != nil {
					return "", err
				}
				return "applied the patch (nothing is committed):\n" + stat, nil
			}
			return "", fmt.Errorf("unknown op %s", op)
		},
	})
}

// ── git_rebase ─────────────────────────────────────────────────────────

var aiRebaseActions = []string{string(git.RebasePick), string(git.RebaseReword), string(git.RebaseEdit),
	string(git.RebaseSquash), string(git.RebaseFixup), string(git.RebaseDrop)}

// aiRebaseOutcome says how a rebase went.
func aiRebaseOutcome(done string, o git.RebaseOutcome, err error) (string, error) {
	switch {
	case errors.Is(err, git.ErrRebaseDirty):
		return "", errors.New("you have uncommitted changes: commit or stash them, or call again with autostash=true")
	case errors.Is(err, git.ErrNothingStaged):
		return "", errors.New("nothing is staged: stage the changes for the fixup first")
	case len(o.Conflicts) > 0:
		return "", fmt.Errorf("the rebase stopped on conflicts in %s: resolve them with git_conflicts (take, resolve), then git_rebase op continue (or op abort)",
			strings.Join(o.Conflicts, ", "))
	case err != nil:
		return "", err
	case o.Stopped:
		return "the rebase stopped at an edit row: change the commit (git_commit amend=true), then git_rebase op continue", nil
	}
	return done, nil
}

// aiRebaseTodo applies the model's todo rows to t: rows name a commit by
// (a prefix of) its hash; when every commit is named, their order is the
// new order.
func aiRebaseTodo(t git.RebaseTodo, rows []any) (git.RebaseTodo, error) {
	find := func(h string) int {
		for i, it := range t.Items {
			if h != "" && (strings.HasPrefix(it.FullHash, h) || it.Hash == h) {
				return i
			}
		}
		return -1
	}
	var order []int
	seen := map[int]bool{}
	for _, r := range rows {
		row, _ := r.(map[string]any)
		h := strings.TrimSpace(ai.ArgString(row, "hash"))
		i := find(h)
		if i < 0 {
			return t, fmt.Errorf("%q is not a commit of the rebase (op plan lists them)", h)
		}
		if seen[i] {
			return t, fmt.Errorf("%s is in the todo twice", h)
		}
		seen[i] = true
		order = append(order, i)
		if act := strings.TrimSpace(ai.ArgString(row, "action")); act != "" {
			if err := aiOneOf("action", act, aiRebaseActions); err != nil {
				return t, err
			}
			t.Items[i].Action = git.RebaseAction(act)
		}
		t.Items[i].Message = ai.ArgString(row, "message")
	}
	if len(order) == len(t.Items) {
		items := make([]git.RebaseItem, len(order))
		for k, i := range order {
			items[k] = t.Items[i]
		}
		t.Items = items
	}
	return t, t.Validate()
}

func aiGitRebaseTool(root string, call aiIDECaller) ai.Tool {
	opt := func(a map[string]any) git.RebaseOptions {
		return git.RebaseOptions{Autostash: ai.ArgBool(a, "autostash")}
	}
	full := func(a map[string]any) (string, error) {
		if strings.TrimSpace(ai.ArgString(a, "rev")) == "" {
			return "", errors.New("give rev")
		}
		return aiRev(root, a)
	}
	return aiGitOpTool(root, call, aiGitOpSpec{
		name: "git_rebase",
		desc: "Rebase. At once: status (a rebase, merge, cherry-pick or revert in progress, its step and conflicts), plan (base: the commits after it, oldest first, as rows to rebase). " +
			"Asking every time: start (base, todo: rows {hash, action pick|reword|edit|squash|fixup|drop, message for reword}; rows not named stay pick; " +
			"naming every row sets the order), onto (onto, keep_merges), continue, skip, abort, fixup (rev: fold the staged changes into it), " +
			"autosquash (fold the fixup!/squash! commits), reword (rev, message), drop (rev), squash (rev into the commit before it). " +
			"autostash=true stashes uncommitted changes first. A conflict stops it: resolve with git_conflicts, then continue.",
		ops: map[string]aiAsk{"status": aiAskNo, "plan": aiAskNo, "start": aiAskEvery, "onto": aiAskEvery, "continue": aiAskEvery, "skip": aiAskEvery,
			"abort": aiAskEvery, "fixup": aiAskEvery, "autosquash": aiAskEvery, "reword": aiAskEvery, "drop": aiAskEvery, "squash": aiAskEvery},
		params: map[string]any{
			"base": aiStr("plan / start: the commit the rows come after"), "onto": aiStr("onto: the branch, tag or commit"),
			"todo": map[string]any{"type": "array", "description": "start: the rows to change", "items": map[string]any{"type": "object",
				"properties": map[string]any{"hash": aiStr("the commit"), "action": map[string]any{"type": "string", "enum": aiRebaseActions},
					"message": aiStr("reword: the new message")}, "required": []string{"hash"}}},
			"rev": aiStr("fixup / reword / drop / squash: the commit"), "message": aiStr("reword: the new message"),
			"keep_merges": aiBool("onto: keep merge commits"), "autostash": aiBool("stash uncommitted changes first, put them back after"),
		},
		disk: func(op string, _ map[string]any) bool { return op != "status" && op != "plan" },
		check: func(op string, a map[string]any) error {
			for _, k := range []string{"base", "onto", "rev"} {
				if _, err := aiName(a, k); err != nil {
					return err
				}
			}
			switch op {
			case "start", "plan":
				if strings.TrimSpace(ai.ArgString(a, "base")) == "" {
					return aiNeed(op, "base")
				}
			case "onto":
				_, err := aiNeedName(op, a, "onto")
				return err
			case "fixup", "drop", "squash":
				_, err := aiNeedName(op, a, "rev")
				return err
			case "reword":
				if _, err := aiNeedName(op, a, "rev"); err != nil {
					return err
				}
				if strings.TrimSpace(ai.ArgString(a, "message")) == "" {
					return aiNeed(op, "message")
				}
			}
			return nil
		},
		describe: func(op string, a map[string]any) string {
			rev := ai.ArgString(a, "rev")
			switch op {
			case "start":
				t, err := git.LoadRebaseTodo(root, ai.ArgString(a, "base"))
				if err == nil {
					rows, _ := a["todo"].([]any)
					if t2, err := aiRebaseTodo(t, rows); err == nil {
						return "Rebase " + orCurrent(aiCurrentBranch(root)) + "? " + t2.Summary() + "\n" + t2.Format()
					}
				}
				return "Rebase onto " + ai.ArgString(a, "base") + " interactively?"
			case "onto":
				d := "Rebase " + orCurrent(aiCurrentBranch(root)) + " onto " + ai.ArgString(a, "onto") + "?"
				if cs, err := git.CommitsNotOn(root, ai.ArgString(a, "onto"), 0); err == nil {
					d += fmt.Sprintf(" %d commit%s are replayed (new hashes).", len(cs), plural(len(cs)))
				}
				return d
			case "continue":
				return "Continue the rebase?"
			case "skip":
				return "Skip the commit the rebase stopped at? Its changes are dropped."
			case "abort":
				return "Abort the rebase? The branch goes back to where it was before it."
			case "fixup":
				return "Fold the staged changes into " + rev + "? The commits after it get new hashes."
			case "autosquash":
				return "Fold the fixup! / squash! commits into their targets?"
			case "reword":
				return fmt.Sprintf("Change the message of %s to %q?", rev, firstLineOf(ai.ArgString(a, "message")))
			case "drop":
				return "Drop commit " + rev + "? Its changes go; the newer commits stay."
			case "squash":
				return "Squash " + rev + " into the commit before it (both messages kept)?"
			case "plan":
				return "Plan a rebase after " + ai.ArgString(a, "base")
			}
			return "Rebase status"
		},
		run: func(ctx context.Context, op string, a map[string]any) (string, error) {
			switch op {
			case "status":
				if st := git.RebaseStatus(root); st.InProgress {
					out := fmt.Sprintf("a rebase is in progress on %s: step %d of %d (%s %s %s)", orCurrent(st.Branch), st.Step, st.Total, st.Action, st.Hash, st.Subject)
					if len(st.Conflicts) > 0 {
						out += "; conflicts in " + strings.Join(st.Conflicts, ", ")
					}
					return out, nil
				}
				if op := git.StoppedOp(root); op != "" {
					return "no rebase in progress; a " + op + " is stopped (git_conflicts lists its conflicts, op continue / abort ends it)", nil
				}
				return "no rebase in progress", nil
			case "plan":
				t, err := git.LoadRebaseTodo(root, ai.ArgString(a, "base"))
				if err != nil {
					return "", err
				}
				return t.Format(), nil
			case "start":
				t, err := git.LoadRebaseTodo(root, ai.ArgString(a, "base"))
				if err != nil {
					return "", err
				}
				rows, _ := a["todo"].([]any)
				if t, err = aiRebaseTodo(t, rows); err != nil {
					return "", err
				}
				o, err := git.RunRebase(root, t, opt(a))
				return aiRebaseOutcome("rebased: "+t.Summary(), o, err)
			case "onto":
				onto := strings.TrimSpace(ai.ArgString(a, "onto"))
				o, err := git.RebaseOnto(root, onto, ai.ArgBool(a, "keep_merges"), opt(a))
				return aiRebaseOutcome("rebased "+orCurrent(aiCurrentBranch(root))+" onto "+onto, o, err)
			case "continue", "skip", "abort":
				if !git.RebaseStatus(root).InProgress {
					if op := git.StoppedOp(root); op != "" {
						return "", fmt.Errorf("no rebase is in progress; a %s is stopped: use git_conflicts op continue or op abort", op)
					}
					return "", errors.New("no rebase is in progress")
				}
				switch op {
				case "abort":
					if err := git.RebaseAbort(root); err != nil {
						return "", err
					}
					return "aborted the rebase: the branch is back where it was", nil
				case "skip":
					o, err := git.RebaseSkip(root, git.RebaseOptions{})
					return aiRebaseOutcome("skipped the commit; the rebase is done", o, err)
				}
				o, err := git.RebaseContinue(root, git.RebaseOptions{})
				return aiRebaseOutcome("the rebase is done", o, err)
			case "fixup":
				h, err := full(a)
				if err != nil {
					return "", err
				}
				o, err := git.FixupInto(root, h, opt(a))
				return aiRebaseOutcome("folded the staged changes into "+shortHashOf(h), o, err)
			case "autosquash":
				n, _, err := git.AutosquashPlan(root)
				if err != nil {
					return "", err
				}
				if n == 0 {
					return "no fixup! / squash! commits to fold", nil
				}
				o, err := git.Autosquash(root, opt(a))
				return aiRebaseOutcome(fmt.Sprintf("folded %d fixup commit%s", n, plural(n)), o, err)
			case "reword":
				h, err := full(a)
				if err != nil {
					return "", err
				}
				o, err := git.RewordCommit(root, h, ai.ArgString(a, "message"), opt(a))
				return aiRebaseOutcome("changed the message of "+shortHashOf(h), o, err)
			case "drop":
				h, err := full(a)
				if err != nil {
					return "", err
				}
				o, err := git.DropCommit(root, h, opt(a))
				return aiRebaseOutcome("dropped "+shortHashOf(h), o, err)
			case "squash":
				h, err := full(a)
				if err != nil {
					return "", err
				}
				o, err := git.SquashIntoPrevious(root, h, opt(a))
				return aiRebaseOutcome("squashed "+shortHashOf(h)+" into the commit before it", o, err)
			}
			return "", fmt.Errorf("unknown op %s", op)
		},
	})
}
