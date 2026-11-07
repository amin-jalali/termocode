package containers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Container write actions (CTX-19). Every value is checked here so a
// flag or odd text never reaches the engine's argv.

var (
	containerNameRe = lazyre.New(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	signalRe        = lazyre.New(`^(SIG)?[A-Z][A-Z0-9]*(\+[0-9]+)?$|^[0-9]{1,2}$`)
	sizeRe          = lazyre.New(`^[0-9]+[bkmgBKMG]?$`)
	cpusRe          = lazyre.New(`^[0-9]+(\.[0-9]+)?$`)
)

func checkRef(kind, s string) error {
	if s == "" {
		return fmt.Errorf("%s is empty", kind)
	}
	if strings.HasPrefix(s, "-") || strings.ContainsAny(s, " \t\r\n\x00") {
		return fmt.Errorf("%s %q is not valid", kind, s)
	}
	return nil
}

// Kill sends a signal to a container ("" = the engine's default, KILL).
func (e *Engine) Kill(ctx context.Context, id, signal string) error {
	if err := checkRef("container", id); err != nil {
		return err
	}
	args := []string{"kill"}
	if signal != "" {
		if !signalRe.MatchString(signal) {
			return fmt.Errorf("signal %q is not valid", signal)
		}
		args = append(args, "--signal", signal)
	}
	return e.action(ctx, 60*time.Second, append(args, id)...)
}

// Rename gives a container a new name.
func (e *Engine) Rename(ctx context.Context, id, name string) error {
	if err := checkRef("container", id); err != nil {
		return err
	}
	if !containerNameRe.MatchString(name) {
		return fmt.Errorf("name %q is not valid (letters, digits, _ . -)", name)
	}
	return e.action(ctx, 60*time.Second, "rename", id, name)
}

// SetRestartPolicy sets no, always, unless-stopped or on-failure[:n].
func (e *Engine) SetRestartPolicy(ctx context.Context, id, policy string) error {
	if err := checkRef("container", id); err != nil {
		return err
	}
	switch base, n, hasN := strings.Cut(policy, ":"); {
	case !hasN && (base == "no" || base == "always" || base == "unless-stopped" || base == "on-failure"):
	case hasN && base == "on-failure":
		if v, err := strconv.Atoi(n); err != nil || v < 0 {
			return fmt.Errorf("retry count %q is not a number", n)
		}
	default:
		return fmt.Errorf("restart policy %q is not known", policy)
	}
	return e.action(ctx, 60*time.Second, "update", "--restart", policy, id)
}

// Resources are new limits for `update`; "" keeps a value as it is.
type Resources struct {
	CPUs       string // e.g. "1.5"
	Memory     string // e.g. "512m"
	MemorySwap string // e.g. "1g"
}

// SetResources changes the CPU and memory limits of a container.
func (e *Engine) SetResources(ctx context.Context, id string, r Resources) error {
	if err := checkRef("container", id); err != nil {
		return err
	}
	args := []string{"update"}
	if r.CPUs != "" {
		if !cpusRe.MatchString(r.CPUs) {
			return fmt.Errorf("CPUs %q is not a number", r.CPUs)
		}
		args = append(args, "--cpus", r.CPUs)
	}
	for _, s := range []struct{ flag, v string }{{"--memory", r.Memory}, {"--memory-swap", r.MemorySwap}} {
		if s.v == "" {
			continue
		}
		if !sizeRe.MatchString(s.v) {
			return fmt.Errorf("size %q is not valid (like 512m or 1g)", s.v)
		}
		args = append(args, s.flag, s.v)
	}
	if len(args) == 1 {
		return errors.New("no limit to change")
	}
	return e.action(ctx, 60*time.Second, append(args, id)...)
}

// NetworkConnect attaches a container to a network.
func (e *Engine) NetworkConnect(ctx context.Context, network, id string) error {
	return e.network(ctx, "connect", network, id)
}

// NetworkDisconnect detaches a container from a network.
func (e *Engine) NetworkDisconnect(ctx context.Context, network, id string) error {
	return e.network(ctx, "disconnect", network, id)
}

func (e *Engine) network(ctx context.Context, verb, network, id string) error {
	if err := checkRef("network", network); err != nil {
		return err
	}
	if err := checkRef("container", id); err != nil {
		return err
	}
	return e.action(ctx, 60*time.Second, "network", verb, network, id)
}
