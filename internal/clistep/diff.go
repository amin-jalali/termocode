package clistep

import (
	"bytes"
	"context"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// DefaultDiffBytes caps the git diff a CLI step's hand-off carries.
const DefaultDiffBytes = 24 << 10

// DiffCutNote ends a diff that was cut at the cap.
const DiffCutNote = "\n… (the diff was cut here; run git diff for the rest)"

// GitDiff is the work so far in the git tree at dir, for the next step's
// input (CS-2): `git status --short`, then `git diff HEAD` in a fenced
// block, at most max bytes. "" when dir is not a git tree, nothing
// changed or git is missing.
func GitDiff(ctx context.Context, dir string, max int) string {
	if max <= 0 {
		max = DefaultDiffBytes
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	git := func(args ...string) (string, error) {
		cmd := spawn.Command("git", append([]string{"-C", dir, "--no-pager"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
		setProcGroup(cmd)
		// Start here, not in a goroutine: the stop below must see the
		// started process to kill its group.
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Start(); err != nil {
			return "", err
		}
		done := make(chan struct{})
		crash.Go("an AI tool step", func() {
			select {
			case <-ctx.Done():
				killGroup(cmd.Process.Pid)
			case <-done:
			}
		})
		err := cmd.Wait()
		close(done)
		return out.String(), err
	}
	status, err := git("status", "--short", "--untracked-files=normal")
	if err != nil || strings.TrimSpace(status) == "" {
		return ""
	}
	diff, err := git("diff", "HEAD", "--no-color", "--no-ext-diff")
	if err != nil {
		diff, _ = git("diff", "--no-color", "--no-ext-diff") // a repo with no commit yet
	}
	out := strings.TrimRight(status, "\n")
	if d := strings.TrimRight(diff, "\n"); d != "" {
		out += "\n```diff\n" + d + "\n```"
	}
	if len(out) <= max {
		return out
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(out[cut]) {
		cut--
	}
	return out[:cut] + DiffCutNote
}
