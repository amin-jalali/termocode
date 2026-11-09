package app

import (
	"os"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The web panel across a reload (Reload Window, a new build): the
// gateway is a child of this image and would only see the UI go (4012,
// every browser session ends). execReload hands it over first: the bye
// 1012 makes the gateway write its sessions to the folder's resume file
// and close the browsers with 1012 (the page retries); once it is gone
// the exec runs. The next image finds the file at start and starts the
// gateway again by itself with --resume, on the same port (internal/
// webgw/resume.go): each page gets back in with its token, no new link.

// webHandoverWait bounds the wait for the gateway to hand over and stop.
var webHandoverWait = 3 * time.Second

// webHandoverForReload hands the running gateway over (nothing when none
// runs). It blocks until the gateway is gone, webHandoverWait at most:
// the exec follows at once.
func (m *Model) webHandoverForReload() {
	st := m.webPanel
	if st == nil || st.proc == nil {
		return
	}
	if !st.up || m.web == nil {
		st.proc.terminate() // still starting: nothing to hand over
		return
	}
	m.web.ctlBye(webproto.CloseRestarting)
	deadline := time.Now().Add(webHandoverWait)
	for st.proc.alive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if st.proc.alive() {
		recordError("[web] the gateway did not hand over in time; the pages need a new link")
		st.proc.terminate()
	}
}

// webResumeMsg: the start found a gateway's hand-over. try counts the
// starts that failed (the last gateway may still be ending).
type webResumeMsg struct{ port, sshPort, try int }

// webResumeTries bounds the starts of a resumed panel, one a second: a
// browser that does not answer its close frame keeps the last gateway
// up to 5 s.
const webResumeTries = 10

// webResumeInitCmd looks for a hand-over at start (nil when there is
// none: one stat).
func (m Model) webResumeInitCmd() tea.Cmd {
	path, err := webResumeFile()
	if err != nil {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	return func() tea.Msg {
		port, sshPort, ok := webPeekResume(path)
		if !ok {
			return nil
		}
		return webResumeMsg{port: port, sshPort: sshPort}
	}
}

// startWebPanelResume starts the gateway again after a reload, quietly,
// on the port the last one served (the gateway takes the sessions).
func (m *Model) startWebPanelResume(msg webResumeMsg) tea.Cmd {
	if m.webPanel != nil || !webSettings().On {
		return nil
	}
	return m.startWebPanelWith(webStartOpts{sshPort: msg.sshPort, resumePort: msg.port, resume: true, try: msg.try})
}

// webResumeRetry starts a resumed panel again a second after a failed
// start; ok=false: no try left.
func webResumeRetry(st *webPanelState) (tea.Cmd, bool) {
	if !st.resumed || st.resumeTry+1 >= webResumeTries {
		return nil, false
	}
	next := webResumeMsg{port: st.resumePort, sshPort: st.sshPort, try: st.resumeTry + 1}
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return next }), true
}

// webArgsResume sets the port of a resumed gateway (plain and LAN mode;
// tailnet and SSH mode keep their address anyway) and adds --resume.
func webArgsResume(args []string, port int) []string {
	out := append([]string(nil), args...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == "--port" && port > 0 {
			out[i+1] = strconv.Itoa(port)
		}
	}
	return append(out, "--resume")
}
