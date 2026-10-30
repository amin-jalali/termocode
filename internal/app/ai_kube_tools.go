package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/lazyre"
)

// ── AI tools for the Kubernetes view (G-K8S) ─────────────────────────────
//
// Read only, and they run at once: kube_list, kube_describe, kube_logs.
// They use the view's context and namespace (the user's choice; the AI
// can name another namespace, never another context). Restart and
// port-forward are not tools: they stay with the user. Output passes the
// secret redactor; logs are capped at services_max_log_kb.

const aiKubeMaxLines = 1000

// aiKubeNameRe is a Kubernetes object or namespace name (DNS subdomain).
var aiKubeNameRe = lazyre.New(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

func aiKubeValidName(what, s string) error {
	if len(s) > 253 || !aiKubeNameRe.MatchString(s) {
		return fmt.Errorf("%s %q is not a Kubernetes name", what, s)
	}
	return nil
}

// aiKubeClient is kubectl in the view's scope (namespace "" = the view's).
func aiKubeClient(ctx context.Context, call aiIDECaller, namespace string) (kube.Client, error) {
	var c kube.Client
	_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if m.kube != nil {
			c = m.kube.client()
		}
		return "", nil, nil
	})
	if err != nil {
		return c, err
	}
	c.X = servicesExec
	if c.Binary == "" {
		if c.Binary = kube.Find(nil); c.Binary == "" {
			return c, &kube.Error{Kind: kube.ErrNoKubectl}
		}
	}
	if namespace != "" {
		c.Namespace = namespace
	}
	return c, nil
}

func aiKubeHeader(c kube.Client) string {
	ctx, ns := c.Context, c.Namespace
	if ctx == "" {
		ctx = "(kubeconfig's current)"
	}
	if ns == "" {
		ns = "(the context's)"
	}
	return "Kubernetes context " + ctx + " · namespace " + ns
}

// aiKubeListReport is kube_list's answer: one block per kind.
func aiKubeListReport(c kube.Client, pods []kube.Pod, deploys []kube.Deployment, svcs []kube.Service, jobs []kube.Job, errs map[string]error) string {
	now := clock.Now()
	var b strings.Builder
	b.WriteString(aiKubeHeader(c) + "\n")
	section := func(name string, n int, err error) bool {
		fmt.Fprintf(&b, "\n%s (%d)\n", strings.ToUpper(name), n)
		if err != nil {
			b.WriteString("  cannot list: " + err.Error() + "\n")
			return false
		}
		return true
	}
	if section("pods", len(pods), errs["pods"]) {
		for _, p := range pods {
			fmt.Fprintf(&b, "  %s ready %s · %s · %d restarts · age %s · containers %s\n", p.Name, p.Ready(), p.Status, p.Restarts,
				kube.Age(p.Created, now), strings.Join(p.Containers, ","))
		}
	}
	if section("deployments", len(deploys), errs["deployments"]) {
		for _, d := range deploys {
			fmt.Fprintf(&b, "  %s ready %s · updated %d · available %d · age %s\n", d.Name, d.Ready(), d.Updated, d.Available, kube.Age(d.Created, now))
		}
	}
	if section("services", len(svcs), errs["services"]) {
		for _, s := range svcs {
			fmt.Fprintf(&b, "  %s %s · %s · %s\n", s.Name, s.Type, s.ClusterIP, s.PortsText())
		}
	}
	if section("jobs", len(jobs), errs["jobs"]) {
		for _, j := range jobs {
			fmt.Fprintf(&b, "  %s %s · %s done · %d failed · age %s\n", j.Name, j.Status(), j.Completions(), j.Failed, kube.Age(j.Created, now))
		}
	}
	out, _ := aiServicesRedact(b.String())
	return out
}

