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

// Branches, tags and remotes for the AI (git parity): git_branch,
// git_tag and git_remote do what Source Control, GRAPH and the Git:
// palette do with them, and ask where the GUI asks. A delete on the
// remote and a force push need the name typed by the user: the tool
// opens the GUI's own card for that and returns; the user types it there
// (never the model).

// aiCurrentBranch is the branch HEAD is on ("" when detached).
func aiCurrentBranch(root string) string {
	b, err := git.GetBranch(root)
	if err != nil {
		return ""
	}
	return pushBranch(b)
}

// aiLogLines renders commits one per line (oldest or newest first, as given).
func aiLogLines(es []git.LogEntry) string {
	if len(es) == 0 {
		return "no commits"
	}
	return aiFormatLog(es)
}

// aiFileLines renders a CommitInfo's files: "M path (+3 -1)".
func aiFileLines(fs []git.CommitFile) string {
	var b strings.Builder
	for _, f := range fs {
		name := f.Path
		if f.OldPath != "" && f.OldPath != f.Path {
			name = f.OldPath + " -> " + f.Path
		}
		fmt.Fprintf(&b, "%c %s (+%d -%d)\n", f.Status, name, f.Add, f.Del)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ── git_branch ─────────────────────────────────────────────────────────

func aiGitBranchTool(root string, call aiIDECaller) ai.Tool {
	return aiGitOpTool(root, call, aiGitOpSpec{
		name: "git_branch",
		desc: "Branches: list (default; remote=true adds remote branches), compare (base, head: the commits and files head has that base lacks), " +
			"create (name, from: a branch, tag or commit; it switches to the new branch), rename (name, default the current one; new_name), " +
			"set_upstream (upstream like origin/main; branch default current) and unset_upstream ask with Always allow git; " +
			"delete (name; force=true for a branch with commits of its own, they are lost) and merge (name into the current branch; no_ff) ask every time; " +
			"delete_remote (name like origin/x) opens the user's card, where they type the branch name.",
		def: "list",
		ops: map[string]aiAsk{"list": aiAskNo, "compare": aiAskNo, "create": aiAskGit, "rename": aiAskGit,
			"set_upstream": aiAskGit, "unset_upstream": aiAskGit, "delete": aiAskEvery, "merge": aiAskEvery, "delete_remote": aiAskCard},
		params: map[string]any{
			"name": aiStr("the branch (create, rename, delete, merge, delete_remote)"), "new_name": aiStr("rename: the new name"),
			"from": aiStr("create: where it starts (default HEAD)"), "force": aiBool("delete: also a branch with commits no other branch has"),
			"no_ff": aiBool("merge: always make a merge commit"), "upstream": aiStr("set_upstream: like origin/main"),
			"branch": aiStr("set_upstream / unset_upstream: the branch (default the current one)"),
			"base":   aiStr("compare: the base (default the repository's main branch)"), "head": aiStr("compare: the other side (default HEAD)"),
			"remote": aiBool("list: remote branches too"),
		},
		disk: func(op string, a map[string]any) bool {
			return op == "merge" || op == "create" && strings.TrimSpace(ai.ArgString(a, "from")) != ""
		},
		check: func(op string, a map[string]any) error {
			var err error
			switch op {
			case "create", "delete", "merge", "delete_remote":
				_, err = aiNeedName(op, a, "name")
			case "rename":
				if _, err = aiName(a, "name"); err == nil {
					_, err = aiNeedName(op, a, "new_name")
				}
			case "set_upstream":
				_, err = aiNeedName(op, a, "upstream")
			}
			if err == nil {
				for _, k := range []string{"from", "branch", "base", "head"} {
					if _, err = aiName(a, k); err != nil {
						break
					}
				}
			}
			return err
		},
		describe: func(op string, a map[string]any) string {
			name := ai.ArgString(a, "name")
			switch op {
			case "create":
				if from := ai.ArgString(a, "from"); from != "" {
					return "Create branch " + name + " from " + from + " and switch to it"
				}
				return "Create branch " + name + " and switch to it"
			case "rename":
				return "Rename branch " + orCurrent(name) + " to " + ai.ArgString(a, "new_name")
			case "delete":
				if ai.ArgBool(a, "force") {
					return "Delete branch " + name + ", even with commits no other branch has? They are lost (the result names its last commit, so it can be made again)."
				}
				return "Delete branch " + name + "?"
			case "merge":
				d := "Merge " + name + " into " + orCurrent(aiCurrentBranch(root)) + "?"
				if n, err := git.MergeIncoming(root, name); err == nil {
					d += fmt.Sprintf(" %d commit%s come in.", n, plural(n))
				}
				return d + " If files conflict, the merge stops (git_conflicts resolves it, then continue or abort)."
			case "set_upstream":
				return "Set the upstream of " + orCurrent(ai.ArgString(a, "branch")) + " to " + ai.ArgString(a, "upstream")
			case "unset_upstream":
				return "Unset the upstream of " + orCurrent(ai.ArgString(a, "branch"))
			case "delete_remote":
				return "Ask the user to delete " + name + " on the remote (they type its name)"
			case "compare":
				return "Compare branches"
			}
			return "Git branches"
		},
		run: func(ctx context.Context, op string, a map[string]any) (string, error) {
			name := strings.TrimSpace(ai.ArgString(a, "name"))
			branchArg := func() (string, error) {
				if b := strings.TrimSpace(ai.ArgString(a, "branch")); b != "" {
					return b, nil
				}
				if b := aiCurrentBranch(root); b != "" {
					return b, nil
				}
				return "", errors.New("HEAD is detached: give branch")
			}
			switch op {
			case "list":
				bs, err := git.BranchList(root)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				for _, br := range bs {
					mark := "  "
					if br.Current {
						mark = "* "
					}
					b.WriteString(mark + br.Name)
					switch {
					case br.Gone:
						b.WriteString(" (upstream " + br.Upstream + " is gone)")
					case br.Upstream != "":
						fmt.Fprintf(&b, " (%s, %d ahead, %d behind)", br.Upstream, br.Ahead, br.Behind)
					}
					b.WriteByte('\n')
				}
				if ai.ArgBool(a, "remote") {
					rs, err := git.RemoteBranches(root)
					if err != nil {
						return "", err
					}
					for _, r := range rs {
						b.WriteString("  remote " + r + "\n")
					}
				}
				if b.Len() == 0 {
					return "no branches yet (no commit)", nil
				}
				return strings.TrimRight(b.String(), "\n"), nil
			case "compare":
				base, head := strings.TrimSpace(ai.ArgString(a, "base")), strings.TrimSpace(ai.ArgString(a, "head"))
				if base == "" {
					base = git.DefaultBranch(root)
				}
				if head == "" {
					head = "HEAD"
				}
				mb, err := git.MergeBase(root, base, head)
				if err != nil {
					return "", err
				}
				info, err := git.CommitFilesBetween(root, mb, head)
				if err != nil {
					return "", err
				}
				var commits []git.LogEntry
				if info.Commits > 0 {
					commits, err = git.SearchLog(root, git.LogFilter{Rev: head}, min(info.Commits, aiGitLogMax))
					if err != nil {
						return "", err
					}
				}
				out := fmt.Sprintf("%s has %d commit%s that %s lacks (from the merge base %s):\n%s", head, info.Commits, plural(info.Commits), base,
					shortHashOf(mb), aiLogLines(commits))
				if len(info.Files) > 0 {
					out += fmt.Sprintf("\n%d file%s changed (+%d -%d):\n%s", len(info.Files), plural(len(info.Files)), info.Add, info.Del, aiFileLines(info.Files))
				}
				return out, nil
			case "create":
				from := strings.TrimSpace(ai.ArgString(a, "from"))
				if from == "" {
					from = "HEAD"
				}
				if err := git.CheckBranchName(root, name); err != nil {
					return "", err
				}
				if err := git.CreateBranchAt(root, name, from); err != nil {
					return "", err
				}
				return "created " + name + " from " + from + " and switched to it", nil
			case "rename":
				old := name
				if old == "" {
					if old = aiCurrentBranch(root); old == "" {
						return "", errors.New("HEAD is detached: give name")
					}
				}
				nn := strings.TrimSpace(ai.ArgString(a, "new_name"))
				if err := git.RenameBranch(root, old, nn); err != nil {
					return "", err
				}
				return "renamed " + old + " to " + nn, nil
			case "delete":
				if name == aiCurrentBranch(root) {
					return "", errors.New(name + " is the current branch: switch to another one first")
				}
				tip := git.BranchTip(root, name)
				err := git.DeleteBranch(root, name, ai.ArgBool(a, "force"))
				if errors.Is(err, git.ErrBranchNotMerged) {
					return "", fmt.Errorf("%s has commits no other branch has; nothing was deleted. Ask the user, then call again with force=true (those commits are lost)", name)
				}
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("deleted %s (it was at %s; git branch %s %s brings it back)", name, tip, name, tip), nil
			case "merge":
				into := orCurrent(aiCurrentBranch(root))
				n, err := git.MergeIncoming(root, name)
				if err != nil {
					return "", err
				}
				if n == 0 {
					return "already up to date: " + name + " is merged into " + into, nil
				}
				conflicts, err := git.MergeBranch(root, name, ai.ArgBool(a, "no_ff"))
				if len(conflicts) > 0 {
					return "", fmt.Errorf("the merge of %s stopped on conflicts in %s: resolve them with git_conflicts, then git_conflicts op continue (or op abort)",
						name, strings.Join(conflicts, ", "))
				}
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("merged %s into %s (%d commit%s)", name, into, n, plural(n)), nil
			case "set_upstream":
				br, err := branchArg()
				if err != nil {
					return "", err
				}
				up := strings.TrimSpace(ai.ArgString(a, "upstream"))
				if err := git.SetUpstream(root, br, up); err != nil {
					return "", err
				}
				return br + " tracks " + up + " now", nil
			case "unset_upstream":
				br, err := branchArg()
				if err != nil {
					return "", err
				}
				if err := git.UnsetUpstream(root, br); err != nil {
					return "", err
				}
				return br + " tracks nothing now", nil
			case "delete_remote":
				if !contains(git.RemoteTrackingBranches(root), name) {
					return "", fmt.Errorf("%s is not a remote branch here (git_branch op list remote=true lists them)", name)
				}
				return aiGUICard(ctx, call, func(m *Model) (string, tea.Cmd, error) {
					m.onDeleteBranchPicked("delr-" + name)
					return "The user sees the card \"Delete " + name + " on the remote?\" (Cancel first). If they go on, they type the branch name to delete it; " +
						"nothing is deleted until they do. git_branch op list remote=true shows whether it went.", nil, nil
				})
			}
			return "", fmt.Errorf("unknown op %s", op)
		},
	})
}

// orCurrent is name, or "the current branch" when it is empty.
func orCurrent(name string) string {
	if name == "" {
		return "the current branch"
	}
	return name
}

// ── git_tag ────────────────────────────────────────────────────────────

func aiGitTagTool(root string, call aiIDECaller) ai.Tool {
	remoteOf := func() string { return gitPushRemote(root, aiCurrentBranch(root)) }
	return aiGitOpTool(root, call, aiGitOpSpec{
		name: "git_tag",
		desc: "Tags: list (default) and show (name: its commit, tagger, message) run at once; create (name, rev default HEAD, message: annotated, empty: lightweight) " +
			"asks with Always allow git; delete (local only), push (name, or all=true; to the branch's remote or remote) and checkout (name: a detached HEAD) ask every time; " +
			"delete_remote (name) opens the user's card, where they type the tag name.",
		def: "list",
		ops: map[string]aiAsk{"list": aiAskNo, "show": aiAskNo, "create": aiAskGit, "delete": aiAskEvery, "push": aiAskEvery,
			"checkout": aiAskEvery, "delete_remote": aiAskCard},
		params: map[string]any{"name": aiStr("the tag"), "rev": aiStr("create: the commit (default HEAD)"),
			"message": aiStr("create: a message makes an annotated tag"), "all": aiBool("push: every tag"),
			"remote": aiStr("push: the remote (default the current branch's remote)")},
		disk: func(op string, _ map[string]any) bool { return op == "checkout" },
		check: func(op string, a map[string]any) error {
			switch op {
			case "show", "create", "delete", "checkout", "delete_remote":
				_, err := aiNeedName(op, a, "name")
				return err
			case "push":
				if ai.ArgBool(a, "all") {
					return nil
				}
				_, err := aiNeedName(op, a, "name")
				return err
			}
			return nil
		},
		describe: func(op string, a map[string]any) string {
			name := ai.ArgString(a, "name")
			switch op {
			case "create":
				kind := "lightweight tag"
				if strings.TrimSpace(ai.ArgString(a, "message")) != "" {
					kind = "annotated tag"
				}
				rev := ai.ArgString(a, "rev")
				if rev == "" {
					rev = "HEAD"
				}
				return "Create " + kind + " " + name + " at " + rev
			case "delete":
				return "Delete tag " + name + "? It goes from this repository only; a copy on a remote stays."
			case "push":
				remote := ai.ArgString(a, "remote")
				if remote == "" {
					remote = remoteOf()
				}
				if ai.ArgBool(a, "all") {
					return "Push all tags to " + remote + "?"
				}
				return "Push tag " + name + " to " + remote + "?"
			case "checkout":
				return "Check out tag " + name + "? HEAD is detached there; files change."
			case "delete_remote":
				return "Ask the user to delete tag " + name + " on the remote (they type its name)"
			case "show":
				return "Show tag " + name
			}
			return "Git tags"
		},
		run: func(ctx context.Context, op string, a map[string]any) (string, error) {
			name := strings.TrimSpace(ai.ArgString(a, "name"))
			switch op {
			case "list":
				tags, err := git.Tags(root)
				if err != nil {
					return "", err
				}
				if len(tags) == 0 {
					return "no tags", nil
				}
				lines := make([]string, len(tags))
				for i, t := range tags {
					lines[i] = t + " " + git.TagTip(root, t)
				}
				return strings.Join(lines, "\n"), nil
			case "show":
				ti, err := git.TagDetail(root, name)
				if err != nil {
					return "", err
				}
				kind := "lightweight"
				if ti.Annotated {
					kind = "annotated, by " + ti.Tagger
				}
				out := fmt.Sprintf("%s (%s, %s) at %s %s", ti.Name, kind, ti.Date, ti.Commit, ti.Subject)
				if ti.Message != "" {
					out += "\n" + strings.TrimSpace(ti.Message)
				}
				return out, nil
			case "create":
				rev := strings.TrimSpace(ai.ArgString(a, "rev"))
				if rev == "" {
					rev = "HEAD"
				}
				if err := git.CreateTag(root, name, rev, ai.ArgString(a, "message")); err != nil {
					return "", err
				}
				return "created tag " + name + " at " + rev, nil
			case "delete":
				tip := git.TagTip(root, name)
				if err := git.DeleteTag(root, name); err != nil {
					return "", err
				}
				return fmt.Sprintf("deleted tag %s (it was at %s); a copy on a remote stays", name, tip), nil
			case "push":
				remote := strings.TrimSpace(ai.ArgString(a, "remote"))
				if remote == "" {
					remote = remoteOf()
				}
				if remote == "" {
					return "", errors.New("this repository has no remote")
				}
				if ai.ArgBool(a, "all") {
					return "pushed all tags to " + remote, git.PushAllTags(root, remote)
				}
				return "pushed " + name + " to " + remote, git.PushTag(root, remote, name)
			case "checkout":
				if err := git.CheckoutDetached(root, "refs/tags/"+name); err != nil {
					return "", err
				}
				return "checked out " + name + " (HEAD is detached; git_checkout goes back to a branch)", nil
			case "delete_remote":
				tags, err := git.Tags(root)
				if err != nil {
					return "", err
				}
				if !contains(tags, name) {
					return "", fmt.Errorf("no tag %s here", name)
				}
				return aiGUICard(ctx, call, func(m *Model) (string, tea.Cmd, error) {
					if remoteOf() == "" {
						return "", nil, errors.New("this repository has no remote")
					}
					cmd := m.onGitTagPicked(pickerKindGitTagDeleteRemote, "tag-"+name)
					return "The user sees the card \"Delete " + name + " on the remote?\" (Cancel first). If they go on, they type the tag name to delete it; nothing is deleted until they do.", cmd, nil
				})
			}
			return "", fmt.Errorf("unknown op %s", op)
		},
	})
}

// ── git_remote ─────────────────────────────────────────────────────────

func aiGitRemoteTool(root string, call aiIDECaller) ai.Tool {
	return aiGitOpTool(root, call, aiGitOpSpec{
		name: "git_remote",
		desc: "Remotes: list (default), incoming (the upstream's commits HEAD lacks, as of the last fetch) and outgoing (what a push sends) run at once; " +
			"add (name, url), rename (name, new_name), set_url (name, url), fetch_all (every remote, prune) and pull_from (remote, branch; rebase=true) ask with Always allow git; " +
			"remove (name) and push_to (remote, branch: HEAD to that branch) ask every time; force_push opens the user's Force Push card for the current branch " +
			"(with lease), where they type the branch name.",
		def: "list",
		ops: map[string]aiAsk{"list": aiAskNo, "incoming": aiAskNo, "outgoing": aiAskNo, "add": aiAskGit, "rename": aiAskGit, "set_url": aiAskGit,
			"fetch_all": aiAskGit, "pull_from": aiAskGit, "remove": aiAskEvery, "push_to": aiAskEvery, "force_push": aiAskCard},
		params: map[string]any{"name": aiStr("the remote (add, rename, set_url, remove)"), "new_name": aiStr("rename: the new name"),
			"url": aiStr("add / set_url: the URL"), "remote": aiStr("push_to / pull_from: the remote"),
			"branch": aiStr("push_to / pull_from: the branch on the remote"), "rebase": aiBool("pull_from: rebase instead of a merge")},
		disk: func(op string, _ map[string]any) bool { return op == "pull_from" },
		check: func(op string, a map[string]any) error {
			var err error
			switch op {
			case "add", "set_url":
				if _, err = aiNeedName(op, a, "name"); err == nil {
					_, err = aiNeedName(op, a, "url")
				}
			case "rename":
				if _, err = aiNeedName(op, a, "name"); err == nil {
					_, err = aiNeedName(op, a, "new_name")
				}
			case "remove":
				_, err = aiNeedName(op, a, "name")
			case "push_to":
				if _, err = aiNeedName(op, a, "remote"); err == nil {
					_, err = aiNeedName(op, a, "branch")
				}
			case "pull_from":
				if _, err = aiName(a, "remote"); err == nil {
					_, err = aiName(a, "branch")
				}
			}
			return err
		},
		describe: func(op string, a map[string]any) string {
			name := ai.ArgString(a, "name")
			switch op {
			case "add":
				return "Add remote " + name + " (" + ai.ArgString(a, "url") + ")"
			case "rename":
				return "Rename remote " + name + " to " + ai.ArgString(a, "new_name")
			case "set_url":
				return "Change the URL of " + name + " to " + ai.ArgString(a, "url")
			case "remove":
				return "Remove remote " + name + "? Its remote branches go from this repository; nothing changes on the server."
			case "fetch_all":
				return "Fetch all remotes (prune gone branches)"
			case "push_to":
				return "Push HEAD to " + ai.ArgString(a, "remote") + "/" + ai.ArgString(a, "branch") + "?"
			case "pull_from":
				what := "the upstream"
				if r := ai.ArgString(a, "remote"); r != "" {
					what = r + "/" + ai.ArgString(a, "branch")
				}
				if ai.ArgBool(a, "rebase") {
					return "Pull " + what + " (rebase)"
				}
				return "Pull " + what
			case "force_push":
				return "Ask the user to force push the current branch (they type its name)"
			}
			return "Git remotes"
		},
		run: func(ctx context.Context, op string, a map[string]any) (string, error) {
			name := strings.TrimSpace(ai.ArgString(a, "name"))
			switch op {
			case "list":
				rs, err := git.ListRemotes(root)
				if err != nil {
					return "", err
				}
				if len(rs) == 0 {
					return "no remotes", nil
				}
				var b strings.Builder
				for _, r := range rs {
					b.WriteString(r.Name + " " + r.FetchURL)
					if r.PushURL != "" && r.PushURL != r.FetchURL {
						b.WriteString(" (push " + r.PushURL + ")")
					}
					b.WriteByte('\n')
				}
				return strings.TrimRight(b.String(), "\n"), nil
			case "incoming":
				es, err := git.Incoming(root, aiGitLogMax)
				return aiLogLines(es), err
			case "outgoing":
				es, err := git.Outgoing(root, "", aiGitLogMax)
				return aiLogLines(es), err
			case "add":
				return "added remote " + name, git.AddRemote(root, name, strings.TrimSpace(ai.ArgString(a, "url")))
			case "rename":
				nn := strings.TrimSpace(ai.ArgString(a, "new_name"))
				return "renamed remote " + name + " to " + nn, git.RenameRemote(root, name, nn)
			case "set_url":
				return "changed the URL of " + name, git.SetRemoteURL(root, name, strings.TrimSpace(ai.ArgString(a, "url")))
			case "remove":
				return "removed remote " + name, git.RemoveRemote(root, name)
			case "fetch_all":
				return "fetched all remotes", git.FetchAll(root)
			case "push_to":
				remote, branch := strings.TrimSpace(ai.ArgString(a, "remote")), strings.TrimSpace(ai.ArgString(a, "branch"))
				return "pushed HEAD to " + remote + "/" + branch, git.PushTo(root, remote, branch)
			case "pull_from":
				remote, branch := strings.TrimSpace(ai.ArgString(a, "remote")), strings.TrimSpace(ai.ArgString(a, "branch"))
				if err := git.PullFrom(root, remote, branch, ai.ArgBool(a, "rebase")); err != nil {
					return "", err
				}
				return "pulled", nil
			case "force_push":
				return aiGUICard(ctx, call, func(m *Model) (string, tea.Cmd, error) {
					cmd := m.openForcePushCard()
					if !m.confirmOpen {
						return "", cmd, errors.New("there is nothing to force push: the branch is not on a remote yet, or HEAD is detached (git_push sends a new branch)")
					}
					return "The user sees the Force Push card (Cancel first; with lease: git refuses if someone pushed since the last fetch). " +
						"If they go on, they type the branch name; nothing is pushed until they do.", cmd, nil
				})
			}
			return "", fmt.Errorf("unknown op %s", op)
		},
	})
}
