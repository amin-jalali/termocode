package containers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/services"
)

// Login sends the password only through stdin; it is never part of argv.
func (e *Engine) Login(ctx context.Context, host, username, password string) error {
	if strings.TrimSpace(host) == "" {
		return errors.New("registry host is required")
	}
	if strings.ContainsAny(host, "\r\n") || strings.ContainsAny(username, "\r\n") {
		return errors.New("invalid registry credentials")
	}
	d := e.Timeout
	if d <= 0 {
		d = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	x := e.exec()
	out, stderr, code, err := x.CaptureInput(ctx, e.Dir, []byte(password+"\n"), e.Binary, "login", host, "--username", username, "--password-stdin")
	_ = out
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
func (e *Engine) ComposeDown(project, dir string) services.Command {
	return e.Compose(project, dir, "down")
}
