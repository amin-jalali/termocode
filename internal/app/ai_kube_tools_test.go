package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func aiKubeTestTools(t *testing.T) ([]ai.Tool, *Model) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := kubeTestModel(t, 150, 42)
	call := func(ctx context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
	return aiKubeTools(call), m
}

func TestAIKubeToolsAreReadOnly(t *testing.T) {
	tools, _ := aiKubeTestTools(t)
	var names []string
	for _, tl := range tools {
		names = append(names, tl.Name)
		if tl.Mutating || tl.Class != "" || tl.Gate != nil {
			t.Errorf("%s asks or writes", tl.Name)
		}
	}
	if strings.Join(names, ",") != "kube_list,kube_describe,kube_logs" {
		t.Errorf("tools = %v", names)
	}
	for _, bad := range []string{"restart", "forward", "scale", "delete", "exec"} {
		for _, n := range names {
			if strings.Contains(n, bad) {
				t.Errorf("tool %s may change the cluster", n)
			}
		}
	}
}

func TestAIKubeListUsesTheViewScope(t *testing.T) {
	f := kubeFake(t)
	tools, m := aiKubeTestTools(t)
	m.kube.binary, m.kube.context, m.kube.namespace = f.Kubectl(), "dev", "shop"
	out, err := aiContainersTool(t, tools, "kube_list").Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"context dev · namespace shop", "PODS (5)", "worker-5c4b-77k2p ready 0/1 · CrashLoopBackOff · 7 restarts",
		"DEPLOYMENTS (3)", "SERVICES (3)", "web NodePort", "JOBS (3)", "broken Failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if f.Count("--context dev --namespace shop") != 4 {
		t.Errorf("calls = %v", f.Calls())
	}
	// Another namespace, same context.
	if _, err := aiContainersTool(t, tools, "kube_list").Run(context.Background(), map[string]any{"namespace": "kube-system"}); err != nil {
		t.Fatal(err)
	}
	if f.Count("--context dev --namespace kube-system") != 4 {
		t.Errorf("calls = %v", f.Calls())
	}
}

func TestAIKubeLogsAndDescribe(t *testing.T) {
	f := kubeFake(t)
	tools, m := aiKubeTestTools(t)
	m.kube.binary, m.kube.context, m.kube.namespace = f.Kubectl(), "dev", "shop"
	f.Set(t, "logs.txt", "starting\nDB_PASSWORD=hunter2hunter2\n")
	out, err := aiContainersTool(t, tools, "kube_logs").Run(context.Background(), map[string]any{"pod": "api-1", "container": "api", "lines": 5000})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "hunter2") || !strings.Contains(out, "starting") {
		t.Errorf("logs not redacted:\n%s", out)
	}
	if f.Count("logs --tail 1000 --context dev --namespace shop api-1 --container api") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	f.Set(t, "describe.txt", "Name: api\nEvents: <none>\n")
	out, err = aiContainersTool(t, tools, "kube_describe").Run(context.Background(), map[string]any{"kind": "deployment", "name": "api"})
	if err != nil || !strings.Contains(out, "Events: <none>") {
		t.Errorf("describe = %q, %v", out, err)
	}
}

func TestAIKubeToolsRejectBadNames(t *testing.T) {
	tools, _ := aiKubeTestTools(t)
	for name, args := range map[string]map[string]any{
		"kube_describe": {"kind": "secret", "name": "x"},
		"kube_logs":     {"pod": "api; rm -rf /"},
		"kube_list":     {"namespace": "--all-namespaces"},
	} {
		if err := aiContainersTool(t, tools, name).Validate(args); err == nil {
			t.Errorf("%s accepted %v", name, args)
		}
	}
}
