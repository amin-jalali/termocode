package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
)

func TestRegistryDeleteTagCardIsCancelFirstAndNamesTheDigest(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	repo := containers.RemoteRepo{Host: "reg.example.com", APIHost: "reg.example.com", Name: "acme/api"}
	m.onRegistryDigest(registryDigestMsg{repo: repo, tag: "1.0", digest: "sha256:abc"})
	if !m.confirmOpen || m.confirmKind != confirmKindRegistryDeleteTag {
		t.Fatal("no delete card")
	}
	b := m.confirm.Buttons()
	if b[0].ID != "cancel" || b[len(b)-1].Style != confirm.StyleDestructive {
		t.Fatalf("buttons %+v", b)
	}
	if !strings.Contains(m.confirm.View(), "sha256:abc") {
		t.Fatal("card must name the digest")
	}
	if cmd := m.answerRegistryDeleteTag("cancel"); cmd != nil || m.services.regDelete.digest != "" {
		t.Fatal("cancel must do nothing")
	}
}

func TestRegistryAuthCardAsksBeforeUsingTheSavedToken(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	m.askRegistryAuth("ghcr.io", "amin", func(m *Model) tea.Cmd { return nil })
	if !m.confirmOpen || m.confirmKind != confirmKindRegistryAuth {
		t.Fatal("no opt-in card")
	}
	if b := m.confirm.Buttons(); b[0].ID != "cancel" || b[1].ID != "use" {
		t.Fatalf("buttons %+v", b)
	}
	m.answerRegistryAuth("cancel")
	if m.services.regAuthOK["ghcr.io"] != "" {
		t.Fatal("cancel must not opt in")
	}
}

func TestCloudLoginHintCardOffersCopy(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	m.onRegistryPicked("cloud:azure")
	if !m.confirmOpen || m.confirmKind != confirmKindRegistryCloudHint {
		t.Fatal("no hint card")
	}
	if !strings.Contains(strings.Join(strings.Fields(m.confirm.View()), " "), "az acr login") {
		t.Fatal("hint card must show the CLI command")
	}
}
