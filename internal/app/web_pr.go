package app

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ghci"
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

// webGH runs fn with the laptop's gh runner off the UI thread.
func (m *Model) webGH(call *webCall, fn func(ctx context.Context, run ghci.Runner, dir string)) tea.Cmd {
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	run := m.ciW().runner()
	call.later()
	return func() tea.Msg {
		fn(call.ctx, run, root)
		return nil
	}
}

func (m *Model) webPRList(call *webCall) tea.Cmd {
	return m.webGH(call, func(ctx context.Context, run ghci.Runner, dir string) {
		items, err := ghci.ListPRs(ctx, run, dir)
		if err != nil {
			webGHFail(call, err)
			return
		}
		out := webproto.PRList{Items: []webproto.PRItem{}}
		for _, p := range items {
			out.Items = append(out.Items, webPRItem(p))
		}
		call.reply(out)
	})
}

func (m *Model) webPRView(call *webCall) tea.Cmd {
	n := call.args.(*webproto.PRNumberArgs).Number
	return m.webGH(call, func(ctx context.Context, run ghci.Runner, dir string) {
		d, err := ghci.ViewPR(ctx, run, dir, n)
		if err != nil {
			webGHFail(call, err)
			return
		}
		out := webproto.PRView{Item: webPRItem(d.PRItem), State: d.State, Body: webClean(d.Body), Mergeable: webName(d.Mergeable),
			Files: []webproto.PRFile{}, Reviews: []webproto.PRReview{}, Checks: webPRChecks(d.Checks)}
		for _, f := range d.Files {
			out.Files = append(out.Files, webproto.PRFile{Path: webName(f.Path), Add: f.Additions, Del: f.Deletions})
		}
		for _, r := range d.Reviews {
			out.Reviews = append(out.Reviews, webproto.PRReview{Author: webName(r.Author), State: webName(r.State), Body: webClean(r.Body), When: webName(r.When)})
		}
		call.reply(out)
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
