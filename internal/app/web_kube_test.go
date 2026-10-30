package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEBP-4): kube.contexts lists kubectl's contexts with the current
// one and the scope the laptop's view shows (kubectl's own when the view
// never opened); nothing writes the kubeconfig.
func TestKubeContexts(t *testing.T) {
	f := kubeFake(t)
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	var out webproto.KubeContexts
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpKubeContexts, `{}`), &out)
	if strings.Join(out.Contexts, ",") != "dev,prod" || out.Current != "dev" || out.Context != "dev" || out.Namespace != "shop" {
		t.Fatalf("contexts %+v", out)
	}
	for _, call := range f.Calls() {
		if strings.Contains(call, "use-context") || strings.Contains(call, "set-context") {
			t.Fatalf("wrote the kubeconfig: %v", f.Calls())
		}
	}
}

// Spec (WEBP-4): kube.list reads one namespace's four kinds through
// kubectl with --context and --namespace; a context kubectl does not
// list is not found (kubectl never runs with it); a kind that cannot be
// listed says why; a cluster that cannot be reached fails the answer.
func TestKubeListScopes(t *testing.T) {
	f := kubeFake(t)
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	var out webproto.KubeList
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpKubeList, `{}`), &out)
	if out.Context != "dev" || out.Namespace != "shop" || len(out.Pods) != 5 || len(out.Deployments) != 3 || len(out.Services) != 3 || len(out.Jobs) != 3 {
		t.Fatalf("list %+v", out)
	}
	if p := out.Pods[0]; p.Name != "api-7d9f8c6b5-x2x4q" || p.Ready != "2/2" || strings.Join(p.Containers, ",") != "api,sidecar" || p.Created == 0 {
		t.Fatalf("pod %+v", p)
	}
	out = webproto.KubeList{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpKubeList, `{"context":"prod"}`), &out)
	if out.Context != "prod" || out.Namespace != "shop" || f.Count("get pods -o json --context prod --namespace shop") != 1 {
		t.Fatalf("prod: %+v %v", out, f.Calls())
	}
	if fr := webTestCallBg(t, &m, c, webproto.OpKubeList, `{"context":"staging"}`); webTestErr(fr) != "not_found" || f.Count("staging") != 0 {
		t.Fatalf("staging: %v %v", fr, f.Calls())
	}
	f.Set(t, "pods.err", "Error from server (Forbidden): pods is forbidden: User \"dev\" cannot list resource \"pods\"")
	out = webproto.KubeList{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpKubeList, `{}`), &out)
	if out.Errors["pods"] == "" || len(out.Deployments) != 3 {
		t.Fatalf("forbidden pods: %+v", out)
	}
	f.Set(t, "all.err", "Unable to connect to the server: dial tcp 10.0.0.1:6443: connect: connection refused")
	if fr := webTestCallBg(t, &m, c, webproto.OpKubeList, `{"namespace":"shop"}`); webTestErr(fr) != "conflict" {
		t.Fatalf("no cluster: %v", fr)
	}
}

// Spec (WEBP-4): a pod's logs and describe come back redacted like the
// AI's (the secret never reaches the page); the logs ask for the lines
// and the container named.
func TestKubeLogsAndDescribe(t *testing.T) {
	f := kubeFake(t)
	f.Set(t, "logs.txt", "started\npassword="+"hunter"+"2secret\n")
	f.Set(t, "describe.txt", "Name: api\nNamespace: shop\nEnvironment:\n  DB_PASSWORD: "+"s3cr3t"+"-value-9\n")
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	var out webproto.KubeText
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpKubeLogs, `{"pod":"api-7d9f8c6b5-x2x4q","container":"api","lines":50}`), &out)
	if len(out.Lines) != 2 || out.Lines[0] != "started" || strings.Contains(strings.Join(out.Lines, "\n"), "hunter2secret") || out.Redacted == 0 {
		t.Fatalf("logs %+v", out)
	}
	if f.Count("logs --tail 50") != 1 || f.Count("--container api") != 1 {
		t.Fatalf("calls %v", f.Calls())
	}
	out = webproto.KubeText{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpKubeDescribe, `{"kind":"pod","name":"api-7d9f8c6b5-x2x4q"}`), &out)
	if len(out.Lines) < 3 || out.Lines[0] != "Name: api" || strings.Contains(strings.Join(out.Lines, "\n"), "s3cr3t-value-9") {
		t.Fatalf("describe %+v", out)
	}
}

// Spec (WEBP-4): restarting a deployment waits for a laptop card that
// names the deployment, the scope and the command; kubectl runs only
// after the laptop's yes; only a deployment can be restarted.
func TestKubeRestartNeedsLaptopCard(t *testing.T) {
	f := kubeFake(t)
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	if fr := webTestCallBg(t, &m, c, webproto.OpKubeRestart, `{"kind":"pod","name":"api"}`); webTestErr(fr) != "bad_request" {
		t.Fatalf("restart a pod: %v", fr)
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpKubeRestart, `{"context":"prod","namespace":"shop","kind":"deployment","name":"api"}`), &ref)
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
		t.Fatal("no laptop card")
	}
	if v := m.confirm.View(); !strings.Contains(v, "Context: prod · Namespace: shop") || !strings.Contains(v, "kubectl rollout restart deployment/api") {
		t.Errorf("card:\n%s", v)
	}
	if f.Count("rollout") != 0 {
		t.Fatal("restarted before the laptop said so")
	}
	webTestLaptopSelect(t, &m, "confirm")
	if f.Count("rollout restart deployment/api --context prod --namespace shop") != 1 {
		t.Fatalf("after yes: %v", f.Calls())
	}
}
