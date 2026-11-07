package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/confirm"
)

func TestDeployGitPromptOpensACancelFirstCard(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m := newTestModel(t)
	m.services = newServicesState()
	m.onDeployGitPrompt("https://github.com/acme/shop.git#main")
	if !m.confirmOpen || m.confirmKind != confirmKindServicesDeployGit {
		t.Fatalf("no deploy card: open=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	btns := m.confirm.Buttons()
	if len(btns) < 2 || btns[0].ID != "cancel" || btns[1].Style != confirm.StyleDestructive {
		t.Fatalf("buttons %+v", btns)
	}
	if !strings.Contains(m.services.deployGitLine, "git clone --depth 1 --branch main -- https://github.com/acme/shop.git") ||
		!strings.Contains(m.services.deployGitLine, "&& docker compose --project-directory") {
		t.Fatalf("line %q", m.services.deployGitLine)
	}
	m.answerDeployGit("cancel")
	if m.services.deployGitLine != "" {
		t.Fatal("cancel must drop the plan")
	}
}

func TestDeployGitPromptRefusesAFlag(t *testing.T) {
	m := newTestModel(t)
	m.services = newServicesState()
	m.onDeployGitPrompt("--upload-pack=evil")
	if m.confirmOpen {
		t.Fatal("a flag must not reach a card")
	}
}
