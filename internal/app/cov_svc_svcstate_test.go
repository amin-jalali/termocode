package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
)

// A snapshot: an old one is dropped; a service that was up and exits
// non-zero gets one toast; starting rows settle.
func TestCovSvcSnapshotExitToastOnce(t *testing.T) {
	m, _ := covSvcProject(t)
	st := m.services
	st.reqSeq = 5
	if m.onServicesSnapshot(servicesSnapshotMsg{seq: 4, root: st.root}) != nil {
		t.Error("an old snapshot was used")
	}
	snap := func(status map[string]services.Status) servicesSnapshotMsg {
		return servicesSnapshotMsg{seq: 5, root: st.root, det: st.det, cfg: services.Config{Project: "termocode", Services: st.compose}, status: status}
	}
	st.busy[svcKey(svcSecCompose, "api")] = services.Starting
	st.busy[svcKey(svcSecCompose, "redis")] = services.Restarting
	m.onServicesSnapshot(snap(map[string]services.Status{
		"api":   {Name: "api", State: services.Running},
		"redis": {Name: "redis", State: services.Running},
	}))
	if len(st.busy) != 0 {
		t.Errorf("busy kept: %v", st.busy)
	}
	exited := map[string]services.Status{"api": {Name: "api", State: services.Exited, ExitCode: 2, Text: "Exited (2) now"}}
	m.onServicesSnapshot(snap(exited))
	if !strings.Contains(regToast(m), `Service "api" exited with code 2`) {
		t.Errorf("toast = %q", regToast(m))
	}
	before := regToast(m)
	m.onServicesSnapshot(snap(exited))
	if regToast(m) != before {
		t.Error("a second toast for the same exit")
	}
	// No compose any more: the compose rows go.
	msg := snap(nil)
	msg.det.Compose = false
	m.onServicesSnapshot(msg)
	if st.compose != nil || st.project != "" {
		t.Errorf("compose kept: %v %q", st.compose, st.project)
	}
}

// docker events: an old watcher is ignored, an ended stream reads again,
// image / network / volume events reload their section.
func TestCovSvcServicesEvents(t *testing.T) {
	m, f := covSvcHost(t)
	st := m.services
	st.watchGen = 3
	if m.onServicesEvent(servicesEventMsg{gen: 2}) != nil {
		t.Error("an old watcher's event was used")
	}
	for _, typ := range []string{"image", "network", "volume", "container"} {
		n := len(f.Calls())
		pumpHost(t, m, m.onServicesEvent(servicesEventMsg{gen: 3, event: services.Event{Type: typ}}))
		if len(f.Calls()) == n {
			t.Errorf("%s event read nothing", typ)
		}
	}
	pumpHost(t, m, m.onServicesEvent(servicesEventMsg{gen: 3, closed: true}))
	if st.watchCh != nil && st.watchGen == 3 {
		t.Error("an ended stream kept its watcher")
	}
}

// A Services terminal tab that ends: up failed, a local service exited
// by itself, a project command failed, a container tab closed.
func TestCovSvcServiceJobExit(t *testing.T) {
	m, _ := covSvcHost(t)
	st := m.services
	st.busy[svcKey(svcSecCompose, "api")] = services.Starting
	pumpHost(t, m, m.onServiceJobExit(svcTabUp, 1))
	if st.busy[svcKey(svcSecCompose, "api")].Busy() || !strings.Contains(regToast(m), "docker compose up failed") {
		t.Errorf("up exit: %v %q", st.busy, regToast(m))
	}
	pumpHost(t, m, m.onServiceJobExit(svcLocalTab("web"), 3))
	if st.localSt["web"].State != services.Exited || !strings.Contains(regToast(m), `Service "web" exited with code 3`) {
		t.Errorf("local exit: %+v %q", st.localSt["web"], regToast(m))
	}
	st.localStop["web"] = true
	m.onServiceJobExit(svcLocalTab("web"), 143)
	if st.localSt["web"].State != services.Stopped {
		t.Errorf("asked stop: %+v", st.localSt["web"])
	}
	st.busy[containerBusyPrefix+"c4"] = services.Starting
	pumpHost(t, m, m.onServiceJobExit(containerTabPrefix+"compose:shop", 1))
	if st.busy[containerBusyPrefix+"c4"].Busy() || !strings.Contains(regToast(m), "Project command failed") {
		t.Errorf("project exit: %v %q", st.busy, regToast(m))
	}
	pumpHost(t, m, m.onServiceJobExit(containerTabPrefix+"logs:c1", 0))
	if m.onServiceJobExit("task:build", 0) != nil {
		t.Error("a non-service tab was handled")
	}
	if (&Model{}).onServiceJobExit(svcTabUp, 0) != nil {
		t.Error("no state: something ran")
	}
}

// The session remembers what this project started and allowed.
func TestCovSvcServicesSessionRoundTrip(t *testing.T) {
	m, _ := covSvcProject(t)
	st := m.services
	st.startedByUs["redis"] = true
	st.localOK["abc"] = true
	m.servicesSaveSession()
	fresh := newServicesState()
	fresh.root = st.root
	m2 := svcTestModel(fresh, 100, 40)
	m2.servicesLoadSession()
	if !fresh.startedByUs["redis"] || !fresh.localOK["abc"] {
		t.Errorf("loaded %v %v", fresh.startedByUs, fresh.localOK)
	}
	fresh.startedByUs = map[string]bool{}
	m2.servicesLoadSession() // once per state
	if fresh.startedByUs["redis"] {
		t.Error("loaded twice")
	}
}
