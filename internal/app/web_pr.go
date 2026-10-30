package app

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Pull requests and CI checks on the desktop page (WEBP-5). gh runs on
// the laptop through the CI watcher's runner (its sign-in stays there;
// a headless run never starts it), off the UI thread. The page reads
// lists, one pull request and the branch's CI; merging, reviewing and
// checking out wait for a laptop card.

func init() {
	webOps[webproto.OpPRList] = (*Model).webPRList
	webOps[webproto.OpPRView] = (*Model).webPRView
	webOps[webproto.OpCIStatus] = (*Model).webCIStatus
	webAskOps[webproto.OpPRMerge] = (*Model).webPRMerge
	webAskOps[webproto.OpPRReview] = (*Model).webPRReview
	webAskOps[webproto.OpPRCheckout] = (*Model).webPRCheckout
}

var webCIStates = map[ghci.State]string{ghci.StateNone: "none", ghci.StatePending: "pending", ghci.StatePass: "pass", ghci.StateFail: "fail"}

// webGHFail answers why gh gave nothing: gh missing or not signed in,
// no GitHub remote, or gh's own reason.
func webGHFail(call *webCall, err error) {
	switch {
	case errors.Is(err, ghci.ErrNoGitHub):
		call.fail(webproto.CodeConflict, "This repository has no GitHub remote.")
	case ghci.Quiet(err):
		call.fail(webproto.CodeConflict, ciGHHint+" (on the laptop)")
	default:
		call.fail(webproto.CodeInternal, "gh said: "+webName(err.Error()))
	}
}

// webPRChecks are the checks for the page.
func webPRChecks(cs []ghci.Check) []webproto.PRCheck {
	out := []webproto.PRCheck{}
	for _, c := range cs {
		out = append(out, webproto.PRCheck{Name: webName(c.Name), State: webCIStates[c.State], URL: webName(c.URL)})
	}
	return out
}

func webPRItem(p ghci.PRItem) webproto.PRItem {
	return webproto.PRItem{Number: p.Number, Title: webName(p.Title), Author: webName(p.Author), Branch: webName(p.Branch), Base: webName(p.Base),
		URL: webName(p.URL), Draft: p.Draft, Review: webPRReview(p.Review), Checks: webCIStates[p.ChecksState()]}
}

// webPRReview keeps the review decisions the page knows.
func webPRReview(r string) string {
	switch r {
	case "APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED":
		return r
	}
	return ""
}

// webGH runs fn with the laptop's gh runner off the UI thread; what fn
// returns (if not nil) runs on Update after it.
func (m *Model) webGH(call *webCall, fn func(ctx context.Context, run ghci.Runner, dir string) func(m *Model)) tea.Cmd {
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	run := m.ciW().runner()
	call.later()
	return func() tea.Msg {
		if then := fn(call.ctx, run, root); then != nil {
			return applyMsg(func(m *Model) tea.Cmd { then(m); return nil })
		}
		return nil
	}
}

func (m *Model) webPRList(call *webCall) tea.Cmd {
	return m.webGH(call, func(ctx context.Context, run ghci.Runner, dir string) func(m *Model) {
		items, err := ghci.ListPRs(ctx, run, dir)
		if err != nil {
			webGHFail(call, err)
			return nil
		}
		out := webproto.PRList{Items: []webproto.PRItem{}}
		for _, p := range items {
			out.Items = append(out.Items, webPRItem(p))
		}
		call.reply(out)
		return nil
	})
}

func (m *Model) webPRView(call *webCall) tea.Cmd {
	n := call.args.(*webproto.PRNumberArgs).Number
	return m.webGH(call, func(ctx context.Context, run ghci.Runner, dir string) func(m *Model) {
		d, err := ghci.ViewPR(ctx, run, dir, n)
		if err != nil {
			webGHFail(call, err)
			return nil
		}
		out := webproto.PRView{Item: webPRItem(d.PRItem), State: d.State, Body: webClean(d.Body), Mergeable: webName(d.Mergeable),
			Files: []webproto.PRFile{}, Reviews: []webproto.PRReview{}, Checks: webPRChecks(d.Checks)}
		for _, f := range d.Files {
			out.Files = append(out.Files, webproto.PRFile{Path: webName(f.Path), Add: f.Additions, Del: f.Deletions})
		}
		for _, r := range d.Reviews {
			out.Reviews = append(out.Reviews, webproto.PRReview{Author: webName(r.Author), State: webName(r.State), Body: webClean(r.Body), When: webName(r.When)})
		}
		return func(m *Model) {
			if m.web != nil {
				if m.web.more.prs == nil || len(m.web.more.prs) >= 64 {
					m.web.more.prs = map[int]ghci.PRDetail{}
				}
				m.web.more.prs[n] = d
			}
			call.reply(out)
		}
	})
}

// webCIRunState sums one workflow run up.
func webCIRunState(r ghci.Run) string {
	switch {
	case r.Pending():
		return "pending"
	case r.Failed():
		return "fail"
	case r.Conclusion == "success":
		return "pass"
	}
	return "none"
}

