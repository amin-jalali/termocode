//go:build unix

package clistep

import (
	"context"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// A stop while the login check runs gives "unknown", not a wrong answer.
func TestCheckStopped(t *testing.T) {
	f := newFake(t, "FAKE_STATUS_TEXT=Logged in")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := Check(ctx, ai.CLICodex, f.env); got != StatusUnknown {
		t.Errorf("check = %v", got)
	}
}

// A stop while git runs gives no diff (and no data race on the process).
func TestGitDiffStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := GitDiff(ctx, t.TempDir(), 0); got != "" {
		t.Errorf("diff = %q", got)
	}
}
