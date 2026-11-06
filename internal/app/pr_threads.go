package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// GITX-22: the review threads of a pull request. The view lists them
// (open first); Threads… picks one; its card shows the code, every
// comment and a suggested change, with Reply… and Resolve / Reopen
// Thread. After a change the PR is read again and the same thread comes
// back. New line comments start in the diff card (pr_line_comment.go).

const (
	pickerKindPRThreads pickerKindEnum  = 3210
	confirmKindPRThread confirmKindEnum = 3210
	promptKindPRReply   promptKindEnum  = 3210
)

// prThreadRows caps the thread list on the view card.
const prThreadRows = 8

// sortThreads puts open threads first, each group by file and line.
func sortThreads(ts []ghci.Thread) {
	sort.SliceStable(ts, func(i, j int) bool {
		if ts[i].Resolved != ts[j].Resolved {
			return !ts[i].Resolved
		}
		if ts[i].Path != ts[j].Path {
			return ts[i].Path < ts[j].Path
		}
		return ts[i].Line < ts[j].Line
	})
}

// threadWhere is "parser.go:12".
func threadWhere(t ghci.Thread) string {
	if t.Line > 0 {
		return fmt.Sprintf("%s:%d", t.Path, t.Line)
	}
	return t.Path
}

// threadFirstLine is the first line of the first comment, without the
// suggestion block.
func threadFirstLine(t ghci.Thread) string {
	if len(t.Comments) == 0 {
		return ""
	}
	body := strings.TrimSpace(prCommentText(t.Comments[0].Body))
	line, _, _ := strings.Cut(body, "\n")
	return truncRunes(line, 60)
}

// truncRunes keeps the first n runes of s and adds "…" when it cut.
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// threadRow is "○ parser.go:12 · bob: Why a map?".
func threadRow(t ghci.Thread) string {
	mark := "○"
	if t.Resolved {
		mark = "✓"
	}
	row := mark + " " + threadWhere(t)
	if len(t.Comments) > 0 {
		row += " · " + t.Comments[0].Author + ": " + threadFirstLine(t)
	}
	return row
}

