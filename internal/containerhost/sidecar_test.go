package containerhost

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

// sidecarEngine answers the sidecar's engine calls (volume inspect, ps,
// rm, run) and passes the rest to the fake docker.
type sidecarEngine struct {
	*fakedocker.Docker
	mu      sync.Mutex
	volume  bool   // the tools volume exists
	stale   string // an old sidecar's id `ps` reports
	calls   [][]string
	envSeen string // the env-file's text while `run` ran
	envMode os.FileMode
	envPath string
}

func (e *sidecarEngine) verb(argv []string) (string, []string) {
	a := argv[1:]
	for len(a) > 0 && (a[0] == "--context" || a[0] == "--connection") {
		a = a[2:]
	}
	if len(a) == 0 {
		return "", nil
	}
	return a[0], a[1:]
}

func (e *sidecarEngine) Capture(ctx context.Context, dir string, argv ...string) (string, string, int, error) {
	v, rest := e.verb(argv)
	switch v {
	case "volume", "ps", "rm", "run":
	default:
		return e.Docker.Capture(ctx, dir, argv...)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, append([]string(nil), argv...))
	switch v {
	case "volume":
		if !e.volume {
			return "", "Error response from daemon: get " + rest[len(rest)-1] + ": no such volume\n", 1, nil
		}
		return "[]\n", "", 0, nil
	case "ps":
		return e.stale + "\n", "", 0, nil
	case "run":
		for i, a := range rest {
			if a == "--env-file" && i+1 < len(rest) {
				e.envPath = rest[i+1]
				b, _ := os.ReadFile(rest[i+1])
				e.envSeen = string(b)
				if fi, err := os.Stat(rest[i+1]); err == nil {
					e.envMode = fi.Mode().Perm()
				}
			}
		}
		return "abc123\n", "", 0, nil
	}
	return "", "", 0, nil
}

func (e *sidecarEngine) seen(verb string) [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out [][]string
	for _, c := range e.calls {
		if v, _ := e.verb(c); v == verb {
			out = append(out, c)
		}
	}
	return out
}

func mountedSidecarHost(t *testing.T, mounted bool, mod ...func(*Config)) (*Host, *sidecarEngine) {
	t.Helper()
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.Mkdir(t, "/app")
	c.WriteFile(t, "/lib/ld-musl-x86_64.so.1", nil, 0o755) // libc=musl
	c.Image = "golang:1.23"
	if mounted {
		c.Mounts = []fakedocker.Mount{{Type: "bind", Source: "/home/u/proj", Destination: "/app", RW: true}}
	}
	e := &sidecarEngine{Docker: d}
	cfg := Config{Container: "api-1", Context: "prod", Root: "/app", X: e}
	for _, m := range mod {
		m(&cfg)
	}
	h, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return h, e
}

// RC-8 (plan: Sidecar): the tools sidecar shares the target's volumes,
// network and processes, runs as the target's user from the target's own
// image, and gets the tools volume read-only; nothing is put in the
// target itself.
func TestSidecarArgvFollowsThePlan(t *testing.T) {
	h, _ := mountedSidecarHost(t, true)
	uid, gid, ok := h.Owner()
	if !ok {
		t.Fatal("the probe gave no uid")
	}
	got, err := h.SidecarArgv("k1", "/tmp/env")
	if err != nil {
		t.Fatal(err)
	}
	arch, libc := h.Caps().Arch, h.Caps().Libc
	want := []string{"docker", "--context", "prod", "run", "-d", "--name", "termocode-sc-k1",
		"--label", "termocode.sidecar=k1",
		"--volumes-from", "api-1", "--network", "container:api-1", "--pid", "container:api-1",
		"--user", strconv.Itoa(uid) + ":" + strconv.Itoa(gid), "--env-file", "/tmp/env",
		"-v", "termocode-tools-" + arch + "-" + libc + ":/opt/termocode:ro",
		"--entrypoint", "/opt/termocode/bin/tc-sleep", "sha256:golang:1.23"} // the image ID: the exact image, even when its tag moved
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv\n got %q\nwant %q", got, want)
	}
	if SidecarName("k1") != "termocode-sc-k1" {
		t.Fatalf("name %q", SidecarName("k1"))
	}
}

