package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The relay (web_relay, ADR 0022): the gateway also waits for paired
// phones on a blind relay. This side owns the relay identity: an X25519
// key pair and a 128-bit room id, made once and kept in the secret store
// (never in config.json, argv, env or a log). The private key reaches
// the gateway only in a relay_key event on its control connection (the
// 0600 hub socket). The pairing QR code carries the relay address, the
// room and the public key; the relay never sees any of it in plaintext
// after the phone loaded the page.

// webRelayInfo is what the pairing link needs: no private key.
type webRelayInfo struct {
	url  string // the relay's base address, no trailing slash
	room string
	pub  string // the gateway's public key, 43 base64url characters
}

// webRelayArm gives the running panel its relay identity: the gateway's
// control connections get the key, the panel state the link parts.
func (m *Model) webRelayArm(cfg webConfig) error {
	if cfg.Relay == "" || m.webPanel == nil || m.web == nil {
		return nil
	}
	id, err := webRelayIdentity()
	if err != nil {
		return fmt.Errorf("no relay key: %w", err)
	}
	data, _ := json.Marshal(webRelayStored{Key: base64.RawURLEncoding.EncodeToString(id.Key.Private[:]), Room: id.Room})
	m.web.setRelayKey(data)
	m.webPanel.relay = &webRelayInfo{url: cfg.Relay, room: id.Room, pub: id.Key.Public.String()}
	return nil
}

// webRelayStop is Web: Stop Relay (NEW-12): the gateway stops waiting
// on the relay and ends its relay sessions; the panel runs on at its
// own address, and pairing links are that address again. No card:
// stopping is the safe way. The panel's next start (web_relay in
// config.json) turns it on again.
func (m *Model) webRelayStop() tea.Cmd {
	if m.webPanel == nil || m.web == nil || m.webPanel.relay == nil {
		return m.webToast(toast.Info, "The relay is not on.")
	}
	host := webRelayHost(m.webPanel.relay)
	m.webPanel.relay = nil
	m.web.mu.Lock()
	m.web.relayKey = nil
	m.web.mu.Unlock()
	for _, c := range m.web.ctlConns() {
		c.send(&webproto.Event{T: webproto.KindEv, Kind: "relay_off"})
	}
	return m.webToast(toast.Info, "Relay stopped ("+host+"). Phones reach the panel only at its own address until it starts again.")
}

// setRelayKey keeps the relay_key event for control connections that
// come later and sends it to the ones there now.
func (h *webHub) setRelayKey(data []byte) {
	h.mu.Lock()
	h.relayKey = data
	h.mu.Unlock()
	for _, c := range h.ctlConns() {
		c.sendRelayKey(data)
	}
}

func (c *webConn) sendRelayKey(data []byte) {
	c.send(&webproto.Event{T: webproto.KindEv, Kind: "relay_key", Data: data})
}

// webRelayPairLink is the pairing link through the relay: the page the
// relay serves, with the room, the gateway key and the one-time secret
// in the fragment (never sent to the relay by the browser).
func webRelayPairLink(r *webRelayInfo, plain string) string {
	return r.url + "/#r=" + r.room + "&k=" + r.pub + "&p=" + plain
}

// webRelayHost names the relay for a card's caption.
func webRelayHost(r *webRelayInfo) string {
	if u, err := url.Parse(r.url); err == nil {
		return "via " + u.Host
	}
	return "via the relay"
}

// webPairCaption is the QR card's caption: where the phone goes.
func webPairCaption(st *webPanelState) string {
	if st.relay != nil {
		return webRelayHost(st.relay)
	}
	return webPairAddr(st.url)
}
