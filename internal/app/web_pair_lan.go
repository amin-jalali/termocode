package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Pair a Device in loopback mode (the default): the panel is only on
// 127.0.0.1, which a phone cannot reach. Instead of a note about
// config.json, a card asks to use this Wi-Fi; its yes saves
// "web_listen": "lan" in the user's config.json, starts the panel again
// in LAN mode and shows the QR code at once. Only the owner's button
// does it: web-pair is aiNever, and the card is answered from the
// confirm router alone.

const confirmKindWebUseLAN confirmKindEnum = 2606 // pair a phone on this Wi-Fi?

// openWebUseLANCard asks to make the panel reachable on this Wi-Fi.
func (m *Model) openWebUseLANCard() tea.Cmd {
	if m.confirmOpen {
		return m.webToast(toast.Warn, "Answer the open card first.")
	}
	body := "A phone or tablet can only reach the web panel on this computer for now.\n" +
		"Use this Wi-Fi makes it reachable on this Wi-Fi only:\n" +
		"  • on this laptop's private address (192.168.x.x), not the internet,\n" +
		"  • with a pinned certificate the phone checks every time,\n" +
		"  • and every device still needs your yes here.\n\n" +
		"Outside the home? Use \"web_listen\": \"tailnet\" with Tailscale instead."
	m.confirm = confirm.Risky("Pair a phone on this Wi-Fi?", body,
		confirm.Button{ID: "not-now", Title: "Not now"},
		confirm.Button{ID: "use-lan", Title: "Use this Wi-Fi", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWebUseLAN
	return nil
}

// answerWebUseLAN acts on the card; anything but Use this Wi-Fi (Esc
// too) is Not now and changes nothing.
func (m *Model) answerWebUseLAN(id string) tea.Cmd {
	if id != "use-lan" {
		return nil
	}
	return m.webSwitchToLAN()
}

// webSwitchToLAN saves LAN mode, then starts the panel in it (stopping
// the loopback one first) and shows the QR code once it is up.
func (m *Model) webSwitchToLAN() tea.Cmd {
	if _, ok := webLANAddr(); !ok {
		return m.webToast(toast.Warn, "This laptop has no Wi-Fi address. Connect this laptop to Wi-Fi first, then pair again.")
	}
	if err := saveWebListen("lan"); err != nil {
		recordError("[web] web_listen not saved: " + err.Error())
		return m.webToast(toast.Errr, "Could not save the setting in config.json.")
	}
	st := m.webPanel
	if st == nil {
		return m.startWebPanelForPair()
	}
	m.webAccess().restartLAN = true
	if !st.stopping {
		m.stopWebPanelQuiet()
	}
	return m.webToast(toast.Info, "Moving the web panel to this Wi-Fi…")
}

// startWebPanelForPair starts the panel and shows the QR code once it
// is up (onWebPanelUp).
func (m *Model) startWebPanelForPair() tea.Cmd {
	cmd := m.startWebPanel(false, false)
	if m.webPanel != nil {
		m.webPanel.pairOnUp = true
	}
	return cmd
}

// saveWebListen writes "web_listen" into the user's config.json, the
// same way the settings screen writes a setting (other keys kept).
func saveWebListen(v string) error {
	path, err := settingsConfigPath()
	if err != nil {
		return err
	}
	return cfgfile.Update(path, func(merged map[string]any) error {
		merged["web_listen"] = v
		return nil
	})
}

// webPairAddr is the address shown under the QR code: the panel's
// origin without https:// and the last slash (192.168.1.20:47443).
func webPairAddr(url string) string {
	return strings.TrimSuffix(strings.TrimPrefix(url, "https://"), "/")
}
