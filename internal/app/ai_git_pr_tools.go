package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
)

// Pull requests and CI for the AI (git parity): git_pr does what the PR
// list, the PR card, the CI card and the review threads do, through the
// laptop's own gh runner (the CI watcher's: its sign-in stays there).
// Reads run at once; everything that acts on GitHub in the user's name
// (create, merge, review, comment, close …) or changes the files
// (checkout) asks every time on a Cancel-first card, as the web's
// pr.merge / pr.review / pr.checkout cards do. Merges never pass
// --admin, --auto or --delete-branch.

var aiPRReviewKinds = map[string]ghci.ReviewKind{"approve": ghci.ReviewApprove, "request_changes": ghci.ReviewRequestChanges, "comment": ghci.ReviewComment}

var aiCIStateWords = map[ghci.State]string{ghci.StateNone: "none", ghci.StatePending: "pending", ghci.StatePass: "pass", ghci.StateFail: "fail"}

// aiGHError says why gh gave nothing, in the words of the PR card.
func aiGHError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ghci.ErrNoGitHub):
		return errors.New("this repository has no GitHub remote")
	case ghci.Quiet(err):
		return errors.New(ciGHHint)
	}
	return err
}

// aiPRLine is one pull request in a list.
func aiPRLine(p ghci.PRItem) string {
	s := fmt.Sprintf("#%d %s (%s -> %s, by %s", p.Number, p.Title, p.Branch, p.Base, p.Author)
	if p.Draft {
		s += ", draft"
	}
	if p.State != "" && p.State != "OPEN" {
		s += ", " + strings.ToLower(p.State)
	}
	if p.Review != "" {
		s += ", " + p.Review
	}
	if st := p.ChecksState(); st != ghci.StateNone {
		s += ", checks " + aiCIStateWords[st]
	}
	return s + ") " + p.URL
}

