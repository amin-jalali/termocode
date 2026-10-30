package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func covSvcQuitSetting(t *testing.T, body string) {
	t.Helper()
	dir, err := configDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func covSvcIsQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// Quit with services this session started: ask (Keep is first), keep,
// or stop them first, as services_on_quit says.
func TestCovSvcQuitWithStartedServices(t *testing.T) {
	m, f := covSvcProject(t)
	st := m.services
	st.startedByUs["redis"] = true
	m.terminalTabs = []terminalTab{{Service: svcLocalTab("web"), Running: true}}
	cmd, wait := m.servicesBeforeQuit()
	if !wait || cmd != nil || !m.confirmOpen || m.confirmKind != confirmKindServicesQuit {
		t.Fatal("no quit card")
	}
	if card := cardText(m); !strings.Contains(card, "You started: redis") || !strings.Contains(card, "Local services stop anyway: web") {
		t.Errorf("card = %s", card)
	}
	if m.answerServicesQuit("cancel") != nil || st.quitAsked {
		t.Error("cancel quit")
	}
	if !covSvcIsQuit(m.answerServicesQuit("keep")) || !st.quitAsked {
		t.Error("keep did not quit")
	}
	// Stop and quit: compose stop redis, then the quit message.
	st.quitAsked = false
	m.servicesBeforeQuit()
	stop := m.answerServicesQuit("stop")
	if _, ok := stop().(servicesQuitMsg); !ok || !strings.Contains(covSvcCalls(f), "stop redis") {
		t.Errorf("stop calls = %q", f.Calls())
	}
	delete(st.busy, svcKey(svcSecCompose, "redis")) // the stop is over
	// The settings: keep quits at once, stop stops first.
	covSvcQuitSetting(t, `{"services_on_quit":"keep","services_max_log_kb":32,"containers_max_inspect_kb":8}`)
	if got := loadServicesSettings(); got.OnQuit != "keep" || got.MaxLogKB != 32 || got.MaxInspectKB != 8 {
		t.Errorf("settings = %+v", got)
	}
	st.quitAsked = false
	if cmd, wait := m.servicesBeforeQuit(); wait || cmd != nil {
		t.Error("keep waited")
	}
	covSvcQuitSetting(t, `{"services_on_quit":"stop"}`)
	st.quitAsked = false
	if cmd, wait := m.servicesBeforeQuit(); !wait || cmd == nil {
		t.Error("stop did not stop first")
	}
	covSvcQuitSetting(t, `{"services_on_quit":"bogus"}`)
	if loadServicesSettings().OnQuit != "ask" {
		t.Error("a bad value was used")
	}
	// Nothing started: quit at once; no compose: quit at once.
	st.quitAsked = false
	delete(st.startedByUs, "redis")
	if _, wait := m.servicesBeforeQuit(); wait {
		t.Error("nothing started but waited")
	}
	st.det.Compose = false
	if !covSvcIsQuit(m.servicesStopThenQuit([]string{"redis"})) {
		t.Error("no compose: no quit")
	}
	st.opened = false
	if c, l := m.servicesToStopOnQuit(); c != nil || l != nil {
		t.Error("a closed view lists services")
	}
}
