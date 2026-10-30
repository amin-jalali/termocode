package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// More user flows and error paths of the Containers AI tools
// (ai_containers_tools.go): the read tools on the fake docker, engine
// errors, refusals before a card, and the podman / context labels.

func covAIRun(t *testing.T, m *Model, call aiIDECaller, name string, a map[string]any) (string, error) {
	t.Helper()
	if a == nil {
		a = map[string]any{}
	}
	return aiContainersTool(t, m.aiContainersTools(call), name).Run(context.Background(), a)
}

func covAIMustContain(t *testing.T, what, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("%s has no %q:\n%s", what, w, out)
		}
	}
}

// covAIWrapDocker puts a docker in front of the fake that fails for one
// subcommand (stderr carries a secret-looking value) and makes the
// session use it.
func covAIWrapDocker(t *testing.T, m *Model, f *servicestest.Fake, failSub string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	body := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = %q ]; then echo 'boom password=sup3rsecretvalue' >&2; exit 1; fi\nexec %q \"$@\"\n", failSub, f.Docker())
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	m.services.containerEngine = containers.Engine{Binary: path, Kind: "docker"}
}

// Logs: default 100 lines, since passes through, an empty log says so,
// secrets are redacted and a big log keeps the newest lines.
func TestCovAIContainersLogs(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	f.Set(t, "logs.txt", "line one\nAPI_TOKEN=abcdefabcdef1234567890\nlast line\n")
	out, err := covAIRun(t, m, call, "containers_logs", map[string]any{"target": "termocode-api-1", "since": "10m"})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "logs", out, "Logs of termocode-api-1 (9f3c2e1a1234)", "[3 lines", "last line", "Engine: docker")
	if strings.Contains(out, "abcdefabcdef1234567890") {
		t.Errorf("logs leaked a token:\n%s", out)
	}
	if f.Count("--tail 100") != 1 || f.Count("--since 10m") != 1 {
		t.Errorf("logs argv: %q", f.Calls())
	}
	if _, err := covAIRun(t, m, call, "containers_logs", map[string]any{"target": "nginx", "lines": 5000}); err != nil {
		t.Fatal(err)
	}
	if f.Count("--tail 1000") != 1 {
		t.Errorf("lines are not capped at 1000: %q", f.Calls())
	}
	f.Remove("logs.txt")
	out, err = covAIRun(t, m, call, "containers_logs", map[string]any{"target": "nginx"})
	if err != nil || !strings.Contains(out, "(no log output)") || !strings.Contains(out, "[0 lines") {
		t.Errorf("empty logs = %q %v", out, err)
	}
	dir, _ := configDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"services_max_log_kb": 1}`), 0o644)
	f.Set(t, "logs.txt", strings.Repeat("old old old old\n", 200)+"NEWEST\n")
	out, err = covAIRun(t, m, call, "containers_logs", map[string]any{"target": "nginx"})
	if err != nil || !strings.Contains(out, "truncated") || !strings.Contains(out, "NEWEST") || len(out) > 1500 {
		t.Errorf("capped logs: %d bytes %v\n%.300s", len(out), err, out)
	}
	// The tool refuses a since that is not a duration before any call.
	tl := aiContainersTool(t, m.aiContainersTools(call), "containers_logs")
	if err := tl.Validate(map[string]any{"target": "nginx", "since": "yesterday"}); err == nil {
		t.Error("since yesterday accepted")
	}
	if err := tl.Validate(map[string]any{"target": "nginx", "since": "5m"}); err != nil {
		t.Errorf("since 5m refused: %v", err)
	}
	if got := tl.Describe(map[string]any{"target": "nginx"}); got != "Container logs nginx" {
		t.Errorf("describe = %q", got)
	}
}

// An engine error in the middle of a read tool reaches the model as a
// clean error with secrets masked.
func TestCovAIContainersEngineErrorsAreRedacted(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	covAIWrapDocker(t, m, f, "logs")
	_, err := covAIRun(t, m, call, "containers_logs", map[string]any{"target": "nginx"})
	if err == nil || !strings.Contains(err.Error(), "boom") || strings.Contains(err.Error(), "sup3rsecretvalue") {
		t.Errorf("logs error = %v", err)
	}
	for sub, tool := range map[string]string{"stats": "containers_stats", "images": "images_list", "volume": "volumes_list",
		"network": "networks_list", "context": "contexts_list", "ps": "containers_list"} {
		covAIWrapDocker(t, m, f, sub)
		_, err := covAIRun(t, m, call, tool, nil)
		if err == nil || strings.Contains(err.Error(), "sup3rsecretvalue") {
			t.Errorf("%s with a failing %s: %v", tool, sub, err)
		}
	}
	covAIWrapDocker(t, m, f, "ps")
	for _, tool := range []string{"containers_inspect", "containers_logs"} {
		if _, err := covAIRun(t, m, call, tool, map[string]any{"target": "nginx"}); err == nil || !strings.Contains(err.Error(), "docker: ") {
			t.Errorf("%s with a failing ps: %v", tool, err)
		}
	}
	covAIWrapDocker(t, m, f, "inspect")
	if _, err := covAIRun(t, m, call, "containers_inspect", map[string]any{"target": "nginx"}); err == nil || strings.Contains(err.Error(), "sup3rsecretvalue") {
		t.Errorf("inspect error = %v", err)
	}
}

// Stats for every running container, for one target, and when nothing runs.
func TestCovAIContainersStats(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	f.Set(t, "stats.json", `{"Container":"api-1","CPUPerc":"0.12%","MemUsage":"48.2MiB / 7.7GiB","MemPerc":"0.61%","NetIO":"1.2kB / 3.4kB","BlockIO":"0B / 8.19kB","PIDs":"12"}`+"\n")
	out, err := covAIRun(t, m, call, "containers_stats", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "stats", out, "- api-1 · cpu 0.12%", "mem 48.2MiB / 7.7GiB (0.61%)", "pids 12")
	if _, err := covAIRun(t, m, call, "containers_stats", map[string]any{"target": "termocode-api-1"}); err != nil {
		t.Fatal(err)
	}
	if f.Count("9f3c2e1a1234") == 0 {
		t.Errorf("stats did not name the target: %q", f.Calls())
	}
	if _, err := covAIRun(t, m, call, "containers_stats", map[string]any{"target": "nope"}); err == nil {
		t.Error("stats of an unknown container ran")
	}
	f.Remove("stats.json")
	out, err = covAIRun(t, m, call, "containers_stats", nil)
	if err != nil || !strings.HasSuffix(out, "No running containers.") {
		t.Errorf("no stats = %q %v", out, err)
	}
	tl := aiContainersTool(t, m.aiContainersTools(call), "containers_stats")
	if tl.Validate(map[string]any{}) != nil || tl.Validate(map[string]any{"target": "-x"}) == nil {
		t.Error("stats validate: empty must pass, -x must fail")
	}
	if tl.Describe(map[string]any{"target": "x"}) != "Container stats x" {
		t.Error("stats describe")
	}
}

// Images, volumes, networks and contexts lists name every row; dangling
// images and compose projects are marked; long lists are cut.
func TestCovAIContainersHostLists(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	out, err := covAIRun(t, m, call, "images_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "images", out, "Images:", "postgres:16", "<none> (dangling)", "created 3 weeks ago")
	out, err = covAIRun(t, m, call, "volumes_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "volumes", out, "Volumes:", "driver")
	out, err = covAIRun(t, m, call, "networks_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "networks", out, "Networks:", "driver", "scope")
	out, err = covAIRun(t, m, call, "contexts_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "contexts", out, "Contexts: 2", "* default (engine default)", "ssh://deploy@prod.example", "only the user switches")

	var imgs, vols, nets strings.Builder
	for i := 0; i < aiContainersListLimit+3; i++ {
		fmt.Fprintf(&imgs, `{"ID":"sha256:%04d","Repository":"r%d","Tag":"1","Size":"1MB"}`+"\n", i, i)
		fmt.Fprintf(&vols, `{"Name":"v%d","Driver":"local","Scope":"local","Labels":"com.docker.compose.project=p"}`+"\n", i)
		fmt.Fprintf(&nets, `{"ID":"n%d","Name":"net%d","Driver":"bridge","Scope":"local","Labels":"com.docker.compose.project=p"}`+"\n", i, i)
	}
	f.Set(t, "images.json", imgs.String())
	f.Set(t, "volume-ls.json", vols.String())
	f.Set(t, "network-ls.json", nets.String())
	for tool, want := range map[string]string{"images_list": "… 3 more", "volumes_list": "… 3 more", "networks_list": "… 3 more"} {
		out, err := covAIRun(t, m, call, tool, nil)
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("%s long list: %v\n%.200s", tool, err, out[max(0, len(out)-200):])
		}
		if tool != "images_list" && !strings.Contains(out, "project p") {
			t.Errorf("%s does not name the compose project", tool)
		}
	}

	// The session context is starred; an error of a context is shown.
	f.Set(t, "context-ls.json", `{"Name":"default","Current":"*","DockerEndpoint":"unix:///var/run/docker.sock"}`+"\n"+
		`{"Name":"prod","Current":"","DockerEndpoint":"","Error":"cannot reach\n the host"}`+"\n")
	m.services.sessionContext = "prod"
	out, err = covAIRun(t, m, call, "contexts_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "session contexts", out, "  default (engine default)", "* prod (this session)", "error: cannot reach the host")
}

// Registries: hosts and users only, never a secret; an unreadable login
// file is a clean error.
func TestCovAIContainersRegistries(t *testing.T) {
	m, _, call := aiContainersTestModel(t)
	out, err := covAIRun(t, m, call, "registries_list", nil)
	if err != nil || !strings.Contains(out, "Registries: 0") {
		t.Errorf("no login file = %q %v", out, err)
	}
	cfgDir := os.Getenv("DOCKER_CONFIG")
	cfg := `{"auths":{"ghcr.io":{"auth":"c2VjcmV0dG9rZW4="},"docker.io":{}},"credHelpers":{"gcr.io":"gcloud"},"credsStore":"desktop"}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	m.services.registryUsers = map[string]string{"ghcr.io": "amin"}
	out, err = covAIRun(t, m, call, "registries_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "registries", out, "Registries: 3", "- ghcr.io · signed in as amin", "- gcr.io · not signed in · helper gcloud", "store desktop")
	if strings.Contains(out, "c2VjcmV0dG9rZW4") {
		t.Errorf("registries leaked the auth value:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`[1,2]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := covAIRun(t, m, call, "registries_list", nil); err == nil || !strings.Contains(err.Error(), "cannot read config.json") {
		t.Errorf("bad login file: %v", err)
	}
}

// No engine, no Containers view: every tool says so plainly.
func TestCovAIContainersNoEngine(t *testing.T) {
	m, _, call := aiContainersTestModel(t)
	t.Setenv("PATH", t.TempDir())
	for _, tool := range []string{"containers_list", "images_list", "volumes_list", "networks_list", "contexts_list", "registries_list", "containers_stats"} {
		if _, err := covAIRun(t, m, call, tool, nil); err == nil || !strings.Contains(err.Error(), "no container engine installed") {
			t.Errorf("%s without an engine: %v", tool, err)
		}
	}
	noView := func(ctx context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(&Model{})
		return out, err
	}
	if _, err := covAIRun(t, m, noView, "images_list", nil); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Errorf("no view: %v", err)
	}
	if _, err := covAIRun(t, m, noView, "containers_inspect", map[string]any{"target": "postgres:16", "kind": "image"}); err == nil {
		t.Error("inspect without a view ran")
	}
	if got := aiPullDescribe(noView, map[string]any{"ref": "nginx:1.27"}); !strings.Contains(got, "refused: the Containers view is not available") {
		t.Errorf("pull card without a view = %q", got)
	}
	if _, err := covAIRun(t, m, noView, "images_pull", map[string]any{"ref": "nginx:1.27"}); err == nil {
		t.Error("pull without a view ran")
	}
	if _, err := covAIRun(t, m, noView, "compose_up", map[string]any{"project": "termocode"}); err == nil {
		t.Error("compose without a view ran")
	}
	// Always allow on a model without the view is a no-op, not a crash.
	(&Model{}).aiContainersAllowSession()
	(&Model{services: &servicesState{}}).aiContainersAllowSession()
}

