package containerhost

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// Endpoint is where the engine of a context runs: "unix:///var/run/
// docker.sock", "ssh://me@host", "tcp://host:2376"… ("" = unknown).
// binary "" is "docker"; engineCtx "" is the user's current one (then
// DOCKER_HOST / CONTAINER_HOST wins, as the engine CLI does). It runs one
// read-only engine call and never changes the user's engine config.
func Endpoint(ctx context.Context, x services.Exec, binary, engineCtx string) string {
	if binary == "" {
		binary = "docker"
	}
	if x == nil {
		x = services.SpawnExec{}
	}
	ctx, cancel := contextWithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.HasPrefix(filepath.Base(binary), "podman") {
		if engineCtx == "" {
			if v := os.Getenv("CONTAINER_HOST"); v != "" {
				return v
			}
		}
		out, _, code, err := x.Capture(ctx, "", binary, "system", "connection", "list", "--format", "{{.Name}}\t{{.URI}}\t{{.Default}}")
		if err != nil || code != 0 {
			return ""
		}
		for _, line := range strings.Split(out, "\n") {
			f := strings.Split(strings.TrimSpace(line), "\t")
			if len(f) < 3 {
				continue
			}
			if (engineCtx != "" && f[0] == engineCtx) || (engineCtx == "" && f[2] == "true") {
				return f[1]
			}
		}
		return ""
	}
	if engineCtx == "" {
		if v := os.Getenv("DOCKER_HOST"); v != "" {
			return v
		}
	} else if strings.HasPrefix(engineCtx, "-") {
		return ""
	}
	args := []string{binary}
	if engineCtx != "" {
		// The flag too, so every call of a workspace names its context.
		args = append(args, "--context", engineCtx)
	}
	args = append(args, "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
	if engineCtx != "" {
		args = append(args, engineCtx)
	}
	out, _, code, err := x.Capture(ctx, "", args...)
	if err != nil || code != 0 {
		return ""
	}
	return strings.TrimSpace(out)
}

// RemoteEndpoint reports whether an engine endpoint is on another machine
// (ssh:// or tcp://), where every engine call is slow and the shellmux
// pays off.
func RemoteEndpoint(endpoint string) bool {
	e := strings.ToLower(strings.TrimSpace(endpoint))
	return strings.HasPrefix(e, "ssh://") || strings.HasPrefix(e, "tcp://")
}

// SSHEndpoint reports whether the endpoint is reached over ssh: then an
// ssh ControlMaster in ~/.ssh/config makes even one-shot calls fast.
func SSHEndpoint(endpoint string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(endpoint)), "ssh://")
}

func contextWithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, d)
}
