package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// worktree: the AI does what the WORKTREES view does with one worktree
// (git parity), and asks where the GUI asks. pin, sleep / wake, notes,
// link and diff run at once (the web's worktrees.pin / sleep / note /
// link have no card; a sleep that ends agent tabs opens the view's own
// card). create and add_folder ask with Always allow git (the GUI's New
// dialog is the user's click). switch, archive / restore, restack and
// delete ask every time on a Cancel-first card; after yes the view's own
// flow runs with its own cards: a delete of a worktree with changes or
// unmerged commits asks again there, and the user types the branch name
// there, never the model. worktrees_list lists them.

func aiWorktreeTool(root string, call aiIDECaller) ai.Tool {
	// item loads the WORKTREES list on Update when it is not (the view
	// may be hidden) and finds the worktree arg names there.
	item := func(ctx context.Context, arg string) (wtItem, error) {
		if call == nil {
			return wtItem{}, errors.New("the IDE is not available")
		}
		var load tea.Cmd
		if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if st := m.webWTState(); st.off {
				return "", nil, errors.New("the WORKTREES view runs a demo")
			}
			load = m.worktreesLoadFor(false)
			return "", nil, nil
		}); err != nil {
			return wtItem{}, err
		}
		msg, _ := load().(wtLoadMsg)
		msg.web = true // apply it whatever the view's poll does
		var it wtItem
		_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			cmd := m.onWTLoad(msg)
			st := m.wt
			if st.err != "" {
				return "", cmd, errors.New(st.err)
			}
			a := strings.TrimSpace(arg)
			if a == "" {
				a = wtRealDir(root)
			}
			e, ok := wtFind(st.entries, a)
			if !ok {
				for _, x := range st.entries { // an archived or gone one, by its path or branch
					if x.Path == a || x.Branch == a {
						e, ok = x, true
					}
				}
			}
			if !ok {
				return "", cmd, fmt.Errorf("%q is not a worktree of this repository (see worktrees_list)", arg)
			}
			if it, ok = m.wtItemByPath(e.Path); !ok {
				return "", cmd, fmt.Errorf("%q is not in the WORKTREES list", arg)
			}
			return "", cmd, nil
		})
		return it, err
	}
	// onItem runs fn on Update on the worktree arg names.
	onItem := func(ctx context.Context, a map[string]any, fn func(m *Model, it wtItem) (string, tea.Cmd, error)) (string, error) {
		it, err := item(ctx, ai.ArgString(a, "worktree"))
		if err != nil {
			return "", err
		}
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			cur, ok := m.wtItemByPath(it.e.Path)
			if !ok {
				return "", nil, errors.New("the worktree is not in the list any more")
			}
			return fn(m, cur)
		})
	}
	// flow runs one of the view's own flows; a card it opens is the
	// user's to answer.
	flow := func(m *Model, it wtItem, run func(*Model, wtItem) tea.Cmd, what string) (string, tea.Cmd, error) {
		if m.confirmOpen || m.connectOpen {
			return "", nil, errors.New("termocode is asking the user something else; try again after they answer")
		}
		m.wt.cursorPath = it.e.Path
		cmd := run(m, it)
		if m.confirmOpen || m.promptOpen {
			return "The user sees the WORKTREES card for " + wtTitle(it) + " (" + what + "); it says what goes. Nothing more happens until they answer; " +
				"worktrees_list shows the result.", cmd, nil
		}
		return "Started the WORKTREES view's own " + what + " of " + wtTitle(it) + ": its cards (Cancel first) ask the user what goes, and nothing is lost until they answer; worktrees_list shows the result.", cmd, nil
	}
	return aiGitOpTool(root, call, aiGitOpSpec{
		name: "worktree",
		desc: "One worktree of the WORKTREES view (worktree: a path, branch or name from worktrees_list; default this workspace's). " +
			"At once: notes (its notes), diff (its changes vs its base), pin / unpin, sleep / wake, link (kind issue|pr|none, number). " +
			"With Always allow git: create (name or branch, from: where it starts; the folder goes next to the repository), add_folder (add it to this window). " +
			"Asking every time: switch (reopen this window in it), archive (its folder goes, the branch stays) / restore, restack (replay a child onto its parent's branch), " +
			"delete (folder and branch). After yes the view's own cards follow: a delete with changes or unmerged commits asks the user there, and they type the branch name.",
		ops: map[string]aiAsk{"notes": aiAskNo, "diff": aiAskNo, "pin": aiAskNo, "unpin": aiAskNo, "sleep": aiAskNo, "wake": aiAskNo, "link": aiAskNo,
			"create": aiAskGit, "add_folder": aiAskGit, "switch": aiAskEvery, "archive": aiAskEvery, "restore": aiAskEvery, "restack": aiAskEvery, "delete": aiAskEvery},
		params: map[string]any{"worktree": aiStr("a path, branch or name from worktrees_list (default: this workspace's)"),
			"name": aiStr("create: its name (the branch is made from it)"), "branch": aiStr("create: the branch (default from the name)"),
			"from":   aiStr("create: the branch, tag or commit it starts from (default the current branch)"),
			"kind":   map[string]any{"type": "string", "enum": []string{"issue", "pr", "none"}, "description": "link: what number is"},
			"number": aiInt("link: the issue or pull request number")},
		check: func(op string, a map[string]any) error {
			switch op {
			case "create":
				if strings.TrimSpace(ai.ArgString(a, "name")) == "" && strings.TrimSpace(ai.ArgString(a, "branch")) == "" {
					return errors.New("create needs name or branch")
				}
				for _, k := range []string{"branch", "from"} {
					if _, err := aiName(a, k); err != nil {
						return err
					}
				}
			case "link":
				k := ai.ArgString(a, "kind")
				if err := aiOneOf("kind", k, []string{"issue", "pr", "none"}); err != nil {
					return err
				}
				if k != "none" && ai.ArgInt(a, "number") <= 0 {
					return aiNeed(op, "number")
				}
			}
			return nil
		},
		describe: func(op string, a map[string]any) string {
			w := orWord(ai.ArgString(a, "worktree"), "this workspace's worktree")
			switch op {
			case "create":
				return "Make a new worktree " + orWord(ai.ArgString(a, "name"), ai.ArgString(a, "branch")) + " from " + orWord(ai.ArgString(a, "from"), "the current branch")
			case "add_folder":
				return "Add worktree " + w + " as a folder of this window"
			case "switch":
				return "Reopen this window in worktree " + w + "? The session of this folder is kept."
			case "archive":
				return "Archive worktree " + w + "? Its folder goes, its branch stays; the next card says what goes."
			case "restore":
				return "Restore worktree " + w + " from its branch?"
			case "restack":
				return "Restack worktree " + w + " onto its parent's branch? Its commits are replayed (new hashes)."
			case "delete":
				return "Delete worktree " + w + " and its branch? The next cards say what is lost."
			}
			return "Worktree " + w + ": " + op
		},
		run: func(ctx context.Context, op string, a map[string]any) (string, error) {
			switch op {
			case "create":
				return aiWorktreeCreate(root, a)
			case "notes":
				return onItem(ctx, a, func(_ *Model, it wtItem) (string, tea.Cmd, error) {
					ns := it.e.Meta.Notes
					if len(ns) == 0 {
						return wtTitle(it) + " has no notes", nil, nil
					}
					var b strings.Builder
					for _, n := range ns {
						fmt.Fprintf(&b, "%s %s\n", n.At.Format("2006-01-02 15:04"), aiCleanText(n.Text))
					}
					return strings.TrimRight(b.String(), "\n"), nil, nil
				})
			case "diff":
				var path, base string
				if _, err := onItem(ctx, a, func(m *Model, it wtItem) (string, tea.Cmd, error) {
					if it.e.Gone {
						return "", nil, errors.New("its folder is gone")
					}
					path, base = it.e.Path, m.wtDiffBase(it)
					return "", nil, nil
				}); err != nil {
					return "", err
				}
				if base == "" {
					b, err := git.DefaultBase(path)
					if err != nil {
						return "", err
					}
					base = b
				}
				mb, files, err := git.BaseDiffFiles(path, base)
				if err != nil {
					return "", err
				}
				d, cut, err := git.BranchDiff(path, base, aiGitShowMax)
				if err != nil {
					return "", err
				}
				out := fmt.Sprintf("%d file%s changed since %s (merge base %s): %s\n\n%s", len(files), plural(len(files)), base, shortHashOf(mb), strings.Join(files, ", "), d)
				if cut {
					out += fmt.Sprintf("\n[cut at %d KB]", aiGitShowMax>>10)
				}
				return out, nil
			case "pin", "unpin":
				on := op == "pin"
				return onItem(ctx, a, func(m *Model, it wtItem) (string, tea.Cmd, error) {
					if err := m.wtTouchMeta(it.e.Path, func(md *worktrees.Meta) {
						if md.Pinned != on {
							md.Pinned, md.Order = on, 0
						}
					}); err != nil {
						return "", nil, err
					}
					return op + "ned " + wtTitle(it), nil, nil
				})
			case "sleep", "wake":
				return onItem(ctx, a, func(m *Model, it wtItem) (string, tea.Cmd, error) {
					asleep := it.e.Meta.State == worktrees.StateAsleep
					if asleep == (op == "sleep") {
						return wtTitle(it) + " is " + map[bool]string{true: "asleep", false: "awake"}[asleep] + " already", nil, nil
					}
					if m.confirmOpen {
						return "", nil, errors.New("termocode is asking the user something else; try again after they answer")
					}
					cmd := m.wtSleepItem(it)
					if m.confirmOpen && m.confirmKind == confirmKindWTSleep {
						return "The user sees the sleep card (it would end agent tabs or take children along): Keep running is first.", cmd, nil
					}
					return map[string]string{"sleep": "put " + wtTitle(it) + " to sleep", "wake": "woke " + wtTitle(it)}[op], cmd, nil
				})
			case "link":
				return onItem(ctx, a, func(m *Model, it wtItem) (string, tea.Cmd, error) {
					kind, n := ai.ArgString(a, "kind"), ai.ArgInt(a, "number")
					if err := m.wtTouchMeta(it.e.Path, func(md *worktrees.Meta) {
						md.Issue, md.PR, md.Link = 0, 0, worktrees.LinkInfo{}
						switch kind {
						case "issue":
							md.Issue = n
						case "pr":
							md.PR = n
						}
					}); err != nil {
						return "", nil, err
					}
					if kind == "none" {
						return "unlinked " + wtTitle(it), nil, nil
					}
					return fmt.Sprintf("linked %s #%d to %s (its title, state and CI are read on the next load)", kind, n, wtTitle(it)), nil, nil
				})
			case "add_folder":
				return onItem(ctx, a, func(m *Model, it wtItem) (string, tea.Cmd, error) {
					if why := wtUsable(it); why != "" {
						return "", nil, errors.New(why)
					}
					return "added " + wtTitle(it) + " as a folder of this window", wtAddFolder(m, it.e.Path), nil
				})
			case "switch":
				return onItem(ctx, a, func(m *Model, it wtItem) (string, tea.Cmd, error) {
					if why := wtUsable(it); why != "" {
						return "", nil, errors.New(why)
					}
					if it.here {
						return "this window is in " + wtTitle(it) + " already", nil, nil
					}
					_ = m.wtTouchMeta(it.e.Path, func(md *worktrees.Meta) { md.LastSeen = clock.Now() })
					return "reopening this window in " + it.e.Path, wtOpenFolder(m, it.e.Path), nil
				})
			case "archive", "restore":
				return onItem(ctx, a, func(m *Model, it wtItem) (string, tea.Cmd, error) {
					archived := it.e.Meta.State == worktrees.StateArchived
					switch {
					case op == "archive" && archived:
						return wtTitle(it) + " is archived already (op restore brings it back)", nil, nil
					case op == "restore" && !archived:
						return "", nil, errors.New(wtTitle(it) + " is not archived")
					}
					if t, d := wtRefuse(it); t != "" {
						return "", nil, errors.New(t + ". " + d)
					}
					return flow(m, it, (*Model).wtArchiveItem, op)
				})
			case "restack":
				return onItem(ctx, a, func(m *Model, it wtItem) (string, tea.Cmd, error) {
					if onto, _ := m.wtStackOnto(it); onto == "" {
						return "", nil, errors.New(wtTitle(it) + " is not stacked: only a child worktree restacks onto its parent's branch")
					}
					return flow(m, it, func(m *Model, _ wtItem) tea.Cmd { return m.worktreeRestack() }, "restack")
				})
			case "delete":
				return onItem(ctx, a, func(m *Model, it wtItem) (string, tea.Cmd, error) {
					if t, d := wtRefuse(it); t != "" {
						return "", nil, errors.New(t + ". " + d)
					}
					return flow(m, it, (*Model).wtDeleteItem, "delete")
				})
			}
			return "", fmt.Errorf("unknown op %s", op)
		},
	})
}

