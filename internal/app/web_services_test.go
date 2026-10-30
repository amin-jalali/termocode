package app

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestServices puts a loaded SERVICES view in the Model: a compose
// project with db (running, port 5432 open) and api (exited 1), and a
// local command web (running).
func webTestServices(t *testing.T, m *Model) *servicesState {
	t.Helper()
	st := newServicesState()
	st.opened, st.loaded, st.root, st.project = true, true, t.TempDir(), "shop"
	st.det = services.Detection{ComposeFiles: []string{"compose.yaml"}, Compose: true,
		Local: []services.Service{{Name: "web", Backend: "local", Command: "npm run dev", Source: "Procfile", Ports: []services.Port{{Published: 3000, Target: 3000}}}}}
	st.compose = []services.Service{
		{Name: "db", Backend: "compose", Image: "postgres:17", Source: "compose.yaml:3", Ports: []services.Port{{Published: 5432, Target: 5432, Proto: "tcp"}}},
		{Name: "api", Backend: "compose", Source: "compose.yaml:9"},
	}
	up := time.Unix(1_700_000_000, 0)
	st.status = map[string]services.Status{
		"db":  {Name: "db", State: services.Healthy, Health: "healthy", StartedAt: up, Text: "Up 2 hours (healthy)", Ports: []services.Port{{Published: 5432, Target: 5432, Proto: "tcp"}}},
		"api": {Name: "api", State: services.Exited, ExitCode: 1, Text: "Exited (1) 3 minutes ago"},
	}
	st.probes = map[string]bool{"compose/db": true}
	st.local = st.det.Local
	st.localSt = map[string]services.Status{"web": {Name: "web", State: services.Running, StartedAt: up}}
	m.services = st
	return st
}

type webTestSvcLog struct{ calls []string }

// webTestFakeServices swaps the laptop's service actions for fakes that
// record "verb section/name"; a local command the laptop never ran asks
// there (pendingLocal), as the real start does.
func webTestFakeServices(t *testing.T, askLocal bool) *webTestSvcLog {
	t.Helper()
	l := &webTestSvcLog{}
	oldStart, oldRestart, oldStop, oldLogs := webSvcStart, webSvcRestart, webSvcStop, webSvcComposeLogs
	rec := func(verb string) func(m *Model, section, name string) (string, tea.Cmd) {
		return func(m *Model, section, name string) (string, tea.Cmd) {
			if section == svcSecLocal && askLocal && verb != "stop" {
				m.services.pendingLocal = name
				return "asked the user to allow the command first", nil
			}
			l.calls = append(l.calls, verb+" "+section+"/"+name)
			if verb == "start" && m.services.statusOf(section, name).State.Up() {
				return "service \"" + name + "\" is already running", nil
			}
			return "", nil
		}
	}
	webSvcStart, webSvcRestart, webSvcStop = rec("start"), rec("restart"), rec("stop")
	webSvcComposeLogs = func(_ context.Context, _ *services.Compose, name string, lines int) (string, error) {
		l.calls = append(l.calls, "logs "+name)
		return "db-1  | ready\ndb-1  | password=" + "hunter" + "2secret\n", nil
	}
	t.Cleanup(func() {
		webSvcStart, webSvcRestart, webSvcStop, webSvcComposeLogs = oldStart, oldRestart, oldStop, oldLogs
	})
	return l
}

func webTestServicesSnap(t *testing.T, frames []map[string]any) (webproto.ServicesSnap, int) {
	t.Helper()
	var s webproto.ServicesSnap
	n := 0
	for _, f := range frames {
		if f["topic"] == "services" {
			s = webproto.ServicesSnap{}
			webTestData(t, f, &s)
			n++
		}
	}
	return s, n
}

// Spec (WEBP-4): the services topic is the laptop's SERVICES list: each
// compose service with its state, health, exit code, compose's words and
// published ports (open as the laptop's probe saw), each local command
// with its command line; a busy service says so.
func TestServicesTopicSnapshot(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	st := webTestServices(t, &m)
	c := webTestSub(t, &m, "services")
	s, _ := webTestServicesSnap(t, webTestNow(t, c))
	if s.State != "ready" || s.Project != "shop" || strings.Join(s.ComposeFiles, ",") != "compose.yaml" || len(s.Services) != 3 {
		t.Fatalf("snap %+v", s)
	}
	db, api, web := s.Services[0], s.Services[1], s.Services[2]
	if db.Name != "db" || db.Kind != "compose" || db.State != "healthy" || db.Image != "postgres:17" || db.Since != 1_700_000_000_000 ||
		len(db.Ports) != 1 || db.Ports[0] != (webproto.ServicePort{Published: 5432, Target: 5432, Proto: "tcp", Open: "yes"}) {
		t.Fatalf("db %+v", db)
	}
	if api.State != "exited" || api.Exit != 1 || api.Status != "Exited (1) 3 minutes ago" || api.Source != "compose.yaml:9" {
		t.Fatalf("api %+v", api)
	}
	if web.Kind != "local" || web.State != "running" || web.Command != "npm run dev" || web.Ports[0].Published != 3000 {
		t.Fatalf("web %+v", web)
	}
	st.busy["compose/api"] = services.Starting
	st.touch()
	m.webAfterUpdate(nil)
	s, n := webTestServicesSnap(t, webTestNow(t, c))
	if n != 1 || s.Services[1].State != "starting" || !s.Services[1].Busy {
		t.Fatalf("busy: %d %+v", n, s.Services[1])
	}
	m.webAfterUpdate(nil)
	if _, n := webTestServicesSnap(t, webTestNow(t, c)); n != 0 {
		t.Fatalf("%d frames without a change", n)
	}
}

