package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Port preview (WEB-54): see a dev server of this computer (a port on
// its loopback) in the browser of the web panel. It is the panel's
// biggest door after the shell, so:
//   - only the user asks, here on the laptop (Web: Forward Port…); the
//     AI, MCP and project files never can (aiNever on both ids);
//   - the only input is a port: the target host is always 127.0.0.1
//     here, there is no host field;
//   - a Risky card asks first, Not now first and focused;
//   - the app gets its own origin (its own port), never a path under the
//     panel, so its pages can not open the panel's /ws or read its
//     storage;
//   - every forward ends with the panel, and is audited (fwd_open,
//     fwd_close) in the gateway's log.
//
// Per panel mode:
//   - SSH mode (termocode ssh --web): an in-band request to the local
//     termocode ssh (session.PortForwardAPC), which runs ssh -L from a
//     free 127.0.0.1 port there and opens the browser there.
//   - Tailnet mode: tailscale serve on an https port of its own, only
//     while the panel runs; never Funnel.
//   - Local mode (127.0.0.1 panel): nothing to forward; the browser is on
//     this computer and opens http://127.0.0.1:<port>/ after the card.

const (
	confirmKindWebForward  confirmKindEnum = 2620 // open this port in the browser?
	pickerKindWebForwards  pickerKindEnum  = 2621 // the open forwards (Enter stops one)
	promptKindWebForwardTo promptKindEnum  = 2622 // which port?
)

// maxWebForwards bounds the open forwards (termocode ssh has its own cap).
const maxWebForwards = 8

// webForwardHTTPSPorts are the tailnet https ports of port previews.
var webForwardHTTPSPorts = []int{8443, 8444, 8445, 8446, 8447, 8448, 8449, 8450}

// webFwdServe is a running tailscale serve of a port preview.
type webFwdServe interface{ Stop() }

// webForwardServe starts tailscale serve on httpsPort for target (nil
// where Tailscale serve can not run). Tests swap it.
var webForwardServe func(httpsPort int, target string) (webFwdServe, error)

// webForwardWrite sends an in-band request to termocode ssh (xferWrite:
// one whole write to the terminal). Tests swap it.
var webForwardWrite = xferWrite

// webForwardOpenURL opens the browser on this computer. Tests swap it.
var webForwardOpenURL = func(u string) error { return osOpen(u) }

// webFwd is one open port preview.
type webFwd struct {
	id    string
	port  int
	mode  string // "ssh" or "tailnet"
	url   string // tailnet: the browser's address; ssh: "" (opened there)
	since time.Time
	serve webFwdServe
}

func webForwardPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "web-forward-port", Title: "Web: Forward Port…"},
		{ID: "web-forwarded-ports", Title: "Web: Forwarded Ports…"},
	}
}

// dispatchWebForwardPalette runs the port preview ids.
func (m *Model) dispatchWebForwardPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "web-forward-port":
		return m.openWebForwardPrompt(), true
	case "web-forwarded-ports":
		return m.openWebForwards(), true
	}
	return nil, false
}

// webForwardReady says why no forward can start now ("" when it can).
func (m *Model) webForwardReady() string {
	if !webSettings().On {
		return webOffText
	}
	st := m.webPanel
	if st == nil || !st.up || m.web == nil {
		return "Start the web panel first: Web: Open in Browser."
	}
	if m.confirmOpen {
		return "Answer the open card first."
	}
	return ""
}

// openWebForwardPrompt asks for the port (nothing else: no host).
func (m *Model) openWebForwardPrompt() tea.Cmd {
	if why := m.webForwardReady(); why != "" {
		return m.webToast(toast.Warn, why)
	}
	m.prompt = prompt.New(" Forward a port ", "Port on this computer (1–65535):", "")
	m.prompt.SetNote("Only 127.0.0.1 of this computer. A card asks before anything opens.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindWebForwardTo
	return nil
}

// parseWebForwardPort reads the prompt: a port number and nothing else
// (a host, a colon, a URL are refused: the target is always loopback).
func parseWebForwardPort(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("type a port number, for example 3000")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("only a port number: the target is always 127.0.0.1 here")
		}
	}
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("a port is a number from 1 to 65535")
	}
	return p, nil
}