// aiWorktreeCreate makes a worktree as the New dialog's Create does: the
// branch and folder drafted from the name, the include / shared files of
// .termocode/worktree.json copied or linked, the name saved. The setup
// commands are not run (they wait for the user's trust in the view).
func aiWorktreeCreate(root string, a map[string]any) (string, error) {
	prep := wtNewPrep(root, worktreeDirSetting())
	if prep.err != nil {
		return "", prep.err
	}
	f := prep.f
	from := orWord(strings.TrimSpace(ai.ArgString(a, "from")), f.from)
	name := strings.TrimSpace(ai.ArgString(a, "name"))
	branch, folder := f.draft(wtFormVals{name: name, branch: ai.ArgString(a, "branch"), from: from})
	if branch == "" {
		return "", errors.New("give a name or a branch")
	}
	if err := git.CheckBranchName(root, branch); err != nil {
		return "", err
	}
	files, err := wtCreateRun(f, folder, branch, from)
	if err != nil {
		return "", errors.New(wtCreateError(err))
	}
	_ = worktrees.Update(f.common, func(mm map[string]worktrees.Meta) {
		md := mm[folder]
		if md.Name == "" && name != "" && filepath.Base(folder) != name && worktreeform.FolderName(branch) != name {
			md.Name = name
		}
		md.Created, md.LastSeen = clock.Now(), clock.Now()
		mm[folder] = md
	})
	out := "made worktree " + folder + " on the new branch " + branch + " from " + from
	if c, l := worktreeform.Counts(files); c+l > 0 {
		out += fmt.Sprintf("; copied %d and linked %d file%s", c, l, plural(c+l))
	}
	if len(f.cfg.Setup) > 0 {
		out += "; its setup commands did not run (the user starts them from WORKTREES)"
	}
	return out, nil
}
