package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Helpers shared by the GITX tests (gitx_hub.go).

// gitxPickerIDs is the ids of the last GITX picker's rows.
func gitxPickerIDs(m *Model) []string {
	var ids []string
	for _, it := range m.gitx.items {
		ids = append(ids, it.ID)
	}
	return ids
}

// gitxCardIDList is the card's button ids, in order.
func gitxCardIDList(m *Model) string {
	var ids []string
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	return strings.Join(ids, ",")
}

// gitxUpstreamRepo is covGitRepo pushed to a bare "origin" (main tracks
// origin/main).
func gitxUpstreamRepo(t *testing.T) (*Model, string, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	bare := t.TempDir()
	covGit(t, dir, "init", "-q", "--bare", "-b", "main", bare)
	covGit(t, dir, "remote", "add", "origin", bare)
	covGit(t, dir, "push", "-q", "-u", "origin", "main")
	covRefresh(m)
	return m, dir, bare
}

// gitxDone runs cmd and applies the gitRemoteDoneMsg it ends with.
func gitxDone(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	m.gitRemoteDone(covMsg[gitRemoteDoneMsg](t, cmd))
}

// The hub's card is Cancel-first; each answer goes to the feature that
// opened it once, and a card, picker or prompt of another kind is not
// taken.
func TestGitxHubRoutes(t *testing.T) {
	m := &Model{w: 120, h: 40}
	var got []string
	m.gitxCard("Do it?", "text", func(m *Model, id string) tea.Cmd { got = append(got, "card:"+id); return nil },
		confirm.Button{ID: "do", Title: "Do"})
	if m.confirm.SafeID() != "cancel" || gitxCardIDList(m) != "cancel,do" {
		t.Fatalf("card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if _, ok := m.handleGitxConfirm("do"); ok {
		t.Error("a card answered twice")
	}
	m.gitxPick("Pick", []picker.Item{{ID: "a"}}, func(m *Model, id string) tea.Cmd { got = append(got, "pick:"+id); return nil })
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "a"})
	m.gitxPrompt("Ask", "label", "v", func(m *Model, v string) tea.Cmd { got = append(got, "prompt:"+v); return nil })
	m.promptOpen = false
	m.handlePromptSubmit("typed")
	m.confirmKind = confirmKindDeletePath
	if _, ok := m.handleGitxConfirm("do"); ok {
		t.Error("took another card's answer")
	}
	if strings.Join(got, ",") != "card:cancel,pick:a,prompt:typed" {
		t.Errorf("answers %v", got)
	}
	for _, it := range gitxPaletteItems() {
		if _, ok := aiActionPolicies[it.ID]; !ok {
			t.Errorf("%s has no AI policy", it.ID)
		}
	}
}
