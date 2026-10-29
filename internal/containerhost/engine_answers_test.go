package containerhost

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
)

// Engines answer inspect and the probe in many ways (old versions,
// distroless images, odd arches, broken output). scripted is an engine
// whose answers a test writes per command; it never runs anything.
type scripted struct {
	mu    sync.Mutex
	calls [][]string
	// answer gets the args after the binary and global flags.
	answer func(args []string) (out, stderr string, code int, err error)
}

func (s *scripted) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	s.mu.Lock()
	s.calls = append(s.calls, argv[1:])
	s.mu.Unlock()
	return s.answer(argv[1:])
}

func (s *scripted) CaptureInput(ctx context.Context, dir string, _ []byte, argv ...string) (string, string, int, error) {
	return s.Capture(ctx, dir, argv...)
}

func (s *scripted) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("no streams here")
}

func (s *scripted) called(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if strings.Contains(strings.Join(c, " "), sub) {
			return true
		}
	}
	return false
}

var _ services.Exec = (*scripted)(nil)

const runningInspect = `[{"State":{"Running":true},"Config":{"User":"","Image":"alpine:3"},"Image":"sha256:abc","Mounts":[]}]`

// engine answers inspect with inspectOut and the probe with probe.
func engine(inspectOut string, probe func(args []string) (string, string, int, error)) *scripted {
	return &scripted{answer: func(args []string) (string, string, int, error) {
		switch args[0] {
		case "inspect":
			return inspectOut, "", 0, nil
		case "exec":
			return probe(args)
		}
		return "", "", 0, nil
	}}
}

func openWith(t *testing.T, x services.Exec, user string) (*Host, error) {
	t.Helper()
	return New(context.Background(), Config{Container: "api-1", Root: "/app", X: x, User: user})
}

func TestNewInspectAnswers(t *testing.T) {
	cases := []struct {
		name string
		out  string
		code int
		err  error
		want string
	}{
		{"garbage", "Error: not json", 0, nil, "cannot read the container's details"},
		{"empty list", "[]", 0, nil, "cannot read the container's details"},
		{"silent failure", "", 1, nil, "engine command exited 1"},
		{"engine cannot start", "", -1, errors.New("exec: \"docker\": executable file not found in $PATH"), "executable file not found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := &scripted{answer: func([]string) (string, string, int, error) { return c.out, "", c.code, c.err }}
			_, err := openWith(t, x, "")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestProbeArchAndUser(t *testing.T) {
	cases := map[string]string{"armv7l": "arm", "armv6l": "arm", "i686": "386", "i386": "386", "x86_64": "amd64", "aarch64": "arm64", "riscv64": "riscv64"}
	for uname, want := range cases {
		x := engine(runningInspect, func([]string) (string, string, int, error) {
			return "uid=1000\ngid=1001\narch=" + uname + "\nnoise\n", "", 0, nil
		})
		h, err := openWith(t, x, "")
		if err != nil {
			t.Fatal(err)
		}
		c := h.Caps()
		if c.Arch != want || c.User != "1000" || !c.HasShell {
			t.Errorf("%s: caps = %+v", uname, c)
		}
		if uid, gid, ok := h.Owner(); !ok || uid != 1000 || gid != 1001 {
			t.Errorf("%s: owner = %d %d %v", uname, uid, gid, ok)
		}
	}
}

func TestProbeFailures(t *testing.T) {
	x := engine(runningInspect, func([]string) (string, string, int, error) {
		return "", "OCI runtime exec failed: exec failed: unable to start container process: permission denied\n", 126, nil
	})
	if _, err := openWith(t, x, ""); err == nil || !strings.Contains(err.Error(), "unable to start container process") {
		t.Fatalf("exec refused: err = %v", err)
	}
	x = engine(runningInspect, func([]string) (string, string, int, error) {
		return "", "", -1, context.DeadlineExceeded
	})
	if _, err := openWith(t, x, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("probe timeout: err = %v", err)
	}
}

// A distroless image: no sh. The user comes from the session setting
// (over the container's), else root; the arch from the image, whose name
// is the config's when the engine gives no image id.
func TestProbeNoShellUserAndImage(t *testing.T) {
	noShell := func([]string) (string, string, int, error) {
		return "", "OCI runtime exec failed: exec: \"sh\": executable file not found in $PATH\n", 127, nil
	}
	inspect := `[{"State":{"Running":true},"Config":{"User":"nobody","Image":"gcr.io/distroless/static"},"Image":""}]`
	x := engine(inspect, noShell)
	inner := x.answer
	x.answer = func(args []string) (string, string, int, error) {
		if args[0] == "image" {
			return `[{"Architecture":"arm64"}]`, "", 0, nil
		}
		return inner(args)
	}
	h, err := openWith(t, x, "65532:65532")
	if err != nil {
		t.Fatal(err)
	}
	if c := h.Caps(); c.User != "65532:65532" || c.Arch != "arm64" || c.HasShell {
		t.Fatalf("caps = %+v", c)
	}
	if !x.called("image inspect gcr.io/distroless/static") {
		t.Fatalf("calls = %q", x.calls)
	}
	// No user anywhere: root.
	inspect = `[{"State":{"Running":true},"Config":{"User":"","Image":""},"Image":""}]`
	h, err = openWith(t, engine(inspect, noShell), "")
	if err != nil {
		t.Fatal(err)
	}
	if c := h.Caps(); c.User != "0" {
		t.Fatalf("caps = %+v", c)
	}
	if uid, gid, ok := h.Owner(); !ok || uid != 0 || gid != 0 {
		t.Fatalf("owner = %d %d %v", uid, gid, ok)
	}
}

// With no Exec set, the host runs the engine through spawn: a missing
// engine binary is an error, not a hang.
func TestNewWithoutExecUsesSpawn(t *testing.T) {
	_, err := New(context.Background(), Config{Binary: t.TempDir() + "/docker", Container: "api-1", Root: "/app"})
	if err == nil {
		t.Fatal("a missing engine opened")
	}
}
