// Package clistep runs one agent step with an official CLI and the
// user's own subscription login (CS-1): Claude Code (`claude -p
// --output-format stream-json`) or Codex (`codex exec --json`). termocode
// never handles the login: the CLI reads its own. The prompt goes in on
// stdin (never in argv), the working dir is the project root, and the
// environment is the user's minus termocode's own secrets and the
// vendors' API keys (so the CLI uses the subscription, not a billed
// key). The CLI's JSON event stream becomes ai.Events (text, tool
// calls, files) and its token counts an ai.Usage. A timeout or a Stop
// kills the CLI's whole process group. A usage limit is an
// *ai.LimitError.
package clistep

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// DefaultTimeout ends a CLI step that runs longer than this.
const DefaultTimeout = 30 * time.Minute

// maxLine caps one JSON line of the stream (a longer one is skipped).
const maxLine = 8 << 20

// Spec is one CLI step run.
type Spec struct {
	CLI string // ai.CLIClaude or ai.CLICodex
	// Write lets the CLI change files in Dir (Claude Code: edit tools
	// with acceptEdits; Codex: the workspace-write sandbox). Off, it
	// only reads.
	Write bool
	// Model is the model the CLI is asked for (--model / -m); "" runs
	// the CLI's default. An id that could pass for a flag is left out.
	Model string
	Dir   string   // the project root (the CLI's working dir)
	Env   []string // the environment (nil: os.Environ() with termocode's tools/bin first); CleanEnv is applied
	// Timeout ends the run (0: DefaultTimeout).
	Timeout time.Duration
	// Now is the clock for reset times ("try again in 2 hours"); nil:
	// time.Now.
	Now func() time.Time
}

// Result is what a finished CLI step gave.
type Result struct {
	Text  string    // the final answer
	Usage *ai.Usage // the token counts the CLI reported (nil: none)
	Files []string  // files the CLI changed, relative to Dir when inside it
}

// Bin is the program of a CLI.
func Bin(cli string) string { return cli }

// Args are the CLI's arguments (no prompt: it goes on stdin).
func Args(s Spec) []string {
	model := s.Model != "" && ai.ValidModelID(s.Model)
	switch s.CLI {
	case ai.CLIClaude:
		out := []string{"-p", "--output-format", "stream-json", "--verbose"}
		if model {
			out = append(out, "--model", s.Model)
		}
		if s.Write {
			// Edits, but no shell: it cannot run commands.
			return append(out, "--tools", "Read,Grep,Glob,Edit,Write", "--permission-mode", "acceptEdits")
		}
		return append(out, "--tools", "Read,Grep,Glob")
	case ai.CLICodex:
		sandbox := "read-only"
		if s.Write {
			sandbox = "workspace-write"
		}
		out := []string{"exec", "--json", "--color", "never"}
		if model {
			out = append(out, "-m", s.Model)
		}
		return append(out, "--sandbox", sandbox, "-C", s.Dir, "-")
	}
	return nil
}

// CleanEnv drops the vendors' API keys (the step runs on the
// subscription login, never a billed key) and termocode's own secrets
// (TERMOCODE_* names with KEY, TOKEN, SECRET or PASSWORD).
func CleanEnv(env []string) []string { return lspinstall.DropSecretEnv(env) }

// envGet is the value of k in env.
func envGet(env []string, k string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], k+"="); ok {
			return v
		}
	}
	return ""
}