func aiGitPRTool(root string, call aiIDECaller) ai.Tool {
	// gh is the laptop's CI runner (read on Update: tests swap it).
	gh := func(ctx context.Context) (ghci.Runner, error) {
		if call == nil {
			return nil, errors.New("the IDE is not available")
		}
		var run ghci.Runner
		_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			run = m.ciW().runner()
			return "", nil, nil
		})
		return run, err
	}
	number := func(a map[string]any) int { return ai.ArgInt(a, "number") }
	ref := func(a map[string]any) string { return prRef(number(a)) }
	needsNumber := map[string]bool{"view": true, "diff": true, "threads": true, "checkout": true, "merge": true, "review": true,
		"reply": true, "comment": true, "draft": true, "ready": true, "close": true, "reopen": true}
	return aiGitOpTool(root, call, aiGitOpSpec{
		name: "git_pr",
		desc: "GitHub pull requests and CI (gh, signed in on this machine). At once: list (state open|closed|merged|all, author, search: GitHub search words, mine=true), " +
			"view (number: state, branches, mergeable, checks, files, reviews), diff (number), threads (number: review threads with their comment ids), " +
			"checks (number, or none: the current branch's CI runs and its PR), ci_log (run: the failed log of a workflow run). " +
			"Asking every time: create (title, body, base, draft, reviewers, labels; the current branch, pushed), checkout (number), merge (number, method squash|merge|rebase), " +
			"review (number, kind approve|request_changes|comment, body), reply (number, comment_id, body), resolve_thread (thread_id, resolved), " +
			"comment (number, path, line, side RIGHT|LEFT, body), draft / ready / close / reopen (number), rerun (number, or none: the branch's failed runs).",
		def: "list",
		ops: map[string]aiAsk{"list": aiAskNo, "view": aiAskNo, "diff": aiAskNo, "threads": aiAskNo, "checks": aiAskNo, "ci_log": aiAskNo,
			"create": aiAskEvery, "checkout": aiAskEvery, "merge": aiAskEvery, "review": aiAskEvery, "reply": aiAskEvery, "resolve_thread": aiAskEvery,
			"comment": aiAskEvery, "draft": aiAskEvery, "ready": aiAskEvery, "close": aiAskEvery, "reopen": aiAskEvery, "rerun": aiAskEvery},
		params: map[string]any{
			"number": aiInt("the pull request number"), "state": map[string]any{"type": "string", "enum": []string{"open", "closed", "merged", "all"}, "description": "list: which"},
			"author": aiStr("list: a login or @me"), "search": aiStr("list: GitHub search words"), "mine": aiBool("list: the ones asking for my review and mine"),
			"title": aiStr("create: the title"), "body": aiStr("create / review / reply / comment: the text"), "base": aiStr("create: the base branch"),
			"draft":      aiBool("create: as a draft"),
			"reviewers":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "create: logins or org/team"},
			"labels":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "create: labels"},
			"method":     map[string]any{"type": "string", "enum": []string{"squash", "merge", "rebase"}, "description": "merge: how (default squash)"},
			"kind":       map[string]any{"type": "string", "enum": []string{"approve", "request_changes", "comment"}, "description": "review: which"},
			"comment_id": aiInt("reply: the comment id from threads"), "thread_id": aiStr("resolve_thread: the thread id from threads"),
			"resolved": aiBool("resolve_thread: true resolves, false opens it again"), "path": aiStr("comment: the file"),
			"line": aiInt("comment: the line"), "side": map[string]any{"type": "string", "enum": []string{"RIGHT", "LEFT"}, "description": "comment: the new (RIGHT) or old (LEFT) file"},
			"run": aiInt("ci_log: the workflow run id (from checks)"),
		},
		disk: func(op string, _ map[string]any) bool { return op == "checkout" },
		check: func(op string, a map[string]any) error {
			if needsNumber[op] && number(a) <= 0 {
				return aiNeed(op, "number")
			}
			switch op {
			case "create":
				if strings.TrimSpace(ai.ArgString(a, "title")) == "" {
					return aiNeed(op, "title")
				}
				if _, err := aiName(a, "base"); err != nil {
					return err
				}
			case "merge":
				if m := ai.ArgString(a, "method"); m != "" {
					return aiOneOf("method", m, []string{"squash", "merge", "rebase"})
				}
			case "review":
				k := ai.ArgString(a, "kind")
				if _, ok := aiPRReviewKinds[k]; !ok {
					return errors.New("give kind: approve, request_changes or comment")
				}
				if k != "approve" && strings.TrimSpace(ai.ArgString(a, "body")) == "" {
					return errors.New(k + " needs body")
				}
			case "reply":
				if ai.ArgInt(a, "comment_id") <= 0 || strings.TrimSpace(ai.ArgString(a, "body")) == "" {
					return errors.New("reply needs comment_id and body")
				}
			case "resolve_thread":
				if strings.TrimSpace(ai.ArgString(a, "thread_id")) == "" {
					return aiNeed(op, "thread_id")
				}
			case "comment":
				if strings.TrimSpace(ai.ArgString(a, "path")) == "" || ai.ArgInt(a, "line") <= 0 || strings.TrimSpace(ai.ArgString(a, "body")) == "" {
					return errors.New("comment needs path, line and body")
				}
			case "ci_log":
				if ai.ArgInt(a, "run") <= 0 {
					return aiNeed(op, "run")
				}
			}
			return nil
		},
		describe: func(op string, a map[string]any) string {
			body := strings.TrimSpace(ai.ArgString(a, "body"))
			if r := []rune(body); len(r) > 300 {
				body = string(r[:300]) + "…"
			}
			switch op {
			case "create":
				d := fmt.Sprintf("Open a pull request from %s into %s as you on GitHub? Title: %q", orCurrent(aiCurrentBranch(root)),
					orWord(ai.ArgString(a, "base"), "the default branch"), ai.ArgString(a, "title"))
				if ai.ArgBool(a, "draft") {
					d += " (draft)"
				}
				if body != "" {
					d += "\n\n" + body
				}
				return d
			case "checkout":
				return "Switch to the branch of " + ref(a) + "? Files change; with uncommitted changes it stops."
			case "merge":
				return "Merge " + ref(a) + " (" + orWord(ai.ArgString(a, "method"), "squash") + ") on GitHub? The branch stays; the repository's rules still apply."
			case "review":
				d := map[string]string{"approve": "Approve ", "request_changes": "Request changes on ", "comment": "Comment on "}[ai.ArgString(a, "kind")] + ref(a) + " as you on GitHub?"
				if body != "" {
					d += " Message: " + body
				}
				return d
			case "reply":
				return "Reply on " + ref(a) + " as you? " + body
			case "resolve_thread":
				if ai.ArgBool(a, "resolved") {
					return "Resolve a review thread as you?"
				}
				return "Open a review thread again as you?"
			case "comment":
				return fmt.Sprintf("Comment on %s:%d of %s as you? %s", ai.ArgString(a, "path"), ai.ArgInt(a, "line"), ref(a), body)
			case "draft":
				return "Turn " + ref(a) + " back into a draft?"
			case "ready":
				return "Mark " + ref(a) + " ready for review?"
			case "close":
				return "Close " + ref(a) + " without merging?"
			case "reopen":
				return "Reopen " + ref(a) + "?"
			case "rerun":
				if number(a) > 0 {
					return "Re-run the failed checks of " + ref(a) + "?"
				}
				return "Re-run the failed CI runs of " + orCurrent(aiCurrentBranch(root)) + "?"
			case "view", "diff", "threads":
				return "Read " + ref(a)
			}
			return "Pull requests: " + op
		},
		run: func(ctx context.Context, op string, a map[string]any) (string, error) {
			run, err := gh(ctx)
			if err != nil {
				return "", err
			}
			n := number(a)
			switch op {
			case "list":
				var items []ghci.PRItem
				if ai.ArgBool(a, "mine") {
					review, mine, err := ghci.MyPRs(ctx, run, root)
					if err != nil {
						return "", aiGHError(err)
					}
					var b strings.Builder
					b.WriteString("asking for my review:\n")
					for _, p := range review {
						b.WriteString("- " + aiPRLine(p) + "\n")
					}
					b.WriteString("mine:\n")
					for _, p := range mine {
						b.WriteString("- " + aiPRLine(p) + "\n")
					}
					return strings.TrimRight(b.String(), "\n"), nil
				}
				items, err = ghci.ListPRsFilter(ctx, run, root, ghci.PRFilter{State: ai.ArgString(a, "state"), Author: ai.ArgString(a, "author"), Search: ai.ArgString(a, "search")})
				if err != nil {
					return "", aiGHError(err)
				}
				if len(items) == 0 {
					return "no pull requests", nil
				}
				lines := make([]string, len(items))
				for i, p := range items {
					lines[i] = aiPRLine(p)
				}
				return strings.Join(lines, "\n"), nil
			case "view":
				d, err := ghci.ViewPR(ctx, run, root, n)
				if err != nil {
					return "", aiGHError(err)
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%s\nstate %s, mergeable %s, head %s\n", aiPRLine(d.PRItem), d.State, d.Mergeable, shortHashOf(d.HeadSHA))
				if s := strings.TrimSpace(d.Body); s != "" {
					b.WriteString("\n" + aiGitCap(s, 8<<10) + "\n")
				}
				for _, c := range d.Checks {
					fmt.Fprintf(&b, "check %s: %s %s\n", c.Name, aiCIStateWords[c.State], c.URL)
				}
				for _, f := range d.Files {
					fmt.Fprintf(&b, "file %s (+%d -%d)\n", f.Path, f.Additions, f.Deletions)
				}
				for _, r := range d.Reviews {
					fmt.Fprintf(&b, "%s %s %s: %s\n", r.When, r.Author, strings.ToLower(r.State), aiGitCap(strings.TrimSpace(r.Body), 2<<10))
				}
				return strings.TrimRight(b.String(), "\n"), nil
			case "diff":
				out, errOut, err := run(ctx, root, "pr", "diff", strconv.Itoa(n))
				if err != nil {
					return "", aiGHError(fmt.Errorf("gh pr diff: %v %s", err, strings.TrimSpace(errOut)))
				}
				return aiGitCap(out, aiGitPatchMax), nil
			case "threads":
				ts, err := ghci.Threads(ctx, run, root, n)
				if err != nil {
					return "", aiGHError(err)
				}
				if len(ts) == 0 {
					return "no review threads", nil
				}
				var b strings.Builder
				for _, t := range ts {
					st := "open"
					if t.Resolved {
						st = "resolved"
					}
					if t.Outdated {
						st += ", outdated"
					}
					fmt.Fprintf(&b, "thread %s on %s:%d (%s)\n", t.ID, t.Path, t.Line, st)
					for _, c := range t.Comments {
						fmt.Fprintf(&b, "  comment %d by %s: %s\n", c.ID, c.Author, aiGitCap(strings.TrimSpace(c.Body), 2<<10))
					}
				}
				return strings.TrimRight(b.String(), "\n"), nil
			case "checks":
				if n > 0 {
					d, err := ghci.ViewPR(ctx, run, root, n)
					if err != nil {
						return "", aiGHError(err)
					}
					if len(d.Checks) == 0 {
						return ref(a) + " has no checks", nil
					}
					var b strings.Builder
					fmt.Fprintf(&b, "%s checks: %s\n", ref(a), aiCIStateWords[d.ChecksState()])
					for _, c := range d.Checks {
						fmt.Fprintf(&b, "%s: %s %s\n", c.Name, aiCIStateWords[c.State], c.URL)
					}
					return strings.TrimRight(b.String(), "\n"), nil
				}
				branch := aiCurrentBranch(root)
				if branch == "" {
					return "", errors.New("HEAD is detached: give number")
				}
				s, err := ghci.Fetch(ctx, run, root, branch)
				if err != nil {
					return "", aiGHError(err)
				}
				var b strings.Builder
				fmt.Fprintf(&b, "CI of %s at %s: %s\n", branch, shortHashOf(s.HeadSHA), aiCIStateWords[s.State()])
				for _, r := range s.Runs {
					st := "pass"
					switch {
					case r.Pending():
						st = "pending"
					case r.Failed():
						st = "fail"
					case r.Conclusion != "success":
						st = r.Conclusion
					}
					fmt.Fprintf(&b, "run %d %s: %s (%s) %s\n", r.ID, r.Workflow, st, r.Title, r.URL)
				}
				if p := s.PR; p != nil {
					fmt.Fprintf(&b, "pull request #%d %s (%s, review %s)\n", p.Number, p.Title, strings.ToLower(p.State), orWord(p.Review, "none"))
				} else {
					b.WriteString("no pull request for this branch\n")
				}
				return strings.TrimRight(b.String(), "\n"), nil
			case "ci_log":
				fl, err := ghci.FetchFailedLog(ctx, run, root, int64(ai.ArgInt(a, "run")))
				if err != nil {
					return "", aiGHError(err)
				}
				out := fmt.Sprintf("job %s, step %s (%d lines):\n%s", fl.Job, fl.Step, fl.Total, strings.Join(fl.Lines, "\n"))
				if len(fl.OtherJobs) > 0 {
					out += "\nother failed jobs: " + strings.Join(fl.OtherJobs, ", ")
				}
				return out, nil
			case "create":
				branch := aiCurrentBranch(root)
				switch {
				case branch == "":
					return "", errors.New("HEAD is detached: switch to the branch of the pull request first")
				case !git.HasUpstream(root):
					return "", errors.New(branch + " is not on a remote yet: push it first (git_push, or git_remote op push_to)")
				}
				url, err := ghci.Create(ctx, run, root, ghci.NewPR{Title: ai.ArgString(a, "title"), Body: ai.ArgString(a, "body"),
					Base: strings.TrimSpace(ai.ArgString(a, "base")), Branch: branch, Draft: ai.ArgBool(a, "draft"),
					Reviewers: aiArgList(a, "reviewers"), Labels: aiArgList(a, "labels")})
				if err != nil {
					return "", aiGHError(err)
				}
				return "opened the pull request " + url, nil
			case "checkout":
				if dirty, err := git.Dirty(root); err != nil || dirty {
					return "", errors.New("not checked out: there are uncommitted changes; commit, stash or discard them first")
				}
				return "checked out " + ref(a), aiGHError(ghci.Checkout(ctx, run, root, n))
			case "merge":
				method := ghci.MergeMethod(orWord(ai.ArgString(a, "method"), "squash"))
				return "merged " + ref(a) + " (" + string(method) + ")", aiGHError(ghci.Merge(ctx, run, root, n, method))
			case "review":
				k := ai.ArgString(a, "kind")
				return "sent the review (" + k + ") of " + ref(a), aiGHError(ghci.Review(ctx, run, root, n, aiPRReviewKinds[k], ai.ArgString(a, "body")))
			case "reply":
				return "replied on " + ref(a), aiGHError(ghci.Reply(ctx, run, root, n, int64(ai.ArgInt(a, "comment_id")), ai.ArgString(a, "body")))
			case "resolve_thread":
				on := ai.ArgBool(a, "resolved")
				word := map[bool]string{true: "resolved", false: "opened again"}[on]
				return word + " the thread", aiGHError(ghci.SetResolved(ctx, run, root, strings.TrimSpace(ai.ArgString(a, "thread_id")), on))
			case "comment":
				d, err := ghci.ViewPR(ctx, run, root, n)
				if err != nil {
					return "", aiGHError(err)
				}
				side := orWord(ai.ArgString(a, "side"), "RIGHT")
				err = ghci.AddComment(ctx, run, root, n, ghci.LineComment{Commit: d.HeadSHA, Path: ai.ArgString(a, "path"), Line: ai.ArgInt(a, "line"), Side: side, Body: ai.ArgString(a, "body")})
				return "commented on " + ref(a), aiGHError(err)
			case "draft", "ready":
				return map[string]string{"draft": "turned " + ref(a) + " into a draft", "ready": "marked " + ref(a) + " ready for review"}[op],
					aiGHError(ghci.SetDraft(ctx, run, root, n, op == "draft"))
			case "close", "reopen":
				return map[string]string{"close": "closed ", "reopen": "reopened "}[op] + ref(a), aiGHError(ghci.SetClosed(ctx, run, root, n, op == "close"))
			case "rerun":
				var ids []int64
				if n > 0 {
					d, err := ghci.ViewPR(ctx, run, root, n)
					if err != nil {
						return "", aiGHError(err)
					}
					ids = ghci.FailedRunIDs(d.Checks)
				} else {
					s, err := ghci.Fetch(ctx, run, root, aiCurrentBranch(root))
					if err != nil {
						return "", aiGHError(err)
					}
					for _, r := range s.Runs {
						if r.Failed() {
							ids = append(ids, r.ID)
						}
					}
				}
				if err := ghci.RerunFailed(ctx, run, root, ids); err != nil {
					return "", aiGHError(err)
				}
				return fmt.Sprintf("re-ran the failed jobs of %d run%s", len(ids), plural(len(ids))), nil
			}
			return "", fmt.Errorf("unknown op %s", op)
		},
	})
}
