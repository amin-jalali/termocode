package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// OM-1: a workspace's AGENTS.md / .termocode/rules.md. The user trusts
// the exact text once per workspace; a changed file asks again.

func writeAIRules(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// aiRulesModel is a plain-chat model (default profile) whose recording
// provider keeps every request, in a temp workspace with AGENTS.md.
func aiRulesModel(t *testing.T, rules string) (*Model, *recordingAIProvider, string) {
	t.Helper()
	rec := &recordingAIProvider{}
	m, dir := aiIDETestModel(t, rec)
	if rules != "" {
		writeAIRules(t, dir, "AGENTS.md", rules)
	}
	return m, rec, dir
}

func (m *Model) answerAIRulesForTest(id string) tea.Cmd {
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm(id)
	return cmd
}

// Spec: the first message in a workspace with AGENTS.md opens the trust
// card before anything is sent: Not in this project is the safe answer,
// the box shows the file and its first line.
func TestAIRulesTrustCardBeforeSend(t *testing.T) {
	m, rec, _ := aiRulesModel(t, "Always run go vet.\nSecond rule.\n")
	cmd := m.sendAIChat("hello")
	if !m.confirmOpen || m.confirmKind != confirmKindAIRulesTrust {
		t.Fatal("no rules trust card before the first send")
	}
	if got := m.confirm.SafeID(); got != "deny" {
		t.Errorf("safe answer %q, want deny", got)
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"Use this project's rules file?", "AGENTS.md", "Always run go vet.", "Not in this project", "Always in this project", "steers the AI"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	if cmd != nil || m.ai.streaming || len(m.ai.conv.Messages) != 0 || len(rec.reqs) != 0 {
		t.Fatalf("sent before the answer: streaming %v, %d messages", m.ai.streaming, len(m.ai.conv.Messages))
	}
}

// Spec: Always in this project sends the waiting message and remembers
// the answer for this text; the next send asks nothing.
func TestAIRulesTrustUseSendsAndRemembers(t *testing.T) {
	m, rec, _ := aiRulesModel(t, "Rule one.\n")
	m.sendAIChat("hello")
	driveAIChat(t, m, m.answerAIRulesForTest("use"), nil)
	if len(m.ai.conv.Messages) != 2 || m.ai.conv.Messages[0].Text != "hello" || len(rec.reqs) != 1 {
		t.Fatalf("not sent after Use: %+v", m.ai.conv.Messages)
	}
	if sess := loadSession(); sess.AIRulesTrusted == "" || sess.AIRulesRefused != "" {
		t.Errorf("session trusted %q refused %q", sess.AIRulesTrusted, sess.AIRulesRefused)
	}
	driveAIChat(t, m, m.sendAIChat("again"), nil)
	if m.confirmOpen || len(rec.reqs) != 2 {
		t.Fatalf("asked again after Use (card %v, %d requests)", m.confirmOpen, len(rec.reqs))
	}
}

// Spec: Not in this project sends the message (without rules) and is
// remembered; Esc is no answer: nothing is sent, the text goes back into
// the input, and the next send asks again.
func TestAIRulesTrustDenyAndEsc(t *testing.T) {
	m, rec, _ := aiRulesModel(t, "Rule one.\n")
	m.sendAIChat("hello")
	driveAIChat(t, m, m.answerAIRulesForTest("deny"), nil)
	if len(m.ai.conv.Messages) != 2 || len(rec.reqs) != 1 {
		t.Fatalf("not sent after Not in this project: %+v", m.ai.conv.Messages)
	}
	if sess := loadSession(); sess.AIRulesRefused == "" || sess.AIRulesTrusted != "" {
		t.Errorf("session trusted %q refused %q", sess.AIRulesTrusted, sess.AIRulesRefused)
	}
	driveAIChat(t, m, m.sendAIChat("again"), nil)
	if m.confirmOpen || len(rec.reqs) != 2 {
		t.Fatal("asked again after Not in this project")
	}

	m2, rec2, _ := aiRulesModel(t, "Rule one.\n")
	m2.sendAIChat("keep me")
	m2.confirmOpen = false
	m2.onAIConfirmClosed() // Esc
	if string(m2.ai.input) != "keep me" || len(m2.ai.conv.Messages) != 0 || len(rec2.reqs) != 0 {
		t.Fatalf("Esc: input %q, %d messages", string(m2.ai.input), len(m2.ai.conv.Messages))
	}
	if sess := loadSession(); sess.AIRulesTrusted != "" || sess.AIRulesRefused != "" {
		t.Error("Esc stored an answer")
	}
	m2.ai.input = nil
	m2.sendAIChat("keep me")
	if !m2.confirmOpen || m2.confirmKind != confirmKindAIRulesTrust {
		t.Error("no card after Esc")
	}
}

// Spec: an edited rules file (for example from a pulled commit) asks
// again, after Use and after Not in this project.
func TestAIRulesChangedAsksAgain(t *testing.T) {
	for _, answer := range []string{"use", "deny"} {
		t.Run(answer, func(t *testing.T) {
			m, _, dir := aiRulesModel(t, "Rule one.\n")
			m.sendAIChat("hello")
			driveAIChat(t, m, m.answerAIRulesForTest(answer), nil)
			writeAIRules(t, dir, "AGENTS.md", "Rule one.\nRule two.\n")
			m.sendAIChat("again")
			if !m.confirmOpen || m.confirmKind != confirmKindAIRulesTrust {
				t.Fatal("a changed file did not ask again")
			}
			if !strings.Contains(ansi.Strip(m.confirm.Box()), "Rule two.") {
				t.Error("card does not show the new text")
			}
		})
	}
}

// Spec: no rules file, no card (the plain chat is unchanged).
func TestAIRulesNoFileNoCard(t *testing.T) {
	m, rec, _ := aiRulesModel(t, "")
	driveAIChat(t, m, m.sendAIChat("hello"), nil)
	if m.confirmOpen || len(rec.reqs) != 1 {
		t.Fatal("card or no request without a rules file")
	}
}

// Spec: AI: Project Rules… opens the card to change the answer (it sends
// nothing), and says so when the workspace has no rules file. The AI can
// never run it.
func TestAIRulesPalette(t *testing.T) {
	m, rec, _ := aiRulesModel(t, "")
	m.dispatchAIPalette("ai-rules")
	if m.confirmOpen || !strings.Contains(aiToastText(m), "No AGENTS.md or .termocode/rules.md") {
		t.Fatalf("no-file toast %q", aiToastText(m))
	}

	m2, rec2, _ := aiRulesModel(t, "Rule one.\n")
	m2.sendAIChat("hello")
	driveAIChat(t, m2, m2.answerAIRulesForTest("deny"), nil)
	m2.dispatchAIPalette("ai-rules")
	if !m2.confirmOpen || m2.confirmKind != confirmKindAIRulesTrust {
		t.Fatal("palette did not open the card")
	}
	m2.answerAIRulesForTest("use")
	if sess := loadSession(); sess.AIRulesTrusted == "" || sess.AIRulesRefused != "" {
		t.Errorf("palette answer not stored: %+v", sess)
	}
	if len(rec2.reqs) != 1 || len(rec.reqs) != 0 {
		t.Error("the palette card sent a message")
	}

	if p, ok := aiActionPolicies["ai-rules"]; !ok || p.Gate != aiGateNever {
		t.Errorf("ai-rules policy %+v", p)
	}
}
