package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Review-only rules (CMP-37): the "Code Review Rules" section of
// AGENTS.md (and of a folder AGENTS.md for the files below it) goes into
// AI Review My Changes / AI Review Branch, above the diff, where the user
// reads it before sending. The chat's system prompt does not get it.
// Git: Add Review Rules to AGENTS.md… starts the section after a
// Cancel-first card.

// gitxReviewRulesTemplate is the section the command adds.
const gitxReviewRulesTemplate = `## Code Review Rules

Only AI reviews use this section (AI Review My Changes, AI Review Branch).

- Report real problems only: bugs, security holes, data loss, missing tests.
- Put the most important finding first. Skip style nits.
- Say "No findings" when there are none.
`

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-ai-review-rules", Title: "Git: Add Review Rules to AGENTS.md…"},
		},
		policies: map[string]aiActionPolicy{
			"git-ai-review-rules": aiNever("writes the user's AGENTS.md after a card; the user only"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id != "git-ai-review-rules" {
				return nil, false
			}
			return m.gitxAddReviewRules(), true
		},
	})
}

// gitxReviewRules is the review rules block for a review of diff in the
// repository root ("" for none, or when the user refused the rules).
// Secrets and key-looking strings are hidden.
func gitxReviewRules(root, diff string) string {
	r := ai.LoadProjectRules(root)
	if r.Hash == "" || aiRulesTrust(r) == aiTrustRefused {
		return ""
	}
	rev := r.ReviewFor(gitxDiffPaths(diff))
	if rev == "" {
		return ""
	}
	return "Follow the project's review rules (from AGENTS.md):\n" + ai.Redact(rev)
}

// gitxDiffPaths are the files a git diff touches (the b/ side).
func gitxDiffPaths(diff string) []string {
	var out []string
	for _, l := range strings.Split(diff, "\n") {
		rest, ok := strings.CutPrefix(l, "diff --git ")
		if !ok {
			continue
		}
		if i := strings.LastIndex(rest, " b/"); i >= 0 {
			out = append(out, rest[i+3:])
		}
	}
	return out
}

// gitxReviewRulesRoot is the folder whose AGENTS.md holds the rules.
func (m *Model) gitxReviewRulesRoot() string {
	if root, err := m.gitRootDir(); err == nil {
		return root
	}
	return aiWorkspaceRoot()
}

// gitxAddReviewRules opens the section when AGENTS.md has one; else it
// shows the section on a Cancel-first card and adds it on Add Section.
func (m *Model) gitxAddReviewRules() tea.Cmd {
	p := filepath.Join(m.gitxReviewRulesRoot(), "AGENTS.md")
	text, err := gitxReadAgentsMD(p)
	if err != nil {
		return m.gitxFail("Could not read AGENTS.md", err)
	}
	if line := ai.ReviewRulesLine(text); line > 0 {
		m.openFileAtLine(p, line)
		return m.gitxInfo("AGENTS.md has review rules: edit them there")
	}
	where := "Adds this section to the end of AGENTS.md."
	if text == "" {
		where = "Creates AGENTS.md with this section."
	}
	body := where + " AI Review My Changes and AI Review Branch put it into the request; the chat does not get it.\n\n" +
		strings.TrimRight(gitxReviewRulesTemplate, "\n")
	m.gitxCard("Add review rules to AGENTS.md?", body, func(m *Model, id string) tea.Cmd {
		if id != "add" {
			return nil
		}
		return m.gitxWriteReviewRules(p)
	}, confirm.Button{ID: "add", Title: "Add Section"})
	return nil
}

// gitxReadAgentsMD reads AGENTS.md ("" when it does not exist). A link
// is refused: the command never writes outside the workspace.
func gitxReadAgentsMD(p string) (string, error) {
	st, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("AGENTS.md is a link or not a file: edit it by hand")
	}
	b, err := os.ReadFile(p)
	return string(b), err
}

// gitxWriteReviewRules adds the section to AGENTS.md (read again: the
// file may have changed while the card was open) and opens it there.
func (m *Model) gitxWriteReviewRules(p string) tea.Cmd {
	text, err := gitxReadAgentsMD(p)
	if err != nil {
		return m.gitxFail("Could not read AGENTS.md", err)
	}
	if line := ai.ReviewRulesLine(text); line > 0 {
		m.openFileAtLine(p, line)
		return m.gitxInfo("AGENTS.md has review rules already")
	}
	out := gitxReviewRulesTemplate
	if strings.TrimSpace(text) != "" {
		out = strings.TrimRight(text, "\n") + "\n\n" + gitxReviewRulesTemplate
	}
	if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
		return m.gitxFail("Could not write AGENTS.md", err)
	}
	m.openFileAtLine(p, ai.ReviewRulesLine(out))
	return m.gitxInfo("Added review rules to AGENTS.md: edit them to fit the project")
}