func aiKubeTools(call aiIDECaller) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	nsParam := str("namespace (default: the one the Kubernetes view shows)")
	validNS := func(a map[string]any) error {
		if ns := strings.TrimSpace(ai.ArgString(a, "namespace")); ns != "" {
			return aiKubeValidName("namespace", ns)
		}
		return nil
	}
	return []ai.Tool{
		{
			Name: "kube_list",
			Description: "Read only: the pods, deployments, services and jobs of one Kubernetes namespace, through the user's kubectl, " +
				"in the context the Kubernetes view uses. Pods: ready, status, restarts, age, containers. You cannot change the cluster.",
			Params:   map[string]any{"namespace": nsParam},
			Validate: validNS,
			Describe: func(a map[string]any) string { return "List Kubernetes objects" },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				c, err := aiKubeClient(ctx, call, strings.TrimSpace(ai.ArgString(a, "namespace")))
				if err != nil {
					return "", err
				}
				errs := map[string]error{}
				pods, err := c.Pods(ctx)
				if err != nil {
					if k := kube.KindOf(err); k == kube.ErrNoCluster || k == kube.ErrNoConfig || k == kube.ErrTimeout {
						return "", err
					}
					errs["pods"] = err
				}
				deploys, err := c.Deployments(ctx)
				if err != nil {
					errs["deployments"] = err
				}
				svcs, err := c.Services(ctx)
				if err != nil {
					errs["services"] = err
				}
				jobs, err := c.Jobs(ctx)
				if err != nil {
					errs["jobs"] = err
				}
				return aiKubeListReport(c, pods, deploys, svcs, jobs, errs), nil
			},
		},
		{
			Name:        "kube_describe",
			Description: "Read only: `kubectl describe` of one pod, deployment, service or job (events included), secrets redacted.",
			Params: map[string]any{
				"kind":      map[string]any{"type": "string", "enum": []string{"pod", "deployment", "service", "job"}, "description": "what name is"},
				"name":      str("the object's name (see kube_list)"),
				"namespace": nsParam,
			},
			Required: []string{"kind", "name"},
			Validate: func(a map[string]any) error {
				switch ai.ArgString(a, "kind") {
				case "pod", "deployment", "service", "job":
				default:
					return fmt.Errorf("kind must be pod, deployment, service or job")
				}
				if err := aiKubeValidName("name", strings.TrimSpace(ai.ArgString(a, "name"))); err != nil {
					return err
				}
				return validNS(a)
			},
			Describe: func(a map[string]any) string {
				return "Describe " + ai.ArgString(a, "kind") + " " + ai.ArgString(a, "name")
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				c, err := aiKubeClient(ctx, call, strings.TrimSpace(ai.ArgString(a, "namespace")))
				if err != nil {
					return "", err
				}
				text, err := c.Describe(ctx, ai.ArgString(a, "kind"), strings.TrimSpace(ai.ArgString(a, "name")))
				if err != nil {
					return "", err
				}
				text, _ = aiServicesRedact(text)
				return aiKubeHeader(c) + "\n" + aiCap(text, loadServicesSettings().MaxInspectKB, false), nil
			},
		},
		{
			Name: "kube_logs",
			Description: "Read only: the last log lines of one pod (lines default 100, max 1000), secrets redacted. " +
				"Name the container for a pod with more than one. Capped at services_max_log_kb (newest lines kept).",
			Params: map[string]any{
				"pod":       str("the pod's name (see kube_list)"),
				"container": str("the container (needed when the pod has more than one)"),
				"lines":     map[string]any{"type": "integer", "description": "how many last lines (default 100, max 1000)"},
				"namespace": nsParam,
			},
			Required: []string{"pod"},
			Validate: func(a map[string]any) error {
				if err := aiKubeValidName("pod", strings.TrimSpace(ai.ArgString(a, "pod"))); err != nil {
					return err
				}
				if ctr := strings.TrimSpace(ai.ArgString(a, "container")); ctr != "" {
					if err := aiKubeValidName("container", ctr); err != nil {
						return err
					}
				}
				return validNS(a)
			},
			Describe: func(a map[string]any) string { return "Pod logs " + ai.ArgString(a, "pod") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				c, err := aiKubeClient(ctx, call, strings.TrimSpace(ai.ArgString(a, "namespace")))
				if err != nil {
					return "", err
				}
				lines := ai.ArgInt(a, "lines")
				if lines <= 0 {
					lines = 100
				}
				lines = min(lines, aiKubeMaxLines)
				pod := strings.TrimSpace(ai.ArgString(a, "pod"))
				text, err := c.Logs(ctx, pod, strings.TrimSpace(ai.ArgString(a, "container")), lines)
				if err != nil {
					return "", err
				}
				text, n := aiServicesRedact(text)
				head := fmt.Sprintf("%s\nLogs of pod %s: [%d redacted values]", aiKubeHeader(c), pod, n)
				return head + "\n" + aiCap(text, loadServicesSettings().MaxLogKB, true), nil
			},
		},
	}
}
