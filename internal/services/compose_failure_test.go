package services_test

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

// Compose projects when docker misbehaves: silent failures, broken JSON,
// a missing binary, huge output. Every call goes to the fake docker.

func TestProblemMessages(t *testing.T) {
	cases := map[services.Problem]string{
		{}:                                   "",
		{Kind: services.ProblemNoDocker}:     "No container engine",
		{Kind: services.ProblemNoCompose}:    "Docker Compose v2 is needed",
		{Kind: services.ProblemDaemonDown}:   "Docker is not running",
		{Kind: services.ProblemNoPermission}: "No access to the Docker socket",
		{Kind: services.ProblemBadConfig}:    "compose file is not valid",
		{Kind: services.ProblemBadConfig, Detail: "yaml: line 3"}: "yaml: line 3",
		{Kind: services.ProblemOther, Detail: "boom"}:             "boom",
	}
	for p, want := range cases {
		if got := p.Message(); got != want {
			t.Errorf("%+v.Message() = %q, want %q", p, got, want)
		}
	}
}

func TestStateNamesAndBusy(t *testing.T) {
	if services.Healthy.String() != "healthy" || services.NotCreated.String() != "not created" {
		t.Fatalf("names = %q %q", services.Healthy, services.NotCreated)
	}
	if services.State(99).String() != "unknown" || services.State(-1).String() != "unknown" {
		t.Fatal("an unknown state has a name")
	}
	for s, want := range map[services.State]bool{services.Starting: true, services.Pulling: true, services.Stopping: true, services.Restarting: true, services.Running: false, services.Stopped: false} {
		if s.Busy() != want {
			t.Errorf("%v.Busy() = %v", s, !want)
		}
	}
}

func TestComposeSilentFailureNamesTheExitCode(t *testing.T) {
	c, f := newCompose(t)
	f.Set(t, "stop.err", "")
	f.Set(t, "stop.code", "3")
	if err := c.Stop(context.Background(), []string{"api"}); err == nil || err.Error() != "docker compose stop: exit 3" {
		t.Fatalf("err = %v", err)
	}
}

func TestComposeDownAndRecreate(t *testing.T) {
	c, f := newCompose(t)
	c.Project = "shop"
	if err := c.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls := f.Calls(); len(calls) != 1 || calls[0] != "compose -f compose.yaml -p shop --profile dev down" {
		t.Fatalf("calls = %#v", calls)
	}
	f.Set(t, "down.err", "Error response from daemon: error while removing network: network shop_default has active endpoints\n")
	if err := c.Down(context.Background()); err == nil || !strings.Contains(err.Error(), "active endpoints") {
		t.Fatalf("down err = %v", err)
	}
	cmd := c.Recreate("api")
	want := []string{f.Docker(), "compose", "-f", "compose.yaml", "-p", "shop", "--profile", "dev", "up", "-d", "--force-recreate", "api"}
	if !reflect.DeepEqual(cmd.Argv, want) || cmd.Dir != c.Root {
		t.Fatalf("recreate = %+v", cmd)
	}
}

// No Docker path set: the visible jobs run plain `docker` from PATH.
func TestComposeDefaultBinary(t *testing.T) {
	c := &services.Compose{Root: "/work"}
	if got := c.Up(nil).Argv; !reflect.DeepEqual(got, []string{"docker", "compose", "up", "-d"}) {
		t.Fatalf("argv = %q", got)
	}
}

// With no Exec set, compose runs through spawn (here: the fake first on
// PATH).
func TestComposeWithoutExecUsesSpawn(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "ps.json", "")
	c := &services.Compose{Root: t.TempDir(), Docker: f.Docker()}
	st, p := c.Status(context.Background())
	if p.Kind != services.ProblemNone || len(st) != 0 {
		t.Fatalf("status = %v %+v", st, p)
	}
}

