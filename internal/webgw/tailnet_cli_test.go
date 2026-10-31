//go:build unix

package webgw

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeTailscaleBin puts a shell script named tailscale first on PATH and
// keeps the macOS app path out of the search.
func fakeTailscaleBin(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "tailscale")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	noAppBin(t)
	return bin
}

// noAppBin limits the search to the PATH for the test.
func noAppBin(t *testing.T) {
	t.Helper()
	old := tailscaleBins
	tailscaleBins = []string{"tailscale"}
	t.Cleanup(func() { tailscaleBins = old })
}

func TestTailscaleBinSearch(t *testing.T) {
	old := tailscaleBins
	t.Cleanup(func() { tailscaleBins = old })
	t.Setenv("PATH", t.TempDir())

	dir := t.TempDir()
	app := filepath.Join(dir, "Tailscale")
	if err := os.WriteFile(app, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A missing name on PATH, a missing file and a folder are skipped; the
	// app's CLI is found.
	tailscaleBins = []string{"tailscale-not-here", filepath.Join(dir, "missing"), dir, app}
	if got, err := tailscaleBin(); err != nil || got != app {
		t.Fatalf("tailscaleBin = %q, %v; want %q", got, err, app)
	}
	tailscaleBins = tailscaleBins[:3]
	if _, err := tailscaleBin(); !errors.Is(err, ErrNoTailscale) {
		t.Fatalf("err = %v, want ErrNoTailscale", err)
	}
}

func TestTailscaleNotInstalled(t *testing.T) {
	noAppBin(t)
	t.Setenv("PATH", t.TempDir())
	if _, err := Tailscale(context.Background(), "status", "--json"); !errors.Is(err, ErrNoTailscale) {
		t.Fatalf("Tailscale err = %v", err)
	}
	if _, err := StartTailscaleServe("http://127.0.0.1:1"); !errors.Is(err, ErrNoTailscale) {
		t.Fatalf("StartTailscaleServe err = %v", err)
	}
}

// The real CLI wrapper gives stdout, and on a failure the last line the
// CLI printed on stderr (or the exit status when it printed nothing).
func TestTailscaleRunsTheCLI(t *testing.T) {
	fakeTailscaleBin(t, `case "$1" in
status) echo '{"BackendState":"Running"}' ;;
quiet) exit 4 ;;
*) echo "first line" >&2; echo "  access denied  " >&2; echo >&2; exit 3 ;;
esac
`)
	out, err := Tailscale(context.Background(), "status", "--json")
	if err != nil || strings.TrimSpace(string(out)) != `{"BackendState":"Running"}` {
		t.Fatalf("status = %q, %v", out, err)
	}
	_, err = Tailscale(context.Background(), "serve", "status")
	if err == nil || err.Error() != "tailscale serve: access denied" {
		t.Fatalf("failure = %v, want the last stderr line", err)
	}
	_, err = Tailscale(context.Background(), "quiet")
	if err == nil || !strings.Contains(err.Error(), "exit status 4") {
		t.Fatalf("quiet failure = %v, want the exit status", err)
	}
}

// Serve runs as `tailscale serve --https=443 <target>` until Stop, and
// says why it ended.
func TestStartTailscaleServeRunsUntilStop(t *testing.T) {
	dir := t.TempDir()
	argv := filepath.Join(dir, "argv")
	t.Setenv("TS_ARGV", argv)
	// The line first, then argv: the test stops serve once argv is there,
	// and a stop that came between the two (a slow runner) ended the shell
	// before it printed, so Reason was "tailscale serve ended".
	fakeTailscaleBin(t, `echo "Available within your tailnet"
echo "$@" > "$TS_ARGV.tmp" && mv "$TS_ARGV.tmp" "$TS_ARGV"
exec sleep 60
`)
	p, err := StartTailscaleServe("http://127.0.0.1:41237")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Stop)
	waitFor(t, "serve to start", func() bool { _, err := os.Stat(argv); return err == nil })
	got, _ := os.ReadFile(argv)
	if want := "serve --https=443 http://127.0.0.1:41237"; strings.TrimSpace(string(got)) != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	select {
	case <-p.Done():
		t.Fatal("serve ended before Stop")
	default:
	}
	p.Stop()
	p.Stop() // once only
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not end serve")
	}
	if r := p.Reason(); r != "Available within your tailnet" {
		t.Fatalf("Reason = %q", r)
	}
}

