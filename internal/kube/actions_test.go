package kube_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

func TestDescribeReturnsText(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "describe.txt", "Name:         api-1\nNamespace:    shop\n")
	text, err := c.Describe(context.Background(), "pod", "api-1")
	if err != nil || !strings.Contains(text, "Namespace:    shop") {
		t.Fatalf("describe = %q, %v", text, err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "describe pod api-1 --context dev --namespace shop" {
		t.Errorf("calls = %v", got)
	}
}

func TestRolloutRestart(t *testing.T) {
	f, c := fakeClient(t)
	if err := c.RolloutRestart(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "rollout restart deployment/api --context dev --namespace shop" {
		t.Errorf("calls = %v", got)
	}
	f.Set(t, "restart.err", "Error from server (Forbidden): deployments.apps \"api\" is forbidden: User \"dev\" cannot patch resource\n")
	if err := c.RolloutRestart(context.Background(), "api"); kube.KindOf(err) != kube.ErrForbidden {
		t.Errorf("err = %v", err)
	}
}

func TestTerminalCommands(t *testing.T) {
	c := kube.Client{Binary: "/bin/kubectl", Context: "dev", Namespace: "shop"}
	logs := c.LogsCommand("api-1", "sidecar")
	if want := []string{"/bin/kubectl", "logs", "--follow", "--tail", "200", "--context", "dev", "--namespace", "shop", "api-1", "--container", "sidecar"}; !reflect.DeepEqual(logs.Argv, want) {
		t.Errorf("logs argv = %v", logs.Argv)
	}
	if logs.Label != "logs: api-1/sidecar" {
		t.Errorf("logs label = %q", logs.Label)
	}
	if one := c.LogsCommand("api-1", ""); strings.Contains(strings.Join(one.Argv, " "), "--container") || one.Label != "logs: api-1" {
		t.Errorf("single container logs = %v %q", one.Argv, one.Label)
	}
	sh := c.ShellCommand("api-1", "api")
	got := strings.Join(sh.Argv, " ")
	if !strings.HasPrefix(got, "/bin/kubectl exec -it --context dev --namespace shop api-1 --container api -- sh -c ") {
		t.Errorf("shell argv = %q", got)
	}
	if sh.Label != "shell: api-1/api" {
		t.Errorf("shell label = %q", sh.Label)
	}
}

func TestLogsTail(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "logs.txt", "line 1\nline 2\n")
	text, err := c.Logs(context.Background(), "api-1", "sidecar", 50)
	if err != nil || text != "line 1\nline 2\n" {
		t.Fatalf("logs = %q, %v", text, err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "logs --tail 50 --context dev --namespace shop api-1 --container sidecar" {
		t.Errorf("calls = %v", got)
	}
}
