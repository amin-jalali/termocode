package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// State of the file and terminal views on the hub (WEB-16, WEB-17,
// WEB-19). It lives on the hub, so it starts empty with every web
// session and ends with it. Update only.

func init() {
	webAfterUpdates = append(webAfterUpdates, func(m *Model, _ tea.Msg) tea.Cmd { return m.webTrackFiles() })
}

// webFiles is the hub's state of the tree, the buffers and terminals.
type webFiles struct {
	tree webTree
	bufs map[int]*webBuf // attached buffers (files and terminals) by number

	loading int                   // buf.open calls on their way
	early   map[int][]webBufEvent // callbacks that came before their buf.open answered
	earlyN  int

	bufsList  *webproto.BufsSnap  // the bufs list as last sent
	termsList *webproto.TermsSnap // the terms list as last sent
	typedAt   time.Time           // keys from a browser reached a shell (the status chip's mark)
}

// files returns the hub's file state, making it on first use.
func (h *webHub) filesState() *webFiles {
	if h.files == nil {
		h.files = &webFiles{tree: newWebTree(), bufs: map[int]*webBuf{}, early: map[int][]webBufEvent{}}
	}
	return h.files
}

// webConnKnown reports whether a connection id is live or may resume.
func (h *webHub) webConnKnown(id string) bool {
	if _, ok := h.resume[id]; ok {
		return true
	}
	for c := range h.live {
		if c.id == id {
			return true
		}
	}
	return false
}

// webTrackFiles runs after every Update while the web side is on: it
// publishes the changes of the lists the TUI already keeps (bufs, terms,
// the hidden-files toggle) and lets go of what no browser follows any
// more. It reads Model fields only and calls nvim only to detach.
func (m *Model) webTrackFiles() tea.Cmd {
	h := m.web
	if h == nil || h.files == nil && len(h.rings) == 0 {
		return nil
	}
	f := h.filesState()
	cmd := m.webTrackTree(h, f)
	m.webTrackBufs(h, f)
	m.webTrackTerms(h, f)
	m.webReleaseBufs(h, f)
	return cmd
}
