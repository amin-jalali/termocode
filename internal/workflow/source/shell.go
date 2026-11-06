package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Shell source limits.
const (
	DefaultShellTimeout = 60 * time.Second
	MaxShellTimeout     = 30 * time.Minute
	// MaxShellOutput caps stdout; more fails the step.
	MaxShellOutput = 16 << 20
	stderrTail     = 2 << 10
)

// Parse modes of a shell source.
const (
	ParseJSON  = "json"  // stdout is one JSON document
	ParseJSONL = "jsonl" // one JSON document per line; blank lines skipped
	ParseLines = "lines" // one item per non-blank line, a JSON string
)

// ShellInput is a `"from": "shell"` source.
type ShellInput struct {
	// Argv is the program and its arguments; never a shell string. The
	// program is looked up in PATH or is a path inside the workspace.
	Argv []string
	// Cwd is relative to the workspace ("" or "." is the workspace) and
	// must stay inside it, symlinks included.
	Cwd string
	// Timeout ends the command (0: DefaultShellTimeout).
	Timeout time.Duration
	// Parse is json, jsonl or lines ("" is json).
	Parse string
	Shape
}

// ErrTimeout is returned when a command runs past its timeout.
var ErrTimeout = errors.New("timed out")

// Shell runs a command and returns its items.
func (s *Sources) Shell(ctx context.Context, in ShellInput) (Result, error) {
	if len(in.Argv) == 0 || in.Argv[0] == "" {
		return Result{}, errors.New("shell: argv is empty")
	}
	for i, a := range in.Argv {
		if strings.Contains(a, "${") {
			return Result{}, fmt.Errorf("shell: argv[%d] holds a ${…} reference; secrets and references are not allowed in argv", i)
		}
		if strings.ContainsRune(a, 0) {
			return Result{}, fmt.Errorf("shell: argv[%d] holds a NUL byte", i)
		}
	}
	parse := in.Parse
	if parse == "" {
		parse = ParseJSON
	}
	if parse != ParseJSON && parse != ParseJSONL && parse != ParseLines {
		return Result{}, fmt.Errorf("shell: parse must be json, jsonl or lines, not %q", in.Parse)
	}
	if parse == ParseLines && strings.TrimSpace(in.Items) != "" && strings.TrimSpace(in.Items) != "$" {
		return Result{}, errors.New("shell: items is not used with parse: lines")
	}
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = DefaultShellTimeout
	}
	if timeout > MaxShellTimeout {
		return Result{}, fmt.Errorf("shell: timeout %s is over the limit of %s", timeout, MaxShellTimeout)
	}
	col, err := newCollector(in.Shape)
	if err != nil {
		return Result{}, fmt.Errorf("shell: %w", err)
	}
	cwd, err := s.inside(in.Cwd)
	if err != nil {
		return Result{}, fmt.Errorf("shell: cwd: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := spawn.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = cwd
	stdout := &capped{max: MaxShellOutput}
	stderr := &tail{max: stderrTail}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	runErr := cmd.Run()
	name := in.Argv[0]
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return Result{}, fmt.Errorf("shell: %s: %w after %s", name, ErrTimeout, timeout)
	case stdout.over:
		return Result{}, fmt.Errorf("shell: %s: output is over %d MB", name, MaxShellOutput>>20)
	case runErr != nil:
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return Result{}, fmt.Errorf("shell: %s: %v: %s", name, runErr, msg)
		}
		return Result{}, fmt.Errorf("shell: %s: %v", name, runErr)
	}
	if err := parseOutput(col, stdout.Bytes(), parse); err != nil {
		return Result{}, fmt.Errorf("shell: %s: %w", name, err)
	}
	return col.res, nil
}

func parseOutput(col *collector, out []byte, mode string) error {
	switch mode {
	case ParseJSON:
		if len(bytes.TrimSpace(out)) == 0 {
			return nil
		}
		return col.addDoc(out)
	case ParseJSONL:
		for n, line := range bytes.Split(out, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			if !json.Valid(line) {
				return fmt.Errorf("line %d is not valid JSON", n+1)
			}
			if err := col.addDoc(line); err != nil {
				return err
			}
		}
		return nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		b, _ := json.Marshal(line)
		if err := col.add(b, ""); err != nil {
			return err
		}
	}
	return nil
}

// capped keeps up to max bytes and notes when there was more.
type capped struct {
	bytes.Buffer
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.Len(); len(p) > room {
		c.over = true
		if room > 0 {
			c.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return c.Buffer.Write(p)
}

// tail keeps the last max bytes.
type tail struct {
	buf []byte
	max int
}

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.buf) }
