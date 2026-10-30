package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The web panel's life on the laptop (WEB-9). The hub (web_hub.go) runs
// while web_panel is "on"; the panel is the gateway process the UI
// starts on an explicit act of the user. The UI makes the launch secret
// and sends the gateway only its digest over the control connection, so
// the secret never enters the gateway, an argv, an env or a file other
// than the 0600 redirect page. Stopping is the kill switch: the hub's
// bye 4009 on every control connection (the gateway revokes every token
// and closes every browser), then SIGTERM after 1 s if it still runs.

// webLinkTTL is how long a launch link works (webgw.LaunchSecretTTL).
const webLinkTTL = 60 * time.Second

// webStopGrace: a gateway that ignores the kill switch this long gets
// SIGTERM.
var webStopGrace = time.Second

// webPanelState is the running or starting gateway.
type webPanelState struct {
	proc     *webGatewayProc
	port     int
	up       bool
	stopping bool
	plain    string // the newest launch secret, for the browser link
	open     bool   // open the link in a browser once up
	copy     bool   // copy the link once up
	waiters  []*webCall
}

// webGatewayProc is a started gateway process.
type webGatewayProc struct {
	terminate func()      // SIGTERM
	alive     func() bool // still running
}

// webGatewayStart starts the gateway with args and reports its port
// (webPanelUpMsg / webPanelFailMsg) and its end (webPanelExitMsg)
// through the hub. Tests swap it.
var webGatewayStart = startWebGatewayProcess

// webOpenPath opens the redirect page in the browser. Tests swap it.
var webOpenPath = func(path string) error { return osOpen(path) }

type webPanelUpMsg struct {
	hub  *webHub
	proc *webGatewayProc
	port int
}

type webPanelFailMsg struct {
	hub  *webHub
	proc *webGatewayProc
	err  error
}

type webPanelExitMsg struct {
	hub  *webHub
	proc *webGatewayProc
}

// handleWebPanelMsg routes the panel's messages; ok=false: not ours.
func (m *Model) handleWebPanelMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case webPanelUpMsg:
		return tea.Batch(m.onWebPanelUp(msg), waitWeb(msg.hub)), true
	case webPanelFailMsg:
		return tea.Batch(m.onWebPanelFail(msg), waitWeb(msg.hub)), true
	case webPanelExitMsg:
		return tea.Batch(m.onWebPanelExit(msg), waitWeb(msg.hub)), true
	}
	return nil, false
}

func (m *Model) webToast(level toast.Severity, title string) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.Push(level, title)
	return c
}

// webOffText is E11.
const webOffText = `The web panel is off. Turn it on in config.json: "web_panel": "on"`

// startWebPanel starts the gateway (open: open the link in a browser;
// copy: copy it). Only the user's own act calls it: the start card,
// or `termocode web --yes` from the same user.
func (m *Model) startWebPanel(open, copyLink bool) tea.Cmd {
	cfg := webSettings()
	if !cfg.On {
		return m.webToast(toast.Warn, webOffText)
	}
	var cmds []tea.Cmd
	if m.web == nil {
		cmds = append(cmds, m.startWebHub())
		if m.web == nil {
			return tea.Batch(cmds...)
		}
	}
	if st := m.webPanel; st != nil {
		if st.up {
			cmds = append(cmds, m.webNewLink(open, copyLink))
		}
		return tea.Batch(cmds...)
	}
	plain, err := m.web.newLink()
	if err != nil {
		return m.webToast(toast.Errr, "Web panel not started: no random numbers")
	}
	audit, err := webAuditPath()
	if err != nil {
		return m.webToast(toast.Errr, "Web panel not started: no config folder")
	}
	args := []string{"--dir", aiWorkspaceRoot(), "--audit", audit,
		"--idle-min", strconv.Itoa(cfg.IdleMinutes), "--max-hours", strconv.Itoa(cfg.MaxHours),
		"--viewers", strconv.Itoa(cfg.MaxViewers), "--port", strconv.Itoa(cfg.Port)}
	proc, err := webGatewayStart(m.web, args)
	if err != nil {
		recordError("[web] gateway not started: " + err.Error())
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Errr, "Web panel not started", ai.Redact(err.Error()))
		return c
	}
	m.webPanel = &webPanelState{proc: proc, plain: plain, open: open && cfg.OpenBrowser, copy: copyLink}
	for _, p := range cfg.Problems {
		cmds = append(cmds, m.webToast(toast.Warn, "config.json: "+p))
	}
	cmds = append(cmds, m.webToast(toast.Info, "Starting the web panel…"))
	return tea.Batch(cmds...)
}

