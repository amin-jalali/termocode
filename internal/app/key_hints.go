package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// Key hints: after the leader key in the editor, a small popup lists the
// next keys (internal/keyhint). The key_hints setting turns it off.

// keyHintsEnabled is the key_hints setting (default on).
func keyHintsEnabled(cfg settingsConfig) bool { return cfg.KeyHints == nil || *cfg.KeyHints }

// hideKeyHints closes the popup and forgets the sequence.
func (m *Model) hideKeyHints() {
	if m.hints != nil {
		m.hints.Reset()
	}
}

// toggleKeyHints flips key_hints and says what it is now.
func (m *Model) toggleKeyHints() tea.Cmd {
	cfg := loadSettings()
	on := !keyHintsEnabled(cfg)
	cfg.KeyHints = &on
	if err := saveSettings(cfg); err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Errr, "Settings error", err.Error())
		return c
	}
	var c tea.Cmd
	if on {
		m.toast, c = m.toast.Push(toast.Info, "Key hints on")
	} else {
		m.hideKeyHints()
		m.toast, c = m.toast.Push(toast.Info, "Key hints off")
	}
	return c
}
