package app

import (
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/termfont"
	"github.com/amin-jalali/termocode/internal/toast"
)

// fontTip is set at startup when font_delta asked for a font change the
// terminal could not make (only kitty with remote control can). The UI
// then shows termfont.Tip once.
var fontTip struct {
	sync.Mutex
	pending bool
}

// SetFontResult records the startup font change (cmd/termocode): want is
// font_delta, s what the terminal did.
func SetFontResult(want int, s termfont.State) {
	fontTip.Lock()
	fontTip.pending = s.TipNeeded(want)
	fontTip.Unlock()
}

// fontTipCmd returns the tip toast the first time it is due, else nil.
func fontTipCmd() tea.Cmd {
	fontTip.Lock()
	defer fontTip.Unlock()
	if !fontTip.pending {
		return nil
	}
	fontTip.pending = false
	return func() tea.Msg { return ToastMsg{Level: toast.Info, Title: termfont.Tip} }
}