// A Serve that fails on its own ends Done with its last line as Reason.
func TestStartTailscaleServeFails(t *testing.T) {
	fakeTailscaleBin(t, `echo "serve: Access denied: serve config denied" >&2; exit 1
`)
	p, err := StartTailscaleServe("http://127.0.0.1:41237")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not end")
	}
	if r := p.Reason(); r != "serve: Access denied: serve config denied" {
		t.Fatalf("Reason = %q", r)
	}
}

func TestTailBufKeepsTheEnd(t *testing.T) {
	w := &tailBuf{max: 8}
	for _, s := range []string{"abc", "defgh", "ijklmnop", "q"} {
		if n, err := w.Write([]byte(s)); n != len(s) || err != nil {
			t.Fatalf("Write(%q) = %d, %v", s, n, err)
		}
	}
	if got := w.String(); got != "jklmnopq" {
		t.Fatalf("tail = %q", got)
	}
}

// seqTailscale answers `serve status --json` from a list, the last one
// repeating.
type seqTailscale struct {
	outs []string
	errs []error
	n    int
}

func (s *seqTailscale) run(_ context.Context, _ ...string) ([]byte, error) {
	i := min(s.n, len(s.outs)-1)
	s.n++
	var err error
	if i < len(s.errs) {
		err = s.errs[i]
	}
	return []byte(s.outs[i]), err
}

const ourServe = `{"Web":{"box.tail1234.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:41237"}}}}}`

func TestTailnetServeWaitsForTailscale(t *testing.T) {
	ctx := context.Background()
	newRun := func(ts *seqTailscale, fs *fakeServe, wait time.Duration) *tailnetRun {
		return &tailnetRun{cfg: TailnetConfig{Run: ts.run, Start: fs.start, Wait: wait}, host: "box.tail1234.ts.net"}
	}

	t.Run("already ours", func(t *testing.T) {
		fs := newFakeServe()
		if err := newRun(&seqTailscale{outs: []string{ourServe}}, fs, time.Second).serve(ctx, 41237); err != nil {
			t.Fatal(err)
		}
		if fs.target() != "" {
			t.Fatalf("started serve again for %q", fs.target())
		}
	})
	t.Run("shows up after an unreadable status", func(t *testing.T) {
		fs := newFakeServe()
		ts := &seqTailscale{outs: []string{``, `{`, ourServe}}
		if err := newRun(ts, fs, 10*time.Second).serve(ctx, 41237); err != nil {
			t.Fatal(err)
		}
		if fs.target() != "http://127.0.0.1:41237" || fs.stoppedN() != 0 {
			t.Fatalf("target %q, stopped %d", fs.target(), fs.stoppedN())
		}
	})
	t.Run("funnel turns on while waiting", func(t *testing.T) {
		fs := newFakeServe()
		ts := &seqTailscale{outs: []string{``, `{"AllowFunnel":{"box.tail1234.ts.net:443":true}}`}}
		if err := newRun(ts, fs, 10*time.Second).serve(ctx, 41237); !errors.Is(err, ErrFunnel) {
			t.Fatalf("err = %v, want ErrFunnel", err)
		}
		if fs.stoppedN() != 1 {
			t.Fatalf("serve stopped %d times, want 1", fs.stoppedN())
		}
	})
	t.Run("never shows up", func(t *testing.T) {
		fs := newFakeServe()
		err := newRun(&seqTailscale{outs: []string{``}}, fs, 50*time.Millisecond).serve(ctx, 41237)
		if err == nil || !strings.Contains(err.Error(), "did not start in time") {
			t.Fatalf("err = %v", err)
		}
		if fs.stoppedN() != 1 {
			t.Fatalf("serve stopped %d times, want 1", fs.stoppedN())
		}
	})
	t.Run("serve ends", func(t *testing.T) {
		fs := newFakeServe()
		fs.exit()
		err := newRun(&seqTailscale{outs: []string{``}}, fs, 10*time.Second).serve(ctx, 41237)
		if err == nil || !strings.Contains(err.Error(), "did not start: serve ended") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		fs := newFakeServe()
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := newRun(&seqTailscale{outs: []string{``}}, fs, 10*time.Second).serve(cctx, 41237)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		if fs.stoppedN() != 1 {
			t.Fatalf("serve stopped %d times, want 1", fs.stoppedN())
		}
	})
}
