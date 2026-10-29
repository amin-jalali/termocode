package kube

import (
	"context"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// restartTimeout bounds `rollout restart` (it only patches the object).
const restartTimeout = 30 * time.Second

// Describe is `kubectl describe <kind> <name>` as text (read only).
func (c Client) Describe(ctx context.Context, kind, name string) (string, error) {
	return c.capture(ctx, append([]string{"describe", kind, name}, c.scope(true)...)...)
}

// RolloutRestart is `kubectl rollout restart deployment/<name>`: the one
// write of the view. The app asks the user first; the AI never runs it.
func (c Client) RolloutRestart(ctx context.Context, name string) error {
	if c.Timeout <= 0 || c.Timeout < restartTimeout {
		c.Timeout = restartTimeout
	}
	_, err := c.capture(ctx, append([]string{"rollout", "restart", "deployment/" + name}, c.scope(true)...)...)
	return err
}

func shown(pod, container string) string {
	if container == "" {
		return pod
	}
	return pod + "/" + container
}

// LogsCommand follows one pod's logs in a terminal tab (container ""
// = the pod's only or default container).
func (c Client) LogsCommand(pod, container string) services.Command {
	argv := append([]string{c.Binary, "logs", "--follow", "--tail", "200"}, c.scope(true)...)
	argv = append(argv, pod)
	if container != "" {
		argv = append(argv, "--container", container)
	}
	return services.Command{Label: "logs: " + shown(pod, container), Argv: argv}
}

// ShellCommand opens bash (or sh) in a pod's container in a terminal tab.
func (c Client) ShellCommand(pod, container string) services.Command {
	argv := append([]string{c.Binary, "exec", "-it"}, c.scope(true)...)
	argv = append(argv, pod)
	if container != "" {
		argv = append(argv, "--container", container)
	}
	argv = append(argv, "--", "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh")
	return services.Command{Label: "shell: " + shown(pod, container), Argv: argv}
}
