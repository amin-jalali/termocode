package app

import (
	"path/filepath"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Starting and stopping the web hub (WEB-6). It is off by default: only
// the web side (the gateway the user starts on the laptop) turns it on.
// Stopping closes the socket and every connection; the gateway sees EOF
// and closes the browsers with 4012.

// webHubTarget is the running hub, for StopWebHub.
var webHubTarget atomic.Pointer[webHub]

// StopWebHub closes the web hub's socket and its connections (nothing
// when it does not run). cmd/termocode calls it when Bubble Tea returns,
// next to StopMCPServer.
func StopWebHub() {
	if h := webHubTarget.Swap(nil); h != nil {
		h.stop()
	}
}

// startWebHub starts the hub for the workspace (nil when it runs
// already). An error is one toast and errors.log, never a crash.
func (m *Model) startWebHub() tea.Cmd { return m.startWebHubAt(false) }

// startWebHubAt is startWebHub; quiet (at launch, when the user asked
// for nothing: another termocode in this folder may have the socket)
// says nothing.
func (m *Model) startWebHubAt(quiet bool) tea.Cmd {
	if m.web != nil {
		return nil
	}
	path, err := webHubSocketPath(aiWorkspaceRoot())
	if err == nil {
		ln, lerr := webHubListen(path)
		if lerr == nil {
			hub := newWebHub()
			hub.serve(ln, webHubPeer, func(err error) { recordError("[web] " + err.Error()) })
			m.web = hub
			webHubTarget.Store(hub)
			return waitWeb(hub)
		}
		err = lerr
	}
	if quiet {
		return nil // opening the panel tries again and says why
	}
	recordError("[web] hub not started: " + err.Error())
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Warn, "Web not started", ai.Redact(err.Error()))
	return c
}

// stopWebHub stops the hub (nothing when it does not run).
func (m *Model) stopWebHub() {
	h := m.web
	if h == nil {
		return
	}
	m.web = nil
	webHubTarget.CompareAndSwap(h, nil)
	h.stop()
}

// syncWebHub runs the hub while web_panel is "on" (a private socket only:
// nothing listens on the network until the user starts the panel) and
// stops the panel and the hub when it is turned off.
func (m *Model) syncWebHub() tea.Cmd {
	if webSettings().On {
		return m.startWebHubAt(true)
	}
	var cmd tea.Cmd
	if m.webPanel != nil {
		cmd = m.stopWebPanel()
	}
	m.stopWebHub()
	return cmd
}

// onWebConfigSaved applies web_panel when config.json is saved.
func (m *Model) onWebConfigSaved(path string) tea.Cmd {
	want, err := settingsConfigPath()
	if err != nil || filepath.Clean(path) != filepath.Clean(want) {
		return nil
	}
	return m.syncWebHub()
}
