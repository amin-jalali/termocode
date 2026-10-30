package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The web panel on the laptop (WEB-10): the palette commands, the start
// card, the status chip and its click. The AI can reach none of it
// (aiNever on every id and setting): only the user opens this door. The
// user's own palette command opens the browser in one step; the card is
// for a `termocode web` that no person typed (a script, a hidden run).

// confirmKindWebStart is the start card.
const confirmKindWebStart confirmKindEnum = 2600

func webPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "web-start", Title: "Web: Open in Browser", Lead: "web"},
		{ID: "web-start-panel", Title: "Web: Start Panel"}, // the old name, same command
		{ID: "web-stop", Title: "Web: Stop Panel"},
		{ID: "web-copy-link", Title: "Web: Copy Link"},
	}
}

// dispatchWebPalette runs the web panel's palette ids.
func (m *Model) dispatchWebPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "web-start", "web-start-panel":
		return m.webOpenNow(), true
	case "web-stop":
		return m.stopWebPanel(), true
	case "web-copy-link":
		if !webSettings().On {
			return m.webToast(toast.Warn, webOffText), true
		}
		return m.webNewLink(false, true), true
	}
	return nil, false
}

// webOpenNow is the user's Web: Open in Browser: start the panel and
// open it in the browser, no card; a running panel opens a new one-time
// link.
func (m *Model) webOpenNow() tea.Cmd {
	if !webSettings().On {
		return m.webToast(toast.Warn, webOffText)
	}
	if st := m.webPanel; st != nil {
		if st.up {
			return m.webNewLink(true, false)
		}
		return m.webToast(toast.Info, "The web panel is starting.")
	}
	return m.startWebPanel(true, false)
}

// webStartBody is the start card's text (old plan 5.2).
func webStartBody(cfg webConfig, fromCLI bool) string {
	var b strings.Builder
	b.WriteString("Anyone who controls the browser tab can run any command as you.\n")
	b.WriteString("The browser shows your files, chats and terminals and can type into them.\n")
	if cfg.Tailnet {
		b.WriteString("Your devices on your tailnet can reach it (tailscale serve), not on the internet.\n")
	} else {
		b.WriteString("The panel listens only on this computer (127.0.0.1")
		if cfg.Port != 0 {
			b.WriteString(":" + strconv.Itoa(cfg.Port))
		}
		b.WriteString(").\n")
	}
	b.WriteString("Shell output in the terminal panel is not masked.\n")
	b.WriteString("It stops after " + strconv.Itoa(cfg.IdleMinutes) + " minutes without use, or with Web: Stop Panel.")
	if fromCLI {
		b.WriteString("\n\nAsked by termocode web in a terminal.")
	}
	return b.String()
}

// openWebStartCard asks before the panel starts. call is a waiting
// `termocode web` request (nil from the palette). A running panel needs
// no card: it gets a new one-time link.
func (m *Model) openWebStartCard(call *webCall) tea.Cmd {
	cfg := webSettings()
	if !cfg.On {
		if call != nil {
			call.fail(webproto.CodeForbidden, webOffText)
		}
		return m.webToast(toast.Warn, webOffText)
	}
	if st := m.webPanel; st != nil {
		if call != nil {
			return m.webCtlOpen(call)
		}
		if st.up {
			return m.webNewLink(true, false)
		}
		return m.webToast(toast.Info, "The web panel is starting.")
	}
	if m.confirmOpen {
		if call != nil {
			call.fail(webproto.CodeBusy, "termocode shows another card. Answer it first, then try again.")
		}
		return nil
	}
	m.showWebStartCard(cfg, call)
	return nil
}

// showWebStartCard draws the start card. A risky card: Not now is first
// and focused, so Enter or Esc on a fresh card never opens the door.
func (m *Model) showWebStartCard(cfg webConfig, call *webCall) {
	m.confirm = confirm.Risky("Open this session in a web browser?", webStartBody(cfg, call != nil),
		confirm.Button{ID: "not-now", Title: "Not now"},
		confirm.Button{ID: "open", Title: "Open in browser", Style: confirm.StylePrimary},
		confirm.Button{ID: "copy", Title: "Copy link"})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWebStart
	m.webStartCall = call
}

// webDemo is the start card or the status chip with made-up state
// (DevActionMsg "webdemo", for the goldens): nothing listens and no
// process starts.
func (m *Model) webDemo(variant string) {
	switch variant {
	case "card":
		m.showWebStartCard(webConfig{IdleMinutes: 15, MaxHours: 12, OpenBrowser: true, MaxViewers: 4}, nil)
	case "on", "connected":
		m.webPanel = &webPanelState{up: true, port: 41237}
		if variant == "connected" {
			if m.web == nil {
				m.web = newWebHub()
			}
			m.web.live[&webConn{id: "c-demo"}] = true
		}
	}
}

// answerWebStart acts on the start card; anything but open or copy (Esc
// included) is Not now.
func (m *Model) answerWebStart(id string) tea.Cmd {
	call := m.webStartCall
	m.webStartCall = nil
	if id != "open" && id != "copy" {
		if call != nil {
			call.fail(webproto.CodeForbidden, "Not opened: the answer on the laptop was Not now.")
		}
		return nil
	}
	cmd := m.startWebPanel(id == "open", id == "copy")
	if call != nil {
		if m.webPanel == nil {
			call.fail(webproto.CodeInternal, "The web panel did not start. See the laptop.")
		} else {
			m.webPanel.waiters = append(m.webPanel.waiters, call)
		}
	}
	return cmd
}
