package mcp

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Spec (plan §2.5): progress notifications reset the call deadline, so
// a long call that keeps reporting progress is not cut; a silent call
// still times out after the timeout.
func TestCallProgressResetsDeadline(t *testing.T) {
	var ll logLines
	p := startFake(t, nil, Options{Log: ll.add})
	start := time.Now()
	r, err := p.Client.CallToolTimeout(context.Background(), "slow",
		map[string]any{"total_ms": 900, "progress_ms": 100}, 400*time.Millisecond)
	if err != nil || r.Text() != "slow done" {
		t.Fatalf("call with progress = %q, %v (after %v)", r.Text(), err, time.Since(start))
	}
	start = time.Now()
	_, err = p.Client.CallToolTimeout(context.Background(), "slow",
		map[string]any{"total_ms": 900}, 300*time.Millisecond)
	if !errors.Is(err, ErrCallTimeout) {
		t.Fatalf("silent call: err = %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("timeout took %v", d)
	}
}

// The Manager uses it: an entry timeout of 1 s, a call of 2.4 s that
// reports progress every 300 ms, succeeds.
func TestManagerCallProgress(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]any{"command": os.Args[0], "args": []string{"-test.run=^$"},
		"env": map[string]string{mcptest.Env: "server"}, "timeout": 1}})
	m := e.manager(t, 0, nil)
	r, err := m.Call(context.Background(), nil, "fake", "slow", map[string]any{"total_ms": 2400, "progress_ms": 300})
	if err != nil || r.Text() != "slow done" {
		t.Fatalf("call = %q, %v", r.Text(), err)
	}
	_, err = m.Call(context.Background(), nil, "fake", "slow", map[string]any{"total_ms": 2400})
	if err == nil || !strings.Contains(err.Error(), "no answer in 1s") {
		t.Fatalf("silent call: %v", err)
	}
}
