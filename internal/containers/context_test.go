package containers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// Real `docker context ls --format '{{json .}}'` prints Current as a JSON
// bool, plus Description and Error (an unreachable endpoint).
const contextLsReal = `{"Current":true,"Description":"Current DOCKER_HOST based configuration","DockerEndpoint":"unix:///var/run/docker.sock","Error":"","Name":"default"}
{"Current":false,"Description":"production box","DockerEndpoint":"ssh://deploy@prod.example","Error":"","Name":"prod"}
{"Current":false,"Description":"","DockerEndpoint":"tcp://10.0.0.5:2376","Error":"unable to resolve docker endpoint: open ca.pem: no such file","Name":"lab"}
`

func TestParseContextsRealDockerFormat(t *testing.T) {
	cs, err := ParseContexts([]byte(contextLsReal))
	if err != nil || len(cs) != 3 {
		t.Fatalf("contexts=%+v err=%v", cs, err)
	}
	if !cs[0].Current || cs[1].Current || cs[2].Current {
		t.Errorf("Current flags = %v %v %v, want true false false", cs[0].Current, cs[1].Current, cs[2].Current)
	}
	if cs[1].DockerEndpoint != "ssh://deploy@prod.example" || cs[1].Description != "production box" {
		t.Errorf("prod = %+v", cs[1])
	}
	if !strings.Contains(cs[2].Error, "ca.pem") || cs[0].Error != "" {
		t.Errorf("errors = %q / %q", cs[0].Error, cs[2].Error)
	}
}

func TestEngineContextSetsDockerContextOnEveryCall(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "version.json", `{"Client":{"Version":"27.4"},"Server":{"Version":"27.4"}}`)
	e := Engine{Binary: f.Docker(), Kind: "docker", Reachable: true, X: services.SpawnExec{}, Context: "prod"}
	ctx := context.Background()
	if _, err := e.ListContainers(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ListContexts(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	stream, stop, err := e.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream
	defer stop()
	// Wait on ctx.log itself: the fake writes argv.log first, so a call
	// can be counted before its ctx.log line exists.
	var calls []string
	if !servicestest.WaitFor(2e9, func() bool {
		calls = f.CtxCalls()
		for _, c := range calls {
			if strings.Contains(c, "events") {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("watcher did not start: calls = %q", calls)
	}
	if len(calls) < 4 {
		t.Fatalf("calls = %q", calls)
	}
	for _, c := range calls {
		if !strings.HasPrefix(c, "prod|") {
			t.Errorf("call without DOCKER_CONTEXT=prod: %q", c)
		}
	}
}

func TestEngineWithoutContextLeavesEnvironmentAlone(t *testing.T) {
	f := servicestest.New(t)
	t.Setenv("DOCKER_CONTEXT", "from-shell")
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}}
	if _, err := e.ListContainers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := f.CtxCalls(); len(c) != 1 || !strings.HasPrefix(c[0], "from-shell|") {
		t.Fatalf("calls = %q", c)
	}
}

func TestDetectUsesContextAndEngineChoice(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "version.json", `{"Client":{"Version":"27.4"},"Server":{"Version":"27.4"}}`)
	look := func(name string) (string, error) {
		if name == "docker" {
			return f.Docker(), nil
		}
		return "", context.Canceled
	}
	e := Detect(context.Background(), DetectOptions{LookPath: look, Exec: services.SpawnExec{}, Context: "prod"})
	if !e.Reachable || e.Context != "prod" {
		t.Fatalf("engine = %+v", e)
	}
	if c := f.CtxCalls(); len(c) == 0 || !strings.HasPrefix(c[0], "prod|") {
		t.Fatalf("detect calls = %q", c)
	}
	// containers_engine = podman with only docker installed: no fallback.
	e = Detect(context.Background(), DetectOptions{LookPath: look, Exec: services.SpawnExec{}, Preferred: "podman"})
	if e.Binary != "" {
		t.Fatalf("explicit podman fell back to %q", e.Binary)
	}
}

func TestContextUseRunsPlainCommand(t *testing.T) {
	f := servicestest.New(t)
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}, Context: "prod"}
	if err := e.UseContext(context.Background(), "lab"); err != nil {
		t.Fatal(err)
	}
	c := f.CtxCalls()
	if len(c) != 1 || c[0] != "|context use lab" {
		t.Fatalf("calls = %q (make-default must not carry the session context)", c)
	}
}

func TestDetectTimesOutOnAStuckRemote(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "sleep-version", "5")
	start := time.Now()
	e := Detect(context.Background(), DetectOptions{LookPath: func(string) (string, error) { return f.Docker(), nil },
		Exec: services.SpawnExec{}, Context: "prod", Timeout: 200 * time.Millisecond})
	if time.Since(start) > 3*time.Second {
		t.Fatalf("detect hung for %s", time.Since(start))
	}
	if e.Reachable || e.Problem.Kind == services.ProblemNone || !strings.Contains(e.Problem.Detail, "timed out") {
		t.Fatalf("engine = %+v", e)
	}
}

// downExec answers every call like a stopped engine daemon.
type downExec struct{ services.Exec }

func (downExec) Capture(context.Context, string, ...string) (string, string, int, error) {
	return "", "Cannot connect to the daemon socket: connection refused", 125, nil
}

func TestDetectNamesPodmanWhenItIsDown(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", context.Canceled
	}
	e := Detect(context.Background(), DetectOptions{LookPath: look, Exec: downExec{}})
	if got := e.Problem.Message(); got != "Podman is not running" {
		t.Fatalf("message = %q, want Podman is not running", got)
	}
	e = Engine{Binary: "/usr/bin/podman", Kind: "podman", X: downExec{}}
	if _, err := e.list(context.Background(), "ps"); err == nil || err.Error() != "Podman is not running" {
		t.Fatalf("list err = %v, want Podman is not running", err)
	}
}
