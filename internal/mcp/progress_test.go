package mcp

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
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

// MCPX-19: the call's progress (count, total, message) reaches the
// ctx's progress func, also without a timeout.
func TestCallProgressText(t *testing.T) {
	p := startFake(t, nil, Options{})
	for _, timeout := range []time.Duration{0, 5 * time.Second} {
		var mu sync.Mutex
		var got []string
		ctx := WithProgress(context.Background(), func(pr Progress) {
			mu.Lock()
			got = append(got, pr.Text())
			mu.Unlock()
		})
		r, err := p.Client.CallToolTimeout(ctx, "slow", map[string]any{"total_ms": 500, "progress_ms": 100, "progress_total": 10}, timeout)
		mu.Lock()
		first := ""
		if len(got) > 0 {
			first = got[0]
		}
		mu.Unlock()
		if err != nil || r.Text() != "slow done" || first != "step 1 · 1 of 10 (10%)" {
			t.Fatalf("timeout %v: %q %v, progress %v", timeout, r.Text(), err, got)
		}
	}
	for _, c := range []struct {
		p    Progress
		want string
	}{{Progress{Progress: 3}, "3"}, {Progress{Progress: 0.5, Total: 1}, "0.5 of 1 (50%)"}, {Progress{Message: "x"}, "x"}} {
		if got := c.p.Text(); got != c.want {
			t.Errorf("%+v = %q, want %q", c.p, got, c.want)
		}
	}
}
