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

func TestYAMLReturnsText(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "deployment.yaml", "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\nspec:\n  replicas: 2\n")
	text, err := c.YAML(context.Background(), "deployment", "api")
	if err != nil || !strings.Contains(text, "replicas: 2") {
		t.Fatalf("yaml = %q, %v", text, err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "get deployment api -o yaml --context dev --namespace shop" {
		t.Errorf("calls = %v", got)
	}
}

// secretYAML is a Secret as `kubectl get secret -o yaml` prints it. The
// values are fake ("sk-"+"test" style) so no scanner flags them.
const secretYAML = "apiVersion: v1\n" +
	"data:\n" +
	"  password: " + "c2stdGVzdA" + "==\n" +
	"  token: " + "dG9rLXRlc3Q" + "=\n" +
	"kind: Secret\n" +
	"metadata:\n" +
	"  annotations:\n" +
	"    kubectl.kubernetes.io/last-applied-configuration: |\n" +
	"      {\"data\":{\"password\":\"" + "c2stdGVzdA" + "==\"}}\n" +
	"    team: shop\n" +
	"  name: db\n" +
	"stringData:\n" +
	"  note: |\n" +
	"    line-one-" + "plain\n" +
	"    line-two\n" +
	"type: Opaque\n"

func TestYAMLMasksSecretData(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "secret.yaml", secretYAML)
	text, err := c.YAML(context.Background(), "secret", "db")
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"c2stdGVzdA", "dG9rLXRlc3Q", "line-one-plain", "line-two"} {
		if strings.Contains(text, leak) {
			t.Errorf("secret value %q leaked:\n%s", leak, text)
		}
	}
	// Key names and the rest of the object stay readable.
	for _, keep := range []string{"  password: '***'", "  token: '***'", "  note: '***'", "kubectl.kubernetes.io/last-applied-configuration: '***'", "    team: shop", "  name: db", "type: Opaque"} {
		if !strings.Contains(text, keep) {
			t.Errorf("missing %q:\n%s", keep, text)
		}
	}
}

func TestMaskSecretYAMLByKindLine(t *testing.T) {
	// Masked even when the caller names the kind another way.
	got := kube.MaskSecretYAML("", secretYAML)
	if strings.Contains(got, "c2stdGVzdA") {
		t.Errorf("leaked:\n%s", got)
	}
	plain := "apiVersion: v1\ndata:\n  color: blue\nkind: ConfigMap\n"
	if got := kube.MaskSecretYAML("configmap", plain); got != plain {
		t.Errorf("configmap changed:\n%s", got)
	}
}
func TestScale(t *testing.T) {
	f, c := fakeClient(t)
	if err := c.Scale(context.Background(), "deployment", "api", 5); err != nil {
		t.Fatal(err)
	}
	if err := c.Scale(context.Background(), "statefulset", "db", 0); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"scale deployment/api --replicas=5 --context dev --namespace shop",
		"scale statefulset/db --replicas=0 --context dev --namespace shop",
	}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v", got)
	}
	f.Set(t, "scale.err", "Error from server (Forbidden): deployments.apps \"api\" is forbidden: User \"dev\" cannot patch resource \"deployments/scale\"\n")
	if err := c.Scale(context.Background(), "deployment", "api", 1); kube.KindOf(err) != kube.ErrForbidden {
		t.Errorf("err = %v", err)
	}
}

func TestScaleRefusesBadInput(t *testing.T) {
	f, c := fakeClient(t)
	for _, tc := range []struct {
		kind string
		n    int
	}{{"pod", 1}, {"deployment", -1}, {"daemonset", 2}, {"deployment", kube.ScaleMax + 1}} {
		if err := c.Scale(context.Background(), tc.kind, "api", tc.n); err == nil {
			t.Errorf("Scale(%q, %d) ran", tc.kind, tc.n)
		}
	}
	if got := f.Calls(); len(got) != 0 {
		t.Errorf("calls = %v", got)
	}
}