// lookPath finds bin on the PATH of env.
func lookPath(bin string, env []string) (string, error) {
	for _, d := range filepath.SplitList(envGet(env, "PATH")) {
		if d == "" {
			continue
		}
		p := filepath.Join(d, bin)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// NotInstalled is the error of a CLI that is not on PATH.
func NotInstalled(cli string) error {
	return fmt.Errorf("%s is not installed (%s is not on PATH); install it and sign in with AI: Add AI…, then run the step again", ai.CLITitle(cli), Bin(cli))
}

// NotLoggedIn is the error of a CLI that has no login.
func NotLoggedIn(cli string) error {
	return fmt.Errorf("%s is not logged in: sign in with AI: Add AI… (or run `%s` in a terminal), then run the step again", ai.CLITitle(cli), LoginLine(cli))
}

// Run runs one step: it starts the CLI in s.Dir, writes prompt to its
// stdin, turns its event stream into emit calls (nil: none) and returns
// the final text, usage and changed files. ctx ending (Stop) or the
// timeout kills the CLI's process group.
func Run(ctx context.Context, s Spec, prompt string, emit func(ai.Event)) (Result, error) {
	if emit == nil {
		emit = func(ai.Event) {}
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	env := s.Env
	if env == nil {
		env = lspinstall.EnvWithToolsBin()
	}
	env = CleanEnv(env)
	title := ai.CLITitle(s.CLI)
	if title == "" {
		return Result{}, fmt.Errorf("unknown CLI %q", s.CLI)
	}
	bin, err := lookPath(Bin(s.CLI), env)
	if err != nil {
		return Result{}, NotInstalled(s.CLI)
	}
	if fi, err := os.Stat(s.Dir); err != nil || !fi.IsDir() {
		return Result{}, fmt.Errorf("%s cannot start: the project folder %q is not there", title, s.Dir)
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := spawn.Command(bin, Args(s)...)
	cmd.Dir, cmd.Env = s.Dir, env
	setProcGroup(cmd)
	cmd.Stdin = strings.NewReader(prompt)
	var stderr tailBuf
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("%s did not start: %w", title, err)
	}
	stopped := make(chan struct{})
	var once sync.Once
	kill := func() { once.Do(func() { killGroup(cmd.Process.Pid) }) }
	crash.Go("an AI tool step", func() {
		select {
		case <-runCtx.Done():
			kill()
		case <-stopped:
		}
	})
	emit(ai.Event{Kind: ai.EventTurnStart})
	p := newParser(s, emit)
	readLines(stdout, p.line)
	werr := cmd.Wait()
	close(stopped)
	kill() // what the CLI left behind in its group

	switch {
	case ctx.Err() != nil:
		return Result{}, ctx.Err()
	case runCtx.Err() != nil:
		return Result{}, fmt.Errorf("%s timed out after %s and was stopped", title, timeout.Round(time.Second))
	}
	res := Result{Text: strings.TrimSpace(p.final), Usage: p.usage, Files: p.files}
	if p.limit != "" || (p.failed != "" && isLimit(p.failed)) {
		msg := p.limit
		if msg == "" {
			msg = p.failed
		}
		reset := p.resetAt
		if reset.IsZero() {
			reset = resetTime(msg, s.Now())
		}
		return res, &ai.LimitError{CLI: s.CLI, Msg: oneLine(msg), ResetAt: reset}
	}
	if p.failed != "" {
		if isLoggedOut(p.failed) {
			return res, NotLoggedIn(s.CLI)
		}
		return res, fmt.Errorf("%s: %s", title, oneLine(p.failed))
	}
	if werr != nil {
		why := strings.TrimSpace(stderr.String())
		if isLoggedOut(why) {
			return res, NotLoggedIn(s.CLI)
		}
		if isLimit(why) {
			return res, &ai.LimitError{CLI: s.CLI, Msg: oneLine(why), ResetAt: resetTime(why, s.Now())}
		}
		if why == "" {
			why = werr.Error()
		}
		return res, fmt.Errorf("%s stopped with an error: %s", title, oneLine(lastLines(why, 3)))
	}
	if res.Text == "" && !p.done {
		return res, fmt.Errorf("%s ended without an answer", title)
	}
	return res, nil
}

// readLines calls f for every line of r (a line longer than maxLine is
// skipped).
func readLines(r io.Reader, f func([]byte)) {
	br := bufio.NewReaderSize(r, 64<<10)
	var long bool
	var buf []byte
	for {
		chunk, isPrefix, err := br.ReadLine()
		if len(chunk) > 0 || isPrefix {
			if !long {
				buf = append(buf, chunk...)
				if len(buf) > maxLine {
					buf, long = nil, true
				}
			}
		}
		if !isPrefix && err == nil {
			if !long {
				f(bytes.TrimSpace(buf))
			}
			buf, long = buf[:0], false
		}
		if err != nil {
			if len(buf) > 0 && !long {
				f(bytes.TrimSpace(buf))
			}
			return
		}
	}
}

// tailBuf keeps the last 4 KB written to it.
type tailBuf struct {
	mu sync.Mutex
	b  []byte
}

func (t *tailBuf) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 4096 {
		t.b = append([]byte(nil), t.b[len(t.b)-4096:]...)
	}
	return len(p), nil
}

func (t *tailBuf) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " · ")
}

// oneLine is s on one line, at most 300 runes, secrets redacted.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 300 {
		s = string(r[:299]) + "…"
	}
	return ai.Redact(s)
}
