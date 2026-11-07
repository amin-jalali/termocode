package containerhost

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

// localShPipe is a Piper that runs the engine command's last word ("sh")
// as a local /bin/sh: the framing is tested against a real shell.
type localShPipe struct {
	mu     sync.Mutex
	starts int
	argv   [][]string
	fail   bool
}

func (p *localShPipe) Pipe(ctx context.Context, argv ...string) (io.WriteCloser, io.ReadCloser, io.ReadCloser, func(), error) {
	p.mu.Lock()
	p.starts++
	p.argv = append(p.argv, append([]string(nil), argv...))
	fail := p.fail
	p.mu.Unlock()
	if fail {
		return nil, nil, nil, nil, errors.New("cannot start")
	}
	cmd := exec.Command("/bin/sh")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	errR, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, nil, err
	}
	return in, out, errR, func() { _ = in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }, nil
}

func startLocalMux(t *testing.T) *shellMux {
	t.Helper()
	in, out, errR, stop, err := (&localShPipe{}).Pipe(t.Context(), "sh")
	if err != nil {
		t.Skip("no /bin/sh:", err)
	}
	t.Cleanup(stop)
	return &shellMux{in: in, out: bufio.NewReader(out), errR: bufio.NewReader(errR), stop: stop}
}

func TestShQuote(t *testing.T) {
	for in, want := range map[string]string{"": "''", "a b": "'a b'", "it's": `'it'\''s'`, "$(x)": "'$(x)'"} {
		if got := shQuote(in); got != want {
			t.Errorf("shQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestMuxRunsScriptsWithPositionalArgs(t *testing.T) {
	m := startLocalMux(t)
	args := []string{"it's", "$(touch /tmp/x)", "a  b", "-n"}
	out, errs, code, err := m.run(t.Context(), `printf '%s|' "$@"`, args)
	if err != nil || code != 0 || errs != "" {
		t.Fatalf("run = %q %q %d %v", out, errs, code, err)
	}
	if want := "it's|$(touch /tmp/x)|a  b|-n|"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestMuxKeepsExitCodeStderrAndNUL(t *testing.T) {
	m := startLocalMux(t)
	out, errs, code, err := m.run(t.Context(), "printf 'a\\0b\\n\\n'; echo oops >&2; exit 3", nil)
	if err != nil || code != 3 || out != "a\x00b\n\n" || errs != "oops\n" {
		t.Fatalf("run = %q %q %d %v", out, errs, code, err)
	}
	// The next request starts clean: no cd, variable or exit leaks.
	if _, _, _, err := m.run(t.Context(), "cd /; X=1", nil); err != nil {
		t.Fatal(err)
	}
	out, _, code, err = m.run(t.Context(), `printf '%s' "${X:-unset}"`, nil)
	if err != nil || code != 0 || out != "unset" {
		t.Fatalf("leak: %q %d %v", out, code, err)
	}
}

func TestMuxOutputWithoutNewlineAndLongLines(t *testing.T) {
	m := startLocalMux(t)
	out, _, _, err := m.run(t.Context(), "printf no-newline", nil)
	if err != nil || out != "no-newline" {
		t.Fatalf("out = %q, %v", out, err)
	}
	long := strings.Repeat("x", 200<<10)
	out, _, _, err = m.run(t.Context(), `printf '%s' "$1"`, []string{long})
	if err != nil || out != long {
		t.Fatalf("long line: %d bytes, %v", len(out), err)
	}
}

func TestMuxCommandCannotReadTheNextRequest(t *testing.T) {
	m := startLocalMux(t)
	out, _, code, err := m.run(t.Context(), "cat; echo done", nil)
	if err != nil || code != 0 || out != "done\n" {
		t.Fatalf("run = %q %d %v", out, code, err)
	}
}

func TestMuxTimeoutKillsIt(t *testing.T) {
	m := startLocalMux(t)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, _, _, err := m.run(ctx, "sleep 5", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadUntilNeedsTheMarkerAtALineStart(t *testing.T) {
	n := "tcmuxabc"
	r := bufio.NewReader(strings.NewReader("x" + n + " 1\n\n" + n + " 0\n"))
	out, code, err := readUntil(r, n, true)
	if err != nil || code != 0 || out != "x"+n+" 1\n" {
		t.Fatalf("readUntil = %q %d %v", out, code, err)
	}
	if _, _, err := readUntil(bufio.NewReader(strings.NewReader("cut")), n, true); !errors.Is(err, errMux) {
		t.Fatalf("EOF: %v", err)
	}
}

func TestHostCaptureGoesThroughTheMux(t *testing.T) {
	root := t.TempDir()
	pipe := &localShPipe{}
	h, d, _ := newHostAt(t, root, func(c *Config) { c.Mux, c.Pipe = true, pipe })
	before := len(d.Calls())
	for i := 0; i < 3; i++ {
		out, _, code, err := h.Exec().Capture(t.Context(), "", "pwd")
		if err != nil || code != 0 || strings.TrimSpace(out) != root {
			t.Fatalf("Capture = %q %d %v", out, code, err)
		}
	}
	if len(d.Calls()) != before {
		t.Fatalf("one-shot engine calls: %d", len(d.Calls())-before)
	}
	if pipe.starts != 1 || !h.MuxRunning() {
		t.Fatalf("mux starts = %d, running %v", pipe.starts, h.MuxRunning())
	}
	got := strings.Join(pipe.argv[0], " ")
	if got != "docker exec -i -w / api-1 sh" {
		t.Fatalf("mux argv = %q", got)
	}
	_ = h.Close()
	if h.MuxRunning() {
		t.Fatal("Close left the mux running")
	}
}

func TestHostFallsBackWhenTheMuxCannotStart(t *testing.T) {
	pipe := &localShPipe{fail: true}
	h, _, c := newHost(t, func(cfg *Config) { cfg.Mux, cfg.Pipe = true, pipe })
	c.WriteFile(t, "/app/a.txt", []byte("a"), 0o644)
	for i := 0; i < muxMaxRestarts+3; i++ {
		ents, err := h.FS().ReadDir(t.Context(), "")
		if err != nil || len(ents) != 1 || ents[0].Name() != "a.txt" {
			t.Fatalf("ReadDir = %v, %v", ents, err)
		}
	}
	if pipe.starts > muxMaxRestarts+1 {
		t.Fatalf("the mux was started %d times", pipe.starts)
	}
}

func TestHostWithoutMuxNeverStartsOne(t *testing.T) {
	pipe := &localShPipe{}
	h, _, _ := newHost(t, func(cfg *Config) { cfg.Pipe = pipe })
	if _, _, _, err := h.Exec().Capture(t.Context(), "", "true"); err != nil {
		t.Fatal(err)
	}
	if pipe.starts != 0 {
		t.Fatal("the mux started with Mux off")
	}
}

// newHostAt is newHost with the workspace at root (the same path in the
// fake container and on this disk, for the local-sh mux).
func newHostAt(t *testing.T, root string, mod ...func(*Config)) (*Host, *fakedocker.Docker, *fakedocker.Container) {
	t.Helper()
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.Mkdir(t, root)
	cfg := Config{Container: "api-1", Root: root, X: d}
	for _, m := range mod {
		m(&cfg)
	}
	h, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return h, d, c
}
