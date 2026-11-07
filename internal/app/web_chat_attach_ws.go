package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// MOB-55: project attach kinds and ws on sends (plan §3.3, §3.7).
//
// A chat.send the phone sends with a ws goes through the door (MOB-50) to
// that project's hub, which is this code in that project's UI. Every
// attach names the project it was picked in (Attach.WS, "" = here); this
// hub resolves only its own: an attach of another project fails the
// whole send before anything is read, so one project's files, tasks or
// diffs never land in another project's chat by a mixed-up page.
//
// The project kinds resolve here:
//   - project: a row of the projects topic (name, where, state, agents);
//     for this project also its branch, waiting cards and worktrees.
//   - issue, pr: read with the laptop's gh (title, state, checks; a pr's
//     branches, files and body), off the UI thread. A GitLab merge
//     request ("!12") goes as its reference: the AI reads it with its
//     own tools.

// webOwnWS is this hub's workspace key, as the door names it (a var: tests).
var webOwnWS = func() string { return session.Key(aiWorkspaceRoot()) }

// webAttachWSCheck refuses a send with an attach of another project. own
// is this hub's key ("" when unknown: then only unnamed attaches go).
func webAttachWSCheck(list []webproto.Attach, own string) error {
	for i := range list {
		if ws := list[i].WS; ws != "" && ws != own {
			return webErr(webproto.CodeForbidden, webAttachLabel(&list[i])+" is in another project: send it from that project's chat.")
		}
	}
	return nil
}

// webAttachHere is what a project attach of this hub's own project adds.
type webAttachHere struct {
	branch           string
	ahead, behind    int
	cards, worktrees int
}

// webAttachProjectText is a project for the model.
func webAttachProjectText(p webproto.ProjectItem, here *webAttachHere) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project %s (%s): %s.", p.Name, p.Where, p.State)
	if p.Working > 0 || p.Waiting > 0 {
		fmt.Fprintf(&b, " %d agents working, %d waiting for the user.", p.Working, p.Waiting)
	}
	if here == nil {
		b.WriteString("\nIt is another project: its files are not open in this chat.")
		return b.String()
	}
	if here.branch != "" {
		fmt.Fprintf(&b, "\nBranch: %s", here.branch)
		var ab []string
		if here.ahead > 0 {
			ab = append(ab, fmt.Sprintf("%d ahead", here.ahead))
		}
		if here.behind > 0 {
			ab = append(ab, fmt.Sprintf("%d behind", here.behind))
		}
		if len(ab) > 0 {
			b.WriteString(" (" + strings.Join(ab, ", ") + ")")
		}
	}
	fmt.Fprintf(&b, "\n%d cards wait for the user; %d %s.", here.cards, here.worktrees, webAttachPlural(here.worktrees, "worktree", "worktrees"))
	return b.String()
}

func webAttachPlural(n int, one, more string) string {
	if n == 1 {
		return one
	}
	return more
}

// webAttachProjectPlan resolves a project, issue or pr attach on Update:
// a project now, an issue or pr later (j.read runs in the Cmd).
func (m *Model) webAttachProjectPlan(a *webproto.Attach, j webAttachJob) (webAttachJob, error) {
	switch a.Kind {
	case webproto.AttachProject:
		for _, p := range m.webProjectsBuild().Projects {
			if p.ID != a.ID {
				continue
			}
			var here *webAttachHere
			if p.State == "here" {
				here = &webAttachHere{branch: m.gitBranch.Name, ahead: m.gitBranch.Ahead, behind: m.gitBranch.Behind,
					worktrees: len(m.webWorktreesBuild().Worktrees)}
				if m.web != nil {
					here.cards = len(webCardList(m.webCards(&m.web.cards, false)))
				}
			}
			j.text = webAttachProjectText(p, here)
			return j, nil
		}
		return j, webErr(webproto.CodeNotFound, "That project is gone.")
	case webproto.AttachIssue, webproto.AttachPR:
		n, ok := webproto.AttachNumber(a.Kind, a.ID)
		if !ok {
			return j, webErr(webproto.CodeBadRequest, "That item cannot be sent.")
		}
		if a.Kind == webproto.AttachPR && webproto.AttachIsMR(a.ID) {
			j.text = fmt.Sprintf("GitLab merge request !%d of this repository. Read it with your tools when you need more than its number.", n)
			return j, nil
		}
		kind := a.Kind
		root, err := m.gitRootDir()
		if err != nil || !m.gitIsRepo {
			j.text = webAttachGHRef(kind, n, "")
			return j, nil
		}
		run := m.ciW().runner()
		j.read = func() (string, error) {
			text, err := webAttachGHText(context.Background(), run, root, kind, n)
			if err != nil { // gh missing, signed out or offline: the reference still goes
				return webAttachGHRef(kind, n, webAttachErrText(err)), nil
			}
			return text, nil
		}
		return j, nil
	}
	return j, webErr(webproto.CodeBadRequest, "That item cannot be sent.")
}

