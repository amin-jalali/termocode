package agentenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// maxOutput caps the kept setup output.
const maxOutput = 64 << 10

// capBuf keeps the last maxOutput bytes written.
type capBuf struct{ b bytes.Buffer }

func (c *capBuf) Write(p []byte) (int, error) {
	n := len(p)
	c.b.Write(p)
	if c.b.Len() > maxOutput {
		keep := c.b.Bytes()[c.b.Len()-maxOutput:]
		c.b = *bytes.NewBuffer(append([]byte(nil), keep...))
	}
	return n, nil
}

// RunSetup runs cmds one by one in dir with env (sh -c; cmd /C on
// Windows), all within timeout. It stops at the first failure. The
// output (both streams) and the error have every value of redact
// replaced with ***.
func RunSetup(ctx context.Context, dir string, env, cmds []string, timeout time.Duration, redact []string) (string, error) {
	if timeout <= 0 {
		timeout = DefaultSetupTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var out capBuf
	for i, c := range cmds {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = spawn.CommandContext(ctx, "cmd", "/C", c)
		} else {
			cmd = spawn.CommandContext(ctx, "sh", "-c", c)
		}
		cmd.Dir, cmd.Env = dir, env
		cmd.Stdout, cmd.Stderr = &out, &out
		cmd.WaitDelay = 2 * time.Second
		err := cmd.Run()
		text := Redact(out.b.String(), redact)
		if err == nil {
			continue
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return text, fmt.Errorf("setup %d (%s) timed out after %s", i+1, Redact(short(c), redact), timeout)
		}
		if ctx.Err() != nil {
			return text, ctx.Err()
		}
		last := lastLine(text)
		if last != "" {
			last = ": " + last
		}
		return text, fmt.Errorf("setup %d (%s) failed: %s%s", i+1, Redact(short(c), redact), Redact(err.Error(), redact), last)
	}
	return Redact(out.b.String(), redact), nil
}

func short(s string) string {
	if r := []rune(s); len(r) > 60 {
		return string(r[:57]) + "..."
	}
	return s
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return short(strings.TrimSpace(s))
}
