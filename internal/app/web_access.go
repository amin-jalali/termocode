package app

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"rsc.io/qr"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Who may use the web panel from another device (WEB-32): pairing a
// phone with a QR code and a 6-digit code, the list of paired devices,
// and removing one. Only the user, on the laptop, does any of it: every
// id is aiNever, every answer comes from a card's own button. The secret
// of a pairing link is made here and only its digest goes to the
// gateway; the link itself is only in the QR code and, when asked, the
// clipboard (never in a register or a file).

const (
	confirmKindWebPairQR   confirmKindEnum = 2601 // the QR code of a pairing link
	confirmKindWebPairAsk  confirmKindEnum = 2602 // pair this device? (SAS code)
	confirmKindWebDevice   confirmKindEnum = 2603 // one paired device
	confirmKindWebDeviceRm confirmKindEnum = 2604 // remove it?
	confirmKindWebGrant    confirmKindEnum = 2605 // give a viewer control? (WEB-34)
	pickerKindWebDevices   pickerKindEnum  = 2600
)

// webPairTTL is how long a pairing link works (webgw.PairTTL); a share
// link (sent by message to another person) gets webShareTTL
// (webgw.ShareTTL).
const (
	webPairTTL  = 120 * time.Second
	webShareTTL = 10 * time.Minute
)

// webAccessState is the open pairing link, the waiting pairing question
// and the device a card is about.
type webAccessState struct {
	link      string // the open pairing link (for Copy link), "" when none
	guest     bool   // the open link is a share (WEB-50)
	pairCall  *webCall
	pairArgs  webproto.PairAskArgs
	grantCall *webCall
	device    string
}

func (m *Model) webAccess() *webAccessState {
	if m.webAcc == nil {
		m.webAcc = &webAccessState{}
	}
	return m.webAcc
}

func webAccessPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "web-pair", Title: "Web: Pair a Device…"},
		{ID: "web-devices", Title: "Web: Devices…"},
		{ID: "web-share", Title: "Web: Share as Viewer…"},
		{ID: "web-totp", Title: "Web: Set Up Sign-in Code…"},
	}
}

// dispatchWebAccessPalette runs the access palette ids.
func (m *Model) dispatchWebAccessPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "web-pair":
		return m.openWebPair(false), true
	case "web-devices":
		return m.openWebDevices(), true
	case "web-share":
		return m.openWebPair(true), true
	case "web-totp":
		return m.openWebTotpSetup(), true
	}
	return nil, false
}

// openWebPair opens a pairing link and shows its QR code. guest is a
// share with another person (WEB-50): viewer only, more time.
func (m *Model) openWebPair(guest bool) tea.Cmd {
	if !webSettings().On {
		return m.webToast(toast.Warn, webOffText)
	}
	st := m.webPanel
	if st == nil || !st.up || m.web == nil {
		return m.webToast(toast.Warn, "Start the web panel first: Web: Start Panel.")
	}
	if st.url == "" {
		return m.webToast(toast.Warn, `Pairing a device needs tailnet mode. Set "web_listen": "tailnet" in config.json, then start the panel again.`)
	}
	if m.confirmOpen {
		return m.webToast(toast.Warn, "Answer the open card first.")
	}
	plain, digest, err := webNewLinkSecret()
	if err != nil {
		return m.webToast(toast.Errr, "No pairing link: no random numbers")
	}
	ttl := webPairTTL
	if guest {
		ttl = webShareTTL
	}
	data, _ := json.Marshal(map[string]any{"sha256": hex.EncodeToString(digest[:]), "ttl_s": int(ttl / time.Second), "guest": guest})
	for _, c := range m.web.ctlConns() {
		c.send(&webproto.Event{T: webproto.KindEv, Kind: "pair", Data: data})
	}
	acc := m.webAccess()
	acc.link, acc.guest = st.url+"#p="+plain, guest
	title := "Pair a phone or tablet"
	body := "Scan this code with the phone's camera. It works once, for 2 minutes.\n" +
		"The phone then shows a 6-digit code; you check it here."
	if guest {
		title = "Share as a viewer"
		body = "Send this link to the person you share with (Copy link). It works once, for 10 minutes.\n" +
			"They can only watch: no typing, no answers, no changes.\n" +
			"They need this computer shared to them on Tailscale. When they open the link,\n" +
			"they read you a 6-digit code; you check it here and let them in."
	}
	m.confirm = confirm.New(title, body, []confirm.Button{
		{ID: "close", Title: "Close"},
		{ID: "copy", Title: "Copy link"},
	}).WithPicture(webQRLines(acc.link))
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWebPairQR
	return nil
}