// webAttachGHText reads an issue or pull request with gh for the model.
func webAttachGHText(ctx context.Context, run ghci.Runner, dir, kind string, n int) (string, error) {
	var b strings.Builder
	if kind == webproto.AttachIssue {
		l, err := ghci.ViewLink(ctx, run, dir, ghci.LinkRef{Kind: ghci.LinkIssue, Number: n})
		if err != nil {
			return "", webAttachGHErr(err, fmt.Sprintf("issue #%d", n))
		}
		fmt.Fprintf(&b, "Issue #%d: %s\nState: %s\nURL: %s\nRead its body and comments with your tools when you need them.", l.Number, l.Title, l.State, l.URL)
		return b.String(), nil
	}
	d, err := ghci.ViewPR(ctx, run, dir, n)
	if err != nil {
		return "", webAttachGHErr(err, fmt.Sprintf("pull request #%d", n))
	}
	fmt.Fprintf(&b, "Pull request #%d: %s\nState: %s", d.Number, d.Title, strings.ToLower(d.State))
	if d.Draft {
		b.WriteString(" (draft)")
	}
	fmt.Fprintf(&b, "\nBranch: %s → %s\nAuthor: %s\nURL: %s", d.Branch, d.Base, d.Author, d.URL)
	if r := webPRReview(d.Review); r != "" {
		b.WriteString("\nReview: " + strings.ToLower(strings.ReplaceAll(r, "_", " ")))
	}
	if len(d.Checks) > 0 {
		b.WriteString("\nChecks:")
		for _, c := range d.Checks {
			fmt.Fprintf(&b, "\n- %s: %s", c.Name, webCIStates[c.State])
		}
	}
	if len(d.Files) > 0 {
		b.WriteString("\nFiles:")
		for i, f := range d.Files {
			if i == 50 {
				fmt.Fprintf(&b, "\n… %d more", len(d.Files)-i)
				break
			}
			fmt.Fprintf(&b, "\n- %s (+%d −%d)", f.Path, f.Additions, f.Deletions)
		}
	}
	if body := strings.TrimSpace(d.Body); body != "" {
		b.WriteString("\n\n" + body)
	}
	return b.String(), nil
}

// webAttachGHRef is an issue or pr the laptop could not read: its
// number for the model, and why ("" when there is no git repository).
func webAttachGHRef(kind string, n int, why string) string {
	what := fmt.Sprintf("Issue %d", n)
	if kind == webproto.AttachPR {
		what = fmt.Sprintf("Pull request %d", n)
	}
	text := what + " of this repository. Read it with your tools when you need more than its number."
	if why != "" {
		text += " (The laptop could not read it: " + why + ")"
	}
	return text
}

// webAttachErrText is a webError's text, else the error's.
func webAttachErrText(err error) string {
	var we *webError
	if errors.As(err, &we) {
		return we.msg
	}
	return webName(err.Error())
}

// webAttachGHErr says why gh could not read what the attach names.
func webAttachGHErr(err error, what string) error {
	switch {
	case errors.Is(err, ghci.ErrNoGitHub):
		return webErr(webproto.CodeConflict, "This repository has no GitHub remote.")
	case ghci.Quiet(err):
		return webErr(webproto.CodeConflict, ciGHHint+" (on the laptop)")
	}
	return webErr(webproto.CodeNotFound, "gh could not read "+what+": "+webName(err.Error()))
}
