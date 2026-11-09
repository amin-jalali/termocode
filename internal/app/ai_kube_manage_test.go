package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// kube_manage: everything of the Kubernetes view. The view's switches
// (context, namespace), terminals (shell, logs), port-forwards and the
// reads run at once, as the view does them; restart, scale, delete,
// rollback, set image, apply, cordon / uncordon / drain, the helm writes,
// upload and debug are the view's risky cards: they ask every time with
// no "Always allow". kube_list / kube_describe / kube_logs take a kube
// context too.

func aiKubeManage(t *testing.T) (ai.Tool, *Model, func() []string) {
	t.Helper()
	f := kubeFake(t)
	tools, m := aiKubeTestTools(t)
	m.kube.binary, m.kube.context, m.kube.namespace = f.Kubectl(), "dev", "shop"
	f.Set(t, "contexts.txt", "dev\nprod\n")
	f.Set(t, "current-context.txt", "dev\n")
	return aiContainersTool(t, tools, "kube_manage"), m, f.Calls
}

func TestAIKubeManageGates(t *testing.T) {
	tl, _, _ := aiKubeManage(t)
	cards := []string{"restart", "scale", "delete", "rollout_undo", "set_image", "apply", "cordon", "uncordon", "drain",
		"helm_install", "helm_repo_add", "helm_rollback", "helm_uninstall", "upload", "debug"}
	at := []string{"contexts", "namespaces", "use_context", "use_namespace", "yaml", "events", "top", "nodes", "hpa", "rbac",
		"report", "objects", "kustomize", "helm_list", "helm_status", "helm_history", "rollout_history", "pod_files",
		"snippet", "forwards", "shell", "logs_follow", "port_forward", "stop_forward"}
	ops := aiParamEnum(tl.Params["op"])
	if len(ops) != len(cards)+len(at)+1 { // + download
		t.Errorf("%d ops: %v", len(ops), ops)
	}
	for _, op := range cards {
		if ask, class := tl.Gate(map[string]any{"op": op}); !ask || class != "" {
			t.Errorf("%s gate = %v %q, want a card every time", op, ask, class)
		}
	}
	for _, op := range at {
		if ask, _ := tl.Gate(map[string]any{"op": op}); ask {
			t.Errorf("%s asks; the view does it at once", op)
		}
	}
	if ask, class := tl.Gate(map[string]any{"op": "download"}); !ask || class != ai.ClassFileWrite {
		t.Errorf("download gate = %v %q, want the file write class", ask, class)
	}
}

func TestAIKubeManageSwitchAndWrites(t *testing.T) {
	tl, m, calls := aiKubeManage(t)
	run := func(a map[string]any) string {
		t.Helper()
		if err := tl.Validate(a); err != nil {
			t.Fatalf("%v: %v", a, err)
		}
		out, err := tl.Run(context.Background(), a)
		if err != nil {
			t.Fatalf("%v: %v", a, err)
		}
		return out
	}
	covAIMustContain(t, "contexts", run(map[string]any{"op": "contexts"}), "dev", "prod")
	run(map[string]any{"op": "use_namespace", "name": "payments"})
	if m.kube.namespace != "payments" {
		t.Errorf("namespace = %q", m.kube.namespace)
	}
	run(map[string]any{"op": "use_context", "name": "prod"})
	if m.kube.context != "prod" {
		t.Errorf("context = %q", m.kube.context)
	}
	if err := tl.Validate(map[string]any{"op": "use_context", "name": "nope"}); err == nil {
		t.Error("use_context nope accepted")
	}
	covAIMustContain(t, "restart card", tl.Describe(map[string]any{"op": "restart", "name": "api", "context": "dev", "namespace": "shop"}),
		"Restart deployment api", "kubectl rollout restart deployment/api", "context dev", "namespace shop")
	run(map[string]any{"op": "restart", "name": "api", "context": "dev", "namespace": "shop"})
	run(map[string]any{"op": "scale", "kind": "deployment", "name": "api", "replicas": 3, "context": "dev", "namespace": "shop"})
	run(map[string]any{"op": "delete", "kind": "pod", "name": "api-7d9f8c6b5-x2x4q", "context": "dev", "namespace": "shop"})
	run(map[string]any{"op": "set_image", "name": "api", "container": "api", "image": "ghcr.io/acme/api:1.5", "context": "dev", "namespace": "shop"})
	run(map[string]any{"op": "cordon", "name": "node-1", "context": "dev"})
	got := strings.Join(calls(), "\n")
	for _, w := range []string{"rollout restart deployment/api --context dev --namespace shop",
		"scale deployment/api --replicas=3 --context dev --namespace shop",
		"delete pod api-7d9f8c6b5-x2x4q --wait=false --context dev --namespace shop",
		"set image deployment/api api=ghcr.io/acme/api:1.5 --context dev --namespace shop",
		"cordon node-1 --context dev"} {
		if !strings.Contains(got, w) {
			t.Errorf("no %q in\n%s", w, got)
		}
	}
	for _, bad := range []map[string]any{
		{"op": "scale", "kind": "deployment", "name": "api", "replicas": -1},
		{"op": "scale", "kind": "pod", "name": "api", "replicas": 1},
		{"op": "delete", "kind": "pod", "name": "Bad_Name"},
		{"op": "delete", "kind": "node", "name": "node-1"},
		{"op": "restart", "name": "api", "context": "-x"},
		{"op": "apply", "file": "../x.yaml"},
		{"op": "yaml", "kind": "secret", "name": "db"},
	} {
		if err := tl.Validate(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestAIKubeManageTerminalsAndApply(t *testing.T) {
	tl, m, calls := aiKubeManage(t)
	var jobs []terminalJob
	old := kubeSpawnJob
	kubeSpawnJob = func(_ *Model, j terminalJob) error { jobs = append(jobs, j); return nil }
	t.Cleanup(func() { kubeSpawnJob = old })
	for _, a := range []map[string]any{
		{"op": "shell", "pod": "api-7d9f8c6b5-x2x4q", "container": "api"},
		{"op": "logs_follow", "pod": "api-7d9f8c6b5-x2x4q", "container": "api"},
	} {
		if _, err := tl.Run(context.Background(), a); err != nil {
			t.Fatalf("%v: %v", a, err)
		}
	}
	if len(jobs) != 2 || !strings.Contains(jobs[0].Cmdline, "exec -it --context dev --namespace shop api-7d9f8c6b5-x2x4q --container api") || !strings.Contains(jobs[1].Cmdline, "logs --follow") {
		t.Errorf("jobs = %+v", jobs)
	}
	t.Chdir(t.TempDir()) // the workspace
	man := filepath.Join(aiWorkspaceRoot(), "deploy.yaml")
	if err := os.WriteFile(man, []byte("kind: ConfigMap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := map[string]any{"op": "apply", "file": "deploy.yaml"}
	covAIMustContain(t, "apply card", tl.Describe(a), "Apply deploy.yaml", "kubectl apply -f", "context dev")
	if _, err := tl.Run(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(calls(), "\n"), "apply --context dev --namespace shop -f "+man) {
		t.Errorf("calls %q", calls())
	}
	_ = m
}
