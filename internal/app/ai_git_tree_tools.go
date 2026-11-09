package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// The working tree for the AI (git parity): git_conflicts (CONFLICTS
// and the merge view), git_lines (line staging), git_discard (Discard
// Changes / Discard All / Delete Untracked) and git_repo (the rest of the
// Git: palette on the repository). They ask where the GUI asks: a write
// of a conflict side, a discard, init and bisect every time (Cancel
// first); line staging, mark resolved, .gitignore and submodules with
// Always allow git.

// aiConflictSides are the take sides of git_conflicts.
var aiConflictSides = map[string]git.Resolution{"ours": git.AcceptOurs, "theirs": git.AcceptTheirs, "both": git.AcceptBoth,
	"both_theirs_first": git.AcceptBothTheirsFirst, "base": git.AcceptBase}

// aiConflictedFiles are the unmerged paths of root.
func aiConflictedFiles(root string) ([]string, error) {
	fs, err := git.Status(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range fs {
		if f.Conflicted() {
			out = append(out, f.Path)
		}
	}
	return out, nil
}

// aiStoppedOp is the stopped merge, cherry-pick or revert of root ("").
func aiStoppedOp(root string) string { return git.StoppedOp(root) }

func aiGitConflictsTool(root string, call aiIDECaller) ai.Tool {
	ws := ai.Workspace{Root: root}
	rel := aiRel(root)
	file := func(a map[string]any) (abs, r string, err error) {
		if strings.TrimSpace(ai.ArgString(a, "path")) == "" {
			return "", "", errors.New("give path")
		}
		abs, err = ws.Resolve(ai.ArgString(a, "path"))
		if err != nil {
			return "", "", err
		}
		return abs, rel(abs), nil
	}
	return aiGitOpTool(root, call, aiGitOpSpec{
		name: "git_conflicts",
		desc: "Merge conflicts of a stopped merge, cherry-pick or revert (a rebase: git_rebase continues it). At once: list (the conflicted files and what stopped), " +
			"show (path: each block numbered, with ours, base and theirs). Asking every time: take (path, side ours|theirs|both|both_theirs_first|base, " +
			"block: its number, 0 = every block), take_file (path, side ours|theirs|delete: the whole file, for a binary or deleted one), continue " +
			"(stages the fixed files and makes the commit) and abort (back to before it). resolve (path: mark it resolved; refused while markers are left) " +
			"asks with Always allow git.",
		def: "list",
		ops: map[string]aiAsk{"list": aiAskNo, "show": aiAskNo, "take": aiAskEvery, "take_file": aiAskEvery, "resolve": aiAskGit,
			"continue": aiAskEvery, "abort": aiAskEvery},
		params: map[string]any{"path": aiStr("the conflicted file"),
			"side":  map[string]any{"type": "string", "enum": []string{"ours", "theirs", "both", "both_theirs_first", "base", "delete"}, "description": "take / take_file: which side"},
			"block": aiInt("take: the block number from show (0 = every block)")},
		disk: func(op string, _ map[string]any) bool {
			return op == "take" || op == "take_file" || op == "continue" || op == "abort"
		},
		check: func(op string, a map[string]any) error {
			switch op {
			case "show", "take", "take_file", "resolve":
				if _, _, err := file(a); err != nil {
					return err
				}
			}
			side := ai.ArgString(a, "side")
			switch op {
			case "take":
				if _, ok := aiConflictSides[side]; !ok {
					return errors.New("give side: ours, theirs, both, both_theirs_first or base")
				}
				if ai.ArgInt(a, "block") < 0 {
					return errors.New("block must be 0 or more")
				}
			case "take_file":
				return aiOneOf("side", side, []string{"ours", "theirs", "delete"})
			}
			return nil
		},
		describe: func(op string, a map[string]any) string {
			p, side := ai.ArgString(a, "path"), ai.ArgString(a, "side")
			switch op {
			case "take":
				if b := ai.ArgInt(a, "block"); b > 0 {
					return fmt.Sprintf("Take %s for conflict block %d of %s?", side, b, p)
				}
				return "Take " + side + " for every conflict block of " + p + "?"
			case "take_file":
				if side == "delete" {
					return "Resolve " + p + " by deleting it?"
				}
				return "Resolve " + p + " with the whole file of " + side + "?"
			case "resolve":
				return "Mark " + p + " resolved"
			case "continue":
				return "Continue the " + orWord(aiStoppedOp(root), "merge") + "? The fixed files are staged and the commit is made."
			case "abort":
				return "Abort the " + orWord(aiStoppedOp(root), "merge") + "? Everything goes back to before it; your resolutions are lost."
			case "show":
				return "Show the conflicts of " + p
			}
			return "List merge conflicts"
		},
		run: func(ctx context.Context, op string, a map[string]any) (string, error) {
			stopped := aiStoppedOp(root)
			switch op {
			case "list":
				files, err := aiConflictedFiles(root)
				if err != nil {
					return "", err
				}
				what := "no merge, cherry-pick or revert is stopped"
				if stopped != "" {
					what = "a " + stopped + " is stopped"
				} else if git.RebaseStatus(root).InProgress {
					what = "a rebase is stopped (git_rebase op continue ends it)"
				}
				if len(files) == 0 {
					return what + "; no conflicted files", nil
				}
				var b strings.Builder
				b.WriteString(what + "; conflicted files:\n")
				for _, f := range files {
					st, err := git.ReadConflictStages(root, f)
					switch {
					case err == nil && st.DeletedBy() != "":
						fmt.Fprintf(&b, "- %s (deleted by %s: take_file)\n", f, st.DeletedBy())
					case err == nil && st.Binary():
						fmt.Fprintf(&b, "- %s (binary: take_file)\n", f)
					default:
						data, _ := os.ReadFile(filepath.Join(root, f))
						fmt.Fprintf(&b, "- %s (%d block%s)\n", f, len(git.ParseConflicts(string(data))), plural(len(git.ParseConflicts(string(data)))))
					}
				}
				return strings.TrimRight(b.String(), "\n"), nil
			case "show":
				abs, r, err := file(a)
				if err != nil {
					return "", err
				}
				data, err := os.ReadFile(abs)
				if err != nil {
					return "", err
				}
				blocks := git.ParseConflicts(string(data))
				if len(blocks) == 0 {
					return r + " has no conflict markers", nil
				}
				var b strings.Builder
				for i, bl := range blocks {
					fmt.Fprintf(&b, "block %d (lines %d-%d)\n<<<<<<< ours %s\n%s", i+1, bl.Start+1, bl.End+1, bl.OursLabel, aiLinesText(bl.Ours))
					if bl.Base >= 0 {
						fmt.Fprintf(&b, "||||||| base %s\n%s", bl.BaseLabel, aiLinesText(bl.BaseLn))
					}
					fmt.Fprintf(&b, "=======\n%s>>>>>>> theirs %s\n", aiLinesText(bl.Theirs), bl.TheirsLabel)
				}
				return strings.TrimRight(b.String(), "\n"), nil
			case "take":
				abs, r, err := file(a)
				if err != nil {
					return "", err
				}
				data, err := os.ReadFile(abs)
				if err != nil {
					return "", err
				}
				text := string(data)
				blocks := git.ParseConflicts(text)
				n := ai.ArgInt(a, "block")
				if len(blocks) == 0 || n > len(blocks) {
					return "", fmt.Errorf("%s has %d conflict block%s", r, len(blocks), plural(len(blocks)))
				}
				res := aiConflictSides[ai.ArgString(a, "side")]
				// The last block first: the earlier ones keep their line numbers.
				for i := len(blocks) - 1; i >= 0; i-- {
					if n != 0 && i != n-1 {
						continue
					}
					if res == git.AcceptBase && blocks[i].Base < 0 {
						return "", fmt.Errorf("block %d has no base section (the file was merged without diff3); take ours, theirs or both", i+1)
					}
					text = git.ResolveConflict(text, blocks[i], res)
				}
				if call == nil {
					return "", errors.New("the IDE is not available")
				}
				if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiWriteFile(abs, r, text) }); err != nil {
					return "", err
				}
				left := len(git.ParseConflicts(text))
				if left == 0 {
					return "took " + ai.ArgString(a, "side") + " in " + r + "; no markers are left (op resolve marks it resolved)", nil
				}
				return fmt.Sprintf("took %s in %s; %d block%s left", ai.ArgString(a, "side"), r, left, plural(left)), nil
			case "take_file":
				_, r, err := file(a)
				if err != nil {
					return "", err
				}
				if ai.ArgString(a, "side") == "delete" {
					return "resolved " + r + " by deleting it", git.ResolveDelete(root, r)
				}
				return "resolved " + r + " with " + ai.ArgString(a, "side") + " (staged)", git.ResolveWithSide(root, r, ai.ArgString(a, "side"))
			case "resolve":
				_, r, err := file(a)
				if err != nil {
					return "", err
				}
				if err := git.MarkResolved(root, r); err != nil {
					return "", err
				}
				return "marked " + r + " resolved (staged)", nil
			case "continue", "abort":
				if stopped == "" {
					if git.RebaseStatus(root).InProgress {
						return "", errors.New("a rebase is stopped: use git_rebase op " + op)
					}
					return "", errors.New("no merge, cherry-pick or revert is stopped")
				}
				if op == "abort" {
					if err := git.OpAbort(root, stopped); err != nil {
						return "", err
					}
					return "aborted the " + stopped + ": everything is back to before it", nil
				}
				if err := git.OpContinue(root, stopped); err != nil {
					return "", err
				}
				return "the " + stopped + " is done (committed)", nil
			}
			return "", fmt.Errorf("unknown op %s", op)
		},
	})
}

