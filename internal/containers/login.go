package containers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// DefaultLoginTimeout bounds one `login` (it talks to the registry).
const DefaultLoginTimeout = time.Minute

// Login runs `<engine> login <host> --username <user> --password-stdin`.
// The password goes only to stdin: never into argv, the environment or a
// file. Errors carry the engine's message with the password scrubbed.
func (e *Engine) Login(ctx context.Context, host, username, password string) error {
	if err := ValidRegistryHost(host); err != nil {
		return err
	}
	if err := ValidRegistryUser(username); err != nil {
		return err
	}
	d := e.LoginTimeout
	if d <= 0 {
		d = DefaultLoginTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	_, stderr, code, err := e.exec().CaptureInput(ctx, e.Dir, []byte(password+"\n"), e.Binary, "login", host, "--username", username, "--password-stdin")
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("login timed out after %s", d)
	}
	if err != nil {
		return errors.New(redactPassword(err.Error(), password))
	}
	if code != 0 {
		p := strings.TrimSpace(stderr)
		if p == "" {
			p = fmt.Sprintf("login exited %d", code)
		}
		return errors.New(redactPassword(p, password))
	}
	return nil
}

func redactPassword(message, password string) string {
	if password == "" {
		return message
	}
	return strings.ReplaceAll(message, password, "[REDACTED]")
}

func (e *Engine) ComposeUp(project, dir string) services.Command {
	return e.Compose(project, dir, "up", "-d")
}