// Spec (WEBP-4): a project without services says so; before the laptop
// read them the list is loading, and the first listening browser makes
// the laptop read them.
func TestServicesNoneAndLoading(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	if s := m.webServicesBuild(); s.State != "loading" {
		t.Fatalf("before a load: %+v", s)
	}
	webTestSub(t, &m, "services")
	if cmd := m.webPublishServices(nil); cmd == nil || !m.services.inflight {
		t.Fatal("no load for a listening browser")
	}
	m.services.inflight, m.services.loaded = false, true
	if s := m.webServicesBuild(); s.State != "none" {
		t.Fatalf("no services: %+v", s)
	}
}

// Spec (WEBP-4): start and restart run the laptop's own actions for a
// listed service; a local command the laptop never ran asks there
// first (asked: nothing ran); "already running" comes back as a note;
// an unknown name is not found. Both are audited.
func TestServicesStartRestart(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	webTestServices(t, &m)
	l := webTestFakeServices(t, true)
	c := webTestSub(t, &m)
	var done webproto.ServiceDone
	f, _ := webTestCall(t, &m, c, webproto.OpServicesStart, `{"name":"api"}`)
	webTestData(t, f, &done)
	if done != (webproto.ServiceDone{}) || strings.Join(l.calls, ",") != "start compose/api" {
		t.Fatalf("start api: %+v %v", done, l.calls)
	}
	f, _ = webTestCall(t, &m, c, webproto.OpServicesStart, `{"name":"db"}`)
	webTestData(t, f, &done)
	if done.Note != `Service "db" is already running.` {
		t.Fatalf("start db: %+v", done)
	}
	f, _ = webTestCall(t, &m, c, webproto.OpServicesRestart, `{"name":"web"}`)
	done = webproto.ServiceDone{}
	webTestData(t, f, &done)
	if !done.Asked || strings.Contains(strings.Join(l.calls, ","), "web") {
		t.Fatalf("restart web: %+v %v", done, l.calls)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpServicesStart, `{"name":"nope"}`); webTestErr(f) != "not_found" {
		t.Fatalf("unknown: %v", f)
	}
	m.services.det.Compose = false
	if f, _ := webTestCall(t, &m, c, webproto.OpServicesStart, `{"name":"api"}`); webTestErr(f) != "conflict" {
		t.Fatalf("no compose: %v", f)
	}
	a := webTestAudit(&m)
	if !strings.Contains(a, "services.start  api  by=web:phone  → ok") || !strings.Contains(a, "services.restart  web  by=web:phone  → asked on the laptop") {
		t.Errorf("audit:\n%s", a)
	}
}

// Spec (WEBP-4): stop waits for a laptop card (Cancel first); nothing
// stops before the laptop says yes; a service that is not running is a
// conflict.
func TestServicesStopNeedsLaptopCard(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	webTestServices(t, &m)
	l := webTestFakeServices(t, false)
	c := webTestSub(t, &m)
	if f := webTestCallBg(t, &m, c, webproto.OpServicesStop, `{"name":"api"}`); webTestErr(f) != "conflict" {
		t.Fatalf("stop an exited one: %v", f)
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpServicesStop, `{"name":"db"}`), &ref)
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
		t.Fatal("no laptop card")
	}
	if v := m.confirm.View(); !strings.Contains(v, "Stop the Docker Compose service db?") {
		t.Errorf("card:\n%s", v)
	}
	if len(l.calls) != 0 {
		t.Fatalf("stopped before the laptop said so: %v", l.calls)
	}
	webTestLaptopSelect(t, &m, "confirm")
	if strings.Join(l.calls, ",") != "stop compose/db" {
		t.Fatalf("after yes: %v", l.calls)
	}
}

// Spec (WEBP-4): logs of a compose service are compose's own newest
// lines, redacted like the AI's; a local service that never ran has
// none.
func TestServicesLogs(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestServices(t, &m)
	webTestFakeServices(t, false)
	c := webTestSub(t, &m)
	var out webproto.ServiceLogs
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpServicesLogs, `{"name":"db"}`), &out)
	if len(out.Lines) != 2 || out.Lines[0] != "db-1  | ready" || strings.Contains(strings.Join(out.Lines, "\n"), "hunter2secret") || out.Redacted == 0 {
		t.Fatalf("logs %+v", out)
	}
	if f := webTestCallBg(t, &m, c, webproto.OpServicesLogs, `{"name":"web"}`); webTestErr(f) != "not_found" {
		t.Fatalf("local logs without a tab: %v", f)
	}
}

// Spec (WEBP-4): the services replay golden: a start that settles and an
// exit replay to the final snapshot.
func TestServicesReplayGolden(t *testing.T) {
	m := aiTestModel(t)
	h := webTestHub(t, &m)
	st := webTestServices(t, &m)
	c := webReplayRecord(t, &m, h, "services", func() {
		st.busy["compose/api"] = services.Starting
		st.touch()
		m.webAfterUpdate(nil)
		delete(st.busy, "compose/api")
		st.status["api"] = services.Status{Name: "api", State: services.Running, StartedAt: time.Unix(1_700_000_100, 0), Text: "Up 1 second"}
		st.touch()
		m.webAfterUpdate(nil)
	})
	webReplayCheck(t, h.topics["services"], c)
	webReplayGolden(t, webReplayPath("services", "start_settles"), c)
}