// prViewBody is the view card text: prViewText plus the threads.
func (m *Model) prViewBody() string {
	s := m.prS()
	d := s.detail
	if d == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(prViewText(*d))
	if n := len(s.threads); n > 0 {
		open := ghci.ThreadsOpen(s.threads)
		fmt.Fprintf(&b, "\n\nReview threads (%d open · %d resolved)\n", open, n-open)
		for i, t := range s.threads {
			if i == prThreadRows {
				fmt.Fprintf(&b, "… %d more: Threads…\n", n-i)
				break
			}
			b.WriteString(threadRow(t) + "\n")
		}
	}
	if d.State == "OPEN" && len(d.Files) > 0 {
		b.WriteString("\nFiles… then c comments on a change, s suggests a fix\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// threadOn is the thread on the card (nil: none).
func (s *prState) threadOn() *ghci.Thread {
	for i := range s.threads {
		if s.threads[i].ID == s.thread {
			return &s.threads[i]
		}
	}
	return nil
}

// prOpenThreads lists the threads of the pull request in the view.
func (m *Model) prOpenThreads() tea.Cmd {
	s := m.prS()
	if s.detail == nil || len(s.threads) == 0 {
		return nil
	}
	items := make([]picker.Item, 0, len(s.threads))
	for _, t := range s.threads {
		hint := fmt.Sprintf("%d comment%s", len(t.Comments), plural(len(t.Comments)))
		if t.Outdated {
			hint += " · outdated"
		}
		items = append(items, picker.Item{ID: "thread-" + t.ID, Title: threadRow(t), Hint: hint})
	}
	m.confirmOpen = false
	m.picker = picker.NewItems(" "+prRef(s.detail.Number)+" Review Threads ", items)
	m.picker.SetWidth(min(m.w-4, 110))
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindPRThreads
	return nil
}

// onPRThreadPicked opens the card of the chosen thread.
func (m *Model) onPRThreadPicked(id string) tea.Cmd {
	s := m.prS()
	s.thread = strings.TrimPrefix(id, "thread-")
	m.showPRThread()
	return nil
}

// prCommentText shows a suggestion block as "Suggested change" with
// + lines instead of the raw fence.
func prCommentText(body string) string {
	code, ok := ghci.SuggestedCode(body)
	if !ok {
		return body
	}
	lines := strings.Split(body, "\n")
	var before, after []string
	in, done := false, false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case !in && !done && strings.HasSuffix(t, "suggestion") && strings.Trim(strings.TrimSuffix(t, "suggestion"), "`") == "":
			in = true
		case in && strings.Trim(t, "`") == "" && len(t) >= 3:
			in, done = false, true
		case in:
		case done:
			after = append(after, l)
		default:
			before = append(before, l)
		}
	}
	var b strings.Builder
	if s := strings.TrimSpace(strings.Join(before, "\n")); s != "" {
		b.WriteString(s + "\n")
	}
	b.WriteString("Suggested change:\n")
	if code == "" {
		b.WriteString("- (delete the line)\n")
	}
	for _, c := range strings.Split(code, "\n") {
		if code != "" {
			b.WriteString("+ " + c + "\n")
		}
	}
	if s := strings.TrimSpace(strings.Join(after, "\n")); s != "" {
		b.WriteString(s + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// prThreadText is the thread card body.
func prThreadText(t ghci.Thread) string {
	var b strings.Builder
	state := "Open"
	if t.Resolved {
		state = "Resolved"
	}
	if t.Outdated {
		state += " · Outdated (the code changed since)"
	}
	b.WriteString(threadWhere(t) + " · " + state + "\n")
	if h := strings.TrimSpace(t.Hunk); h != "" {
		lines := strings.Split(h, "\n")
		if len(lines) > 0 && strings.HasPrefix(lines[0], "@@") {
			lines = lines[1:]
		}
		if len(lines) > 6 {
			lines = lines[len(lines)-6:]
		}
		b.WriteString("\n" + strings.Join(lines, "\n") + "\n")
	}
	for _, c := range t.Comments {
		b.WriteString("\n" + c.Author + "\n")
		if body := strings.TrimSpace(prCommentText(c.Body)); body != "" {
			b.WriteString("  " + strings.ReplaceAll(body, "\n", "\n  ") + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// showPRThread (re)draws the card of the thread on screen.
func (m *Model) showPRThread() {
	s := m.prS()
	t := s.threadOn()
	if t == nil || s.detail == nil {
		return
	}
	resolve := "Resolve Thread"
	if t.Resolved {
		resolve = "Reopen Thread"
	}
	buttons := []confirm.Button{
		{ID: "close", Title: "Close"},
		{ID: "reply", Title: "Reply…", Style: confirm.StylePrimary},
		{ID: "resolve", Title: resolve},
		{ID: "browser", Title: "Open in Browser"},
	}
	m.confirm = confirm.New(prRef(s.detail.Number)+" Review Thread", prThreadText(*t), buttons).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindPRThread
}

// answerPRThread runs the thread card's buttons.
func (m *Model) answerPRThread(id string) tea.Cmd {
	s := m.prS()
	t := s.threadOn()
	if t == nil || s.detail == nil {
		return nil
	}
	n, where := s.detail.Number, threadWhere(*t)
	switch id {
	case "browser":
		url := t.URL
		if url == "" {
			url = s.detail.URL
		}
		return m.openLink(url)
	case "reply":
		if len(t.Comments) == 0 {
			return nil
		}
		m.prompt = prompt.New("Reply on "+where, "Your reply:", "")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindPRReply
		return nil
	case "resolve":
		threadID, resolved := t.ID, !t.Resolved
		s.wantThread = threadID
		ok := "Resolved " + where
		if !resolved {
			ok = "Reopened " + where
		}
		return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
			err := ghci.SetResolved(ctx, run, dir, threadID, resolved)
			return prDoneMsg{number: n, ok: ok, failed: "Could not change the thread on " + where, err: err, reopen: true}
		})
	}
	return nil
}

// handlePRReplySubmit sends a reply. ok=false: not ours.
func (m *Model) handlePRReplySubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindPRReply {
		return nil, false
	}
	s := m.prS()
	t := s.threadOn()
	if t == nil || s.detail == nil || len(t.Comments) == 0 {
		return nil, true
	}
	n, where, first := s.detail.Number, threadWhere(*t), t.Comments[0].ID
	s.wantThread = t.ID
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		err := ghci.Reply(ctx, run, dir, n, first, value)
		if errors.Is(err, ghci.ErrNeedsText) {
			err = errors.New("write a reply first")
		}
		return prDoneMsg{number: n, ok: "Replied on " + where, failed: "Could not reply on " + where, err: err, reopen: true}
	}), true
}
