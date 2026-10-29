package kube_test

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/kube/kubetest"
)

func fakeClient(t *testing.T) (*kubetest.Fake, kube.Client) {
	t.Helper()
	f := kubetest.New(t)
	f.Fixtures(t, "testdata")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "config"))
	return f, kube.Client{Binary: f.Kubectl(), Context: "dev", Namespace: "shop"}
}

func TestFindUsesLookPath(t *testing.T) {
	f := kubetest.New(t)
	t.Setenv("PATH", f.Bin)
	if got := kube.Find(nil); got != f.Kubectl() {
		t.Errorf("Find = %q, want %q", got, f.Kubectl())
	}
	t.Setenv("PATH", t.TempDir())
	if got := kube.Find(nil); got != "" {
		t.Errorf("Find without kubectl = %q", got)
	}
}

func TestListsPassContextAndNamespace(t *testing.T) {
	f, c := fakeClient(t)
	ctx := context.Background()
	pods, err := c.Pods(ctx)
	if err != nil || len(pods) != 5 {
		t.Fatalf("pods = %d, %v", len(pods), err)
	}
	if ds, err := c.Deployments(ctx); err != nil || len(ds) != 3 {
		t.Fatalf("deployments = %d, %v", len(ds), err)
	}
	if ss, err := c.Services(ctx); err != nil || len(ss) != 3 {
		t.Fatalf("services = %d, %v", len(ss), err)
	}
	if js, err := c.Jobs(ctx); err != nil || len(js) != 3 {
		t.Fatalf("jobs = %d, %v", len(js), err)
	}
	want := []string{
		"get pods -o json --context dev --namespace shop",
		"get deployments -o json --context dev --namespace shop",
		"get services -o json --context dev --namespace shop",
		"get jobs -o json --context dev --namespace shop",
	}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestNamespacesAndContextsAreReadOnly(t *testing.T) {
	f, c := fakeClient(t)
	ctx := context.Background()
	ns, err := c.Namespaces(ctx)
	if err != nil || !reflect.DeepEqual(ns, []string{"default", "kube-system", "shop"}) {
		t.Fatalf("namespaces = %v, %v", ns, err)
	}
	cur, err := c.CurrentContext(ctx)
	if err != nil || cur != "dev" {
		t.Fatalf("current = %q, %v", cur, err)
	}
	names, err := c.Contexts(ctx)
	if err != nil || !reflect.DeepEqual(names, []string{"dev", "prod"}) {
		t.Fatalf("contexts = %v, %v", names, err)
	}
	def, err := c.ContextNamespace(ctx)
	if err != nil || def != "shop" {
		t.Fatalf("context namespace = %q, %v", def, err)
	}
	f.Set(t, "namespace.txt", "")
	if def, _ := c.ContextNamespace(ctx); def != "default" {
		t.Errorf("no namespace in the context = %q, want default", def)
	}
	for _, call := range f.Calls() {
		for _, bad := range []string{"use-context", "set-context", "set-credentials", "set-cluster", "--kubeconfig"} {
			if strings.Contains(call, bad) {
				t.Errorf("call %q writes or overrides kubeconfig", call)
			}
		}
	}
	// KUBECONFIG reaches kubectl untouched.
	for _, l := range f.EnvCalls() {
		if !strings.HasSuffix(strings.SplitN(l, "|", 2)[0], "config") {
			t.Errorf("KUBECONFIG lost: %q", l)
		}
	}
}

func TestEmptyScopeLeavesKubectlChoice(t *testing.T) {
	f, c := fakeClient(t)
	c.Context, c.Namespace = "", ""
	if _, err := c.Pods(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "get pods -o json" {
		t.Errorf("calls = %v", got)
	}
}

func TestFailuresAreOneLine(t *testing.T) {
	f, c := fakeClient(t)
	ctx := context.Background()
	f.Set(t, "pods.err", "Error from server (Forbidden): pods is forbidden: User \"dev\" cannot list resource \"pods\"\n")
	if _, err := c.Pods(ctx); kube.KindOf(err) != kube.ErrForbidden {
		t.Errorf("forbidden = %v", err)
	}
	f.Set(t, "fail-prod.err", "Unable to connect to the server: dial tcp: lookup prod.example: no such host\n")
	c.Context = "prod"
	if _, err := c.Deployments(ctx); kube.KindOf(err) != kube.ErrNoCluster {
		t.Errorf("down = %v", err)
	}
	f.Remove("current-context.txt")
	if _, err := c.CurrentContext(ctx); kube.KindOf(err) != kube.ErrNoConfig {
		t.Errorf("no context = %v", err)
	}
	none := kube.Client{}
	if _, err := none.Pods(ctx); kube.KindOf(err) != kube.ErrNoKubectl {
		t.Errorf("no kubectl = %v", err)
	}
}

func TestCallsTimeOut(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "jobs.sleep", "5")
	c.Timeout = 200 * time.Millisecond
	start := time.Now()
	_, err := c.Jobs(context.Background())
	if kube.KindOf(err) != kube.ErrTimeout {
		t.Errorf("err = %v, want a timeout", err)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("the call waited %v", d)
	}
}
