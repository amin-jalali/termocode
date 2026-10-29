package kube

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// DefaultTimeout bounds one kubectl call.
const DefaultTimeout = 10 * time.Second

// Client runs kubectl for one context and namespace. The zero Context or
// Namespace leaves kubectl's own choice (the kubeconfig's current one).
type Client struct {
	Binary    string // kubectl's path ("" = not installed)
	Context   string
	Namespace string
	X         services.Exec // nil = services.SpawnExec{}
	Timeout   time.Duration // per call; 0 = DefaultTimeout
}

// Find looks kubectl up on PATH. lookPath nil = exec.LookPath.
func Find(lookPath func(string) (string, error)) string {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	p, err := lookPath("kubectl")
	if err != nil {
		return ""
	}
	return p
}

func (c Client) exec() services.Exec {
	if c.X == nil {
		return services.SpawnExec{}
	}
	return c.X
}

// scope is the --context / --namespace flags of the session.
func (c Client) scope(namespaced bool) []string {
	var out []string
	if c.Context != "" {
		out = append(out, "--context", c.Context)
	}
	if namespaced && c.Namespace != "" {
		out = append(out, "--namespace", c.Namespace)
	}
	return out
}

// capture runs kubectl args with the call timeout; a failure is one
// quiet line (ErrorLine).
func (c Client) capture(parent context.Context, args ...string) (string, error) {
	if c.Binary == "" {
		return "", &Error{Kind: ErrNoKubectl}
	}
	d := c.Timeout
	if d <= 0 {
		d = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	out, stderr, code, err := c.exec().Capture(ctx, "", append([]string{c.Binary}, args...)...)
	if err != nil || code != 0 {
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		return "", ErrorLine(stderr, err)
	}
	return out, nil
}

// CurrentContext is the kubeconfig's current context (read only).
func (c Client) CurrentContext(ctx context.Context) (string, error) {
	out, err := c.capture(ctx, "config", "current-context")
	return strings.TrimSpace(out), err
}

// Contexts lists the kubeconfig's context names (read only).
func (c Client) Contexts(ctx context.Context) ([]string, error) {
	out, err := c.capture(ctx, "config", "get-contexts", "-o", "name")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	return names, nil
}

// ContextNamespace is the namespace the context names ("default" when it
// names none).
func (c Client) ContextNamespace(ctx context.Context) (string, error) {
	args := append([]string{"config", "view", "--minify", "-o", "jsonpath={..namespace}"}, c.scope(false)...)
	out, err := c.capture(ctx, args...)
	if err != nil {
		return "", err
	}
	if ns := strings.TrimSpace(out); ns != "" {
		return ns, nil
	}
	return "default", nil
}

func (c Client) get(ctx context.Context, kind string, namespaced bool) ([]byte, error) {
	args := append([]string{"get", kind, "-o", "json"}, c.scope(namespaced)...)
	out, err := c.capture(ctx, args...)
	return []byte(out), err
}

// Namespaces lists the cluster's namespaces.
func (c Client) Namespaces(ctx context.Context) ([]string, error) {
	b, err := c.get(ctx, "namespaces", false)
	if err != nil {
		return nil, err
	}
	return ParseNamespaces(b)
}

// Pods lists the namespace's pods.
func (c Client) Pods(ctx context.Context) ([]Pod, error) {
	b, err := c.get(ctx, "pods", true)
	if err != nil {
		return nil, err
	}
	return ParsePods(b)
}

// Deployments lists the namespace's deployments.
func (c Client) Deployments(ctx context.Context) ([]Deployment, error) {
	b, err := c.get(ctx, "deployments", true)
	if err != nil {
		return nil, err
	}
	return ParseDeployments(b)
}

// Services lists the namespace's services.
func (c Client) Services(ctx context.Context) ([]Service, error) {
	b, err := c.get(ctx, "services", true)
	if err != nil {
		return nil, err
	}
	return ParseServices(b)
}

// Jobs lists the namespace's jobs.
func (c Client) Jobs(ctx context.Context) ([]Job, error) {
	b, err := c.get(ctx, "jobs", true)
	if err != nil {
		return nil, err
	}
	return ParseJobs(b)
}