// aiLinesText is lines with a newline after each.
func aiLinesText(ls []string) string {
	if len(ls) == 0 {
		return ""
	}
	return strings.Join(ls, "\n") + "\n"
}

// orWord is s, or def when s is empty.
func orWord(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ── git_lines ──────────────────────────────────────────────────────────

func aiGitLinesTool(root string, call aiIDECaller) ai.Tool {
	ws := ai.Workspace{Root: root}
	rel := aiRel(root)
	return aiGitOpTool(root, call, aiGitOpSpec{
		name: "git_lines",
		desc: "Stage, unstage or discard the changes on lines start..end (1-based, of the working file) of one file, as Stage / Unstage / Discard Line do. " +
			"stage and unstage ask with Always allow git; discard asks every time (it rewrites the file and cannot be undone).",
		ops:    map[string]aiAsk{"stage": aiAskGit, "unstage": aiAskGit, "discard": aiAskEvery},
		params: map[string]any{"path": aiStr("the file"), "start": aiInt("first line"), "end": aiInt("last line (default start)")},
		disk:   func(op string, _ map[string]any) bool { return op == "discard" },
		check: func(op string, a map[string]any) error {
			if ai.ArgInt(a, "start") < 1 {
				return errors.New("start must be 1 or more")
			}
			if e := ai.ArgInt(a, "end"); e != 0 && e < ai.ArgInt(a, "start") {
				return errors.New("end is before start")
			}
			if strings.TrimSpace(ai.ArgString(a, "path")) == "" {
				return errors.New("give path")
			}
			_, err := ws.Resolve(ai.ArgString(a, "path"))
			return err
		},
		describe: func(op string, a map[string]any) string {
			s, e := ai.ArgInt(a, "start"), max(ai.ArgInt(a, "end"), ai.ArgInt(a, "start"))
			what := fmt.Sprintf("line %d", s)
			if e > s {
				what = fmt.Sprintf("lines %d-%d", s, e)
			}
			switch op {
			case "discard":
				return "Discard the changes on " + what + " of " + ai.ArgString(a, "path") + "? This cannot be undone."
			case "unstage":
				return "Unstage " + what + " of " + ai.ArgString(a, "path")
			}
			return "Stage " + what + " of " + ai.ArgString(a, "path")
		},
		run: func(_ context.Context, op string, a map[string]any) (string, error) {
			abs, err := ws.Resolve(ai.ArgString(a, "path"))
			if err != nil {
				return "", err
			}
			r := rel(abs)
			s := ai.ArgInt(a, "start")
			e := max(ai.ArgInt(a, "end"), s)
			switch op {
			case "stage":
				err = git.StageLines(root, r, s, e)
			case "unstage":
				err = git.UnstageLines(root, r, s, e)
			default:
				err = git.DiscardLines(root, r, s, e)
			}
			if err != nil {
				return "", fmt.Errorf("%s lines %d-%d of %s: %w", op, s, e, r, err)
			}
			return fmt.Sprintf("%s: lines %d-%d of %s", map[string]string{"stage": "staged", "unstage": "unstaged", "discard": "discarded"}[op], s, e, r), nil
		},
	})
}

// ── git_discard ────────────────────────────────────────────────────────

func aiGitDiscardTool(root string, call aiIDECaller) ai.Tool {
	ws := ai.Workspace{Root: root}
	rel := aiRel(root)
	return aiGitOpTool(root, call, aiGitOpSpec{
		name: "git_discard",
		desc: "Throw changes away; each asks every time (Cancel first). files (paths: their uncommitted changes, staged too; cannot be undone), " +
			"all (every tracked change into a safety stash; untracked=true takes the new files too), untracked (only the new files into a safety stash).",
		ops: map[string]aiAsk{"files": aiAskEvery, "all": aiAskEvery, "untracked": aiAskEvery},
		params: map[string]any{"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "files: the files"},
			"untracked": aiBool("all: the new (untracked) files too")},
		disk: func(string, map[string]any) bool { return true },
		check: func(op string, a map[string]any) error {
			if op != "files" {
				return nil
			}
			ps := aiArgPaths(a)
			if len(ps) == 0 {
				return errors.New("give paths")
			}
			for _, p := range ps {
				if _, err := ws.Resolve(p); err != nil {
					return err
				}
			}
			return nil
		},
		describe: func(op string, a map[string]any) string {
			switch op {
			case "files":
				return "Discard the changes of " + strings.Join(aiArgPaths(a), ", ") + "? This cannot be undone."
			case "untracked":
				return "Delete the untracked files? They go into a safety stash."
			}
			if ai.ArgBool(a, "untracked") {
				return "Discard all changes and the untracked files? They go into a safety stash."
			}
			return "Discard all changes? They go into a safety stash; untracked files stay."
		},
		run: func(_ context.Context, op string, a map[string]any) (string, error) {
			switch op {
			case "files":
				var done []string
				for _, p := range aiArgPaths(a) {
					abs, err := ws.Resolve(p)
					if err != nil {
						return "", err
					}
					if err := git.Discard(root, rel(abs)); err != nil {
						return "", fmt.Errorf("discard %s: %w", rel(abs), err)
					}
					done = append(done, rel(abs))
				}
				return "discarded the changes of " + strings.Join(done, ", "), nil
			case "untracked":
				ref, err := git.DeleteUntracked(root)
				if err != nil {
					return "", err
				}
				return "moved the untracked files into " + ref + " (git_stash op pop brings them back)", nil
			}
			ref, err := git.DiscardAll(root, ai.ArgBool(a, "untracked"))
			if err != nil {
				return "", err
			}
			return "moved all changes into " + ref + " (git_stash op pop brings them back)", nil
		},
	})
}

// ── git_repo ───────────────────────────────────────────────────────────

// aiBisectText is a bisect state in words.
func aiBisectText(s git.BisectState) string {
	switch {
	case s.Done:
		return fmt.Sprintf("bisect done: the first bad commit is %s %q (op bisect_reset ends it)", shortHashOf(s.FirstBad), s.FirstBadSubject)
	case s.Active:
		left := ""
		if s.StepsLeft >= 0 {
			left = fmt.Sprintf(", about %d step%s left", s.StepsLeft, plural(s.StepsLeft))
		}
		return fmt.Sprintf("bisect: test %s %q now%s, then bisect_mark verdict good, bad or skip", s.Current, s.CurrentSubject, left)
	}
	return "no bisect in progress"
}

func aiGitRepoTool(root string, call aiIDECaller) ai.Tool {
	ws := ai.Workspace{Root: root}
	rel := aiRel(root)
	return aiGitOpTool(root, call, aiGitOpSpec{
		name: "git_repo",
		desc: "The repository. At once: identity (the name and e-mail on commits), submodules, contributors, lfs_files, bisect_status. " +
			"With Always allow git: ignore (pattern into .gitignore), untrack (path: stop tracking, the file stays), submodule_update (path, or all), " +
			"unshallow (fetch the full history). Asking every time: init (a new repository here), bisect_start (bad, good), bisect_mark (verdict good|bad|skip), bisect_reset.",
		ops: map[string]aiAsk{"identity": aiAskNo, "submodules": aiAskNo, "contributors": aiAskNo, "lfs_files": aiAskNo, "bisect_status": aiAskNo,
			"ignore": aiAskGit, "untrack": aiAskGit, "submodule_update": aiAskGit, "unshallow": aiAskGit,
			"init": aiAskEvery, "bisect_start": aiAskEvery, "bisect_mark": aiAskEvery, "bisect_reset": aiAskEvery},
		params: map[string]any{"pattern": aiStr("ignore: a .gitignore pattern"), "path": aiStr("untrack / submodule_update: the file or submodule"),
			"bad": aiStr("bisect_start: a bad commit (default HEAD)"), "good": aiStr("bisect_start: a good commit"),
			"verdict": map[string]any{"type": "string", "enum": []string{"good", "bad", "skip"}, "description": "bisect_mark: the commit under test"}},
		disk: func(op string, _ map[string]any) bool {
			return strings.HasPrefix(op, "bisect_") && op != "bisect_status"
		},
		check: func(op string, a map[string]any) error {
			switch op {
			case "ignore":
				if strings.TrimSpace(ai.ArgString(a, "pattern")) == "" {
					return aiNeed(op, "pattern")
				}
			case "untrack":
				if strings.TrimSpace(ai.ArgString(a, "path")) == "" {
					return aiNeed(op, "path")
				}
				_, err := ws.Resolve(ai.ArgString(a, "path"))
				return err
			case "bisect_start":
				if _, err := aiName(a, "bad"); err != nil {
					return err
				}
				_, err := aiNeedName(op, a, "good")
				return err
			case "bisect_mark":
				return aiOneOf("verdict", ai.ArgString(a, "verdict"), []string{"good", "bad", "skip"})
			}
			return nil
		},
		describe: func(op string, a map[string]any) string {
			switch op {
			case "ignore":
				return "Add " + ai.ArgString(a, "pattern") + " to .gitignore"
			case "untrack":
				return "Stop tracking " + ai.ArgString(a, "path") + " (the file stays)"
			case "submodule_update":
				return "Update submodule " + orWord(ai.ArgString(a, "path"), "(all)")
			case "unshallow":
				return "Fetch the full history"
			case "init":
				return "Make a new git repository in " + root + "?"
			case "bisect_start":
				return "Start a bisect between " + orWord(ai.ArgString(a, "bad"), "HEAD") + " (bad) and " + ai.ArgString(a, "good") + " (good)? It checks out commits one by one."
			case "bisect_mark":
				return "Mark the commit under test " + ai.ArgString(a, "verdict") + "? The next one is checked out."
			case "bisect_reset":
				return "End the bisect? HEAD goes back to where it was."
			}
			return "Git repository: " + op
		},
		run: func(_ context.Context, op string, a map[string]any) (string, error) {
			switch op {
			case "identity":
				name, email := git.Identity(root)
				if name == "" && email == "" {
					return "no identity is set (only the user sets it: Git: Set Identity…)", nil
				}
				return name + " <" + email + ">", nil
			case "submodules":
				subs, err := git.Submodules(root)
				if err != nil {
					return "", err
				}
				if len(subs) == 0 {
					return "no submodules", nil
				}
				var b strings.Builder
				state := map[git.SubmoduleState]string{git.SubmoduleOK: "up to date", git.SubmoduleNotInit: "not initialized",
					git.SubmoduleChanged: "at another commit", git.SubmoduleConflict: "conflicts"}
				for _, s := range subs {
					fmt.Fprintf(&b, "%s %s (%s) %s\n", s.Path, shortHashOf(s.SHA), state[s.State], s.Describe)
				}
				return strings.TrimRight(b.String(), "\n"), nil
			case "contributors":
				cs, err := git.Contributors(root)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				for _, c := range cs {
					fmt.Fprintf(&b, "%d %s <%s>\n", c.Commits, c.Name, c.Email)
				}
				return orWord(strings.TrimRight(b.String(), "\n"), "no commits"), nil
			case "lfs_files":
				fs, err := git.LFSFiles(root)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				for _, f := range fs {
					st := "downloaded"
					if f.Pointer {
						st = "pointer only"
					}
					fmt.Fprintf(&b, "%s %d bytes (%s)\n", f.Path, f.Size, st)
				}
				return orWord(strings.TrimRight(b.String(), "\n"), "no LFS files"), nil
			case "bisect_status":
				return aiBisectText(git.BisectStatus(root)), nil
			case "ignore":
				p := strings.TrimSpace(ai.ArgString(a, "pattern"))
				return "added " + p + " to .gitignore", git.AddIgnore(root, p)
			case "untrack":
				abs, err := ws.Resolve(ai.ArgString(a, "path"))
				if err != nil {
					return "", err
				}
				return "stopped tracking " + rel(abs) + " (the file stays; commit to record it)", git.Untrack(root, rel(abs))
			case "submodule_update":
				p := strings.TrimSpace(ai.ArgString(a, "path"))
				return "updated submodule " + orWord(p, "(all)"), git.SubmoduleUpdate(root, p)
			case "unshallow":
				if !git.IsShallow(root) {
					return "the history is complete already", nil
				}
				return "fetched the full history", git.Unshallow(root)
			case "init":
				if git.IsRepo(root) {
					return "", errors.New(root + " is a git repository already")
				}
				cmd := spawn.Command("git", "init", "-q", "-b", "main")
				cmd.Dir = root
				if out, err := cmd.CombinedOutput(); err != nil {
					return "", fmt.Errorf("git init: %v: %s", err, strings.TrimSpace(string(out)))
				}
				return "made a new git repository (branch main) in " + root, nil
			case "bisect_start":
				s, err := git.BisectStart(root, orWord(strings.TrimSpace(ai.ArgString(a, "bad")), "HEAD"), strings.TrimSpace(ai.ArgString(a, "good")))
				if err != nil {
					return "", err
				}
				return aiBisectText(s), nil
			case "bisect_mark":
				s, err := git.BisectMark(root, ai.ArgString(a, "verdict"))
				if err != nil {
					return "", err
				}
				return aiBisectText(s), nil
			case "bisect_reset":
				return "ended the bisect: HEAD is back", git.BisectReset(root)
			}
			return "", fmt.Errorf("unknown op %s", op)
		},
	})
}