// webAuditPath is the gateway's audit log, next to errors.log.
func webAuditPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "web-audit.jsonl"), nil
}

func (m *Model) onWebPanelUp(msg webPanelUpMsg) tea.Cmd {
	st := m.webPanel
	if st == nil || st.proc != msg.proc {
		return nil
	}
	st.up, st.port = true, msg.port
	for _, w := range st.waiters {
		w.reply(map[string]int{"port": st.port})
	}
	st.waiters = nil
	cmd := m.webDeliverLink(st.open, st.copy)
	st.open, st.copy = false, false
	return cmd
}

// webDeliverLink opens and/or copies the newest link; with neither it
// just says the panel is on.
func (m *Model) webDeliverLink(open, copyLink bool) tea.Cmd {
	st := m.webPanel
	link := fmt.Sprintf("http://127.0.0.1:%d/#s=%s", st.port, st.plain)
	st.plain = "" // used once here; a new link makes a new secret
	switch {
	case copyLink:
		m.writeClipboard(link)
		return m.webToast(toast.Info, "Link copied. It works once, for 60 seconds.")
	case open:
		if err := m.webOpenLink(link); err != nil {
			return m.webToast(toast.Warn, "Could not open a browser. Use Web: Copy Link.")
		}
		return m.webToast(toast.Info, "Web panel is on. The link opened in your browser.")
	}
	return m.webToast(toast.Info, "Web panel is on. Use Web: Copy Link to open it.")
}

// webOpenLink writes the 0600 redirect page and opens its path (never the
// link itself: argv is public); the page is removed when the link expires.
func (m *Model) webOpenLink(link string) error {
	path, err := webRedirectPath()
	if err != nil {
		return err
	}
	if err := webWriteRedirect(path, link); err != nil {
		return err
	}
	time.AfterFunc(webLinkTTL, func() { _ = os.Remove(path) })
	return webOpenPath(path)
}

// webNewLink makes a new one-time link for a running panel.
func (m *Model) webNewLink(open, copyLink bool) tea.Cmd {
	st := m.webPanel
	if st == nil || !st.up || m.web == nil {
		return m.webToast(toast.Warn, "The web panel is not running. Use Web: Start Panel.")
	}
	plain, err := m.web.newLink()
	if err != nil {
		return m.webToast(toast.Errr, "No new link: no random numbers")
	}
	st.plain = plain
	return m.webDeliverLink(open, copyLink)
}

func (m *Model) onWebPanelFail(msg webPanelFailMsg) tea.Cmd {
	st := m.webPanel
	if st == nil || st.proc != msg.proc {
		return nil
	}
	m.webPanel = nil
	msg.proc.terminate()
	for _, w := range st.waiters {
		w.fail(webproto.CodeInternal, "The web panel did not start.")
	}
	recordError("[web] gateway did not start: " + msg.err.Error())
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Errr, "Web panel not started", ai.Redact(msg.err.Error()))
	return c
}

func (m *Model) onWebPanelExit(msg webPanelExitMsg) tea.Cmd {
	st := m.webPanel
	if st == nil || st.proc != msg.proc {
		return nil
	}
	m.webPanel = nil
	for _, w := range st.waiters {
		w.fail(webproto.CodeInternal, "The web panel stopped.")
	}
	if st.stopping {
		return nil // the stop said so already
	}
	return m.webToast(toast.Info, "The web panel stopped. Your work is still here.")
}

