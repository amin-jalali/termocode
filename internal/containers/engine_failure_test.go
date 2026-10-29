package containers

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// What the user sees when the engine is missing, off, locked or answers
// nonsense. Every call goes to the fake docker script; no real engine.

func detectFake(t *testing.T, f *servicestest.Fake, timeout time.Duration) Engine {
	t.Helper()
	return Detect(context.Background(), DetectOptions{
		LookPath: func(string) (string, error) { return f.Docker(), nil },
		Exec:     services.SpawnExec{},
		Timeout:  timeout,
	})
}

func TestDetectOnAndroidHasNoEngine(t *testing.T) {
	called := false
	e := Detect(context.Background(), DetectOptions{Android: true, LookPath: func(string) (string, error) {
		called = true
		return "/bin/docker", nil
	}})
	if called || e.Binary != "" || e.Reachable {
		t.Fatalf("android engine = %+v, lookpath called %v", e, called)
	}
}

func TestDetectEngineProblems(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   services.ProblemKind
		detail string
	}{
		{"daemon down", "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?", services.ProblemDaemonDown, "Cannot connect"},
		{"socket permission", "permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock", services.ProblemNoPermission, "permission denied"},
		{"podman socket gone", "Cannot connect to Podman. Please verify your connection to the Linux system", services.ProblemDaemonDown, "Cannot connect to Podman"},
		{"other", "error during connect: this error may indicate that the docker daemon is not running\n", services.ProblemOther, "error during connect"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := servicestest.New(t)
			f.Set(t, "all.err", c.stderr)
			e := detectFake(t, f, 0)
			if e.Reachable || e.Problem.Kind != c.want || !strings.HasPrefix(e.Problem.Detail, c.detail) {
				t.Fatalf("problem = %+v, want kind %v detail %q", e.Problem, c.want, c.detail)
			}
			if e.Timeout != defaultTimeout {
				t.Fatalf("timeout = %v, want the default", e.Timeout)
			}
		})
	}
}

// A non-zero exit with nothing on stderr is "other" with an empty detail:
// the view then says the engine "does not answer".
func TestDetectSilentFailure(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "all.err", "")
	f.Set(t, "all.code", "3")
	e := detectFake(t, f, 0)
	if e.Reachable || e.Problem != (services.Problem{Kind: services.ProblemOther}) {
		t.Fatalf("problem = %+v", e.Problem)
	}
}

func TestDetectMalformedVersionJSON(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "version.json", "Docker version 27.4.0, build bde2b89")
	e := detectFake(t, f, 0)
	if e.Reachable || e.Problem.Kind != services.ProblemOther || e.Problem.Detail != "cannot read engine version" {
		t.Fatalf("problem = %+v", e.Problem)
	}
}

func TestDetectVersionTimeoutIsDaemonDown(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "sleep-version", "30")
	start := time.Now()
	e := detectFake(t, f, 200*time.Millisecond)
	if e.Problem.Kind != services.ProblemDaemonDown || e.Problem.Detail != "engine version timed out" {
		t.Fatalf("problem = %+v", e.Problem)
	}
	if time.Since(start) > 20*time.Second {
		t.Fatal("the version probe did not stop at its timeout")
	}
}

// The binary was found on PATH but is gone when it runs (uninstalled in
// between): no engine, not a vague error.
func TestDetectBinaryVanished(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "docker")
	e := Detect(context.Background(), DetectOptions{LookPath: func(string) (string, error) { return gone, nil }})
	if e.Binary != gone || e.Reachable || e.Problem.Kind != services.ProblemNoDocker {
		t.Fatalf("engine = %+v", e)
	}
}

// With no LookPath and Exec given, Detect uses the real PATH lookup and
// spawn; the fake first on PATH is the one found.
func TestDetectDefaultsUsePath(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "version.json", `{"Client":{"Version":"27.4"},"Server":{"Version":"27.4"}}`)
	t.Setenv("PATH", f.PathEnv())
	e := Detect(context.Background(), DetectOptions{Preferred: "docker"})
	if e.Binary != f.Docker() || e.Kind != "docker" || !e.Reachable {
		t.Fatalf("engine = %+v", e)
	}
}

