//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestCtrTerms records the laptop terminals the web asked for
// (shell, pull, run, compose …): no real terminal is spawned.
func webTestCtrTerms(t *testing.T) *[]string {
	t.Helper()
	var got []string
	old := webCtrTerminal
	webCtrTerminal = func(_ *Model, c services.Command, key string) error {
		got = append(got, key+" "+strings.Join(c.Argv[1:], " "))
		return nil
	}
	t.Cleanup(func() { webCtrTerminal = old })
	return &got
}

func webTestWithDir(c containers.Container, dir string) containers.Container {
	c.WorkingDir = dir
	return c
}

// webTestHost is the containers hub with one registry signed in.
func webTestHost(t *testing.T) (*Model, *webConn, *[]string, func() []string) {
	t.Helper()
	m, c, f := webTestContainers(t)
	cfg := t.TempDir()
	t.Setenv("DOCKER_CONFIG", cfg)
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"auths":{"ghcr.io":{"auth":"`+"c2Vj"+"cmV0"+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.Set(t, "ps.json", `{"ID":"9f3c2e1a1234","Names":"api-1","Image":"postgres:16","ImageID":"sha256:111","State":"running","Status":"Up 2 hours","Ports":"0.0.0.0:5433->5432/tcp","Labels":"com.docker.compose.project=shop"}
{"ID":"aabbccdd1122","Names":"old","Image":"nginx:1.27","State":"paused","Status":"Up 3 days (Paused)","Labels":{}}
`)
	f.Set(t, "network-ls.json", `{"ID":"net123","Name":"shop_default","Driver":"bridge","Scope":"local"}
{"ID":"net000","Name":"bridge","Driver":"bridge","Scope":"local"}
`)
	return m, c, webTestCtrTerms(t), f.Calls
}

// Spec (web parity, Containers): containers.host lists the images (tags,
// in use), networks (built-in marked), volumes, contexts (current) and
// registries (host and signed in, never a credential) from the fake
// engine; no label, mountpoint or auth value reaches the page.
func TestContainersHostLists(t *testing.T) {
	m, c, _, _ := webTestHost(t)
	var h webproto.ContainersHost
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersHost, `{}`), &h)
	if h.Engine != "docker" || len(h.Images) != 2 || len(h.Networks) != 2 || len(h.Volumes) != 1 || len(h.Contexts) != 2 || len(h.Registries) != 1 {
		t.Fatalf("host %+v", h)
	}
	if im := h.Images[0]; im.ID != "111" || len(im.Tags) != 1 || im.Tags[0] != "postgres:16" || !im.InUse {
		t.Errorf("image %+v", im)
	}
	if len(h.Images[1].Tags) != 0 || h.Images[1].InUse {
		t.Errorf("dangling image %+v", h.Images[1])
	}
	if h.Networks[1].Name != "bridge" || !h.Networks[1].BuiltIn || h.Networks[0].BuiltIn {
		t.Errorf("networks %+v", h.Networks)
	}
	if !h.Contexts[0].Current || h.Contexts[1].Name != "prod" {
		t.Errorf("contexts %+v", h.Contexts)
	}
	if r := h.Registries[0]; r.Host != "ghcr.io" || !r.SignedIn {
		t.Errorf("registry %+v", r)
	}
	raw, _ := webData(h)
	for _, bad := range []string{"maintainer", "/var/lib/docker", "c2Vj", "ssh://"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("%q left the hub: %s", bad, raw)
		}
	}
}

// Spec: containers.inspect masks env values and auth fields on the hub;
// containers.history masks the values of ENV steps; a stats sample and
// the disk usage are read from the engine; a container not on the host
// is not_found.
func TestContainersInspectHistoryStats(t *testing.T) {
	m, c, f := webTestContainers(t)
	tok := "sk-" + "test-inspect-000111222333"
	f.Set(t, "inspect.json", `[{"Id":"9f3c2e1a1234","Name":"/api-1","Config":{"Image":"ghcr.io/acme/api:1.4","Env":["API_KEY=`+tok+`","PORT=8080"]},"State":{"Status":"running"}}]`)
	var in webproto.ContainerInspect
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersInspect, `{"kind":"container","id":"9f3c2e1a1234"}`), &in)
	if in.Masked == 0 || !strings.Contains(in.Text, "API_KEY") || strings.Contains(in.Text, tok) || strings.Contains(in.Text, "8080\"") {
		t.Fatalf("inspect %+v", in)
	}
	if fr := webTestCallBg(t, m, c, webproto.OpContainersInspect, `{"kind":"container","id":"0123456789ab"}`); webTestErr(fr) != "not_found" {
		t.Errorf("an unknown container: %v", fr)
	}
	f.Set(t, "history.json", `{"ID":"sha256:111","CreatedAt":"2026-10-17","CreatedBy":"/bin/sh -c #(nop) ENV DB_PASSWORD=`+tok+` MODE=prod","Size":"0B"}
{"ID":"<missing>","CreatedAt":"2026-10-16","CreatedBy":"/bin/sh -c #(nop) CMD [\"postgres\"]","Size":"0B"}
`)
	var hi webproto.ImageHistory
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersHistory, `{"ref":"nginx:1.27"}`), &hi)
	if len(hi.Rows) != 2 || strings.Contains(hi.Rows[0].CreatedBy, tok) || strings.Contains(hi.Rows[0].CreatedBy, "prod") || !strings.Contains(hi.Rows[0].CreatedBy, "DB_PASSWORD=") {
		t.Fatalf("history %+v", hi)
	}
	f.Set(t, "stats.json", `{"Container":"9f3c2e1a1234","CPUPerc":"0.12%","MemUsage":"48.2MiB / 7.7GiB","MemPerc":"0.61%","NetIO":"1.2kB / 3.4kB","BlockIO":"0B / 8.19kB","PIDs":"12"}`)
	var st webproto.ContainerStats
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersStats, `{"id":"9f3c2e1a1234"}`), &st)
	if st.CPU != "0.12%" || st.PIDs != "12" || !strings.Contains(st.Mem, "48.2MiB") {
		t.Errorf("stats %+v", st)
	}
	f.Set(t, "df.txt", "TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE\nImages          2         1         421MB     0B (0%)\n")
	var du webproto.DiskUsage
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersDiskUsage, `{}`), &du)
	if len(du.Lines) != 2 || !strings.HasPrefix(du.Lines[1], "Images") {
		t.Errorf("disk usage %+v", du)
	}
}

// Spec: a Docker Hub search returns clean results (a flag-like name is
// dropped); a repository's tags come from its registry.
func TestContainersSearchAndTags(t *testing.T) {
	m, c, f := webTestContainers(t)
	b, err := os.ReadFile(filepath.Join("..", "containers", "testdata", "docker27", "search.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.Set(t, "search.json", string(b))
	var s webproto.ImageSearch
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersSearch, `{"term":"nginx"}`), &s)
	if len(s.Results) != 2 || s.Results[0].Name != "nginx" || !s.Results[0].Official || s.Results[0].Stars != 20512 {
		t.Fatalf("search %+v", s)
	}
	_, host := tagServer(t, []string{"1.27", "1.26"})
	var tg webproto.RemoteTags
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersTags, `{"ref":"`+host+`/acme/web"}`), &tg)
	if len(tg.Tags) != 2 {
		t.Fatalf("tags %+v", tg)
	}
	if raw, _ := webData(tg); strings.Contains(string(raw), appTestToken) {
		t.Fatal("the registry token reached the page")
	}
}

// Spec: pause toggles (a paused one unpauses); tag, create, compose up
// and restart (in a laptop terminal), the session context and start all
// run at once; services.configure writes the template once and names it.
func TestContainersPageActions(t *testing.T) {
	m, c, terms, calls := webTestHost(t)
	webTestCallBg(t, m, c, webproto.OpContainersList, `{}`)
	m.web.more.ctrRaw["9f3c2e1a1234"] = webTestWithDir(m.web.more.ctrRaw["9f3c2e1a1234"], t.TempDir())
	var done webproto.ContainerDone
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersPause, `{"id":"9f3c2e1a1234"}`), &done)
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersPause, `{"id":"aabbccdd1122"}`), &done)
	all := strings.Join(calls(), "\n")
	if !strings.Contains(all, "pause 9f3c2e1a1234") || !strings.Contains(all, "unpause aabbccdd1122") {
		t.Fatalf("calls %s", all)
	}
	webTestCallBg(t, m, c, webproto.OpContainersHost, `{}`)
	var hd webproto.HostDone
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersTag, `{"ref":"postgres:16","tag":"postgres:local"}`), &hd)
	if fr := webTestCallBg(t, m, c, webproto.OpContainersTag, `{"ref":"redis:7","tag":"redis:x"}`); webTestErr(fr) != "not_found" {
		t.Errorf("tag of an unlisted image: %v", fr)
	}
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersCreate, `{"kind":"network","name":"lab"}`), &hd)
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersCreate, `{"kind":"volume","name":"cache"}`), &hd)
	all = strings.Join(calls(), "\n")
	for _, want := range []string{"tag postgres:16 postgres:local", "network create lab", "volume create cache"} {
		if !strings.Contains(all, want) {
			t.Errorf("no %q in %s", want, all)
		}
	}
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersCompose, `{"project":"shop","verb":"up"}`), &hd)
	if fr := webTestCallBg(t, m, c, webproto.OpContainersCompose, `{"project":"nope","verb":"up"}`); webTestErr(fr) != "not_found" {
		t.Errorf("an unknown project: %v", fr)
	}
	if len(*terms) != 1 || !strings.Contains((*terms)[0], "-p shop") || !strings.HasSuffix((*terms)[0], "up -d") {
		t.Errorf("compose terminals %q", *terms)
	}
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersContext, `{"name":"prod"}`), &hd)
	if m.services.sessionContext != "prod" {
		t.Errorf("session context %q", m.services.sessionContext)
	}
	if fr := webTestCallBg(t, m, c, webproto.OpContainersContext, `{"name":"ghost"}`); webTestErr(fr) != "not_found" {
		t.Errorf("an unknown context: %v", fr)
	}
	var cfg webproto.ServicesConfig
	webTestData(t, webTestCallBg(t, m, c, webproto.OpServicesConfigure, `{}`), &cfg)
	if cfg.Path != services.ConfigPath || !cfg.Created {
		t.Fatalf("configure %+v", cfg)
	}
	cfg = webproto.ServicesConfig{}
	webTestData(t, webTestCallBg(t, m, c, webproto.OpServicesConfigure, `{}`), &cfg)
	if cfg.Created {
		t.Error("the template was written twice")
	}
	if a := webTestAudit(m); !strings.Contains(a, "containers.pause") || !strings.Contains(a, "containers.compose  shop up") {
		t.Errorf("audit:\n%s", a)
	}
}

// Spec: every risky containers op waits for a laptop card (Cancel first)
// and runs nothing before the laptop's yes; then it runs the laptop's
// own command. Items come only from the last listing; built-in networks,
// images in use and unknown registries are refused.
func TestContainersLaptopCards(t *testing.T) {
	m, c, terms, calls := webTestHost(t)
	webTestCallBg(t, m, c, webproto.OpContainersList, `{}`)
	webTestCallBg(t, m, c, webproto.OpContainersHost, `{}`)
	dir := t.TempDir()
	m.web.more.ctrRaw["9f3c2e1a1234"] = webTestWithDir(m.web.more.ctrRaw["9f3c2e1a1234"], dir)
	n, nt := len(calls()), 0
	ask := func(op, args, inCard string) {
		t.Helper()
		var ref webproto.AskRef
		webTestData(t, webTestCallBg(t, m, c, op, args), &ref)
		if ref.Card == "" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
			t.Fatalf("%s: no laptop card", op)
		}
		if v := m.confirm.View(); !strings.Contains(v, inCard) {
			t.Errorf("%s card:\n%s", op, v)
		}
		if len(calls()) != n || len(*terms) != nt {
			t.Fatalf("%s ran before the laptop said yes: %q %q", op, calls()[n:], *terms)
		}
		webTestLaptopSelect(t, m, "confirm")
		n, nt = len(calls()), len(*terms)
	}
	ask(webproto.OpContainersPrune, `{"kind":"images"}`, "untagged images")
	ask(webproto.OpContainersStorageRemove, `{"kind":"volume","name":"termocode_db"}`, "termocode_db")
	ask(webproto.OpContainersContextDefault, `{"name":"prod"}`, "every terminal")
	ask(webproto.OpContainersRegistry, `{"verb":"signout","host":"ghcr.io"}`, "ghcr.io")
	all := strings.Join(calls(), "\n")
	for _, want := range []string{"image prune -f", "volume rm termocode_db", "context use prod", "logout ghcr.io"} {
		if !strings.Contains(all, want) {
			t.Errorf("no %q after the laptop's yes in %s", want, all)
		}
	}
	for _, a := range [][3]string{
		{webproto.OpContainersExec, `{"id":"9f3c2e1a1234","kind":"shell"}`, "api-1"},
		{webproto.OpContainersImage, `{"verb":"pull","ref":"redis:7"}`, "redis:7"},
		{webproto.OpContainersImage, `{"verb":"run","ref":"postgres:16","name":"pg2","ports":["5434:5432"]}`, "-p 5434:5432"},
		{webproto.OpContainersImage, `{"verb":"push","ref":"postgres:16"}`, "registry"},
		{webproto.OpContainersComposeAsk, `{"project":"shop","verb":"down"}`, "Volumes stay"},
	} {
		ask(a[0], a[1], a[2])
	}
	if len(*terms) != 5 || !strings.Contains((*terms)[0], "exec -it 9f3c2e1a1234") || !strings.Contains((*terms)[1], "pull redis:7") ||
		!strings.Contains((*terms)[2], "run --rm -it --name pg2 -p 5434:5432 postgres:16") || !strings.Contains((*terms)[4], "down") {
		t.Errorf("terminals %q", *terms)
	}
	for _, bad := range [][3]string{
		{webproto.OpContainersStorageRemove, `{"kind":"network","name":"bridge"}`, "conflict"},
		{webproto.OpContainersStorageRemove, `{"kind":"volume","name":"ghost"}`, "not_found"},
		{webproto.OpContainersImage, `{"verb":"remove","ref":"postgres:16"}`, "conflict"},
		{webproto.OpContainersImage, `{"verb":"push","ref":"redis:7"}`, "not_found"},
		{webproto.OpContainersRegistry, `{"verb":"signout","host":"quay.io"}`, "not_found"},
		{webproto.OpContainersContextDefault, `{"name":"ghost"}`, "not_found"},
		{webproto.OpContainersOpenDB, `{"id":"aabbccdd1122"}`, "conflict"},
	} {
		if fr := webTestCallBg(t, m, c, bad[0], bad[1]); webTestErr(fr) != bad[2] {
			t.Errorf("%s %s: %v, want %s", bad[0], bad[1], fr, bad[2])
		}
	}
	// A sign-in and Open in Databases open the laptop's own prompt: the
	// password is typed there, never in the page.
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersRegistry, `{"verb":"connect","host":"ghcr.io"}`), &ref)
	webTestLaptopSelect(t, m, "confirm")
	if !m.promptOpen {
		t.Error("the laptop did not ask for the sign-in")
	}
	m.promptOpen = false
	if a := webTestAudit(m); !strings.Contains(a, "containers.prune  images  by=web:phone  → confirmed on the laptop") {
		t.Errorf("audit:\n%s", a)
	}
}

// Spec: start all runs the laptop's own start of every service at once;
// stop all waits for a laptop card.
func TestServicesStartStopAll(t *testing.T) {
	m, c, _ := webTestContainers(t)
	var ran []string
	old := webSvcAll
	webSvcAll = func(_ *Model, verb string) tea.Cmd { ran = append(ran, verb); return nil }
	t.Cleanup(func() { webSvcAll = old })
	var hd webproto.HostDone
	webTestData(t, webTestCallBg(t, m, c, webproto.OpServicesStartAll, `{}`), &hd)
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, m, c, webproto.OpServicesStopAll, `{}`), &ref)
	if strings.Join(ran, ",") != "start-all" || !m.confirmOpen {
		t.Fatalf("ran %q before the card", ran)
	}
	webTestLaptopSelect(t, m, "confirm")
	if strings.Join(ran, ",") != "start-all,stop-all" {
		t.Errorf("ran %q", ran)
	}
}
