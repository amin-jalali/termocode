package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/now"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Review (Now + Review) on the web: review.list reads the pull requests
// that ask for the user's review (they join the inbox as the user's
// turn), review.get reads one in the review's fixed order (question,
// solution, changes main first, proof per commit, risk, suggestion) and
// review.act approves or comments on exactly the commit the user saw:
// it reads the head again first and refuses when new code came in. gh
// runs on the laptop through the CI watcher's runner, off the UI thread.

func init() {
	webOps[webproto.OpReviewList] = (*Model).webReviewList
	webOps[webproto.OpReviewGet] = (*Model).webReviewGet
	webOps[webproto.OpReviewAct] = (*Model).webReviewAct
}

// nowAsk is one pull request that asks for the user's review.
type nowAsk struct {
	number int
	title  string
	author string
	bot    bool
	draft  bool
	at     int64 // when the laptop first saw the ask (Unix ms)
}

// reviewBot reports a bot account (dependabot[bot], renovate-bot).
func reviewBot(login string) bool {
	l := strings.ToLower(login)
	return strings.HasSuffix(l, "[bot]") || strings.HasSuffix(l, "-bot") || l == "dependabot" || l == "renovate"
}

func (m *Model) webReviewList(call *webCall) tea.Cmd {
	return m.webGH(call, func(ctx context.Context, run ghci.Runner, dir string) func(m *Model) {
		items, err := ghci.ListPRsFilter(ctx, run, dir, ghci.PRFilter{Search: "review-requested:@me"})
		if err != nil {
			webGHFail(call, err)
			return nil
		}
		return func(m *Model) {
			seen := map[int]int64{}
			for _, a := range m.nowAsks {
				seen[a.number] = a.at
			}
			at := webNow().UnixMilli()
			out := webproto.ReviewQueue{Items: []webproto.ReviewQueued{}}
			asks := make([]nowAsk, 0, len(items))
			for _, p := range items {
				a := nowAsk{number: p.Number, title: webName(p.Title), author: webName(p.Author), bot: reviewBot(p.Author), draft: p.Draft, at: at}
				if t, ok := seen[p.Number]; ok {
					a.at = t
				}
				asks = append(asks, a)
				out.Items = append(out.Items, webproto.ReviewQueued{Number: a.number, Title: a.title, Author: a.author, Bot: a.bot, Draft: a.draft})
			}
			m.nowAsks = asks
			call.reply(out)
		}
	})
}

// nowAskItems are the review asks as inbox items: the user's turn, a
// bot's never.
func (m *Model) nowAskItems() []webproto.InboxItem {
	out := make([]webproto.InboxItem, 0, len(m.nowAsks))
	for _, a := range m.nowAsks {
		it := webproto.InboxItem{Kind: webproto.InboxPR, Ref: strconv.Itoa(a.number), Title: "#" + strconv.Itoa(a.number) + " " + a.title, Since: a.at,
			State: webproto.InboxNeedsYou, Source: "github", Turn: now.TurnMe, Signal: now.SigReview, Why: a.author + " asked you", Can: []string{"review"}, Bot: a.bot}
		if a.draft {
			it.Turn, it.Why = now.TurnNone, "Draft"
		}
		out = append(out, it)
	}
	return out
}

// reviewProofIn is a check of the head as a proof input.
func reviewProofIn(c ghci.Check, head string) now.ProofIn {
	st := map[ghci.State]string{ghci.StatePass: now.ProofOK, ghci.StateFail: now.ProofFail, ghci.StatePending: now.ProofRunning}[c.State]
	if st == "" {
		st = now.ProofNotRun
	}
	return now.ProofIn{Name: c.Name, State: st, SHA: head, URL: c.URL}
}

// reviewRunIn is a workflow run as a proof input.
func reviewRunIn(r ghci.Run) now.ProofIn {
	st := now.ProofNotRun
	switch {
	case r.Pending():
		st = now.ProofRunning
	case r.Failed():
		st = now.ProofFail
	case r.Conclusion == "success":
		st = now.ProofOK
	}
	return now.ProofIn{Name: r.Workflow, State: st, SHA: r.HeadSHA, URL: r.URL}
}