func TestClassifyEngineErrorNotFound(t *testing.T) {
	err := &os.PathError{Op: "fork/exec", Path: "/usr/bin/docker", Err: errors.New("no such file or directory")}
	if p := classifyEngineError("", err); p.Kind != services.ProblemNoDocker {
		t.Fatalf("problem = %+v", p)
	}
	if p := classifyEngineError("", context.DeadlineExceeded); p.Kind != services.ProblemDaemonDown {
		t.Fatalf("deadline problem = %+v", p)
	}
}

func fakeEngine(f *servicestest.Fake) *Engine {
	return &Engine{Binary: f.Docker(), Kind: "docker", Reachable: true, X: services.SpawnExec{}}
}

// The daemon stops after the view loaded: every list and action says so
// in the engine's words, and a silent failure names the exit code.
func TestListsAndActionsWhenDaemonStops(t *testing.T) {
	f := servicestest.New(t)
	e := fakeEngine(f)
	ctx := context.Background()
	calls := map[string]func() error{
		"images":   func() error { _, err := e.ListImages(ctx); return err },
		"networks": func() error { _, err := e.ListNetworks(ctx); return err },
		"volumes":  func() error { _, err := e.ListVolumes(ctx); return err },
		"inspect":  func() error { _, err := e.Inspect(ctx, "container", "abc"); return err },
		"stats":    func() error { _, err := e.Stats(ctx, "abc"); return err },
		"history":  func() error { _, err := e.History(ctx, "nginx"); return err },
		"df":       func() error { _, err := e.DiskUsage(ctx); return err },
		"start":    func() error { return e.Start(ctx, "abc") },
		"tag":      func() error { return e.Tag(ctx, "nginx", "nginx:mine") },
		"network":  func() error { return e.CreateNetwork(ctx, "net1") },
		"volume":   func() error { return e.CreateVolume(ctx, "vol1") },
	}
	f.Set(t, "all.err", "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?")
	for name, call := range calls {
		if err := call(); err == nil || err.Error() != "Docker is not running" {
			t.Errorf("%s with the daemon down: err = %v", name, err)
		}
	}
	f.Set(t, "all.err", "")
	f.Set(t, "all.code", "125")
	for name, call := range calls {
		if err := call(); err == nil || err.Error() != "engine command exited 125" {
			t.Errorf("%s with a silent failure: err = %v", name, err)
		}
	}
}

// A container removed while the user acts on it: the engine's own reason
// reaches the user.
func TestActionOnRemovedContainerKeepsEngineReason(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "all.err", "Error response from daemon: No such container: abc\n")
	err := fakeEngine(f).Stop(context.Background(), "abc")
	if err == nil || err.Error() != "Error response from daemon: No such container: abc" {
		t.Fatalf("err = %v", err)
	}
}

