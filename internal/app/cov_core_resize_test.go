package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// On a very narrow screen (a small tmux pane, a phone held upright) the
// frame must still fit: the sidebar's minimum width used to push every
// row past the right edge, so the terminal wrapped them and the whole
// screen broke.
func TestCovCoreTinyScreenFits(t *testing.T) {
	withConfigDir(t)
	for w := 1; w <= 40; w++ {
		for _, h := range []int{1, 2, 4, 10} {
			m := covNew(t)
			m, _ = m.update(tea.WindowSizeMsg{Width: w, Height: h})
			covFitsScreen(t, m, "start screen")
			_ = m.dispatchPaletteAction("open-settings-ui")
			covFitsScreen(t, m, "settings")
		}
	}
}
