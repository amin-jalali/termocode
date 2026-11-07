package app

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── The PR pane of a worktree (CMP-35) ───────────────────────────────────
//
// P in WORKTREES (Worktree: Show Pull Request of Selected) opens the PR
// view of the pull request the worktree is linked to (i, WTX-9): its
// checks, files, description, reviews and review threads, with Threads…
// (read, reply, resolve), Actions… (review, merge behind a Cancel-first
// card) and Checks…. Checks… lists every check: a failed GitHub Actions
// check has Show Log (its failed log in a read-only tab) and Fix with AI
// (the log fills the chat; the user reads it and presses Enter); any
// other check with a link opens its page. Nothing here changes the pull
// request by itself.

const (
	wtPRPaletteID  = "worktree-pr"
	wtPRNoLinkHint = "No pull request is linked to this worktree: press i to link one"

	pickerKindPRChecks pickerKindEnum = 3220
)

func init() {
	aiActionPolicies[wtPRPaletteID] = aiAuto() // opens the read-only PR view; every change there asks the user
}

// worktreePR opens the PR view of the selected worktree's pull request.
func (m *Model) worktreePR() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return nil
	}
	if it.e.Meta.PR <= 0 {
		return m.aiInfo(wtPRNoLinkHint)
	}
	return m.openPRView(it.e.Meta.PR)
}

// prCheckRunID is the workflow run of a failed GitHub Actions check (0:
// none, or the check did not fail).
func prCheckRunID(c ghci.Check) int64 {
	if ids := ghci.FailedRunIDs([]ghci.Check{c}); len(ids) > 0 {
		return ids[0]
	}
	return 0
}

// prCheckItems are the rows of Checks…, in gh's order (the card lists
// the checks the same way).
func prCheckItems(cs []ghci.Check) []picker.Item {
	items := make([]picker.Item, 0, len(cs)+2)
	for i, c := range cs {
		n := strconv.Itoa(i)
		title := ciMark(c.State) + " " + c.Name
		switch {
		case prCheckRunID(c) > 0:
			items = append(items,
				picker.Item{ID: "check-log-" + n, Title: title + " · Show Log", Hint: "the failed part of its log"},
				picker.Item{ID: "check-fix-" + n, Title: title + " · Fix with AI", Hint: "fills the chat; you press Enter"})
		case c.URL != "":
			items = append(items, picker.Item{ID: "check-open-" + n, Title: title, Hint: "open its page"})
		default:
			items = append(items, picker.Item{ID: "check-" + n, Title: title})
		}
	}
	return items
}

// prOpenChecks lists the checks of the pull request in the view.
func (m *Model) prOpenChecks() tea.Cmd {
	d := m.prS().detail
	if d == nil || len(d.Checks) == 0 {
		return nil
	}
	m.confirmOpen = false
	m.picker = picker.NewItems(" "+prRef(d.Number)+" Checks ", prCheckItems(d.Checks))
	m.picker.SetWidth(min(m.w-4, 110))
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindPRChecks
	return nil
}

// onPRCheckPicked shows the log of a failed check, fills the chat with
// it, or opens the check's page.
func (m *Model) onPRCheckPicked(id string) tea.Cmd {
	d := m.prS().detail
	if d == nil {
		return nil
	}
	rest := strings.TrimPrefix(id, "check-")
	kind := ""
	if k, num, ok := strings.Cut(rest, "-"); ok {
		kind, rest = k, num
	}
	i, err := strconv.Atoi(rest)
	if err != nil || i < 0 || i >= len(d.Checks) {
		return nil
	}
	c := d.Checks[i]
	switch kind {
	case "log", "fix":
		return m.prCheckLog(d.Branch, c, kind == "fix")
	case "open":
		return m.openLink(c.URL)
	}
	return nil
}

// prCheckLog reads the failed log of check c off the main loop; the
// answer is a ciLogMsg (ci_card.go shows it or fills the chat).
func (m *Model) prCheckLog(branch string, c ghci.Check, fix bool) tea.Cmd {
	rid := prCheckRunID(c)
	if rid <= 0 {
		return nil
	}
	r := ghci.Run{ID: rid, Workflow: c.Name, URL: c.URL, Conclusion: "failure", Status: "completed"}
	fetch := m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		fl, err := ghci.FetchFailedLog(ctx, run, dir, rid)
		return ciLogMsg{fix: fix, branch: branch, run: r, log: fl, err: err}
	})
	var t tea.Cmd
	m.toast, t = m.toast.Push(toast.Info, "Reading the CI log…")
	return tea.Batch(t, fetch)
}