// Inspect of an image, volume or network goes by kind; bad kinds and
// targets are refused before any call.
func TestCovAIContainersInspectKinds(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	f.Set(t, "inspect.json", `[{"Id":"sha256:111","RepoTags":["postgres:16"]}]`)
	out, err := covAIRun(t, m, call, "containers_inspect", map[string]any{"target": "postgres:16", "kind": "image"})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "image inspect", out, "Inspect image postgres:16", "postgres:16")
	if f.Count("inspect --type image postgres:16") != 1 {
		t.Errorf("inspect argv: %q", f.Calls())
	}
	for _, ok := range [][2]string{{"image", "sha256:abcdef01"}, {"image", "abcd1234"}, {"volume", "pgdata"}, {"network", "bridge"}, {"", "nginx"}} {
		if err := aiValidInspectTarget(ok[0], ok[1]); err != nil {
			t.Errorf("%v refused: %v", ok, err)
		}
	}
	for _, bad := range [][2]string{{"", ""}, {"image", "-q"}, {"image", "NOPE!"}, {"secret", "x"}, {"volume", "a b"}} {
		if err := aiValidInspectTarget(bad[0], bad[1]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if _, err := covAIRun(t, m, call, "containers_inspect", map[string]any{"target": "x", "kind": "secret"}); err == nil {
		t.Error("inspect with a bad kind ran")
	}
	if _, err := covAIRun(t, m, call, "containers_inspect", map[string]any{"target": "nope"}); err == nil {
		t.Error("inspect of an unknown container ran")
	}
	if got := aiContainersTool(t, m.aiContainersTools(call), "containers_inspect").Describe(map[string]any{"target": "x"}); got != "Inspect x" {
		t.Errorf("describe = %q", got)
	}
}

// Finding a container: full id, name, unique hex prefix; an ambiguous
// prefix, an empty host and a long name list each explain themselves.
func TestCovAIFindContainer(t *testing.T) {
	list := []containers.Container{{ID: "abcd1111", Names: []string{"/one"}}, {ID: "abcd2222", Names: []string{"two"}}, {ID: "ffff0000"}}
	if c, err := aiFindContainer(list, "ffff"); err != nil || c.ID != "ffff0000" {
		t.Errorf("unique prefix: %v %v", c, err)
	}
	if c, err := aiFindContainer(list, "one"); err != nil || c.ID != "abcd1111" {
		t.Errorf("by name: %v %v", c, err)
	}
	if _, err := aiFindContainer(list, "abcd"); err == nil || !strings.Contains(err.Error(), "matches 2 containers") {
		t.Errorf("ambiguous prefix: %v", err)
	}
	if _, err := aiFindContainer(nil, "x"); err == nil || !strings.Contains(err.Error(), "the host has no containers") {
		t.Errorf("empty host: %v", err)
	}
	var many []containers.Container
	for i := 0; i < 25; i++ {
		many = append(many, containers.Container{ID: fmt.Sprintf("%02dzz", i), Names: []string{fmt.Sprintf("c%02d", i)}})
	}
	if _, err := aiFindContainer(many, "nope"); err == nil || !strings.Contains(err.Error(), "c19, …") || strings.Contains(err.Error(), "c24") {
		t.Errorf("long list: %v", err)
	}
	if aiContainerName(containers.Container{ID: "0123456789abcdef"}) != "0123456789ab" || aiContainerName(containers.Container{ID: "abc"}) != "abc" {
		t.Error("name falls back to the short id")
	}
	if isHex("") || isHex("xyz") || !isHex("09af") {
		t.Error("isHex")
	}
}

// No-op actions are refused with a reason; real changes pass.
func TestCovAIControlNoop(t *testing.T) {
	for _, c := range []struct{ action, state, want string }{
		{"start", "running", "already running"}, {"start", "paused", "use unpause"}, {"start", "exited", ""},
		{"stop", "exited", "already stopped"}, {"stop", "dead", "already stopped"}, {"stop", "running", ""},
		{"pause", "paused", "already paused"}, {"pause", "exited", "not running (exited)"}, {"pause", "running", ""},
		{"unpause", "running", "not paused"}, {"unpause", "paused", ""},
		{"restart", "paused", "use unpause"}, {"restart", "running", ""},
	} {
		got := aiControlNoop(c.action, c.state)
		if (c.want == "" && got != "") || !strings.Contains(got, c.want) {
			t.Errorf("%s on %s = %q, want %q", c.action, c.state, got, c.want)
		}
	}
}

// A busy container is refused before the card; a failing stop reaches
// the model redacted and the busy mark is cleared.
func TestCovAIControlBusyAndFailure(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	ctl := aiContainersTool(t, m.aiContainersTools(call), "containers_control")
	a := map[string]any{"action": "stop", "target": "termocode-api-1"}
	m.services.busy[containerBusyPrefix+"9f3c2e1a1234"] = services.Stopping
	if err := ctl.Validate(a); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Errorf("busy container: %v", err)
	}
	if got := ctl.Describe(a); !strings.Contains(got, "refused:") || !strings.Contains(got, "busy") {
		t.Errorf("busy card = %q", got)
	}
	delete(m.services.busy, containerBusyPrefix+"9f3c2e1a1234")
	covAIWrapDocker(t, m, f, "stop")
	_, err := ctl.Run(context.Background(), a)
	if err == nil || !strings.Contains(err.Error(), "stop termocode-api-1 failed") || strings.Contains(err.Error(), "sup3rsecretvalue") {
		t.Errorf("failed stop: %v", err)
	}
	if len(m.services.busy) != 0 {
		t.Errorf("busy mark left: %v", m.services.busy)
	}
	// Start, pause and restart run their own verbs.
	m.services.containerEngine = containers.Engine{}
	for _, c := range [][2]string{{"start", "nginx"}, {"pause", "termocode-api-1"}, {"restart", "termocode-api-1"}} {
		out, err := ctl.Run(context.Background(), map[string]any{"action": c[0], "target": c[1]})
		if err != nil || !strings.Contains(out, "ran `docker "+c[0]) {
			t.Errorf("%s: %q %v", c[0], out, err)
		}
	}
	if got := aiControlDescribe(nil, map[string]any{"action": "kill", "target": "x"}); got != "Control container x" {
		t.Errorf("nil caller card = %q", got)
	}
}

