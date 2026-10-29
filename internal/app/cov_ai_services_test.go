package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// The Services AI tools (ai_services_tools.go) on a real project folder
// and the fake docker: a tiny event loop runs the commands the tools
// start and feeds their answers back, like the Bubble Tea loop.

type covSvcEnv struct {
	m    *Model
	f    *servicestest.Fake
	root string
	call aiIDECaller
	mu   *sync.Mutex
}

const covComposeYAML = "services:\n  api:\n    build: .\n  mailhog:\n    image: mailhog/mailhog\n  postgres:\n    image: postgres:16\n  redis:\n    image: redis:7\n"

func covSvcSetup(t *testing.T) *covSvcEnv {
	t.Helper()
	f := servicestest.New(t)
	t.Setenv("PATH", f.PathEnv())
	t.Setenv("DOCKER_CONTEXT", "")
	for name, src := range map[string]string{"config.json": "config.json", "ps.json": "ps-lines.json"} {
		b, err := os.ReadFile(filepath.Join("..", "services", "testdata", src))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	root := t.TempDir()
	for name, body := range map[string]string{"compose.yaml": covComposeYAML, "Procfile": "web: npm run dev\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st := newServicesState()
	st.opened, st.sessLoaded, st.root = true, true, root
	m := &Model{w: 120, h: 40, services: st}
	e := &covSvcEnv{m: m, f: f, root: root, mu: &sync.Mutex{}}
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			select {
			case <-done:
				return
			default:
			}
			if b, ok := msg.(tea.BatchMsg); ok {
				for _, x := range b {
					run(x)
				}
				return
			}
			e.mu.Lock()
			next, _ := e.m.handleServicesMsg(msg)
			e.mu.Unlock()
			run(next)
		}()
	}
	e.call = func(ctx context.Context, fn aiIDEFunc) (string, error) {
		e.mu.Lock()
		out, cmd, err := fn(e.m)
		e.mu.Unlock()
		run(cmd)
		return out, err
	}
	return e
}

