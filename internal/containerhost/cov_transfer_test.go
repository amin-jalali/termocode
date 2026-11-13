package containerhost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
)

// uploadFails lets everything through except the tar stream of an upload.
type uploadFails struct {
	services.Exec
	code int
	err  error
}

func (u uploadFails) CaptureInput(_ context.Context, _ string, _ []byte, _ ...string) (string, string, int, error) {
	return "", "no space left", u.code, u.err
}

func TestCopyInReportsAFailedUpload(t *testing.T) {
	src := filepath.Join(t.TempDir(), "n.txt")
	if err := os.WriteFile(src, []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, _, _ := newHost(t, func(c *Config) { c.X = uploadFails{Exec: c.X, code: 1} })
	if _, err := h.CopyIn(t.Context(), []string{src}, "", 0); err == nil || !strings.Contains(err.Error(), "no space left") {
		t.Errorf("an engine that refuses the upload = %v", err)
	}
	h2, _, _ := newHost(t, func(c *Config) { c.X = uploadFails{Exec: c.X, err: context.DeadlineExceeded} })
	if _, err := h2.CopyIn(t.Context(), []string{src}, "", 0); err == nil {
		t.Error("an engine call that fails: want error")
	}
}

func TestCopyOutNeverReplacesLocalFiles(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/a.txt", []byte("remote"), 0o644)
	c.WriteFile(t, "/app/dir/b.txt", []byte("b"), 0o644)
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "a.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.CopyOut(t.Context(), "a.txt", dest, 0); err == nil {
		t.Error("a file that is there already must not be replaced")
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(b) != "mine" {
		t.Errorf("the local file changed: %q", b)
	}
	// A file where the folder should go.
	if err := os.WriteFile(filepath.Join(dest, "dir"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.CopyOut(t.Context(), "dir", dest, 0); err == nil {
		t.Error("a local file in the way of a folder: want error")
	}
}

func TestCopyInReportsAnUnreadableFile(t *testing.T) {
	readOnlyRoot(t)
	src := t.TempDir()
	locked := filepath.Join(src, "secret.txt")
	if err := os.WriteFile(locked, []byte("s"), 0o000); err != nil {
		t.Fatal(err)
	}
	h, _, _ := newHost(t)
	if _, err := h.CopyIn(t.Context(), []string{locked}, "", 0); err == nil {
		t.Error("an unreadable file: want error")
	}
}

func readOnlyRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
}

// startAnswers makes `docker start` answer without starting anything.
type startAnswers struct {
	services.Exec
	code   int
	stderr string
}

func (s startAnswers) Capture(ctx context.Context, dir string, argv ...string) (string, string, int, error) {
	if len(argv) > 1 && argv[len(argv)-2] == "start" {
		return "", s.stderr, s.code, nil
	}
	return s.Exec.Capture(ctx, dir, argv...)
}

func TestStartSaysWhyItFailed(t *testing.T) {
	h, _, c := newHost(t, func(cfg *Config) { cfg.X = startAnswers{Exec: cfg.X, code: 1, stderr: "port is already allocated"} })
	c.Stop()
	err := h.Start(t.Context())
	if err == nil || errors.Is(err, ErrNotRunning) || !strings.Contains(err.Error(), "port is already allocated") {
		t.Errorf("a refused start = %v", err)
	}
	h2, _, c2 := newHost(t, func(cfg *Config) { cfg.X = startAnswers{Exec: cfg.X} })
	c2.Stop()
	if err := h2.Start(t.Context()); !errors.Is(err, ErrNotRunning) {
		t.Errorf("a start that leaves it stopped = %v", err)
	}
}

func TestBrowseDownloadGivesUpOnTooManyCopies(t *testing.T) {
	fsys, _ := browseFS(t)
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "hosts"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 1000; i++ {
		if err := os.WriteFile(filepath.Join(dest, fmt.Sprintf("hosts (%d)", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := BrowseDownload(t.Context(), fsys, "/etc/hosts", dest); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Errorf("1000 copies later = %v", err)
	}
}