// webQRLines draws text as a QR code with half blocks for a card's
// picture (white on black): light modules are drawn, dark ones are the
// black background, with a light quiet zone of 2 modules around it.
func webQRLines(text string) []string {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return []string{"(no QR code: the link is too long; use Copy link)"}
	}
	const quiet = 2
	n := code.Size + 2*quiet
	light := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		if x < 0 || y < 0 || x >= code.Size || y >= code.Size {
			return true
		}
		return !code.Black(x, y)
	}
	var out []string
	for y := 0; y < n; y += 2 {
		var b strings.Builder
		for x := 0; x < n; x++ {
			top, bot := light(x, y), y+1 < n && light(x, y+1)
			switch {
			case top && bot:
				b.WriteRune('█')
			case top:
				b.WriteRune('▀')
			case bot:
				b.WriteRune('▄')
			default:
				b.WriteRune(' ')
			}
		}
		out = append(out, b.String())
	}
	return out
}

// answerWebPairQR acts on the QR card: Copy link, or Close (Esc too),
// which closes the link at the gateway.
func (m *Model) answerWebPairQR(id string) tea.Cmd {
	acc := m.webAccess()
	if id == "copy" && acc.link != "" {
		link := acc.link
		// Keep the card: the user may still scan it.
		m.confirmOpen = true
		if writeSystemClipboard(link) == clipFailed {
			return m.webToast(toast.Warn, "Could not copy the link. Scan the code instead.")
		}
		if acc.guest {
			return m.webToast(toast.Info, "Link copied. It works once, for 10 minutes.")
		}
		return m.webToast(toast.Info, "Link copied. It works once, for 2 minutes.")
	}
	acc.link = ""
	if m.web != nil {
		for _, c := range m.web.ctlConns() {
			c.send(&webproto.Event{T: webproto.KindEv, Kind: "pair_close"})
		}
	}
	return nil
}

// The gateway's pairing question (a control op): a Risky card.
func init() {
	webOps[webproto.OpWebPairAsk] = func(m *Model, call *webCall) tea.Cmd { return m.webPairAsk(call) }
}

// webPairAsk shows "Pair this device?" with the phone's code. It refuses
// (busy) while another card is open, so no key meant for that card can
// answer this one.
func (m *Model) webPairAsk(call *webCall) tea.Cmd {
	args, _ := call.args.(*webproto.PairAskArgs)
	acc := m.webAccess()
	if args == nil || acc.pairCall != nil || (m.confirmOpen && m.confirmKind != confirmKindWebPairQR) {
		call.fail(webproto.CodeBusy, "The laptop is busy with another question. Try again in a moment.")
		return nil
	}
	acc.link = "" // the link is used now
	acc.pairCall, acc.pairArgs = call, *args
	call.later()
	name := webdevices.CleanName(args.Name)
	code := args.Code[:3] + " " + args.Code[3:]
	var body strings.Builder
	actions := []confirm.Button{}
	title := "Pair this device?"
	if args.Guest {
		title = "Let someone watch?"
		fmt.Fprintf(&body, "%s opened your share link.\n\n", name)
		fmt.Fprintf(&body, "Code on their screen: %s\nLet them in only if they tell you the same code.\n\n", code)
		body.WriteString("They will see your files, chats and terminals, but cannot type or change anything.")
		actions = append(actions, confirm.Button{ID: "viewer", Title: "Let them watch", Style: confirm.StylePrimary})
	} else {
		fmt.Fprintf(&body, "%s wants to pair with this termocode.\n\n", name)
		fmt.Fprintf(&body, "Code on the phone: %s\nPair only if the phone shows the same code.\n\n", code)
		body.WriteString("A viewer sees your files, chats and terminals.\nWith control it can also type and run any command as you.")
		actions = append(actions,
			confirm.Button{ID: "viewer", Title: "Pair as viewer", Style: confirm.StylePrimary},
			confirm.Button{ID: "control", Title: "Pair with control"})
	}
	m.confirm = confirm.Risky(title, body.String(), confirm.Button{ID: "not-now", Title: "Not now"}, actions...)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWebPairAsk
	return nil
}

