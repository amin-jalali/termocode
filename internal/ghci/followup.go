package ghci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The PR follow-up loop (CMP-31): new review comments, PR conversation
// comments that call the agent (@termocode or @agent) and a failed CI
// run of a worktree's pull request, put together for the agent that
// works in that worktree. This file only reads and formats; the app
// asks the user before anything is typed into an agent.

// Caps of one feedback file.
const (
	maxFeedbackThreads = 30
	maxFeedbackBody    = 2000 // runes per comment
)

// Feedback is what is new on a pull request for its lane's agent.
type Feedback struct {
	Branch   string
	PR       int
	Threads  []Thread        // open threads with a comment not seen yet (whole thread, for context)
	Mentions []ThreadComment // conversation comments that call the agent, not seen yet
	Run      *Run            // a failed run not sent yet
	Log      FailedLog       // its failing log (filled by FetchFeedback)
	Seen     map[int64]bool  // the comment ids seen before (marks the new ones)
}

// Empty reports nothing to send.
func (f Feedback) Empty() bool { return len(f.Threads) == 0 && len(f.Mentions) == 0 && f.Run == nil }

// newComments counts the review comments not seen before.
func (f Feedback) newComments() int {
	n := 0
	for _, t := range f.Threads {
		for _, c := range t.Comments {
			if !f.Seen[c.ID] {
				n++
			}
		}
	}
	return n
}

// Summary is one line: "2 review comments · 1 mention · CI failed (CI)".
func (f Feedback) Summary() string {
	var parts []string
	if n := f.newComments(); n > 0 {
		parts = append(parts, plural(n, "review comment"))
	}
	if n := len(f.Mentions); n > 0 {
		parts = append(parts, plural(n, "mention"))
	}
	if f.Run != nil {
		s := "CI failed"
		if f.Run.Workflow != "" {
			s += " (" + f.Run.Workflow + ")"
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return "nothing new"
	}
	return strings.Join(parts, " · ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// SeenIDs are the comment ids this feedback covers (to mark them seen).
func (f Feedback) SeenIDs() []int64 {
	var out []int64
	for _, t := range f.Threads {
		for _, c := range t.Comments {
			out = append(out, c.ID)
		}
	}
	for _, c := range f.Mentions {
		out = append(out, c.ID)
	}
	return out
}

// MentionsAgent reports a comment that calls the agent: @termocode or
// @agent as a whole word (not an e-mail address, not @agents).
func MentionsAgent(body string) bool {
	low := strings.ToLower(body)
	for _, name := range []string{"@termocode", "@agent"} {
		for i := 0; ; {
			j := strings.Index(low[i:], name)
			if j < 0 {
				break
			}
			at := i + j
			end := at + len(name)
			if mentionEdge(low, at-1, true) && mentionEdge(low, end, false) {
				return true
			}
			i = at + 1
		}
	}
	return false
}

// mentionEdge: the byte at i does not continue the name (start: not a
// word or address character before the @; end: not a word character
// or '-' after the name).
func mentionEdge(s string, i int, before bool) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	var r rune
	if before {
		r, _ = utf8.DecodeLastRuneInString(s[:i+1])
	} else {
		r, _ = utf8.DecodeRuneInString(s[i:])
	}
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
		return false
	}
	return before || r != '.' || i+1 >= len(s) || !isWordByte(s[i+1])
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_'
}

// NewFeedback picks what is new: open threads with a comment not in
// seen, conversation comments that call the agent and are not in seen,
// and the newest failed run when its id is not sentRun.
func NewFeedback(branch string, pr int, threads []Thread, convo []ThreadComment, snap Snapshot, seen map[int64]bool, sentRun int64) Feedback {
	f := Feedback{Branch: branch, PR: pr, Seen: seen}
	for _, t := range threads {
		if t.Resolved || len(f.Threads) == maxFeedbackThreads {
			continue
		}
		for _, c := range t.Comments {
			if !seen[c.ID] {
				f.Threads = append(f.Threads, t)
				break
			}
		}
	}
	for _, c := range convo {
		if !seen[c.ID] && MentionsAgent(c.Body) {
			f.Mentions = append(f.Mentions, c)
		}
	}
	if runs := snap.FailedRuns(); len(runs) > 0 && runs[0].ID != sentRun {
		r := runs[0]
		f.Run = &r
	}
	return f
}

// clip keeps a comment short and its secrets out.
func clip(s string) string {
	s = ai.Redact(strings.TrimSpace(s))
	if utf8.RuneCountInString(s) > maxFeedbackBody {
		s = string([]rune(s)[:maxFeedbackBody]) + "…"
	}
	return s
}

// Prompt is the feedback file the agent reads (Markdown).
func (f Feedback) Prompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Feedback on PR #%d (branch %s)\n\n", f.PR, f.Branch)
	b.WriteString("Address each point below in this worktree. Reply in your summary which ones you fixed and which you did not (and why). Commit your changes; do not push.\n")
	if len(f.Threads) > 0 {
		b.WriteString("\n## Review comments\n")
		for _, t := range f.Threads {
			place := t.Path
			if t.Line > 0 {
				place += ":" + strconv.Itoa(t.Line)
			}
			if t.Outdated {
				place += " (outdated: the line moved)"
			}
			fmt.Fprintf(&b, "\n### %s\n", place)
			for _, c := range t.Comments {
				mark := ""
				if !f.Seen[c.ID] {
					mark = " (new)"
				}
				fmt.Fprintf(&b, "- %s%s: %s\n", c.Author, mark, strings.ReplaceAll(clip(c.Body), "\n", "\n  "))
			}
		}
	}
	if len(f.Mentions) > 0 {
		b.WriteString("\n## Asked in the PR conversation\n")
		for _, c := range f.Mentions {
			fmt.Fprintf(&b, "- %s: %s\n", c.Author, strings.ReplaceAll(clip(c.Body), "\n", "\n  "))
		}
	}
	if f.Run != nil {
		b.WriteString("\n## Failing CI\n\n")
		b.WriteString(FixPrompt(f.Branch, *f.Run, f.Log))
		b.WriteString("\n")
	}
	return b.String()
}

