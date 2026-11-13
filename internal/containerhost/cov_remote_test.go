package containerhost

import (
	"context"
	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
)

// answerExec answers one engine call with a fixed output.
type answerExec struct {
	services.Exec
	out  string
	code int
	argv []string
}

func (a *answerExec) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	a.argv = argv
	return a.out, "", a.code, nil
}

func TestEndpointOfPodmanConnections(t *testing.T) {
	t.Setenv("CONTAINER_HOST", "")
	list := "local\tunix:///run/podman.sock\tfalse\nprod\tssh://me@prod/run/podman.sock\ttrue\nshort\n"
	x := &answerExec{out: list}
	if got := Endpoint(t.Context(), x, "/usr/bin/podman", ""); got != "ssh://me@prod/run/podman.sock" {
		t.Errorf("the default connection = %q", got)
	}
	if got := Endpoint(t.Context(), x, "podman", "local"); got != "unix:///run/podman.sock" {
		t.Errorf("a named connection = %q", got)
	}
	if got := Endpoint(t.Context(), x, "podman", "nope"); got != "" {
		t.Errorf("an unknown connection = %q", got)
	}
	if got := Endpoint(t.Context(), &answerExec{code: 1}, "podman", ""); got != "" {
		t.Errorf("a failing list = %q", got)
	}
	t.Setenv("CONTAINER_HOST", "tcp://box:1")
	if got := Endpoint(t.Context(), x, "podman", ""); got != "tcp://box:1" {
		t.Errorf("CONTAINER_HOST wins for the current connection = %q", got)
	}
	if got := Endpoint(t.Context(), x, "podman", "local"); got != "unix:///run/podman.sock" {
		t.Errorf("a named connection ignores CONTAINER_HOST = %q", got)
	}
}

func TestEndpointDefaultsAndFailures(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	x := &answerExec{out: "unix:///x.sock\n"}
	if got := Endpoint(nil, x, "", ""); got != "unix:///x.sock" || x.argv[0] != "docker" {
		t.Errorf("Endpoint = %q via %v", got, x.argv)
	}
	if got := Endpoint(t.Context(), &answerExec{code: 1}, "", ""); got != "" {
		t.Errorf("a failing inspect = %q", got)
	}
	if got := Endpoint(t.Context(), nil, "/no/such/docker", ""); got != "" {
		t.Errorf("a missing engine program = %q", got)
	}
}

func TestEscapingPathsAreRefusedByEveryFSCall(t *testing.T) {
	h, _, _ := newHost(t)
	fsys := h.FS()
	ctx := t.Context()
	bad := "../../etc/passwd"
	if _, err := fsys.Stat(ctx, bad); err == nil {
		t.Error("Stat outside the workspace")
	}
	if _, err := fsys.ReadFile(ctx, bad); err == nil {
		t.Error("ReadFile outside the workspace")
	}
	if _, err := fsys.ReadDir(ctx, bad); err == nil {
		t.Error("ReadDir outside the workspace")
	}
	if err := fsys.WriteFile(ctx, bad, []byte("x"), 0o644); err == nil {
		t.Error("WriteFile outside the workspace")
	}
	if err := fsys.Mkdir(ctx, bad, 0o755); err == nil {
		t.Error("Mkdir outside the workspace")
	}
	if err := fsys.Remove(ctx, bad); err == nil {
		t.Error("Remove outside the workspace")
	}
	if err := fsys.Rename(ctx, bad, "x"); err == nil {
		t.Error("Rename from outside the workspace")
	}
	if err := fsys.Rename(ctx, "x", bad); err == nil {
		t.Error("Rename to outside the workspace")
	}
}

// deadPipe is a shell that exits at once: the mux cannot start, and the
// host falls back to one engine call per command.
type deadPipe struct{ starts int }

type nopWC struct{}

func (nopWC) Write(p []byte) (int, error) { return len(p), nil }
func (nopWC) Close() error                { return nil }

func (p *deadPipe) Pipe(_ context.Context, _ ...string) (io.WriteCloser, io.ReadCloser, io.ReadCloser, func(), error) {
	p.starts++
	return nopWC{}, io.NopCloser(strings.NewReader("")), io.NopCloser(strings.NewReader("")), func() {}, nil
}

func TestMuxThatDiesAtOnceFallsBack(t *testing.T) {
	pipe := &deadPipe{}
	h, _, c := newHost(t, func(cfg *Config) { cfg.Mux, cfg.Pipe = true, pipe })
	c.WriteFile(t, "/app/a.txt", []byte("a"), 0o644)
	for i := 0; i < muxMaxRestarts+2; i++ {
		ents, err := h.FS().ReadDir(t.Context(), "")
		if err != nil || len(ents) != 1 {
			t.Fatalf("ReadDir = %v, %v", ents, err)
		}
	}
	if pipe.starts == 0 || pipe.starts > muxMaxRestarts+1 {
		t.Errorf("the mux was started %d times", pipe.starts)
	}
	if h.MuxRunning() {
		t.Error("a dead mux is not running")
	}
}

func TestExecWithoutACommandIsRefused(t *testing.T) {
	h, _, _ := newHost(t)
	x := h.Exec()
	if _, _, code, err := x.CaptureInput(t.Context(), "", []byte("in")); err == nil || code != -1 {
		t.Errorf("CaptureInput without a command = %d, %v", code, err)
	}
	if _, _, err := x.Stream(t.Context(), ""); err == nil {
		t.Error("Stream without a command: want error")
	}
	if _, _, code, err := x.Capture(t.Context(), ""); err == nil || code != -1 {
		t.Errorf("Capture without a command = %d, %v", code, err)
	}
}

// switchedExec fails every engine call once down is set.
type switchedExec struct {
	services.Exec
	down *bool
}

func (s switchedExec) Capture(ctx context.Context, dir string, argv ...string) (string, string, int, error) {
	if *s.down {
		return "", "something broke", 1, nil
	}
	return s.Exec.Capture(ctx, dir, argv...)
}

func TestFindCommandsReportsAnEngineFailure(t *testing.T) {
	down := false
	h, _, _ := newHost(t, func(c *Config) { c.X = switchedExec{Exec: c.X, down: &down} })
	if !h.Caps().HasShell {
		t.Skip("the fake image has no shell")
	}
	down = true
	if _, err := h.FindCommands(t.Context(), []string{"gopls"}); err == nil {
		t.Error("an engine that fails: want error")
	}
}

func TestMountFSNotUsedWhenTheSourceIsAFile(t *testing.T) {
	h, _, _ := mounted(t, true, func(c *fakedocker.Container) []fakedocker.Mount {
		file := filepath.Join(c.Root, "afile")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return []fakedocker.Mount{{Type: "bind", Source: file, Destination: "/app", RW: true}}
	})
	if h.Caps().MountOf != "" {
		t.Errorf("a file cannot stand in for the folder, MountOf = %q", h.Caps().MountOf)
	}
}
