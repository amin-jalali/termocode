package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestContainers is a web hub on the fake docker (docker27 fixtures):
// no real engine is ever called.
func webTestContainers(t *testing.T) (*Model, *webConn, *servicestest.Fake) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	m := aiTestModel(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := hostFake(t)
	f.Set(t, "ps.json", `{"ID":"9f3c2e1a1234","Names":"api-1","Image":"ghcr.io/acme/api:1.4","State":"running","Status":"Up 2 hours","Ports":"0.0.0.0:8081->8080/tcp","Labels":"com.docker.compose.project=shop"}
{"ID":"aabbccdd1122","Names":"old","Image":"nginx:1.27","State":"exited","Status":"Exited (1) 3 days ago","Labels":{}}
`)
	m.services = newServicesState()
	m.services.root = t.TempDir()
	m.output = newOutputStore()
	webTestHub(t, &m)
	return &m, webTestSub(t, &m), f
}

// Spec (WEB-44): containers.list lists the host's containers (id, name,
// image, state, ports, project) from the fake engine, never labels, env
// or mounts; containers.logs tails one container, redacted; a container
// not on the host is not_found.
func TestContainersListAndLogs(t *testing.T) {
	m, c, f := webTestContainers(t)
	var l webproto.ContainersList
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersList, `{}`), &l)
	if l.Engine != "docker" || len(l.Containers) != 2 {
		t.Fatalf("list %+v", l)
	}
	if api := l.Containers[0]; api.ID != "9f3c2e1a1234" || api.Name != "api-1" || api.State != "running" || api.Project != "shop" || !strings.Contains(api.Ports, "8081->8080") {
		t.Errorf("api %+v", api)
	}
	if raw, _ := webData(l); strings.Contains(string(raw), "com.docker") {
		t.Errorf("labels left the hub: %s", raw)
	}
	tok := "sk-" + "test-logs-000111222333"
	f.Set(t, "logs.txt", "2026-10-30T10:00:00Z start\n2026-10-30T10:00:01Z API_KEY="+tok+"\n")
	var lg webproto.ContainerLogs
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersLogs, `{"id":"9f3c2e1a1234","lines":50}`), &lg)
	if len(lg.Lines) != 2 || lg.Redacted == 0 {
		t.Fatalf("logs %+v", lg)
	}
	if raw, _ := webData(lg); strings.Contains(string(raw), tok) {
		t.Fatalf("a secret in the logs reached the page: %s", raw)
	}
	if !strings.Contains(strings.Join(f.Calls(), "\n"), "logs --tail 50 --timestamps 9f3c2e1a1234") {
		t.Errorf("calls %q", f.Calls())
	}
	if fr := webTestCallBg(t, m, c, webproto.OpContainersLogs, `{"id":"0123456789ab"}`); webTestErr(fr) != "not_found" {
		t.Errorf("an unknown container: %v", fr)
	}
}

// Spec (WEB-44): start and restart run at once (the page asked first);
// stop and remove wait for a laptop card and take only a container the
// last list gave; a running container cannot be removed, a stopped one
// not stopped. Nothing is stopped before the laptop says yes.
func TestContainersStopNeedsLaptopCard(t *testing.T) {
	m, c, f := webTestContainers(t)
	var done webproto.ContainerDone
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersRestart, `{"id":"9f3c2e1a1234"}`), &done)
	if done.State != "running" || f.Count("restart") != 1 {
		t.Fatalf("restart %+v calls %q", done, f.Calls())
	}
	if fr := webTestCallBg(t, m, c, webproto.OpContainersStop, `{"id":"9f3c2e1a1234"}`); webTestErr(fr) != "not_found" {
		t.Fatalf("stop before the list: %v", fr)
	}
	webTestCallBg(t, m, c, webproto.OpContainersList, `{}`)
	if fr := webTestCallBg(t, m, c, webproto.OpContainersRemove, `{"id":"9f3c2e1a1234"}`); webTestErr(fr) != "conflict" {
		t.Fatalf("remove a running one: %v", fr)
	}
	if fr := webTestCallBg(t, m, c, webproto.OpContainersStop, `{"id":"aabbccdd1122"}`); webTestErr(fr) != "conflict" {
		t.Fatalf("stop a stopped one: %v", fr)
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersStop, `{"id":"9f3c2e1a1234"}`), &ref)
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
		t.Fatal("no laptop card")
	}
	if v := m.confirm.View(); !strings.Contains(v, "Stop api-1") {
		t.Errorf("card:\n%s", v)
	}
	if f.Count("stop") != 0 {
		t.Fatal("stopped before the laptop said so")
	}
	webTestLaptopSelect(t, m, "confirm")
	if f.Count("stop") != 1 {
		t.Fatalf("not stopped after the laptop's yes: %q", f.Calls())
	}
	if a := webTestAudit(m); !strings.Contains(a, "containers.stop  9f3c2e1a1234 api-1  by=web:phone  → confirmed on the laptop") ||
		!strings.Contains(a, "containers.restart  9f3c2e1a1234 api-1") {
		t.Errorf("audit:\n%s", a)
	}
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersRemove, `{"id":"aabbccdd1122"}`), &ref)
	if v := m.confirm.View(); !strings.Contains(v, "cannot be undone") {
		t.Errorf("remove card:\n%s", v)
	}
}