// webCIStatus answers the laptop's CI watch of the branch as it is (the
// watcher asks gh on its own schedule); before its first answer it asks
// now and says it is loading.
func (m *Model) webCIStatus(call *webCall) tea.Cmd {
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	w := m.ciW()
	out := webproto.CIStatus{Branch: webName(w.branch), State: "none", Runs: []webproto.CIRun{}}
	var cmd tea.Cmd
	switch {
	case !w.loaded:
		out.Loading = true
		if !w.busy {
			cmd = m.ciTrigger()
		}
	case w.err != nil:
		if ghci.Quiet(w.err) {
			out.Error = ciGHHint + " (on the laptop)"
		} else {
			out.Error = webName(w.err.Error())
		}
	default:
		s := w.snap
		out.State = webCIStates[s.State()]
		out.Head = s.HeadSHA[:min(7, len(s.HeadSHA))]
		for _, r := range s.Runs {
			out.Runs = append(out.Runs, webproto.CIRun{Workflow: webName(r.Workflow), Title: webName(r.Title), Status: webName(r.Status),
				Conclusion: webName(r.Conclusion), State: webCIRunState(r), URL: webName(r.URL)})
		}
		if p := s.PR; p != nil {
			item := webPRItem(ghci.PRItem{Number: p.Number, Title: p.Title, Branch: w.branch, URL: p.URL, Draft: p.Draft, Review: p.Review, Checks: p.Checks})
			out.PR = &item
		}
	}
	call.reply(out)
	return cmd
}

// webPRRead is the pull request pr.view read last (the cards name it).
func (m *Model) webPRRead(call *webCall, n int) (ghci.PRDetail, bool) {
	if _, ok := m.webGitRoot(call); !ok {
		return ghci.PRDetail{}, false
	}
	d, ok := m.web.more.prs[n]
	if !ok {
		call.fail(webproto.CodeNotFound, "Open the pull request again.")
	}
	return d, ok
}

// webPRDone is the end of a gh change from the web: the laptop's own
// toast; its PR card is not opened (the page shows the pull request).
func webPRDone(n int, ok, detail, failed string, refresh bool, err error) tea.Msg {
	return prDoneMsg{number: n, ok: ok, detail: detail, failed: failed, err: err, refresh: refresh}
}

var webMergeLabels = map[string]string{"squash": "Squash and merge", "merge": "Merge commit", "rebase": "Rebase and merge"}

func (m *Model) webPRMerge(call *webCall) *webAsk {
	a := call.args.(*webproto.PRMergeArgs)
	d, ok := m.webPRRead(call, a.Number)
	if !ok {
		return nil
	}
	if d.State != "OPEN" || d.Draft {
		call.fail(webproto.CodeConflict, prRef(d.Number)+" is not open for merging (a draft or closed).")
		return nil
	}
	body := "Merge " + prRef(d.Number) + " \"" + webName(d.Title) + "\" from " + webName(d.Branch) + " into " + webName(d.Base) + " (" + strings.ToLower(webMergeLabels[a.Method]) + ")?"
	if w := prMergeWarnings(d); len(w) > 0 {
		body += " " + strings.Join(w, " ")
	}
	body += " The branch stays. Rules of the repository still apply."
	n, method := d.Number, ghci.MergeMethod(a.Method)
	return &webAsk{summary: prRef(n) + " " + a.Method, title: "Merge " + prRef(n) + "?", body: body, action: webMergeLabels[a.Method],
		run: func(m *Model) tea.Cmd {
			return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
				return webPRDone(n, "Merged "+prRef(n), string(method), "Could not merge "+prRef(n), false, ghci.Merge(ctx, run, dir, n, method))
			})
		}}
}

func (m *Model) webPRReview(call *webCall) *webAsk {
	a := call.args.(*webproto.PRReviewArgs)
	d, ok := m.webPRRead(call, a.Number)
	if !ok {
		return nil
	}
	n := d.Number
	kind := map[string]ghci.ReviewKind{"approve": ghci.ReviewApprove, "request_changes": ghci.ReviewRequestChanges, "comment": ghci.ReviewComment}[a.Kind]
	verb := map[string]string{"approve": "Approve", "request_changes": "Request changes on", "comment": "Comment on"}[a.Kind]
	said := map[string]string{"approve": "Approved ", "request_changes": "Asked for changes on ", "comment": "Commented on "}[a.Kind]
	body := verb + " " + prRef(n) + " \"" + webName(d.Title) + "\" as you on GitHub?"
	if msg := strings.TrimSpace(a.Body); msg != "" {
		if r := []rune(msg); len(r) > 300 {
			msg = string(r[:300]) + "…"
		}
		body += " Message: " + webName(msg)
	}
	text := a.Body
	return &webAsk{summary: prRef(n) + " " + a.Kind, title: verb + " " + prRef(n) + "?", body: body, action: strings.Fields(verb)[0],
		run: func(m *Model) tea.Cmd {
			return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
				return webPRDone(n, said+prRef(n), "", "Could not send the review of "+prRef(n), false, ghci.Review(ctx, run, dir, n, kind, text))
			})
		}}
}

func (m *Model) webPRCheckout(call *webCall) *webAsk {
	a := call.args.(*webproto.PRNumberArgs)
	d, ok := m.webPRRead(call, a.Number)
	if !ok {
		return nil
	}
	n, branch := d.Number, d.Branch
	return &webAsk{summary: prRef(n), title: "Check out " + prRef(n) + "?",
		body:   "Switch the laptop to " + webName(branch) + ", the branch of " + prRef(n) + "? Files on the laptop change. With uncommitted changes it stops.",
		action: "Check out",
		run: func(m *Model) tea.Cmd {
			dir, err := m.rootDir()
			if err != nil {
				return nil
			}
			if dirty, err := git.Dirty(dir); err != nil || dirty {
				return m.prInfo("Not checked out: you have changes", "Commit, stash or discard them first, then check out "+prRef(n)+" again.")
			}
			return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
				return webPRDone(n, "Checked out "+prRef(n), branch, "Could not check out "+prRef(n), true, ghci.Checkout(ctx, run, dir, n))
			})
		}}
}