// The list report: ports from the Ports array, the compose service, an
// unknown state, a long list and a host with no published ports.
func TestCovAIContainersListReport(t *testing.T) {
	c := containers.Container{ID: "abc", Names: []string{"web"}, Ports: []containers.Port{{PublicPort: 8080, PrivatePort: 80, Type: "tcp"}, {PrivatePort: 9000}},
		Labels: map[string]string{"com.docker.compose.service": "web"}}
	line := aiContainerLine(c)
	covAIMustContain(t, "line", line, "unknown", "ports 8080->80/tcp", "service web")
	if l := aiContainerLine(containers.Container{ID: "x", State: "exited", Ports: []containers.Port{{PrivatePort: 80}}}); strings.Contains(l, "ports") {
		t.Errorf("unpublished port shown: %q", l)
	}
	var list []containers.Container
	for i := 0; i < aiContainersListLimit+2; i++ {
		list = append(list, containers.Container{ID: fmt.Sprintf("id%d", i), Names: []string{fmt.Sprintf("c%d", i)}, State: "exited"})
	}
	out := aiContainersListReport("Engine: docker", "", list)
	covAIMustContain(t, "report", out, "Containers: 202 (0 running)", "… 2 more containers not shown", "No published host ports.")
	out = aiContainersListReport("Engine: docker", "", []containers.Container{{ID: "a", Names: []string{"a"}, State: "running", PortsText: "0.0.0.0:5432->5432"}})
	covAIMustContain(t, "report", out, "port 5432 → a (5432/tcp, running)")
	if got := aiCap("short", 1, true); got != "short" {
		t.Errorf("aiCap short = %q", got)
	}
	if got := aiCap(strings.Repeat("a\n", 2000), 1, true); !strings.HasPrefix(got, "… [truncated") {
		t.Errorf("aiCap keepEnd = %.60q", got)
	}
}

