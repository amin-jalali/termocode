package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"rsc.io/qr"

	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Pair a Device… and Devices… in the web panel (desktop web), with no
// step on the laptop. A controller page opens the pairing link
// (web.pair_open) through the laptop's own path (webPairNewLink: the
// same secret, digest and time); the link and its QR code go to that
// page alone, as the op's answer. When the phone asks, that page gets
// the pair.ask ev and the laptop its card as before (also when another
// card is open there: the page can answer); the first answer counts and
// the other closes (pair.end to the page, the card closed on the
// laptop). A page that leaves closes its link. Devices… lists the
// paired devices; Remove is a laptop-card op (the page asks, Cancel
// first) that removes the device as the laptop's card does.

func init() {
	webOps[webproto.OpWebPairOpen] = (*Model).webPairOpenOp
	webOps[webproto.OpWebPairClose] = (*Model).webPairCloseOp
	webOps[webproto.OpWebPairAnswer] = (*Model).webPairAnswerOp
	webOps[webproto.OpWebDevices] = (*Model).webDevicesOp
	webAskOps[webproto.OpWebDeviceRemove] = (*Model).webDeviceRemoveAsk
}

// webPalettePageItems are the laptop palette's access commands the page
// opens itself (PaletteItem.Page), for a controller.
func webPalettePageItems() []webproto.PaletteItem {
	var out []webproto.PaletteItem
	for _, it := range webAccessPaletteItems() {
		if it.ID == "web-pair" || it.ID == "web-devices" {
			out = append(out, webproto.PaletteItem{ID: it.ID, Title: it.Title, Page: true})
		}
	}
	return out
}

// webPairPageLive is the page whose pairing link is open, if it is still
// connected.
func (m *Model) webPairPageLive() *webConn {
	p := m.webAccess().page
	if p == nil || m.web == nil || !m.web.live[p] || p.ctx.Err() != nil {
		return nil
	}
	return p
}

// webPairPageSend sends the page whose link is open one ev.
func webPairPageSend(c *webConn, kind string, v any) {
	if c == nil {
		return
	}
	data, err := webData(v)
	if err != nil {
		recordError("[web] " + kind + ": " + err.Error())
		return
	}
	c.send(&webproto.Event{T: webproto.KindEv, Kind: kind, Data: data})
}

// webPairCloseLink closes the open pairing link at the gateway.
func (m *Model) webPairCloseLink() {
	if m.web == nil {
		return
	}
	for _, c := range m.web.ctlConns() {
		c.send(&webproto.Event{T: webproto.KindEv, Kind: "pair_close"})
	}
}

// webPairOpenOp opens a pairing link for this page.
func (m *Model) webPairOpenOp(call *webCall) tea.Cmd {
	st := m.webPanel
	acc := m.webAccess()
	switch {
	case st == nil || !st.up:
		call.fail(webproto.CodeGone, "The web panel is not running.")
		return nil
	case st.url == "":
		call.fail(webproto.CodeForbidden, `A phone cannot reach this computer yet. Set "web_listen" to "lan" (same Wi-Fi) or "tailnet" in config.json, then start the panel again.`)
		return nil
	case acc.pairCall != nil:
		call.fail(webproto.CodeBusy, "A device is pairing now. Answer it first.")
		return nil
	}
	if m.confirmOpen && m.confirmKind == confirmKindWebPairQR {
		m.confirmOpen = false // the laptop's own link closes: this one replaces it
	}
	link, err := m.webPairNewLink(st, false, call.conn)
	if err != nil {
		call.fail(webproto.CodeInternal, "No pairing link: no random numbers.")
		return nil
	}
	acc.link, acc.guest = "", false
	acc.page = call.conn
	path, size := webQRPath(link)
	call.reply(webproto.PairLink{Link: link, QR: path, Size: size, TTL: int(webPairTTL / time.Second), Addr: webPairAddr(st.url)})
	return nil
}

// webPairCloseOp closes this page's link (its dialog closed, or the link
// ran out). A question that waits for this page is Not now, unless the
// laptop's card shows it.
func (m *Model) webPairCloseOp(call *webCall) tea.Cmd {
	acc := m.webAccess()
	call.reply(nil)
	if acc.page != call.conn {
		return nil
	}
	return m.webPairPageLeft()
}

// webPairPageLeft ends the page's part in its pairing: an unused link
// closes; a question the laptop does not show is Not now.
func (m *Model) webPairPageLeft() tea.Cmd {
	acc := m.webAccess()
	acc.page = nil
	if acc.pairCall == nil {
		m.webPairCloseLink()
		return nil
	}
	if !(m.confirmOpen && m.confirmKind == confirmKindWebPairAsk) {
		_, _ = m.webPairDecide("", "")
	}
	return nil
}

// webPairGone: a page that leaves ends its part in its pairing.
func (m *Model) webPairGone(h *webHub, c *webConn) {
	if m.web == h && m.webAcc != nil && m.webAcc.page == c {
		m.webPairPageLeft()
	}
}

