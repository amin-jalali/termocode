package containerhost

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDefaultTimeoutIsTenSeconds(t *testing.T) {
	h, _, _ := newHost(t)
	if h.cfg.Timeout != 10*time.Second {
		t.Fatalf("timeout = %v", h.cfg.Timeout)
	}
}

func TestEveryFileOpHasATimeLimit(t *testing.T) {
	h, d, c := newHost(t)
	c.WriteFile(t, "/app/f", []byte("x"), 0o644)
	h.cfg.Timeout = 50 * time.Millisecond
	d.SetDelay("cp", time.Minute)
	d.SetDelay("exec", time.Minute)
	f, ctx := h.FS(), t.Context()
	ops := map[string]func() error{
		"Stat":      func() error { _, err := f.Stat(ctx, "f"); return err },
		"ReadFile":  func() error { _, err := f.ReadFile(ctx, "f"); return err },
		"ReadDir":   func() error { _, err := f.ReadDir(ctx, "."); return err },
		"WriteFile": func() error { return f.WriteFile(ctx, "f", nil, 0o644) },
		"Mkdir":     func() error { return f.Mkdir(ctx, "d", 0o755) },
		"Rename":    func() error { return f.Rename(ctx, "f", "g") },
		"Remove":    func() error { return f.Remove(ctx, "f") },
	}
	for name, op := range ops {
		start := time.Now()
		err := op()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s err = %v", name, err)
		}
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("%s took %v", name, took)
		}
	}
}
