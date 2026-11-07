package trigger

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Event kinds.
const (
	KindIssueOpened  = "issue_opened"
	KindIssueLabeled = "issue_labeled"
	KindPROpened     = "pr_opened"
	KindMention      = "mention"
	KindCIFailed     = "ci_failed"
	KindWebhook      = "webhook"
)

// Event caps: outside text is cut before it reaches a card or a var.
const (
	MaxTitle = 200
	MaxBody  = 4 << 10
)

// Event is one outside event that may start a workflow.
type Event struct {
	Workflow string // the workflow it is for
	Trigger  int    // index in the file's "on"
	Kind     string // one of the Kind* values
	// Key is unique per event; it is the dedupe key ("issue:12",
	// "run:9876", "hook:deploy:<id>").
	Key    string
	Source string // "github", "slack", "linear", "jira", "generic"
	Repo   string
	Number int
	Title  string
	URL    string
	Body   string
	Author string
	Branch string
	Labels []string
}

// Vars are the run vars of e: trigger_kind, trigger_key, trigger_source,
// trigger_repo, trigger_number, trigger_title, trigger_url,
// trigger_body, trigger_author, trigger_branch, trigger_labels. They
// are all strings (vars are flat); a step reads them as
// {{vars.trigger_title}}.
func (e Event) Vars() map[string]any {
	v := map[string]any{
		"trigger_kind":   e.Kind,
		"trigger_key":    e.Key,
		"trigger_source": e.Source,
		"trigger_repo":   e.Repo,
		"trigger_title":  clip(e.Title, MaxTitle),
		"trigger_url":    clip(e.URL, 500),
		"trigger_body":   clip(e.Body, MaxBody),
		"trigger_author": clip(e.Author, 100),
		"trigger_branch": clip(e.Branch, 200),
		"trigger_labels": clip(strings.Join(e.Labels, ","), 500),
		"trigger_number": "",
	}
	if e.Number > 0 {
		v["trigger_number"] = strconv.Itoa(e.Number)
	}
	return v
}

// Summary is one line about e for a card or a list.
func (e Event) Summary() string {
	what := map[string]string{
		KindIssueOpened:  "new issue",
		KindIssueLabeled: "issue labeled " + strings.Join(e.Labels, ", "),
		KindPROpened:     "new pull request",
		KindMention:      "mention",
		KindCIFailed:     "CI failed",
		KindWebhook:      e.Source + " webhook",
	}[e.Kind]
	if what == "" {
		what = e.Kind
	}
	s := what
	if e.Number > 0 {
		s += " #" + strconv.Itoa(e.Number)
	}
	if e.Branch != "" && e.Kind == KindCIFailed {
		s += " on " + e.Branch
	}
	if t := oneLine(clip(e.Title, 80)); t != "" {
		s += " · " + t
	}
	return s
}

// clip cuts s to at most n bytes on a rune border.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

// oneLine drops control characters and folds lines (outside text on a
// card must not move the cursor or fake another row).
func oneLine(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// Clean is oneLine for callers (cards, the web).
func Clean(s string) string { return oneLine(s) }

// hasAny reports whether have has one of want (case is ignored).
func hasAny(have, want []string) bool {
	for _, w := range want {
		for _, h := range have {
			if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(w)) {
				return true
			}
		}
	}
	return false
}
