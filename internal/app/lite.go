package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// Lite mode (CMP-99): termocode without the embedded editor, for a light
// install. Terminals, agents, git, worktrees, runs and the AI work; the
// editor area stays empty. It runs when asked (`termocode --lite`,
// TERMOCODE_LITE=1) or by itself when nvim is not on PATH, so a machine
// without Neovim gets a working tool instead of an "nvim:" error.

// EnvLite set to 1 (or true) runs Lite mode.
const EnvLite = "TERMOCODE_LITE"

// Why Lite mode runs (Model.lite; "" = the full editor).
const (
	liteAsked  = "asked"   // --lite or TERMOCODE_LITE=1
	liteNoNvim = "no-nvim" // nvim is not on PATH
)

// liteReason is why this start runs Lite mode ("" when it does not).
func liteReason(getenv func(string) string, lookPath func(string) (string, error)) string {
	switch strings.ToLower(strings.TrimSpace(getenv(EnvLite))) {
	case "1", "true", "yes", "on":
		return liteAsked
	}
	if _, err := lookPath("nvim"); err != nil {
		return liteNoNvim
	}
	return ""
}

// The start tips of Lite mode.
const (
	liteAskedTip  = "Lite mode: no editor. Terminals, agents, git and worktrees work. For the editor, install Neovim 0.10+ (termocode setup) and start without --lite."
	liteNoNvimTip = "Neovim was not found, so termocode runs in Lite mode: terminals, agents, git and worktrees. For the editor, install Neovim 0.10+ (termocode setup)."
)

// liteTipCmd is the start toast of Lite mode (nil for the full editor).
func liteTipCmd(reason string) tea.Cmd {
	tip := ""
	switch reason {
	case liteAsked:
		tip = liteAskedTip
	case liteNoNvim:
		tip = liteNoNvimTip
	default:
		return nil
	}
	return func() tea.Msg { return ToastMsg{Level: toast.Info, Title: tip} }
}