// answerWebPairAsk sends the owner's pick to the gateway; anything but a
// role (Esc included) is Not now.
func (m *Model) answerWebPairAsk(id string) tea.Cmd {
	acc := m.webAccess()
	call, args := acc.pairCall, acc.pairArgs
	acc.pairCall = nil
	if call == nil {
		return nil
	}
	role := ""
	switch id {
	case "viewer":
		role = webdevices.RoleViewer
	case "control":
		if !args.Guest {
			role = webdevices.RoleControl
		}
	}
	if role == "" {
		call.fail(webproto.CodeForbidden, "Not paired: the answer on the laptop was Not now.")
		return nil
	}
	if call.ctx.Err() != nil {
		return m.webToast(toast.Warn, "That pairing ended before you answered. Make a new link.")
	}
	call.reply(map[string]string{"role": role})
	return m.webToast(toast.Info, webdevices.CleanName(args.Name)+" may pair now. The device finishes it.")
}

// openWebDevices lists the paired devices.
func (m *Model) openWebDevices() tea.Cmd {
	dir, err := webDevicesDir()
	if err != nil {
		return m.webToast(toast.Errr, "No config folder")
	}
	ds, err := webdevices.Open(dir).List()
	if err != nil {
		return m.webToast(toast.Errr, "Could not read the device list")
	}
	if len(ds) == 0 {
		return m.webToast(toast.Info, "No paired devices. Use Web: Pair a Device… to add one.")
	}
	live := m.webLiveDevices()
	items := make([]picker.Item, 0, len(ds))
	for _, d := range ds {
		items = append(items, picker.Item{ID: d.ID, Title: d.Name, Hint: webDeviceHint(d, live[d.ID], time.Now())})
	}
	m.picker = picker.NewItems(" Web devices ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWebDevices
	return nil
}

// webLiveDevices are the devices with a browser connected now.
func (m *Model) webLiveDevices() map[string]bool {
	out := map[string]bool{}
	if m.web == nil {
		return out
	}
	for c := range m.web.live {
		out[c.device] = true
	}
	return out
}

// webDeviceHint is a device's row hint: role, connection, last seen.
func webDeviceHint(d webdevices.Device, live bool, now time.Time) string {
	parts := []string{d.Role}
	if d.Guest {
		parts = append(parts, "guest")
	}
	switch {
	case d.NeedsRepair:
		parts = append(parts, "must pair again")
	case live:
		parts = append(parts, "connected")
	case !d.LastSeen.IsZero():
		parts = append(parts, "last seen "+webAgo(now.Sub(d.LastSeen)))
	}
	return strings.Join(parts, " · ")
}

// webAgo says how long ago, in rough words.
func webAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	}
	return fmt.Sprintf("%d days ago", int(d/(24*time.Hour)))
}

// handleWebAccessPicker routes the device list. ok=false: not ours.
func (m *Model) handleWebAccessPicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindWebDevices {
		return nil, false
	}
	return m.openWebDeviceCard(id), true
}

// openWebDeviceCard shows one device: Close first, then Remove.
func (m *Model) openWebDeviceCard(id string) tea.Cmd {
	dir, _ := webDevicesDir()
	d, ok, _ := webdevices.Open(dir).Get(id)
	if !ok {
		return m.webToast(toast.Info, "That device is not paired any more.")
	}
	m.webAccess().device = id
	body := fmt.Sprintf("Role: %s\nPaired: %s\n%s", d.Role, d.Created.Local().Format("2 Jan 2006 15:04"),
		webDeviceHint(d, m.webLiveDevices()[id], time.Now()))
	buttons := []confirm.Button{{ID: "close", Title: "Close"}}
	if d.Role == webdevices.RoleControl {
		buttons = append(buttons, confirm.Button{ID: "viewer", Title: "Make viewer"})
	} else if !d.Guest {
		body += "\nTo give it control, tap Ask for control on that device."
	}
	buttons = append(buttons, confirm.Button{ID: "remove", Title: "Remove device", Style: confirm.StyleDestructive})
	m.confirm = confirm.New(d.Name, body, buttons)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWebDevice
	return nil
}

