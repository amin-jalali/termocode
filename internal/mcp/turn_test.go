package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Spec (plan §8): after two failed calls to the same server in one chat
// turn, its tools answer "unavailable, do not retry" without calling it;
// other servers are not affected, and a new turn starts fresh.
func TestTurnCallerStopsAfterTwoFailures(t *testing.T) {
	calls := map[string]int{}
	base := func(ctx context.Context, server, tool string, args map[string]any) (CallResult, error) {
		calls[server]++
		if server == "bad" {
			return CallResult{}, errors.New("connection refused")
		}
		return CallResult{Content: []Content{{Type: "text", Text: "ok"}}}, nil
	}
	turn := TurnCaller(base)
	for i := 0; i < 2; i++ {
		if _, err := turn(context.Background(), "bad", "x", nil); err == nil || !strings.Contains(err.Error(), "refused") {
			t.Fatalf("failure %d: %v", i, err)
		}
	}
	_, err := turn(context.Background(), "bad", "y", nil)
	if err == nil || !strings.Contains(err.Error(), "unavailable") || !strings.Contains(err.Error(), "do not retry") {
		t.Fatalf("third call: %v", err)
	}
	if calls["bad"] != 2 {
		t.Errorf("server called %d times, want 2", calls["bad"])
	}
	if r, err := turn(context.Background(), "good", "x", nil); err != nil || r.Text() != "ok" {
		t.Errorf("other server: %q %v", r.Text(), err)
	}
	if _, err := TurnCaller(base)(context.Background(), "bad", "x", nil); err == nil || strings.Contains(err.Error(), "unavailable") {
		t.Errorf("a new turn did not start fresh: %v", err)
	}
	// A cancelled turn is not the server's failure.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	turn2 := TurnCaller(func(ctx context.Context, server, tool string, args map[string]any) (CallResult, error) {
		return CallResult{}, ctx.Err()
	})
	for i := 0; i < 3; i++ {
		if _, err := turn2(ctx, "s", "x", nil); err == nil || strings.Contains(err.Error(), "unavailable") {
			t.Fatalf("cancel counted as failure: %v", err)
		}
	}
}
