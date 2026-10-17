package services_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

func newCompose(t *testing.T) (*services.Compose, *servicestest.Fake) {
	t.Helper()
	f := servicestest.New(t)
	cfg, _ := os.ReadFile("testdata/config.json")
	ps, _ := os.ReadFile("testdata/ps-lines.json")
	f.Set(t, "config.json", string(cfg))
	f.Set(t, "ps.json", string(ps))
	c := &services.Compose{Root: t.TempDir(), Files: []string{"compose.yaml"}, Profiles: []string{"dev"},
		Docker: f.Docker(), X: services.SpawnExec{}}
	return c, f
}

func TestComposeLoadAndStatus(t *testing.T) {
	c, f := newCompose(t)
	ctx := context.Background()
	cfg, p := c.Load(ctx)
	if p.Kind != services.ProblemNone || cfg.Project != "termocode" || len(cfg.Services) != 4 {
		t.Fatalf("Load = %+v, %+v", cfg, p)
	}
	st, p := c.Status(ctx)
	if p.Kind != services.ProblemNone || st["postgres"].State != services.Healthy {
		t.Fatalf("Status = %+v, %+v", st, p)
	}
	calls := f.Calls()
	if len(calls) != 2 ||
		calls[0] != "compose -f compose.yaml --profile dev config --format json" ||
		calls[1] != "compose -f compose.yaml --profile dev ps --all --format json" {
		t.Errorf("calls = %q", calls)
	}
}

func TestComposeProblems(t *testing.T) {
	c, f := newCompose(t)
	f.Set(t, "all.err", "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?\n")
	if _, p := c.Status(context.Background()); p.Kind != services.ProblemDaemonDown {
		t.Errorf("daemon down → %+v", p)
	}
	f.Remove("all.err")
	f.Set(t, "config.err", "yaml: line 5: mapping values are not allowed in this context\n")
	f.Set(t, "config.code", "15")
	if _, p := c.Load(context.Background()); p.Kind != services.ProblemBadConfig || !strings.Contains(p.Detail, "line 5") {
		t.Errorf("bad config → %+v", p)
	}
	c.Docker = "/nonexistent/docker"
	if _, p := c.Status(context.Background()); p.Kind != services.ProblemNoDocker {
		t.Errorf("missing binary → %+v", p)
	}
}

func TestComposeStopRestartLogs(t *testing.T) {
	c, f := newCompose(t)
	ctx := context.Background()
	if err := c.Stop(ctx, []string{"postgres", "redis"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Restart(ctx, []string{"api"}); err != nil {
		t.Fatal(err)
	}
	f.Set(t, "logs-api.txt", "api-1 | dial tcp 127.0.0.1:5432: connect: connection refused\n")
	out, err := c.Logs(ctx, "api", 60, 10*time.Minute)
	if err != nil || !strings.Contains(out, "connection refused") {
		t.Fatalf("Logs = %q, %v", out, err)
	}
	all, _ := c.Logs(ctx, "", 50, 0)
	_ = all
	calls := f.Calls()
	want := []string{
		"compose -f compose.yaml --profile dev stop postgres redis",
		"compose -f compose.yaml --profile dev restart api",
		"compose -f compose.yaml --profile dev logs --no-color --timestamps --tail 60 --since 10m0s api",
		"compose -f compose.yaml --profile dev logs --no-color --timestamps --tail 50",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls =\n%s\nwant\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
	f.Set(t, "stop.err", "no such service: nope\n")
	if err := c.Stop(ctx, []string{"nope"}); err == nil || !strings.Contains(err.Error(), "no such service") {
		t.Errorf("stop error = %v", err)
	}
}

func TestComposeVisibleCommands(t *testing.T) {
	c, _ := newCompose(t)
	up := c.Up([]string{"postgres"})
	if up.Label != "services: up" || strings.Join(up.Argv[1:], " ") != "compose -f compose.yaml --profile dev up -d postgres" || up.Dir != c.Root {
		t.Errorf("Up = %+v", up)
	}
	if all := c.Up(nil); strings.Join(all.Argv[1:], " ") != "compose -f compose.yaml --profile dev up -d" {
		t.Errorf("Up all = %+v", all)
	}
	lf := c.LogsFollow("postgres")
	if lf.Label != "logs: postgres" || strings.Join(lf.Argv[1:], " ") != "compose -f compose.yaml --profile dev logs -f --tail 200 postgres" {
		t.Errorf("LogsFollow = %+v", lf)
	}
	if la := c.LogsFollow(""); la.Label != "logs: compose" || !strings.HasSuffix(strings.Join(la.Argv, " "), "logs -f --tail 100") {
		t.Errorf("LogsFollow all = %+v", la)
	}
	sh := c.Shell("api")
	if sh.Label != "shell: api" || strings.Join(sh.Argv[1:], " ") != "compose -f compose.yaml --profile dev exec api sh" {
		t.Errorf("Shell = %+v", sh)
	}
}

// A cancelled call kills the whole process group: the script and its
// sleep child.
func TestComposeCancelKillsGroup(t *testing.T) {
	c, f := newCompose(t)
	f.Set(t, "ps.sleep", "30")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Status(ctx)
		close(done)
	}()
	if !servicestest.WaitFor(5*time.Second, func() bool { return f.PID("ps.child") > 0 }) {
		t.Fatal("the fake never started its child")
	}
	pid, child := f.PID("ps.pid"), f.PID("ps.child")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Status did not return after cancel")
	}
	if !servicestest.WaitGone(pid, 3*time.Second) || !servicestest.WaitGone(child, 3*time.Second) {
		t.Errorf("leftover processes: script %d alive=%v, child %d alive=%v", pid, servicestest.Alive(pid), child, servicestest.Alive(child))
	}
}

func TestComposeTimeout(t *testing.T) {
	c, f := newCompose(t)
	c.Timeout = 200 * time.Millisecond
	f.Set(t, "ps.sleep", "30")
	start := time.Now()
	_, p := c.Status(context.Background())
	if time.Since(start) > 5*time.Second {
		t.Fatal("timeout not applied")
	}
	if p.Kind == services.ProblemNone {
		t.Error("a timed out ps must be a problem")
	}
}

func TestCaptureCapsOutput(t *testing.T) {
	f := servicestest.New(t)
	big := strings.Repeat("x", 3<<20)
	f.Set(t, "logs-big.txt", big)
	out, _, code, err := services.SpawnExec{}.Capture(context.Background(), t.TempDir(), f.Docker(), "compose", "logs", "big")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if len(out) > services.MaxCapture {
		t.Errorf("captured %d bytes, cap %d", len(out), services.MaxCapture)
	}
}
