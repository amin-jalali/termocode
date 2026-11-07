package ai

import (
	"strings"
)

// Review-only rules (CMP-37): a "## Code Review Rules" section (also
// "Review Rules", "Code Review", "Review Guidelines"; any heading level)
// of AGENTS.md or CLAUDE.md is for AI reviews only. LoadProjectRules cuts
// it out of the chat rules and keeps it in Rules.Review (folder files:
// ScopedRule.Review); the AI review requests put it above the diff.

// reviewHeadings are the section titles, lower case, without a colon.
var reviewHeadings = map[string]bool{
	"code review rules":      true,
	"review rules":           true,
	"code review guidelines": true,
	"review guidelines":      true,
	"code review":            true,
}

// mdHeading reads a Markdown ATX heading line ("## Title").
func mdHeading(l string) (level int, title string, ok bool) {
	t := strings.TrimLeft(l, " ")
	if len(l)-len(t) > 3 {
		return 0, "", false
	}
	for level < len(t) && t[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || (level < len(t) && t[level] != ' ' && t[level] != '\t') {
		return 0, "", false
	}
	title = strings.TrimSpace(t[level:])
	title = strings.TrimSpace(strings.TrimRight(title, "#"))
	return level, title, true
}

// isReviewHeading says whether a heading title starts a review section.
func isReviewHeading(title string) bool {
	t := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(title), ":")))
	return reviewHeadings[t]
}

// fenceStep tracks a ``` or ~~~ code fence: it returns the open fence
// after line l ("" when none is open).
func fenceStep(fence, l string) string {
	t := strings.TrimSpace(l)
	switch {
	case fence != "":
		if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
			return ""
		}
		return fence
	case strings.HasPrefix(t, "```"):
		return "```"
	case strings.HasPrefix(t, "~~~"):
		return "~~~"
	}
	return ""
}

// SplitReviewRules cuts the review sections out of a rules text. rest is
// the text without them (unchanged when there are none); review is their
// bodies (without the heading lines), joined by a blank line. A section
// ends at the next heading of the same or a higher level; headings in a
// code fence are text.
func SplitReviewRules(text string) (rest, review string) {
	lines := strings.Split(text, "\n")
	var keep, cur, sections []string
	in, found := false, false
	level := 0
	fence := ""
	flush := func() {
		if s := strings.Trim(strings.Join(cur, "\n"), " \t\n"); s != "" {
			sections = append(sections, s)
		}
		cur = nil
	}
	for _, l := range lines {
		if fence == "" {
			if lv, title, ok := mdHeading(l); ok {
				if in && lv <= level {
					flush()
					in = false
				}
				if !in && isReviewHeading(title) {
					in, found, level = true, true, lv
					continue
				}
			}
		}
		fence = fenceStep(fence, l)
		if in {
			cur = append(cur, l)
		} else {
			keep = append(keep, l)
		}
	}
	if in {
		flush()
	}
	if !found {
		return text, ""
	}
	// Removing a section can leave a run of blank lines: keep one.
	var out []string
	blank := false
	for _, l := range keep {
		b := strings.TrimSpace(l) == ""
		if b && blank {
			continue
		}
		blank = b
		out = append(out, l)
	}
	return strings.Trim(strings.Join(out, "\n"), " \t\n"), strings.Join(sections, "\n\n")
}

// ReviewRulesLine is the 1-based line of the first review section
// heading in text (0 when there is none).
func ReviewRulesLine(text string) int {
	fence := ""
	for i, l := range strings.Split(text, "\n") {
		if fence == "" {
			if _, title, ok := mdHeading(l); ok && isReviewHeading(title) {
				return i + 1
			}
		}
		fence = fenceStep(fence, l)
	}
	return 0
}

// ReviewFor is the review rules for a change of the files paths
// (relative to the root): the root ones, then each folder file's whose
// folder holds one of the paths (under "# <file>"). At most
// RulesMaxBytes; "" for none.
func (r Rules) ReviewFor(paths []string) string {
	var parts []string
	if r.Review != "" {
		parts = append(parts, r.Review)
	}
	for _, s := range r.Scoped {
		if s.Review == "" || s.Dir == "" {
			continue
		}
		for _, p := range paths {
			if strings.HasPrefix(strings.TrimPrefix(p, "./"), s.Dir+"/") {
				parts = append(parts, "# "+s.File+"\n"+s.Review)
				break
			}
		}
	}
	out := strings.Join(parts, "\n\n")
	if len(out) > RulesMaxBytes {
		out = cutRules(out, RulesMaxBytes)
	}
	return out
}