// The context label: podman connections, DOCKER_CONTEXT and the default.
func TestCovAIContainersContextLabel(t *testing.T) {
	ctx := context.Background()
	f := hostFake(t)
	t.Setenv("DOCKER_CONTEXT", "")
	pod := containers.Engine{Kind: "podman", Binary: f.InstallPodman(t), X: servicesExec}
	if got := (aiContainersHost{e: pod, session: "remote"}).contextLabel(ctx); got != "connection remote (this session)" {
		t.Errorf("podman session = %q", got)
	}
	t.Setenv("CONTAINER_CONNECTION", "box")
	if got := (aiContainersHost{e: pod}).contextLabel(ctx); got != "connection box (CONTAINER_CONNECTION)" {
		t.Errorf("podman env = %q", got)
	}
	t.Setenv("CONTAINER_CONNECTION", "")
	// Podman runs locally on Linux only; elsewhere it talks to a
	// machine, named like a docker context (none current here).
	wantLocal := "local podman"
	if runtime.GOOS != "linux" {
		wantLocal = "context default"
	}
	if got := (aiContainersHost{e: pod}).contextLabel(ctx); got != wantLocal {
		t.Errorf("podman local = %q, want %q", got, wantLocal)
	}
	dock := containers.Engine{Kind: "docker", Binary: f.Docker(), X: servicesExec}
	t.Setenv("DOCKER_CONTEXT", "staging")
	if got := (aiContainersHost{e: dock}).contextLabel(ctx); got != "context staging (DOCKER_CONTEXT)" {
		t.Errorf("env context = %q", got)
	}
	t.Setenv("DOCKER_CONTEXT", "")
	f.Set(t, "context-ls.json", `{"Name":"default","Current":"","DockerEndpoint":"unix:///x"}`+"\n")
	if got := (aiContainersHost{e: dock}).contextLabel(ctx); got != "context default" {
		t.Errorf("no current context = %q", got)
	}
	h := aiContainersHost{e: containers.Engine{Binary: "/usr/bin/podman"}}
	if h.bin() != "podman" || !strings.HasPrefix(h.header(ctx), "Engine: podman") {
		t.Errorf("bin from path = %q %q", h.bin(), h.header(ctx))
	}
}

