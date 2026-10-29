package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
)

// The PR view: one card with the title, state, checks, changed files,
// description and reviews of a pull request. Files… opens its diff,
// Actions… lists what the user can do with it (pr_actions.go).

const confirmKindPRView confirmKindEnum = 3200

// prBodyLines caps the description on the card; Read Description opens
// the whole text.
const prBodyLines = 14

// prViewMsg is a finished `gh pr view`.
type prViewMsg struct {
	number int
	detail ghci.PRDetail
	err    error
}

// openPRView reads pull request n; the card opens when gh answers.
func (m *Model) openPRView(n int) tea.Cmd {
	fetch := m.prViewCmd(n)
	if fetch == nil {
		return nil
	}
	return tea.Batch(m.aiInfo("Reading "+prRef(n)+"…"), fetch)
}

// prViewCmd runs `gh pr view` off the main loop.
func (m *Model) prViewCmd(n int) tea.Cmd {
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		d, err := ghci.ViewPR(ctx, run, dir, n)
		return prViewMsg{number: n, detail: d, err: err}
	})
}

// onPRView shows the card.
func (m *Model) onPRView(msg prViewMsg) tea.Cmd {
	if msg.err != nil {
		return m.prFailToast("Could not read "+prRef(msg.number), msg.err)
	}
	s := m.prS()
	d := msg.detail
	s.detail, s.diff = &d, nil
	m.showPRView()
	return nil
}

// prViewOpen reports the card on screen.
func (m *Model) prViewOpen() bool { return m.confirmOpen && m.confirmKind == confirmKindPRView }

// showPRView (re)draws the card from the state.
func (m *Model) showPRView() {
	d := m.prS().detail
	if d == nil {
		return
	}
	buttons := []confirm.Button{{ID: "actions", Title: "Actions…", Style: confirm.StylePrimary}}
	if len(d.Files) > 0 {
		buttons = append(buttons, confirm.Button{ID: "files", Title: "Files…"})
	}
	buttons = append(buttons, confirm.Button{ID: "close", Title: "Close"})
	m.confirm = confirm.New(prRef(d.Number)+" Pull Request", prViewText(*d), buttons).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindPRView
}

// prStateText is "Open · Approved · Draft".
func prStateText(d ghci.PRDetail) string {
	state := ciTitle(d.State)
	if d.Draft {
		state += " · Draft"
	}
	if r := ciReviewText(d.Review); r != "" {
		state += " · " + r
	}
	return state
}

// prViewText is the card body.
func prViewText(d ghci.PRDetail) string {
	var b strings.Builder
	b.WriteString(d.Title + "\n")
	b.WriteString(prStateText(d) + "\n")
	line := d.Branch
	if d.Base != "" {
		line += " → " + d.Base
	}
	if d.Author != "" {
		line += " · by " + d.Author
	}
	b.WriteString(line + "\n")
	if d.Mergeable == "CONFLICTING" {
		b.WriteString("Has merge conflicts with " + d.Base + "\n")
	}
	if len(d.Checks) > 0 {
		b.WriteString("\nChecks\n")
		for _, c := range d.Checks {
			b.WriteString(ciMark(c.State) + " " + c.Name + "\n")
		}
	}
	if len(d.Files) > 0 {
		fmt.Fprintf(&b, "\nFiles (%d)\n", len(d.Files))
		for _, f := range d.Files {
			fmt.Fprintf(&b, "%s  +%d -%d\n", f.Path, f.Additions, f.Deletions)
		}
	}
	if body := strings.TrimSpace(d.Body); body != "" {
		b.WriteString("\nDescription\n")
		b.WriteString(clipLines(body, prBodyLines) + "\n")
	}
	if len(d.Reviews) > 0 {
		b.WriteString("\nReviews and comments\n")
		for _, r := range d.Reviews {
			head := r.Author
			if st := prReviewState(r.State); st != "" {
				head += " · " + st
			}
			b.WriteString(head + "\n")
			if body := strings.TrimSpace(r.Body); body != "" {
				b.WriteString("  " + strings.ReplaceAll(clipLines(body, 6), "\n", "\n  ") + "\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// clipLines keeps the first n lines and says how many were left out.
func clipLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n… %d more lines", len(lines)-n)
}

func prReviewState(s string) string {
	switch s {
	case "APPROVED":
		return "Approved"
	case "CHANGES_REQUESTED":
		return "Changes requested"
	case "COMMENTED":
		return "Commented"
	case "DISMISSED":
		return "Dismissed"
	}
	return ""
}

// handlePRConfirm answers the view's buttons. ok=false: not ours.
func (m *Model) handlePRConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindPRView {
		return nil, false
	}
	switch id {
	case "files":
		return m.prOpenFiles(), true
	case "actions":
		return m.prOpenActions(), true
	}
	return nil, true
}
