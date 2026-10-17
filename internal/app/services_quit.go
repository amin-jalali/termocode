package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/statusbar"
)

// ── Services settings, the quit rule and the status chip (SV-7) ─────────

// servicesSettings are the config.json keys of the Services view.
type servicesSettings struct {
	OnQuit   string `json:"services_on_quit"`    // ask (default) | keep | stop
	MaxLogKB int    `json:"services_max_log_kb"` // AI log cap (16)
}

func loadServicesSettings() servicesSettings {
	s := servicesSettings{OnQuit: "ask", MaxLogKB: 16}
	dir, err := configDir()
	if err != nil {
		return s
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return s
	}
	var raw servicesSettings
	if json.Unmarshal(b, &raw) == nil {
		switch raw.OnQuit {
		case "ask", "keep", "stop":
			s.OnQuit = raw.OnQuit
		}
		if raw.MaxLogKB > 0 {
			s.MaxLogKB = raw.MaxLogKB
		}
	}
	return s
}

// servicesToStopOnQuit are the compose services this session started
// that still run, and the running local services.
func (m *Model) servicesToStopOnQuit() (compose, local []string) {
	st := m.services
	if st == nil || !st.opened {
		return nil, nil
	}
	for _, s := range st.compose {
		if st.startedByUs[s.Name] && st.statusOf(svcSecCompose, s.Name).State.Up() {
			compose = append(compose, s.Name)
		}
	}
	for _, s := range st.local {
		if _, ok := m.svcTabRunning(svcLocalTab(s.Name)); ok {
			local = append(local, s.Name)
		}
	}
	return compose, local
}

// servicesBeforeQuit applies services_on_quit. ok=true means quitting
// waits (a card is open, or a stop runs first).
func (m *Model) servicesBeforeQuit() (tea.Cmd, bool) {
	st := m.services
	if st == nil || st.quitAsked {
		st.shutdown()
		return nil, false
	}
	compose, local := m.servicesToStopOnQuit()
	if len(compose) == 0 {
		st.shutdown()
		return nil, false
	}
	switch loadServicesSettings().OnQuit {
	case "keep":
		st.shutdown()
		return nil, false
	case "stop":
		st.quitAsked = true
		return m.servicesStopThenQuit(compose), true
	}
	body := "You started: " + strings.Join(compose, ", ") + "."
	if len(local) > 0 {
		body += "\nLocal services stop anyway: " + strings.Join(local, ", ") + "."
	}
	m.confirm = confirm.New("Stop the services you started?", body, []confirm.Button{
		{ID: "keep", Title: "Keep running and quit", Style: confirm.StylePrimary},
		{ID: "stop", Title: "Stop and quit"},
		{ID: "cancel", Title: "Cancel"},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindServicesQuit
	return nil, true
}

// answerServicesQuit handles the quit card.
func (m *Model) answerServicesQuit(id string) tea.Cmd {
	st := m.services
	switch id {
	case "keep":
		st.quitAsked = true
		return m.quitNow()
	case "stop":
		st.quitAsked = true
		compose, _ := m.servicesToStopOnQuit()
		return m.servicesStopThenQuit(compose)
	}
	return nil
}

// servicesQuitMsg quits after the stop on quit.
type servicesQuitMsg struct{}

// servicesStopThenQuit runs `compose stop <names>` (30 s cap), then
// quits.
func (m *Model) servicesStopThenQuit(names []string) tea.Cmd {
	st := m.services
	c := st.composeBackend()
	st.shutdown()
	if c == nil || len(names) == 0 {
		return m.quitNow()
	}
	for _, n := range names {
		st.busy[svcKey(svcSecCompose, n)] = services.Stopping
	}
	st.touch()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = c.Stop(ctx, names)
		return servicesQuitMsg{}
	}
}

// ── Status chip ──────────────────────────────────────────────────────────

// fillServicesStatus sets the ☷ 3/4 chip: only for a project with
// services whose view was opened this session.
func (m Model) fillServicesStatus(s *statusbar.State) {
	st := m.services
	if st == nil || !st.everShown || !st.loaded {
		return
	}
	up, total, bad := st.counts()
	if total == 0 {
		return
	}
	s.ShowServices = true
	s.ServicesUp, s.ServicesTotal, s.ServicesBad = up, total, bad
	s.ServicesStale = st.stale
}