// onWebForwardPort shows the Risky card for the typed port.
func (m *Model) onWebForwardPort(value string) tea.Cmd {
	port, err := parseWebForwardPort(value)
	if err != nil {
		return m.webToast(toast.Warn, "No forward: "+err.Error()+".")
	}
	if why := m.webForwardReady(); why != "" {
		return m.webToast(toast.Warn, why)
	}
	st := m.webPanel
	if port == st.port {
		return m.webToast(toast.Warn, "That is the web panel's own port.")
	}
	acc := m.webAccess()
	for _, f := range acc.fwds {
		if f.port == port {
			return m.webToast(toast.Info, "Port "+strconv.Itoa(port)+" is forwarded already. See Web: Forwarded Ports….")
		}
	}
	if len(acc.fwds) >= maxWebForwards {
		return m.webToast(toast.Warn, "Too many forwards. Stop one in Web: Forwarded Ports….")
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "this computer"
	}
	body := "The app on port " + strconv.Itoa(port) + " can see what you type into it.\n" +
		"It opens on its own address, not the web panel's, and only while the panel runs.\n"
	switch {
	case st.sshPort != 0:
		body += "termocode ssh on your computer forwards it over SSH (127.0.0.1 on both ends).\n"
	case st.url != "":
		body += "Your devices on your tailnet can reach it (tailscale serve), not the internet.\n"
	default:
		body += "The panel runs on this computer: the browser opens 127.0.0.1:" + strconv.Itoa(port) + ".\n"
	}
	body += "Stop it any time in Web: Forwarded Ports…"
	acc.fwdPort = port
	m.confirm = confirm.Risky("Open port "+strconv.Itoa(port)+" of "+host+" in your browser?", body,
		confirm.Button{ID: "not-now", Title: "Not now"},
		confirm.Button{ID: "open", Title: "Open port", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWebForward
	return nil
}

// answerWebForward opens the forward on "open"; anything else (Esc
// included) is Not now.
func (m *Model) answerWebForward(id string) tea.Cmd {
	acc := m.webAccess()
	port := acc.fwdPort
	acc.fwdPort = 0
	if id != "open" || port == 0 {
		return nil
	}
	st := m.webPanel
	if st == nil || !st.up || m.web == nil {
		return m.webToast(toast.Warn, "The web panel stopped before you answered.")
	}
	ps := strconv.Itoa(port)
	switch {
	case st.sshPort != 0:
		f := &webFwd{id: randomID(), port: port, mode: "ssh", since: time.Now()}
		apc := session.PortForwardAPC(session.PortForward{Open: true, Port: port, ID: f.id})
		if apc == "" || webForwardWrite(apc) != nil {
			return m.webToast(toast.Errr, "Could not ask termocode ssh for the forward.")
		}
		acc.fwds = append(acc.fwds, f)
		m.webAuditForward("fwd_open", "confirmed_laptop", f)
		return m.webToast(toast.Info, "Port "+ps+" opens in the browser on your computer.")
	case st.url != "":
		if webForwardServe == nil {
			return m.webToast(toast.Warn, "Tailscale serve can not run here.")
		}
		https := m.webForwardFreeHTTPS()
		if https == 0 {
			return m.webToast(toast.Warn, "Too many forwards. Stop one in Web: Forwarded Ports….")
		}
		sv, err := webForwardServe(https, "http://127.0.0.1:"+ps)
		if err != nil {
			return m.webToast(toast.Errr, "No forward: "+err.Error())
		}
		f := &webFwd{id: randomID(), port: port, mode: "tailnet", since: time.Now(), serve: sv,
			url: webForwardTailnetURL(st.url, https)}
		acc.fwds = append(acc.fwds, f)
		m.webAuditForward("fwd_open", "confirmed_laptop", f)
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "Port "+ps+" is on your tailnet", f.url)
		return c
	}
	// Local mode: nothing to forward, the browser is here.
	u := "http://127.0.0.1:" + ps + "/"
	if err := webForwardOpenURL(u); err != nil {
		return m.webToast(toast.Warn, "Could not open a browser. Open "+u+" by hand.")
	}
	return nil
}

// webForwardFreeHTTPS is a tailnet https port no forward uses (0: none).
func (m *Model) webForwardFreeHTTPS() int {
	used := map[string]bool{}
	for _, f := range m.webAccess().fwds {
		used[f.url] = true
	}
	st := m.webPanel
	for _, p := range webForwardHTTPSPorts {
		if !used[webForwardTailnetURL(st.url, p)] {
			return p
		}
	}
	return 0
}

// webForwardTailnetURL is the panel's tailnet address on https port p.
func webForwardTailnetURL(panel string, p int) string {
	host := strings.TrimSuffix(strings.TrimPrefix(panel, "https://"), "/")
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	return "https://" + host + ":" + strconv.Itoa(p) + "/"
}

// openWebForwards lists the open forwards; Enter stops one.
func (m *Model) openWebForwards() tea.Cmd {
	fwds := m.webAccess().fwds
	if len(fwds) == 0 {
		return m.webToast(toast.Info, "No forwarded ports. Use Web: Forward Port… to add one.")
	}
	items := make([]picker.Item, 0, len(fwds))
	for _, f := range fwds {
		hint := f.mode + " · since " + f.since.Format("15:04")
		if f.url != "" {
			hint += " · " + f.url
		}
		items = append(items, picker.Item{ID: f.id, Title: "Port " + strconv.Itoa(f.port) + " (Enter stops it)", Hint: hint})
	}
	m.picker = picker.NewItems(" Forwarded ports ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWebForwards
	return nil
}

// stopWebForward ends one forward (stopping is the safe way: no card).
func (m *Model) stopWebForward(id string) tea.Cmd {
	acc := m.webAccess()
	for i, f := range acc.fwds {
		if f.id != id {
			continue
		}
		acc.fwds = append(acc.fwds[:i:i], acc.fwds[i+1:]...)
		m.closeWebForward(f)
		return m.webToast(toast.Info, "Port "+strconv.Itoa(f.port)+" is not forwarded any more.")
	}
	return m.webToast(toast.Info, "That forward ended already.")
}

// closeWebForward ends f where it runs and audits it.
func (m *Model) closeWebForward(f *webFwd) {
	switch f.mode {
	case "ssh":
		if apc := session.PortForwardAPC(session.PortForward{ID: f.id}); apc != "" {
			_ = webForwardWrite(apc)
		}
	case "tailnet":
		if f.serve != nil {
			f.serve.Stop()
		}
	}
	m.webAuditForward("fwd_close", "stopped", f)
}

// closeWebForwards ends every forward: the panel stopped or ended.
func (m *Model) closeWebForwards() {
	if m.webAcc == nil {
		return
	}
	fwds := m.webAcc.fwds
	m.webAcc.fwds = nil
	for _, f := range fwds {
		m.closeWebForward(f)
	}
}

// webAuditForward writes op for f to the gateway's audit log.
func (m *Model) webAuditForward(op, why string, f *webFwd) {
	if m.web == nil {
		return
	}
	m.web.auditNote(webproto.AuditNote{Device: "laptop", Op: op, OK: true, Why: why,
		Summary: "port " + strconv.Itoa(f.port) + " (" + f.mode + ")"})
}