// webReviewSnap builds the review of d with the branch's runs.
func webReviewSnap(d ghci.PRDetail, runs []ghci.Run) webproto.ReviewSnap {
	q, task, sol := now.ReviewText(d.Title, d.Body)
	out := webproto.ReviewSnap{Number: d.Number, Title: webName(d.Title), Author: webName(d.Author), URL: webName(d.URL), Head: d.HeadSHA,
		Branch: webName(d.Branch), Files: len(d.Files), Question: webName(q), Task: webName(task), Solution: webName(sol),
		Groups: []webproto.ReviewGroup{}, Proof: []webproto.ReviewProof{}, State: d.State, Approved: d.Review == "APPROVED"}
	files := make([]now.ReviewFile, 0, len(d.Files))
	for _, f := range d.Files {
		files = append(files, now.ReviewFile{Path: f.Path, Add: f.Additions, Del: f.Deletions})
		out.Add += f.Additions
		out.Del += f.Deletions
	}
	for _, g := range now.ReviewGroups(files) {
		wg := webproto.ReviewGroup{Title: webName(g.Title), Add: g.Add, Del: g.Del, Main: g.Main, Files: []string{}}
		for _, f := range g.Files {
			wg.Files = append(wg.Files, webName(f))
		}
		out.Groups = append(out.Groups, wg)
	}
	var in []now.ProofIn
	for _, c := range d.Checks {
		in = append(in, reviewProofIn(c, d.HeadSHA))
	}
	seen := map[string]bool{}
	for _, r := range runs { // newest first: the newest run of each workflow
		if seen[r.Workflow] {
			continue
		}
		seen[r.Workflow] = true
		in = append(in, reviewRunIn(r))
	}
	rows := now.ReviewProof(d.HeadSHA, in)
	for _, r := range rows {
		out.Proof = append(out.Proof, webproto.ReviewProof{Name: webName(r.Name), State: r.State, SHA: r.SHA, URL: webName(r.URL), Fix: r.Fix, Again: r.Again})
	}
	risk := now.ReviewRisk(files)
	out.Risk = webproto.ReviewRisk{Level: risk.Level, Reasons: risk.Reasons}
	s := now.ReviewSuggestion(rows)
	out.Suggestion = webproto.ReviewSuggestion{Text: webName(s.Text), Notes: s.Notes}
	return out
}

func (m *Model) webReviewGet(call *webCall) tea.Cmd {
	n := call.args.(*webproto.PRNumberArgs).Number
	return m.webGH(call, func(ctx context.Context, run ghci.Runner, dir string) func(m *Model) {
		d, err := ghci.ViewPR(ctx, run, dir, n)
		if err != nil {
			webGHFail(call, err)
			return nil
		}
		runs, err := ghci.BranchRuns(ctx, run, dir, d.Branch)
		if err != nil {
			runs = nil // the checks of the head still show; older runs are a bonus
		}
		out := webReviewSnap(d, runs)
		return func(m *Model) {
			// An agent's pull request: its lane's task is the question.
			if m.wt != nil && m.wt.loaded {
				for _, w := range m.webWorktreesBuild().Worktrees {
					if w.Branch != "" && w.Branch == out.Branch && w.Agent != "" {
						out.Agent = w.Agent
						if out.Question == "" {
							out.Question = w.Title
						}
					}
				}
			}
			call.reply(out)
		}
	})
}

// reviewActTimeout caps the head read and the review before it is sent.
const reviewActTimeout = 30 * time.Second

func (m *Model) webReviewAct(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ReviewActArgs)
	return m.webGH(call, func(ctx context.Context, run ghci.Runner, dir string) func(m *Model) {
		ctx, cancel := context.WithTimeout(ctx, reviewActTimeout)
		defer cancel()
		d, err := ghci.ViewPR(ctx, run, dir, a.Number)
		if err != nil {
			webGHFail(call, err)
			return nil
		}
		if err := now.CanApprove(a.Head, d.HeadSHA); err != nil {
			call.fail(webproto.CodeConflict, err.Error())
			return nil
		}
		kind := ghci.ReviewApprove
		if a.Do == "comment" {
			kind = ghci.ReviewComment
		}
		if err := ghci.Review(ctx, run, dir, a.Number, kind, a.Text); err != nil {
			webGHFail(call, err)
			return nil
		}
		call.reply(webproto.ReviewActResult{Number: a.Number, Head: now.ShortSHA(d.HeadSHA)})
		return nil
	})
}
