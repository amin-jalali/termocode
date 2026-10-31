package app

import (
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestGitMutationCardsSafeFirst(t *testing.T) {
	for _, action := range []string{"revert", "cherry-pick", "stage"} {
		t.Run(action, func(t *testing.T) {
			m := Model{w: 100, h: 40, commitView: &commitViewState{info: git.CommitInfo{FullHash: "123456789abcdef"}}}
			if action == "stage" {
				m.openStageAllCommitConfirm()
			} else {
				m.openCommitPickConfirm(action)
			}
			if buttons := m.confirm.Buttons(); len(buttons) != 2 || buttons[0].ID != "cancel" || m.confirm.SafeID() != "cancel" {
				t.Errorf("unsafe card: %+v", buttons)
			}
			_, cmd := m.confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("Enter returned no choice")
			}
			if msg, ok := cmd().(confirm.SelectMsg); !ok || msg.ID != "cancel" {
				t.Errorf("Enter = %#v", cmd())
			}
			_, cmd = m.confirm.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if cmd == nil {
				t.Fatal("Esc did nothing")
			}
			if _, ok := cmd().(confirm.CloseMsg); !ok {
				t.Errorf("Esc = %#v", cmd())
			}
		})
	}
}
