package services

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// MaxCapture caps a captured stdout or stderr (bytes).
const MaxCapture = 1 << 20

// Exec runs docker. Capture is a short call with its output; Stream is
// a long one (docker events) read line by line until stop is called.
type Exec interface {
	Capture(ctx context.Context, dir string, argv ...string) (stdout, stderr string, code int, err error)
	CaptureInput(ctx context.Context, dir string, input []byte, argv ...string) (stdout, stderr string, code int, err error)
	Stream(ctx context.Context, dir string, argv ...string) (out io.ReadCloser, stop func(), err error)
}

// SpawnExec is the real Exec: spawn (the Android trampoline), its own
// process group (a cancel kills docker and its children), SIGTERM when
// termocode dies (Linux), output capped at MaxCapture.
type SpawnExec struct {
	Env []string // nil = this process's environment
}

type capBuf struct {
	b bytes.Buffer
}

func (c *capBuf) Write(p []byte) (int, error) {
	if room := MaxCapture - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

func (e SpawnExec) cmd(ctx context.Context, dir string, argv []string) *exec.Cmd {
	cmd := spawn.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	if e.Env != nil {
		cmd.Env = e.Env
	}
	setProcGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			killGroup(cmd.Process.Pid)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// Capture runs argv and returns its output. err is set only when the
// program could not start or was cancelled; a non-zero exit is code.
func (e SpawnExec) Capture(ctx context.Context, dir string, argv ...string) (string, string, int, error) {
	if len(argv) == 0 {
		return "", "", -1, errors.New("no command")
	}
	var out, errb capBuf
	cmd := e.cmd(ctx, dir, argv)
	cmd.Stdout, cmd.Stderr = &out, &errb
	exited, err := startAndWait(cmd)
	if err != nil {
		return "", "", -1, err
	}
	err = <-exited
	if ctx.Err() != nil {
		return out.b.String(), errb.b.String(), -1, ctx.Err()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.b.String(), errb.b.String(), ee.ExitCode(), nil
	}
	if err != nil {
		return out.b.String(), errb.b.String(), -1, err
	}
	return out.b.String(), errb.b.String(), 0, nil
}

// CaptureInput runs argv with input on stdin. Input is never included in
// process arguments or error text.
func (e SpawnExec) CaptureInput(ctx context.Context, dir string, input []byte, argv ...string) (string, string, int, error) {
	if len(argv) == 0 {
		return "", "", -1, errors.New("no command")
	}
	var out, errb capBuf
	cmd := e.cmd(ctx, dir, argv)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout, cmd.Stderr = &out, &errb
	exited, err := startAndWait(cmd)
	if err != nil {
		return "", "", -1, err
	}
	err = <-exited
	if ctx.Err() != nil {
		return out.b.String(), errb.b.String(), -1, ctx.Err()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.b.String(), errb.b.String(), ee.ExitCode(), nil
	}
	if err != nil {
		return out.b.String(), errb.b.String(), -1, err
	}
	return out.b.String(), errb.b.String(), 0, nil
}

// Stream starts argv with its stdout on a pipe. stop kills the process
// group and waits for it; it is safe to call more than once.
func (e SpawnExec) Stream(ctx context.Context, dir string, argv ...string) (io.ReadCloser, func(), error) {
	if len(argv) == 0 {
		return nil, nil, errors.New("no command")
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	cmd := e.cmd(ctx, dir, argv)
	cmd.Stdout = w
	exited, err := startAndWait(cmd)
	w.Close() // the child holds its own copy
	if err != nil {
		r.Close()
		return nil, nil, err
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			killGroup(cmd.Process.Pid)
			<-exited
			r.Close()
		})
	}
	return r, stop, nil
}
