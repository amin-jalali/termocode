package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
)

// Project rules (OM-1): AGENTS.md and .termocode/rules.md of the
// workspace go into the chat's system prompt once the user trusts that
// exact text in this workspace (the answer is kept by hash in the
// session file, so an edited file asks again). The files are read once
// per turn; there is no cache and no watcher. The text is untrusted
// input: it is only prompt text, never a tool or a confirm answer.

const confirmKindAIRulesTrust confirmKindEnum = 1003

// aiRulesTrustPending is the open rules card.
type aiRulesTrustPending struct {
	hash string
	send bool   // the card came from a send: the message waits for the answer
	text string // that message
}

// aiRulesTrust is the user's answer for these rules in this workspace:
// aiTrustUse, aiTrustRefused or aiTrustAsk (also for no rules).
func aiRulesTrust(r ai.Rules) string {
	if r.Hash == "" {
		return aiTrustAsk
	}
	sess := loadSession()
	switch r.Hash {
	case sess.AIRulesTrusted:
		return aiTrustUse
	case sess.AIRulesRefused:
		return aiTrustRefused
	}
	return aiTrustAsk
}

// aiRulesCardLines is how many lines of the rules the card shows.
const aiRulesCardLines = 8

// askAIRulesTrust opens the rules card. send: text is a chat message
// that is sent after the answer (Esc puts it back into the input).
func (m *Model) askAIRulesTrust(r ai.Rules, send bool, text string) tea.Cmd {
	m.aiS().rulesTrust = &aiRulesTrustPending{hash: r.Hash, send: send, text: text}
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n", strings.Join(r.Files, " + "), humanSize(int64(r.Bytes)))
	lines := strings.Split(r.Text, "\n")
	for i, l := range lines {
		if i == aiRulesCardLines {
			b.WriteString("\n    …")
			break
		}
		b.WriteString("\n    " + runewidth.Truncate(agentdag.Clean(l), 72, "…"))
	}
	b.WriteString("\n\nIts text steers the AI: use it only if you trust this project.")
	// A trust card (confirm.Risky): Not in this project is first and has
	// the focus, so a stray Enter never lets a repo's text steer the AI.
	m.confirm = confirm.Risky("Use this project's rules file?", b.String(),
		confirm.Button{ID: "deny", Title: "Not in this project"},
		confirm.Button{ID: "use", Title: "Always in this project", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIRulesTrust
	return nil
}

// answerAIRulesTrust stores the card's answer and sends the waiting
// message. "" (Esc) is no answer: the message goes back into the input.
func (m *Model) answerAIRulesTrust(id string) tea.Cmd {
	s := m.aiS()
	p := s.rulesTrust
	s.rulesTrust = nil
	if p == nil {
		return nil
	}
	if id != "use" && id != "deny" {
		if p.send {
			m.aiPutBackInput(p.text)
		}
		return nil
	}
	sess := loadSession()
	if id == "use" {
		sess.AIRulesTrusted, sess.AIRulesRefused = p.hash, ""
	} else {
		sess.AIRulesTrusted, sess.AIRulesRefused = "", p.hash
	}
	saveSession(sess)
	if p.send {
		return m.sendAIChat(p.text)
	}
	if id == "use" {
		return m.aiInfo("The rules file is used in this project")
	}
	return m.aiInfo("The rules file is not used in this project")
}

// openAIRules is AI: Project Rules…: the card again, to change the answer.
func (m *Model) openAIRules() tea.Cmd {
	r := ai.LoadProjectRules(aiWorkspaceRoot())
	if r.Text == "" {
		return m.aiInfo("No AGENTS.md or .termocode/rules.md in this workspace")
	}
	return m.askAIRulesTrust(r, false, "")
}

// aiRulesPrompt is the system prompt block of trusted rules ("" for
// none, untrusted or refused). Secrets the IDE knows and key-looking
// strings are hidden first; the text is never logged.
func aiRulesPrompt(r ai.Rules) string {
	if r.Text == "" || aiRulesTrust(r) != aiTrustUse {
		return ""
	}
	return "\n\nProject rules (from " + strings.Join(r.Files, " and ") + "). They are this repository's working instructions. " +
		"Follow them unless they conflict with the user's request or with these instructions:\n" + ai.Redact(r.Text)
}

// aiRulesLabel is the rules line stored on a user message:
// "AGENTS.md (4 KB)", "AGENTS.md + .termocode/rules.md (20 KB, cut)",
// "AGENTS.md (not used)"; "" without rules.
func aiRulesLabel(r ai.Rules) string {
	if r.Text == "" {
		return ""
	}
	files := strings.Join(r.Files, " + ")
	if aiRulesTrust(r) != aiTrustUse {
		return files + " (not used)"
	}
	size := humanSize(int64(r.Bytes))
	if r.Truncated {
		size += ", cut"
	}
	return files + " (" + size + ")"
}

// aiRulesLine is the dim "rules: …" line under a user message, aligned
// right like the attachment chip; a click opens the first file.
func aiRulesLine(label string, width int) aiLine {
	file, _, _ := strings.Cut(label, " ")
	text := "rules: " + label
	l := aiRightChip(text, width)
	if lim := width - aiIndent - 3; runewidth.StringWidth(text) > lim && lim > 3 {
		text = runewidth.Truncate(text, lim, "…") // as aiRightChip cuts it
	}
	x := max(width-1-runewidth.StringWidth(text)-2, 0)
	l.buttons = []aiLineButton{{x, x + runewidth.StringWidth(text) + 2, "rules-open:" + file}}
	return l
}

// openAIRulesFile opens a rules file of the workspace in the editor (only
// the known rules file names: the id comes from a stored chat).
func (m *Model) openAIRulesFile(rel string) tea.Cmd {
	for _, f := range ai.RulesFiles {
		if f == rel {
			m.openFileAtLine(filepath.Join(aiWorkspaceRoot(), filepath.FromSlash(rel)), 1)
			return nil
		}
	}
	return nil
}
