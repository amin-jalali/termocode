package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/transfer"
)

// SCN-27: a kitty (or iTerm2) download talks to the terminal that was
// attached when it started. When the link drops and a terminal attaches
// again, that terminal knows nothing about the transfer: termocode stops
// it at once and says why, instead of waiting for a timeout that ends in
// "is this really kitty?".
func TestTerminalChangeStopsTerminalDownload(t *testing.T) {
	for _, method := range []transfer.Method{transfer.Kitty, transfer.ITerm2} {
		t.Run(string(method), func(t *testing.T) {
			canceled := false
			m := &Model{} // no size: applyTerminalChange skips the layout
			m.xfer = &xferRun{name: "big.txt", method: method, cancel: func() { canceled = true }, ch: make(chan tea.Msg, 1)}
			m.applyTerminalChange(TerminalChangedMsg{Gen: 2, Note: "Reconnected after 3 s"})
			if !canceled {
				t.Fatal("the download to the old terminal still runs after a new attach")
			}
			m.handleXferMsg(xferDoneMsg{name: "big.txt", method: method, err: transfer.ErrCanceled})
			got := covSvcToast(m)
			if !strings.Contains(got, "Download stopped") || !strings.Contains(got, "connection dropped") {
				t.Errorf("toast = %q, want it to say the connection dropped", got)
			}
			if strings.Contains(got, "Download canceled") {
				t.Errorf("toast = %q: the user did not cancel", got)
			}
			if m.xfer != nil {
				t.Error("the download is still marked as running")
			}
		})
	}
}

// A local copy (no terminal in the path) goes on across a re-attach.
func TestTerminalChangeKeepsLocalDownload(t *testing.T) {
	canceled := false
	m := &Model{}
	m.xfer = &xferRun{name: "big.txt", method: transfer.Local, cancel: func() { canceled = true }, ch: make(chan tea.Msg, 1)}
	m.applyTerminalChange(TerminalChangedMsg{Gen: 2})
	if canceled {
		t.Error("a local copy was stopped by a new attach")
	}
	if m.xfer == nil {
		t.Error("the local copy was dropped")
	}
}

// A cancel by the user is still a plain "Download canceled".
func TestUserCancelStillSaysCanceled(t *testing.T) {
	m := &Model{}
	m.xfer = &xferRun{name: "big.txt", method: transfer.Kitty, cancel: func() {}, ch: make(chan tea.Msg, 1)}
	m.cancelDownload()
	m.handleXferMsg(xferDoneMsg{name: "big.txt", method: transfer.Kitty, err: transfer.ErrCanceled})
	if got := covSvcToast(m); !strings.Contains(got, "Download canceled") || strings.Contains(got, "connection dropped") {
		t.Errorf("toast = %q", got)
	}
}