func TestListsReadEngineJSON(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "images.json", `{"ID":"sha256:1","Repository":"nginx","Tag":"latest","Size":"187MB"}`+"\n")
	f.Set(t, "network-ls.json", `{"ID":"n1","Name":"bridge","Driver":"bridge"}`+"\n")
	f.Set(t, "volume-ls.json", `{"Name":"data","Driver":"local"}`+"\n")
	f.Set(t, "stats.json", `{"ID":"abc","Name":"web","CPUPerc":"1.5%","MemUsage":"10MiB / 1GiB"}`+"\n")
	f.Set(t, "history.json", `{"ID":"sha256:1","CreatedBy":"/bin/sh -c #(nop) CMD [\"nginx\"]","Size":"0B"}`+"\n")
	f.Set(t, "inspect.json", `[{"Id":"abc","Config":{"Env":["A=1"]}}]`)
	f.Set(t, "df.txt", "Images space usage:\n")
	e := fakeEngine(f)
	ctx := context.Background()
	imgs, err := e.ListImages(ctx)
	if err != nil || len(imgs) != 1 || len(imgs[0].RepoTags) != 1 || imgs[0].RepoTags[0] != "nginx:latest" {
		t.Fatalf("images = %+v, %v", imgs, err)
	}
	nets, err := e.ListNetworks(ctx)
	if err != nil || len(nets) != 1 || nets[0].Name != "bridge" {
		t.Fatalf("networks = %+v, %v", nets, err)
	}
	vols, err := e.ListVolumes(ctx)
	if err != nil || len(vols) != 1 || vols[0].Name != "data" {
		t.Fatalf("volumes = %+v, %v", vols, err)
	}
	stats, err := e.Stats(ctx, "abc")
	if err != nil || len(stats) != 1 || stats[0].Name != "web" {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	hist, err := e.History(ctx, "nginx")
	if err != nil || len(hist) != 1 {
		t.Fatalf("history = %+v, %v", hist, err)
	}
	v, err := e.Inspect(ctx, "container", "abc")
	if err != nil || v == nil {
		t.Fatalf("inspect = %v, %v", v, err)
	}
	df, err := e.DiskUsage(ctx)
	if err != nil || df != "Images space usage:\n" {
		t.Fatalf("df = %q, %v", df, err)
	}
	want := []string{
		"images --no-trunc --format {{json .}}",
		"network ls --format {{json .}}",
		"volume ls --format {{json .}}",
		"stats --no-stream --format {{json .}} abc",
		"history --no-trunc --format {{json .}} nginx",
		"inspect abc",
		"system df -v",
	}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %#v", got)
	}
}

// Malformed engine JSON is an error, not an empty list.
func TestListMalformedJSON(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "images.json", "{\"ID\":\"a\"}\nnot json\n")
	if _, err := fakeEngine(f).ListImages(context.Background()); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v", err)
	}
	f.Set(t, "inspect.json", "")
	if _, err := fakeEngine(f).Inspect(context.Background(), "image", "x"); err == nil {
		t.Fatal("empty inspect accepted")
	}
	f.Set(t, "inspect.json", "[{")
	if _, err := fakeEngine(f).InspectType(context.Background(), "image", "x"); err == nil {
		t.Fatal("broken inspect accepted")
	}
}

func TestInspectTypeFailsWithEngineReason(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "all.err", "Error: No such object: x\n")
	if _, err := fakeEngine(f).InspectType(context.Background(), "volume", "x"); err == nil || err.Error() != "Error: No such object: x" {
		t.Fatalf("err = %v", err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "inspect --type volume x" {
		t.Fatalf("calls = %#v", got)
	}
}

func TestLogsJoinsStderrAndReportsFailures(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "logs.txt", "2026-10-29T10:00:00Z hello\n")
	e := fakeEngine(f)
	got, err := e.Logs(context.Background(), "abc", 50, "")
	if err != nil || got != "2026-10-29T10:00:00Z hello" {
		t.Fatalf("logs = %q, %v", got, err)
	}
	if c := f.Calls(); c[len(c)-1] != "logs --tail 50 --timestamps abc" {
		t.Fatalf("argv = %q", c[len(c)-1])
	}
	f.Set(t, "all.err", "Error response from daemon: No such container: abc\nmore\n")
	if _, err := e.Logs(context.Background(), "abc", 50, ""); err == nil || err.Error() != "Error response from daemon: No such container: abc" {
		t.Fatalf("err = %v", err)
	}
	f.Set(t, "all.err", "")
	f.Set(t, "all.code", "2")
	if _, err := e.Logs(context.Background(), "abc", 50, ""); err == nil || err.Error() != "engine command exited 2: " {
		t.Fatalf("silent err = %v", err)
	}
}

// stderrLogs is an Exec whose logs come only on stderr (a container that
// writes to stderr), so Logs must keep them.
type stderrLogs struct {
	services.SpawnExec
	out, errText string
	err          error
}

func (s stderrLogs) Capture(ctx context.Context, dir string, argv ...string) (string, string, int, error) {
	return s.out, s.errText, 0, s.err
}