// answerWebDevice acts on the device card.
func (m *Model) answerWebDevice(id string) tea.Cmd {
	acc := m.webAccess()
	if id == "viewer" {
		return m.webMakeViewer(acc.device)
	}
	if id != "remove" {
		acc.device = ""
		return nil
	}
	dir, _ := webDevicesDir()
	d, ok, _ := webdevices.Open(dir).Get(acc.device)
	if !ok {
		return nil
	}
	m.confirm = confirm.Risky("Remove "+d.Name+"?", "It is disconnected at once and must pair again to come back.",
		confirm.Button{ID: "keep", Title: "Keep"},
		confirm.Button{ID: "remove", Title: "Remove", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWebDeviceRm
	return nil
}

// answerWebDeviceRemove removes the device and tells the gateway to drop
// it now (4003); Keep and Esc keep it.
func (m *Model) answerWebDeviceRemove(id string) tea.Cmd {
	acc := m.webAccess()
	dev := acc.device
	acc.device = ""
	if id != "remove" || dev == "" {
		return nil
	}
	dir, _ := webDevicesDir()
	d, ok, err := webdevices.Open(dir).Remove(dev)
	if err != nil {
		return m.webToast(toast.Errr, "Could not remove the device")
	}
	if !ok {
		return nil
	}
	m.webRevokeDevice(dev)
	return m.webToast(toast.Info, "Removed "+d.Name+". It was disconnected.")
}

// webRevokeDevice tells every gateway to drop a device's tokens and links.
func (m *Model) webRevokeDevice(dev string) {
	if m.web == nil {
		return
	}
	data, _ := json.Marshal(map[string]string{"device": dev})
	for _, c := range m.web.ctlConns() {
		c.send(&webproto.Event{T: webproto.KindEv, Kind: "revoke", Data: data})
	}
}

// webMakeViewer takes control back from a device (no card: it is the
// safe direction). The gateway closes its links with 4013 and they come
// back as a viewer.
func (m *Model) webMakeViewer(dev string) tea.Cmd {
	m.webAccess().device = ""
	dir, _ := webDevicesDir()
	var name string
	ok, err := webdevices.Open(dir).Change(dev, func(d *webdevices.Device) { d.Role, name = webdevices.RoleViewer, d.Name })
	if err != nil || !ok {
		return m.webToast(toast.Warn, "That device is not paired any more.")
	}
	if m.web != nil {
		data, _ := json.Marshal(map[string]string{"device": dev, "role": webdevices.RoleViewer})
		for _, c := range m.web.ctlConns() {
			c.send(&webproto.Event{T: webproto.KindEv, Kind: "role", Data: data})
		}
	}
	return m.webToast(toast.Info, name+" can only watch now.")
}

// The gateway's control question (a control op): a Risky card.
func init() {
	webOps[webproto.OpWebGrantAsk] = func(m *Model, call *webCall) tea.Cmd { return m.webGrantAsk(call) }
}

// webGrantAsk shows "Give control?" for a viewer that confirmed with its
// passkey; busy while another card is open.
func (m *Model) webGrantAsk(call *webCall) tea.Cmd {
	args, _ := call.args.(*webproto.GrantAskArgs)
	acc := m.webAccess()
	if args == nil || acc.grantCall != nil || m.confirmOpen {
		call.fail(webproto.CodeBusy, "The laptop is busy with another question. Try again in a moment.")
		return nil
	}
	acc.grantCall = call
	call.later()
	name := webdevices.CleanName(args.Name)
	body := name + " asks for control. It confirmed with its passkey just now.\n\n" +
		"With control it can type and run any command as you.\nYou can take it back any time in Web: Devices…"
	m.confirm = confirm.Risky("Give control to "+name+"?", body,
		confirm.Button{ID: "not-now", Title: "Not now"},
		confirm.Button{ID: "give", Title: "Give control", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWebGrant
	return nil
}

// answerWebGrant sends the owner's answer; anything but give is Not now.
func (m *Model) answerWebGrant(id string) tea.Cmd {
	acc := m.webAccess()
	call := acc.grantCall
	acc.grantCall = nil
	if call == nil {
		return nil
	}
	if id != "give" {
		call.fail(webproto.CodeForbidden, "Not given: the answer on the laptop was Not now.")
		return nil
	}
	if call.ctx.Err() != nil {
		return m.webToast(toast.Warn, "That request ended before you answered.")
	}
	call.reply(nil)
	return nil
}

// handleWebAccessConfirm routes the access cards. ok=false: not ours.
func (m *Model) handleWebAccessConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindWebPairQR:
		return m.answerWebPairQR(id), true
	case confirmKindWebPairAsk:
		return m.answerWebPairAsk(id), true
	case confirmKindWebDevice:
		return m.answerWebDevice(id), true
	case confirmKindWebDeviceRm:
		return m.answerWebDeviceRemove(id), true
	case confirmKindWebGrant:
		return m.answerWebGrant(id), true
	}
	return m.handleWebTotpConfirm(id)
}