func TestComposeBrokenJSON(t *testing.T) {
	c, f := newCompose(t)
	f.Set(t, "config.json", "{not json")
	if _, p := c.Load(context.Background()); p.Kind != services.ProblemOther || !strings.HasPrefix(p.Detail, "compose config:") {
		t.Fatalf("load problem = %+v", p)
	}
	f.Set(t, "ps.json", "WARN[0000] "+strings.Repeat("x", 100)+"\nmore")
	_, p := c.Status(context.Background())
	if p.Kind != services.ProblemOther || !strings.HasPrefix(p.Detail, "cannot read compose ps: WARN[0000] ") || !strings.HasSuffix(p.Detail, "…") {
		t.Fatalf("status problem = %+v", p)
	}
	if n := len([]rune(strings.TrimPrefix(p.Detail, "cannot read compose ps: "))); n != 60 {
		t.Fatalf("clipped to %d runes, want 60", n)
	}
	f.Set(t, "ps.json", "[{")
	if _, p := c.Status(context.Background()); p.Kind != services.ProblemOther {
		t.Fatalf("broken array problem = %+v", p)
	}
}

func TestComposeLogsFailure(t *testing.T) {
	c, f := newCompose(t)
	f.Set(t, "logs.err", "no such service: nope\n")
	if _, err := c.Logs(context.Background(), "nope", 20, 0); err == nil || err.Error() != "no such service: nope" {
		t.Fatalf("err = %v", err)
	}
}

// compose ps on different versions: blank lines, no Service field (only
// Name), replicas (a down replica shows over the healthy ones), state
// from text.
func TestParsePSVersions(t *testing.T) {
	in := "\n" +
		`{"Name":"legacy-1","Status":"Up 2 minutes (Paused)"}` + "\n\n" +
		`{"Service":"web","State":"running","Status":"Up 1 hour"}` + "\n" +
		`{"Service":"web","State":"exited","Status":"Exited (1) 2 minutes ago","ExitCode":1}` + "\n" +
		`{"Service":"web","State":"running","Status":"Up 3 hours"}` + "\n" +
		`{"Service":"db","Status":"Created"}` + "\n" +
		`{"Service":"cache","Status":"Exited (0) 5 seconds ago"}` + "\n" +
		`{"Service":"api","State":"running","Status":"Up 5 seconds (unhealthy)"}` + "\n" +
		`{"Service":"worker","State":"running","Status":"Up 1 second (health: starting)"}` + "\n"
	st, err := services.ParsePS([]byte(in), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]services.State{
		"legacy-1": services.Paused, "web": services.Exited, "db": services.Stopped, "cache": services.Stopped,
		"api": services.Unhealthy, "worker": services.Starting,
	}
	for name, s := range want {
		if st[name].State != s {
			t.Errorf("%s = %v, want %v", name, st[name].State, s)
		}
	}
	if _, err := services.ParsePS([]byte("[{\"Service\":"), time.Now()); err == nil {
		t.Fatal("a broken array was accepted")
	}
}

func TestStripJSONCKeepsEscapedQuotes(t *testing.T) {
	in := `{"cmd": "echo \"// not a comment\" /* nor this */", // real
  "path": "C:\\dir\\", /* block */ "x": [1,],}`
	out := string(services.StripJSONC([]byte(in)))
	want := "{\"cmd\": \"echo \\\"// not a comment\\\" /* nor this */\", \n  \"path\": \"C:\\\\dir\\\\\",  \"x\": [1]}"
	if out != want {
		t.Fatalf("StripJSONC =\n%q\nwant\n%q", out, want)
	}
}

// eventsExec streams a fixed text as `docker events`.
type eventsExec struct {
	services.SpawnExec
	text string
	err  error
	argv *[]string
}

func (e eventsExec) Stream(_ context.Context, _ string, argv ...string) (io.ReadCloser, func(), error) {
	if e.argv != nil {
		*e.argv = argv
	}
	if e.err != nil {
		return nil, nil, e.err
	}
	return io.NopCloser(strings.NewReader(e.text)), func() {}, nil
}