func TestLogsKeepStderrLines(t *testing.T) {
	e := &Engine{Binary: "docker", X: stderrLogs{out: "out line\n", errText: "err line\n"}}
	got, err := e.Logs(context.Background(), "abc", 10, "5m")
	if err != nil || got != "out line\nerr line" {
		t.Fatalf("logs = %q, %v", got, err)
	}
	e.X = stderrLogs{errText: "only err\n"}
	if got, _ := e.Logs(context.Background(), "abc", 10, ""); got != "only err" {
		t.Fatalf("stderr-only logs = %q", got)
	}
	e.X = stderrLogs{err: context.Canceled}
	if _, err := e.Logs(context.Background(), "abc", 10, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled logs err = %v", err)
	}
	if _, err := e.Inspect(context.Background(), "", "abc"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspect err = %v", err)
	}
	if _, err := e.InspectType(context.Background(), "", "abc"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspect type err = %v", err)
	}
	if _, err := e.ListImages(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled list err = %v", err)
	}
	if err := e.Start(context.Background(), "abc"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled action err = %v", err)
	}
	if _, err := e.Search(context.Background(), "nginx", 5); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search err = %v", err)
	}
}

// An engine with no Exec set still runs (it falls back to spawn).
func TestEngineWithoutExecUsesSpawn(t *testing.T) {
	f := servicestest.New(t)
	e := &Engine{Binary: f.Docker(), Kind: "docker"}
	if err := e.Start(context.Background(), "abc"); err != nil {
		t.Fatal(err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "start abc" {
		t.Fatalf("calls = %#v", got)
	}
}

func TestVisibleCommandsArgv(t *testing.T) {
	e := &Engine{Binary: "/usr/bin/podman", Kind: "podman"}
	cases := []struct {
		cmd   services.Command
		label string
		argv  []string
	}{
		{e.LogsFollow("abc"), "logs: abc", []string{"/usr/bin/podman", "logs", "-f", "--tail", "200", "abc"}},
		{e.Shell("abc"), "shell: abc", []string{"/usr/bin/podman", "exec", "-it", "abc", "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"}},
		{e.Attach("abc"), "attach: abc", []string{"/usr/bin/podman", "attach", "--sig-proxy=false", "abc"}},
		{e.StatsFollow("abc"), "stats: abc", []string{"/usr/bin/podman", "stats", "abc"}},
		{e.PullCommand("nginx:1.27"), "pull: nginx:1.27", []string{"/usr/bin/podman", "pull", "nginx:1.27"}},
	}
	for _, c := range cases {
		if c.cmd.Label != c.label || !reflect.DeepEqual(c.cmd.Argv, c.argv) {
			t.Errorf("command = %+v, want %q %q", c.cmd, c.label, c.argv)
		}
	}
}

// streamFail is an Exec whose event stream cannot start.
type streamFail struct{ services.SpawnExec }

func (streamFail) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("cannot start events")
}

func TestWatchStartFailure(t *testing.T) {
	e := &Engine{Binary: "docker", X: streamFail{}}
	ch, stop, err := e.Watch(context.Background())
	if err == nil || ch != nil || stop != nil {
		t.Fatalf("watch = %v %v %v", ch, stop != nil, err)
	}
}

// eventStream is an Exec whose event stream is a fixed text.
type eventStream struct {
	services.SpawnExec
	text    string
	stopped chan struct{}
}

func (s eventStream) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return io.NopCloser(strings.NewReader(s.text)), func() {
		select {
		case <-s.stopped:
		default:
			close(s.stopped)
		}
	}, nil
}

// Garbage lines in the event stream are skipped; good events after them
// still arrive, and the channel closes when the stream ends.
func TestWatchSkipsBadEventLines(t *testing.T) {
	text := "not json\n" + `{"Type":"container","Action":"start","Actor":{"ID":"abc","Attributes":{"name":"web"}},"time":1700000000}` + "\n"
	es := eventStream{text: text, stopped: make(chan struct{})}
	e := &Engine{Binary: "docker", X: es}
	ch, stop, err := e.Watch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	var got []Event
	for ev := range ch {
		got = append(got, ev)
	}
	if len(got) != 1 || got[0].Action != "start" || got[0].Actor != "web" {
		t.Fatalf("events = %+v", got)
	}
	select {
	case <-es.stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the stream was not stopped after it ended")
	}
}