// Podman contexts are called connections.
func TestCovAIContainersPodmanConnections(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	m.services.containerEngine = containers.Engine{Binary: f.InstallPodman(t), Kind: "podman"}
	f.Set(t, "connection-ls.json", `[{"Name":"box","URI":"ssh://core@box","Default":true}]`)
	out, err := covAIRun(t, m, call, "contexts_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "podman", out, "Podman connections:", "Engine: podman")
}

// images_pull: the card names the engine and the context; a pull already
// running in its tab is focused, not started twice; without the editor
// the tab cannot open and the model is told.
func TestCovAIPullFlows(t *testing.T) {
	m, _, call := aiContainersTestModel(t)
	pull := aiContainersTool(t, m.aiContainersTools(call), "images_pull")
	if err := pull.Validate(map[string]any{"ref": "nginx:1.27"}); err != nil {
		t.Fatal(err)
	}
	if got := pull.Describe(map[string]any{"ref": "nginx:1.27"}); got != "Pull image nginx:1.27 (docker pull nginx:1.27) in a terminal tab · context default" {
		t.Errorf("pull card = %q", got)
	}
	if got := pull.Describe(map[string]any{"ref": "-q"}); !strings.Contains(got, "refused:") {
		t.Errorf("bad ref card = %q", got)
	}
	if got := aiPullDescribe(nil, map[string]any{"ref": "nginx"}); got != "Pull image nginx" {
		t.Errorf("nil caller card = %q", got)
	}
	if _, err := pull.Run(context.Background(), map[string]any{"ref": "-q"}); err == nil {
		t.Error("bad ref ran")
	}
	_, err := pull.Run(context.Background(), map[string]any{"ref": "nginx:1.27"})
	if err == nil || !strings.Contains(err.Error(), "cannot open the pull tab") {
		t.Errorf("pull without the editor: %v", err)
	}
	key := containerTabPrefix + "pull:nginx:1.27"
	m.terminalTabs = []terminalTab{{Service: key, Running: true, BufID: 3}}
	out, err := pull.Run(context.Background(), map[string]any{"ref": "nginx:1.27"})
	if err != nil || out != "already pulling nginx:1.27 in its tab (no change)" {
		t.Errorf("second pull = %q %v", out, err)
	}
}

// compose_up: services must be names, the project must be on the host
// and its folder must exist; the card shows the exact command.
func TestCovAIComposeFlows(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	dir := t.TempDir()
	f.Set(t, "ps.json", `{"ID":"9f3c2e1a1234","Names":"api-1","Image":"api","State":"exited","Labels":"com.docker.compose.project=shop,com.docker.compose.project.working_dir=`+dir+`,com.docker.compose.project.config_files=`+filepath.Join(dir, "compose.yml")+`"}`+"\n")
	up := aiContainersTool(t, m.aiContainersTools(call), "compose_up")
	a := map[string]any{"project": "shop", "services": []any{"api"}}
	if err := up.Validate(a); err != nil {
		t.Fatalf("validate: %v", err)
	}
	got := up.Describe(a)
	covAIMustContain(t, "compose card", got, "Start compose project shop (docker compose", "-p shop", "up -d api", "context default")
	if _, err := up.Run(context.Background(), a); err == nil || !strings.Contains(err.Error(), "cannot open the compose tab") {
		t.Errorf("compose without the editor: %v", err)
	}
	for _, bad := range []map[string]any{
		{"project": "shop", "services": "api"},
		{"project": "shop", "services": []string{"-x"}},
		{"project": "nope"},
	} {
		if _, err := aiComposeResolve(context.Background(), call, bad); err == nil {
			t.Errorf("compose %v accepted", bad)
		}
	}
	if tr, err := aiComposeResolve(context.Background(), call, map[string]any{"project": "shop", "services": []string{"api", "db"}}); err != nil || len(tr.services) != 2 || len(tr.files) != 1 {
		t.Errorf("string services: %+v %v", tr, err)
	}
	if got := aiComposeDescribe(call, map[string]any{"project": "nope"}); !strings.Contains(got, "refused: no compose project \"nope\"") {
		t.Errorf("refused card = %q", got)
	}
	if got := aiComposeDescribe(nil, map[string]any{"project": "shop"}); got != "Start compose project shop" {
		t.Errorf("nil caller card = %q", got)
	}
	// This workspace's own project counts even with no containers; a
	// missing folder is refused.
	f.Set(t, "ps.json", "")
	m.services.project = "termocode"
	if _, err := aiComposeResolve(context.Background(), call, map[string]any{"project": "termocode"}); err != nil {
		t.Errorf("own project: %v", err)
	}
	if _, err := aiComposeResolve(context.Background(), call, map[string]any{"project": "shop"}); err == nil || !strings.Contains(err.Error(), "projects: none") {
		t.Errorf("no projects: %v", err)
	}
	f.Set(t, "ps.json", `{"ID":"aa","Names":"x","State":"exited","Labels":"com.docker.compose.project=gone,com.docker.compose.project.working_dir=/no/such/dir"}`+"\n")
	if _, err := aiComposeResolve(context.Background(), call, map[string]any{"project": "gone"}); err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Errorf("missing folder: %v", err)
	}
	covAIWrapDocker(t, m, f, "ps")
	if _, err := aiComposeResolve(context.Background(), call, map[string]any{"project": "gone"}); err == nil {
		t.Error("ps failure accepted")
	}
}