// webPairAnswerOp is the page's answer to the pairing question.
func (m *Model) webPairAnswerOp(call *webCall) tea.Cmd {
	acc := m.webAccess()
	if acc.page != call.conn || acc.pairCall == nil {
		call.fail(webproto.CodeGone, "That pairing ended. Make a new link.")
		return nil
	}
	if !m.web.askHere(call.conn) {
		call.fail(webproto.CodeForbidden, "Answer on the laptop: web_approve_on_laptop is on.")
		return nil
	}
	role := call.args.(*webproto.PairAnswerArgs).Role
	by := webName(call.conn.device)
	if by == "" {
		by = "web"
	}
	cmd, msg := m.webPairDecide(role, by)
	if msg != "" {
		call.fail(webproto.CodeGone, msg)
		return cmd
	}
	call.reply(nil)
	return cmd
}

// webPairDecide answers the waiting pairing question with role ("" is
// Not now). by is "laptop" for the laptop's card, the page's device
// name for the page, "" when the page left. The other side closes: the
// laptop's card, or the page's question (pair.end). msg says why the
// answer came too late.
func (m *Model) webPairDecide(role, by string) (cmd tea.Cmd, msg string) {
	acc := m.webAccess()
	call, args := acc.pairCall, acc.pairArgs
	page := m.webPairPageLive()
	acc.pairCall, acc.page = nil, nil
	if call == nil {
		return nil, ""
	}
	if by != "laptop" && m.confirmOpen && m.confirmKind == confirmKindWebPairAsk {
		m.confirmOpen = false // answered in the page: the laptop's card closes
	}
	if role == webdevices.RoleControl && args.Guest {
		role = ""
	}
	// Edit rights (G-COL) are for a share, and only the laptop's card
	// gives them.
	if role == webdevices.RoleEdit && (!args.Guest || by != "laptop") {
		role = ""
	}
	if role != webdevices.RoleViewer && role != webdevices.RoleControl && role != webdevices.RoleEdit {
		role = ""
	}
	if by == "laptop" {
		webPairPageSend(page, webproto.EventPairEnd, webproto.PairEnd{Why: "answered", Role: role})
	}
	name := webdevices.CleanName(args.Name)
	if role == "" {
		where := "on the laptop"
		if by != "laptop" {
			where = "in the web page"
		}
		call.fail(webproto.CodeForbidden, "Not paired: the answer "+where+" was Not now.")
		return nil, ""
	}
	if call.ctx.Err() != nil {
		return m.webToast(toast.Warn, "That pairing ended before you answered. Make a new link."), "That pairing ended before you answered. Make a new link."
	}
	call.reply(map[string]string{"role": role})
	if by != "laptop" {
		var t tea.Cmd
		m.toast, t = m.toast.PushDetail(toast.Info, "Approved on "+by, name+" may pair now ("+role+").")
		return t, ""
	}
	return m.webToast(toast.Info, name+" may pair now. The device finishes it."), ""
}

// webQRPath draws text as a QR code for the page: the SVG path of its
// dark modules, in module units, and the modules a side (0: no code).
func webQRPath(text string) (string, int) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return "", 0
	}
	var b strings.Builder
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; {
			if !code.Black(x, y) {
				x++
				continue
			}
			n := 1
			for x+n < code.Size && code.Black(x+n, y) {
				n++
			}
			fmt.Fprintf(&b, "M%d %dh%dv1h-%dz", x, y, n, n)
			x += n
		}
	}
	return b.String(), code.Size
}

// webDevicesOp lists the paired devices, as Web: Devices… does.
func (m *Model) webDevicesOp(call *webCall) tea.Cmd {
	dir, err := webDevicesDir()
	if err != nil {
		call.fail(webproto.CodeInternal, "No config folder.")
		return nil
	}
	ds, err := webdevices.Open(dir).List()
	if err != nil {
		call.fail(webproto.CodeInternal, "Could not read the device list.")
		return nil
	}
	live := m.webLiveDevices()
	out := webproto.WebDeviceList{Items: []webproto.WebDevice{}}
	for _, d := range ds {
		w := webproto.WebDevice{ID: d.ID, Name: webdevices.CleanName(d.Name), Role: d.Role, Guest: d.Guest, Bookmark: d.Bookmark != nil,
			Live: live[d.ID], Self: call.conn.device == d.ID, Repair: d.NeedsRepair, Created: d.Created.Unix()}
		if !d.LastSeen.IsZero() {
			w.LastSeen = d.LastSeen.Unix()
		}
		out.Items = append(out.Items, w)
	}
	call.reply(out)
	return nil
}

// webDeviceRemoveAsk asks before a device is removed, with the laptop
// card's text.
func (m *Model) webDeviceRemoveAsk(call *webCall) *webAsk {
	id := call.args.(*webproto.DeviceArgs).ID
	dir, _ := webDevicesDir()
	d, ok, _ := webdevices.Open(dir).Get(id)
	if !ok {
		call.fail(webproto.CodeNotFound, "That device is not paired any more.")
		return nil
	}
	name := webdevices.CleanName(d.Name)
	return &webAsk{
		summary: name,
		title:   "Remove " + name + "?",
		body:    webDeviceRemoveText,
		action:  "Remove",
		run:     func(m *Model) tea.Cmd { return m.webRemoveDevice(id) },
	}
}