func (e *covSvcEnv) tool(t *testing.T, name string) ai.Tool {
	t.Helper()
	tl, ok := ai.FindTool(e.m.aiServicesTools(e.call), name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	return tl
}

func (e *covSvcEnv) run(t *testing.T, name string, a map[string]any) (string, error) {
	t.Helper()
	if a == nil {
		a = map[string]any{}
	}
	return e.tool(t, name).Run(context.Background(), a)
}

// services_list reads the project fresh: compose services with their
// source line, the local command, env names only.
func TestCovAIServicesList(t *testing.T) {
	e := covSvcSetup(t)
	out, err := e.run(t, "services_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "services_list", out, `Docker Compose: files compose.yaml, project "termocode"`, "- postgres [compose] image postgres:16",
		"source compose.yaml:6", "Local commands", `- web [local]`, `command "npm run dev"; source Procfile`, "env keys POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER")
	if strings.Contains(out, "hunter2hunter2") {
		t.Errorf("services_list leaked a value:\n%s", out)
	}
	// A broken services.json and a compose error are named.
	if err := os.MkdirAll(filepath.Join(e.root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(e.root, ".termocode", "services.json"), []byte("{broken"), 0o644)
	e.f.Set(t, "ps.err", "Cannot connect to the Docker daemon at unix:///var/run/docker.sock")
	out, err = e.run(t, "services_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "broken services_list", out, "Problem:", "Local commands (each runs in its own terminal tab):\n(")
}

// An empty folder has no services: list says so, validate refuses.
func TestCovAIServicesNoServices(t *testing.T) {
	e := covSvcSetup(t)
	for _, n := range []string{"compose.yaml", "Procfile"} {
		_ = os.Remove(filepath.Join(e.root, n))
	}
	out, err := e.run(t, "services_list", nil)
	if err != nil || !strings.HasPrefix(out, "This project has no services") {
		t.Errorf("empty project: %q %v", out, err)
	}
	if err := e.tool(t, "services_start").Validate(map[string]any{"all": true}); err == nil || !strings.Contains(err.Error(), "no services") {
		t.Errorf("start all in an empty project: %v", err)
	}
	if e.m.aiServicesPromptLine(e.root) != "" {
		t.Error("prompt line without services")
	}
}

// Validate: name or all; all only where allowed; unknown names list the
// known ones.
func TestCovAIServicesValidate(t *testing.T) {
	e := covSvcSetup(t)
	start, restart := e.tool(t, "services_start"), e.tool(t, "services_restart")
	if err := restart.Validate(map[string]any{"all": true}); err == nil || err.Error() != "pass one service name" {
		t.Errorf("restart all: %v", err)
	}
	if err := restart.Validate(map[string]any{}); err == nil || err.Error() != "pass name (see services_list)" {
		t.Errorf("restart nothing: %v", err)
	}
	if err := start.Validate(map[string]any{}); err == nil || !strings.Contains(err.Error(), "or all: true") {
		t.Errorf("start nothing: %v", err)
	}
	if err := start.Validate(map[string]any{"name": "nope"}); err == nil || !strings.Contains(err.Error(), "known: api, mailhog, postgres, redis, web") {
		t.Errorf("unknown: %v", err)
	}
	for _, a := range []map[string]any{{"name": "web"}, {"name": "redis"}, {"all": true}} {
		if err := start.Validate(a); err != nil {
			t.Errorf("%v refused: %v", a, err)
		}
	}
	st := e.tool(t, "services_status")
	if st.Validate(map[string]any{}) != nil || st.Validate(map[string]any{"name": "nope"}) == nil {
		t.Error("status validate")
	}
	logs := e.tool(t, "services_logs")
	if logs.Validate(map[string]any{"name": "all"}) != nil || logs.Validate(map[string]any{"name": "api", "since": "later"}) == nil {
		t.Error("logs validate")
	}
	if got := st.Describe(map[string]any{"name": "x"}) + "|" + logs.Describe(map[string]any{"name": "x"}) + "|" + e.tool(t, "services_list").Describe(nil); got != "Service status x|Service logs x|List services" {
		t.Errorf("describe = %q", got)
	}
}

// services_status: all rows, one row, a wait that settles at once and a
// wait that times out.
func TestCovAIServicesStatus(t *testing.T) {
	e := covSvcSetup(t)
	out, err := e.run(t, "services_status", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "status", out, "- postgres [compose]: healthy, health healthy", "- api [compose]: exited, exit code 1", "- web [local]")
	out, err = e.run(t, "services_status", map[string]any{"name": "api", "wait_ms": 5000})
	if err != nil || !strings.HasPrefix(out, "- api [compose]: exited") {
		t.Errorf("settled wait: %q %v", out, err)
	}
	ps, _ := os.ReadFile(filepath.Join("..", "services", "testdata", "ps-lines.json"))
	e.f.Set(t, "ps.json", strings.Replace(string(ps), `"State":"running","Status":"Up 2 hours"}`, `"State":"restarting","Status":"Restarting (1) 1 second ago"}`, 1))
	out, err = e.run(t, "services_status", map[string]any{"name": "redis", "wait_ms": 600})
	if err != nil || !strings.HasPrefix(out, "still not settled after 600ms") {
		t.Errorf("timed-out wait: %q %v", out, err)
	}
	e.f.Set(t, "ps.err", "Cannot connect to the Docker daemon")
	out, err = e.run(t, "services_status", nil)
	if err != nil || !strings.Contains(out, "Problem:") {
		t.Errorf("daemon down: %q %v", out, err)
	}
}

// services_logs: one compose service, all of them, a local service that
// never ran, the size cap that keeps the newest lines, and a compose
// error.
func TestCovAIServicesLogs(t *testing.T) {
	e := covSvcSetup(t)
	e.f.Set(t, "logs-api.txt", "boot\nDATABASE_URL=postgres://app:hunter2hunter2@localhost:5432/app\nready\n")
	out, err := e.run(t, "services_logs", map[string]any{"name": "api", "since": "10m"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "[3 lines, ") || strings.Contains(out, "hunter2hunter2") || !strings.Contains(out, "ready") {
		t.Errorf("api logs:\n%s", out)
	}
	if _, err := e.run(t, "services_logs", map[string]any{"name": "all", "lines": 5000}); err != nil {
		t.Errorf("all logs: %v", err)
	}
	if e.f.Count("--tail 1000") == 0 {
		t.Errorf("lines not capped: %q", e.f.Calls())
	}
	out, err = e.run(t, "services_logs", map[string]any{"name": "redis"})
	if err != nil || !strings.HasSuffix(out, "(no log output)") {
		t.Errorf("empty logs: %q %v", out, err)
	}
	if _, err := e.run(t, "services_logs", map[string]any{"name": "web"}); err == nil || !strings.Contains(err.Error(), "has not run in this session") {
		t.Errorf("local without a tab: %v", err)
	}
	if _, err := e.run(t, "services_logs", map[string]any{"name": "nope"}); err == nil || !strings.Contains(err.Error(), "unknown service") {
		t.Errorf("unknown: %v", err)
	}
	dir, _ := configDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"services_max_log_kb": 1}`), 0o644)
	e.f.Set(t, "logs-api.txt", strings.Repeat("older line older line\n", 200)+"NEWEST\n")
	out, err = e.run(t, "services_logs", map[string]any{"name": "api"})
	if err != nil || !strings.Contains(out, "… [truncated, ") || !strings.HasSuffix(out, "NEWEST") || len(out) > 1200 {
		t.Errorf("capped logs: %d bytes %v", len(out), err)
	}
	e.f.Set(t, "logs.err", "no such service")
	if _, err := e.run(t, "services_logs", map[string]any{"name": "api"}); err == nil {
		t.Error("a failing compose logs gave no error")
	}
	_ = os.Remove(filepath.Join(e.root, "compose.yaml"))
	if _, err := e.run(t, "services_logs", map[string]any{"name": "all"}); err == nil || !strings.Contains(err.Error(), "Docker Compose is not available") {
		t.Errorf("all logs without a compose file: %v", err)
	}
}

// Stop: one service runs `compose stop` and reports the new state; a
// failing stop reaches the model; stop all also covers the local ones.
func TestCovAIServicesStop(t *testing.T) {
	e := covSvcSetup(t)
	if _, err := e.run(t, "services_list", nil); err != nil {
		t.Fatal(err)
	}
	out, err := e.run(t, "services_stop", map[string]any{"name": "redis"})
	if err != nil || !strings.HasPrefix(out, "stopped. Now:\n- redis [compose]") {
		t.Errorf("stop redis: %q %v", out, err)
	}
	if e.f.Count("stop redis") != 1 {
		t.Errorf("stop argv: %q", e.f.Calls())
	}
	out, err = e.run(t, "services_stop", map[string]any{"all": true})
	if err != nil || !strings.Contains(out, "- web [local]") || !strings.Contains(out, "- postgres [compose]") {
		t.Errorf("stop all: %q %v", out, err)
	}
	e.f.Set(t, "stop.err", "boom: container is wedged")
	if _, err := e.run(t, "services_stop", map[string]any{"name": "postgres"}); err == nil || !strings.Contains(err.Error(), "wedged") {
		t.Errorf("failing stop: %v", err)
	}
	if got := aiServicesDescribe("stop", map[string]any{}); got != "Stop all services" {
		t.Errorf("describe = %q", got)
	}
	// A local service that is not running: nothing to stop.
	out, err = e.run(t, "services_stop", map[string]any{"name": "web"})
	if err != nil || !strings.HasSuffix(out, "(no change)") {
		t.Errorf("stop a stopped local service: %q %v", out, err)
	}
	// The chat stops while the stop waits.
	ctx, cancel := context.WithCancel(context.Background())
	e.f.Remove("stop.err")
	e.f.Set(t, "stop.sleep", "5")
	cancel()
	if _, err := aiServicesStop(ctx, e.call, "redis", false); err != errAIChatStopped {
		t.Errorf("stopped chat: %v", err)
	}
}

// Restart: compose `restart`, then a second look 2 s on names a service
// that died at once.
func TestCovAIServicesRestart(t *testing.T) {
	e := covSvcSetup(t)
	if _, err := e.run(t, "services_list", nil); err != nil {
		t.Fatal(err)
	}
	out, err := e.run(t, "services_restart", map[string]any{"name": "api"})
	if err != nil || !strings.HasPrefix(out, `restarted, but api exited (1) 2 s after restart — read services_logs(name: "api").`) {
		t.Errorf("restart api: %q %v", out, err)
	}
	if e.f.Count("restart api") != 1 {
		t.Errorf("restart argv: %q", e.f.Calls())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := aiServicesRestart(ctx, e.call, "redis"); err != errAIChatStopped {
		t.Errorf("stopped chat: %v", err)
	}
	e.f.Set(t, "restart.err", "boom")
	if _, err := e.run(t, "services_restart", map[string]any{"name": "redis"}); err == nil {
		t.Error("a failing restart gave no error")
	}
}

// Start: a running service is "no change"; compose up is refused when
// another up runs; a broken compose file says compose is not available.
func TestCovAIServicesStartNotes(t *testing.T) {
	e := covSvcSetup(t)
	if _, err := e.run(t, "services_list", nil); err != nil {
		t.Fatal(err)
	}
	out, err := e.run(t, "services_start", map[string]any{"name": "redis"})
	if err != nil || out != `service "redis" is already running (no change)` {
		t.Errorf("start a running service: %q %v", out, err)
	}
	e.m.terminalTabs = []terminalTab{{Service: svcLocalTab("web"), Running: true}, {Service: svcTabUp, Running: true}}
	out, err = e.run(t, "services_start", map[string]any{"name": "web"})
	if err != nil || out != "web is already running (no change)" {
		t.Errorf("start a running local service: %q %v", out, err)
	}
	out, err = e.run(t, "services_start", map[string]any{"all": true})
	if err != nil || !strings.Contains(out, "still running in the services: up tab (no change)") {
		t.Errorf("start all while up runs: %q %v", out, err)
	}
	e.m.terminalTabs = nil
	e.m.services.compose = nil
	if _, err := aiServicesStart(context.Background(), e.call, "", true); err == nil || err.Error() != "no Docker Compose services here" {
		t.Errorf("start all without compose: %v", err)
	}
}

// Trust: "Always in this project" stops the questions; AI: Services
// Trust… can forget it.
func TestCovAIServicesTrust(t *testing.T) {
	e := covSvcSetup(t)
	m := e.m
	if m.openServicesTrust() == nil || m.pickerOpen {
		t.Error("not allowed: the trust picker must not open, a toast says why")
	}
	m.aiServicesAllowProject()
	if !m.services.aiAllowed.Load() {
		t.Fatal("not allowed after Always in this project")
	}
	m.openServicesTrust()
	if !m.pickerOpen {
		t.Error("the trust picker did not open")
	}
	if m.handleServicesTrustSelect("svc-trust:keep") != nil || !m.services.aiAllowed.Load() {
		t.Error("keep changed the trust")
	}
	if m.handleServicesTrustSelect("svc-trust:forget") == nil || m.services.aiAllowed.Load() {
		t.Error("forget did not forget")
	}
	if !strings.Contains(m.aiServicesPromptLine(e.root), "Services tools") {
		t.Error("no prompt line with services")
	}
	if got := aiRedactResults([]ai.Tool{{Name: "x"}}); got[0].Run != nil {
		t.Error("a tool without Run got one")
	}
	if svcAbs("/a", "/b/c") != "/b/c" || svcAbs("/a", "b") != filepath.Join("/a", "b") {
		t.Error("svcAbs")
	}
	// A refresh in flight queues the next one.
	m.services.inflight = true
	seq := m.services.reqSeq
	if got, cmd := m.servicesRefreshTarget(); got != seq+1 || cmd != nil || !m.services.pending {
		t.Errorf("in-flight refresh target = %d %v", got, cmd != nil)
	}
}