// v1: only a bind-mounted folder (the writable layer needs ptrace), and
// not on podman.
func TestSidecarNeedsAMountedFolderOnDocker(t *testing.T) {
	h, _ := mountedSidecarHost(t, false)
	if _, err := h.SidecarArgv("k1", "/tmp/env"); !errors.Is(err, ErrSidecarNeedsMount) {
		t.Fatalf("err = %v, want ErrSidecarNeedsMount", err)
	}
	h, _ = mountedSidecarHost(t, true, func(c *Config) { c.Kind = "podman" })
	if _, err := h.SidecarArgv("k1", "/tmp/env"); !errors.Is(err, ErrSidecarPodman) {
		t.Fatalf("err = %v, want ErrSidecarPodman", err)
	}
}

// Start checks the tools volume first (missing: nothing runs), removes
// an old sidecar of the same workspace, passes the env only in a 0600
// file that is gone after the run, and Stop removes the sidecar.
func TestStartSidecarChecksSweepsRunsAndStops(t *testing.T) {
	h, e := mountedSidecarHost(t, true)
	env := map[string]string{"API_TOKEN": "s3cr3t-value"}
	if _, err := h.StartSidecar(context.Background(), "k1", env); !errors.Is(err, ErrNoToolsVolume) {
		t.Fatalf("err = %v, want ErrNoToolsVolume", err)
	}
	if len(e.seen("run")) != 0 {
		t.Fatal("a sidecar ran without its tools volume")
	}
	e.volume, e.stale = true, "old999"
	sc, err := h.StartSidecar(context.Background(), "k1", env)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Name != "termocode-sc-k1" {
		t.Fatalf("sidecar %+v", sc)
	}
	ps := e.seen("ps")
	if len(ps) != 1 || !strings.Contains(strings.Join(ps[0], " "), "label=termocode.sidecar=k1") {
		t.Fatalf("ps = %q", ps)
	}
	rm := e.seen("rm")
	if len(rm) != 1 || rm[0][len(rm[0])-1] != "old999" {
		t.Fatalf("the old sidecar was not removed: %q", rm)
	}
	runs := e.seen("run")
	if len(runs) != 1 {
		t.Fatalf("runs = %q", runs)
	}
	if strings.Contains(strings.Join(runs[0], " "), "s3cr3t-value") {
		t.Fatal("a secret is in the argv")
	}
	if !strings.Contains(e.envSeen, "API_TOKEN=s3cr3t-value") || e.envMode != 0o600 {
		t.Fatalf("env-file %q mode %v", e.envSeen, e.envMode)
	}
	if _, err := os.Stat(e.envPath); !os.IsNotExist(err) {
		t.Fatalf("the env-file is left behind: %v", err)
	}
	if got := strings.Join(sc.ExecArgv("/opt/termocode/bin/dlv", "dap"), " "); got != "docker --context prod exec -i termocode-sc-k1 /opt/termocode/bin/dlv dap" {
		t.Fatalf("exec argv %q", got)
	}
	if err := sc.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	rm = e.seen("rm")
	if len(rm) != 2 || strings.Join(rm[1][len(rm[1])-2:], " ") != "-f termocode-sc-k1" {
		t.Fatalf("stop = %q", rm)
	}
}

// An env value with a newline cannot be written to an env-file safely.
func TestStartSidecarRefusesAMultiLineEnvValue(t *testing.T) {
	h, e := mountedSidecarHost(t, true)
	e.volume = true
	if _, err := h.StartSidecar(context.Background(), "k1", map[string]string{"A": "x\nB=y"}); err == nil {
		t.Fatal("a value with a newline was accepted")
	}
	if len(e.seen("run")) != 0 {
		t.Fatal("ran anyway")
	}
}