// AgentLine is the one line typed into the agent: where the file is
// and what it holds.
func AgentLine(file string, f Feedback) string {
	return fmt.Sprintf("New feedback on PR #%d (%s). Read %s and address it here; commit, do not push.", f.PR, f.Summary(), file)
}

// IssueComments reads the conversation comments of pull request number.
func IssueComments(ctx context.Context, run Runner, dir string, number int) ([]ThreadComment, error) {
	if number <= 0 {
		return nil, errors.New("no pull request")
	}
	out, errOut, err := run(ctx, dir, "api", fmt.Sprintf("repos/{owner}/{repo}/issues/%d/comments?per_page=100", number))
	if err != nil {
		return nil, classify(err, errOut)
	}
	var raw []struct {
		ID   int64 `json:"id"`
		User *struct {
			Login string `json:"login"`
		} `json:"user"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		URL       string `json:"html_url"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("gh api comments: %w", err)
	}
	cs := make([]ThreadComment, 0, len(raw))
	for _, c := range raw {
		who := "ghost"
		if c.User != nil && c.User.Login != "" {
			who = c.User.Login
		}
		cs = append(cs, ThreadComment{ID: c.ID, Author: who, Body: c.Body, When: c.CreatedAt, URL: c.URL})
	}
	return cs, nil
}

// FetchFeedback reads a pull request's threads, conversation and CI
// with gh and keeps what is new; the failing log is read only for a run
// not sent yet.
func FetchFeedback(ctx context.Context, run Runner, dir, branch string, pr int, seen map[int64]bool, sentRun int64) (Feedback, error) {
	threads, err := Threads(ctx, run, dir, pr)
	if err != nil {
		return Feedback{}, err
	}
	convo, err := IssueComments(ctx, run, dir, pr)
	if err != nil {
		return Feedback{}, err
	}
	snap, err := Fetch(ctx, run, dir, branch)
	if err != nil {
		return Feedback{}, err
	}
	f := NewFeedback(branch, pr, threads, convo, snap, seen, sentRun)
	if f.Run != nil {
		if fl, err := FetchFailedLog(ctx, run, dir, f.Run.ID); err == nil {
			f.Log = fl
		}
	}
	return f, nil
}