// stopWebPanel is the kill switch: no card, stopping is the safe way.
func (m *Model) stopWebPanel() tea.Cmd {
	st := m.webPanel
	if st == nil {
		return m.webToast(toast.Info, "The web panel is not running.")
	}
	st.stopping = true
	if m.web != nil {
		m.web.ctlBye(webproto.CloseStopped)
	}
	proc := st.proc
	time.AfterFunc(webStopGrace, func() {
		if proc.alive() {
			proc.terminate()
		}
	})
	return m.webToast(toast.Info, "Web panel stopped. All browsers were disconnected.")
}

// newLink makes a launch secret, remembers its digest for webLinkTTL and
// sends it to every control connection (the gateway). The plain secret
// stays with the caller.
func (h *webHub) newLink() (string, error) {
	plain, digest, err := webNewLinkSecret()
	if err != nil {
		return "", err
	}
	h.mu.Lock()
	h.link, h.linkUntil = digest, time.Now().Add(webLinkTTL)
	var ctls []*webConn
	for _, c := range h.all {
		if c != nil && c.role == webproto.RoleCtl {
			ctls = append(ctls, c)
		}
	}
	h.mu.Unlock()
	for _, c := range ctls {
		c.sendLink(digest)
	}
	return plain, nil
}

// linkFor gives a new control connection the open link, if one is fresh
// (the gateway connects after the user started it). h.mu is held.
func (h *webHub) linkForLocked() ([sha256.Size]byte, bool) {
	return h.link, time.Now().Before(h.linkUntil)
}

// sendLink tells a control connection the digest of a new launch link.
func (c *webConn) sendLink(digest [sha256.Size]byte) {
	data, _ := json.Marshal(map[string]string{"sha256": hex.EncodeToString(digest[:])})
	c.send(&webproto.Event{T: webproto.KindEv, Kind: "link", Data: data})
}

// ctlBye ends the web side: every control connection gets a bye with
// code (the gateway closes every browser with it).
func (h *webHub) ctlBye(code webproto.CloseCode) {
	h.mu.Lock()
	h.linkUntil = time.Time{}
	var ctls []*webConn
	for _, c := range h.all {
		if c != nil && c.role == webproto.RoleCtl {
			ctls = append(ctls, c)
		}
	}
	h.mu.Unlock()
	for _, c := range ctls {
		c.send(&webproto.ServerBye{T: webproto.KindBye, Code: code})
	}
}

// The control requests of `termocode web` (same user, hub socket).
func init() {
	webOps[webproto.OpWebOpen] = func(m *Model, call *webCall) tea.Cmd { return m.webCtlOpen(call) }
	webOps[webproto.OpWebStop] = func(m *Model, call *webCall) tea.Cmd {
		cmd := m.stopWebPanel()
		call.reply(nil)
		return cmd
	}
}

// webCtlOpen answers web.open: the port once the panel serves. Without
// --yes the laptop must say yes (webAskStart).
func (m *Model) webCtlOpen(call *webCall) tea.Cmd {
	if st := m.webPanel; st != nil {
		if st.up {
			call.reply(map[string]int{"port": st.port})
			return nil
		}
		st.waiters = append(st.waiters, call)
		call.later()
		return nil
	}
	args, _ := call.args.(*webproto.WebOpenArgs)
	if args == nil || !args.Yes {
		return m.webAskStart(call)
	}
	cmd := m.startWebPanel(true, false)
	if m.webPanel == nil {
		call.fail(webproto.CodeInternal, "The web panel did not start. See the laptop.")
		return cmd
	}
	m.webPanel.waiters = append(m.webPanel.waiters, call)
	call.later()
	return cmd
}

// webAskStart shows the laptop's start card for `termocode web`.
func (m *Model) webAskStart(call *webCall) tea.Cmd {
	call.later()
	return m.openWebStartCard(call)
}