// Old docker reports the action as Status; a garbage line still says
// "something changed" (an empty event).
func TestComposeWatchOldDockerAndGarbage(t *testing.T) {
	var argv []string
	text := `{"Type":"container","Status":"die","Actor":{"Attributes":{"com.docker.compose.service":"db"}}}` + "\nnot json\n"
	c := &services.Compose{Docker: "docker", X: eventsExec{text: text, argv: &argv}}
	ch, stop, err := c.Watch(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	var got []services.Event
	for ev := range ch {
		got = append(got, ev)
	}
	want := []services.Event{{Type: "container", Action: "die", Service: "db"}, {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %+v", got)
	}
	if strings.Contains(strings.Join(argv, " "), "label=") {
		t.Fatalf("no project, but a label filter: %q", argv)
	}
	c.X = eventsExec{err: errors.New("cannot start")}
	if _, _, err := c.Watch(context.Background(), "p"); err == nil {
		t.Fatal("a failed stream start was not reported")
	}
}

func TestComposeWatchDefaultExec(t *testing.T) {
	f := servicestest.New(t)
	c := &services.Compose{Docker: f.Docker()}
	ch, stop, err := c.Watch(context.Background(), "shop")
	if err != nil {
		t.Fatal(err)
	}
	if !servicestest.WaitFor(10*time.Second, func() bool { return f.PID("events.pid") > 0 }) {
		stop()
		t.Fatal("events never started")
	}
	defer stop()
	// tail may start after the first push: push until one arrives.
	deadline := time.After(10 * time.Second)
	for {
		f.PushEvent(t, `{"Type":"container","Action":"start","Actor":{"Attributes":{"com.docker.compose.service":"api"}}}`)
		select {
		case ev := <-ch:
			if ev.Service != "api" || ev.Action != "start" {
				t.Fatalf("event = %+v", ev)
			}
			return
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatal("no event")
		}
	}
}

func TestSpawnExecStartFailures(t *testing.T) {
	x := services.SpawnExec{}
	ctx := context.Background()
	if _, _, _, err := x.Capture(ctx, ""); err == nil {
		t.Error("Capture with no command")
	}
	if _, _, _, err := x.CaptureInput(ctx, "", nil); err == nil {
		t.Error("CaptureInput with no command")
	}
	if _, _, err := x.Stream(ctx, ""); err == nil {
		t.Error("Stream with no command")
	}
	gone := filepath.Join(t.TempDir(), "docker")
	if _, _, code, err := x.Capture(ctx, "", gone, "ps"); err == nil || code != -1 {
		t.Errorf("Capture of a missing binary: code %d err %v", code, err)
	}
	if _, _, code, err := x.CaptureInput(ctx, "", []byte("pw\n"), gone, "login"); err == nil || code != -1 {
		t.Errorf("CaptureInput of a missing binary: code %d err %v", code, err)
	}
	if _, _, err := x.Stream(ctx, "", gone, "events"); err == nil {
		t.Error("Stream of a missing binary")
	}
	// A working dir that does not exist (deleted project) fails to start.
	if _, _, _, err := x.Capture(ctx, filepath.Join(t.TempDir(), "gone"), "sh", "-c", "true"); err == nil {
		t.Error("Capture in a missing dir")
	}
}

// Huge output (a chatty `compose logs`) is capped, not kept whole.
func TestSpawnExecCapsOutput(t *testing.T) {
	out, _, code, err := services.SpawnExec{}.Capture(context.Background(), "", "sh", "-c", "head -c 1100000 /dev/zero")
	if err != nil || code != 0 {
		t.Fatalf("code %d err %v", code, err)
	}
	if len(out) != services.MaxCapture {
		t.Fatalf("captured %d bytes, want %d", len(out), services.MaxCapture)
	}
}

// otherExec is not a SpawnExec: WithEnv cannot add env to it and returns
// it as it is.
type otherExec struct{ services.SpawnExec }

func TestWithEnvLeavesOtherExecAlone(t *testing.T) {
	x := &otherExec{}
	if got := services.WithEnv(x, "DOCKER_CONTEXT=prod"); got != services.Exec(x) {
		t.Fatalf("WithEnv = %#v", got)
	}
}

func TestEnvFileValues(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	if got := services.EnvFileValues(p); got != nil {
		t.Fatalf("missing file = %v", got)
	}
	if err := os.WriteFile(p, []byte("# comment\nexport DB_URL=\"postgres://u:p@h/db\"\nnot a pair\nNAME='app'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := services.EnvFileValues(p)
	if !reflect.DeepEqual(got, map[string]string{"DB_URL": "postgres://u:p@h/db", "NAME": "app"}) {
		t.Fatalf("values = %v", got)
	}
}

// Text that is already masked is not masked (or counted) again.
func TestRedactIsIdempotent(t *testing.T) {
	once, n := services.Redact("DATABASE_URL=postgres://u:hunter2@db/app API_TOKEN=abc")
	if n == 0 {
		t.Fatal("nothing masked")
	}
	twice, m := services.Redact(once)
	if twice != once || m != 0 {
		t.Fatalf("second pass = %q (%d masks)", twice, m)
	}
}
