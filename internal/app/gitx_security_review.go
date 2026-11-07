package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/secreview"
)

// Security review with a threat model (CMP-57): the request (threat
// model, scan hints, diff) waits in the chat box; the user reads it and
// sends it. Edit Threat Model opens .termocode/threat-model.md and asks
// on a Cancel-first card before it writes a new one.

const gitxSecurityHints = 40 // most scan hints put in one request

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-ai-security-changes", Title: "Git: AI Security Review My Changes (Threat Model)"},
			{ID: "git-ai-security-branch", Title: "Git: AI Security Review Branch (Threat Model)"},
			{ID: "git-threat-model", Title: "Git: Edit Threat Model…"},
		},
		policies: map[string]aiActionPolicy{
			"git-ai-security-changes": aiNever("fills the user's chat box; the user reads it and sends it"),
			"git-ai-security-branch":  aiNever("fills the user's chat box; the user reads it and sends it"),
			"git-threat-model":        aiNever("opens the threat model for the user; a card asks before it writes the file"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-ai-security-changes":
				return m.gitxSecurityReview(false), true
			case "git-ai-security-branch":
				return m.gitxSecurityReview(true), true
			case "git-threat-model":
				return m.gitxThreatModel(), true
			}
			return nil, false
		},
	})
}

// gitxSecurityReview puts a security review request in the chat box.
func (m *Model) gitxSecurityReview(branch bool) tea.Cmd {
	d, c, ok := m.gitxReviewDiff(branch)
	if !ok {
		return c
	}
	model, _, err := secreview.Load(d.root)
	if err != nil {
		return m.gitxFail("Could not read the threat model", err)
	}
	return m.gitxFillChat(secreview.Prompt(secreview.Input{
		Branch: branch, Base: d.base, Diff: d.diff, Cut: d.cut, Model: model,
		Hints: secreview.ScanDiff(d.diff, gitxSecurityHints),
	}))
}

// gitxThreatModel opens the threat model; when there is none, a
// Cancel-first card asks before it writes the template.
func (m *Model) gitxThreatModel() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	root, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	p := secreview.Path(root)
	if _, err := os.Stat(p); err == nil {
		m.openFileAtLine(p, 1)
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return m.gitxFail("Could not read the threat model", err)
	}
	m.gitxCard("Create a threat model?",
		"Writes "+filepath.Join(".termocode", "threat-model.md")+" with the sections to fill in (assets, entry points, trust boundaries, STRIDE threats).\n"+
			"The security reviews send it to the AI with the diff.",
		func(m *Model, id string) tea.Cmd {
			if id != "create" {
				return nil
			}
			if _, err := os.Stat(p); err == nil {
				m.openFileAtLine(p, 1) // made meanwhile: keep it
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return m.gitxFail("Could not create the threat model", err)
			}
			if err := os.WriteFile(p, []byte(secreview.Template(filepath.Base(root))), 0o644); err != nil {
				return m.gitxFail("Could not create the threat model", err)
			}
			m.openFileAtLine(p, 1)
			return m.gitxInfo("Fill in the threat model: the security reviews use it")
		}, confirm.Button{ID: "create", Title: "Create File"})
	return nil
}
