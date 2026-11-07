package kube_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

func TestCordonUncordonAreClusterScoped(t *testing.T) {
	f, c := fakeClient(t)
	if err := c.Cordon(context.Background(), "node-1"); err != nil {
		t.Fatal(err)
	}
	if err := c.Uncordon(context.Background(), "node-1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"cordon node-1 --context dev", "uncordon node-1 --context dev"}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestDrainFlagsAndErrors(t *testing.T) {
	f, c := fakeClient(t)
	if err := c.Drain(context.Background(), "node-1"); err != nil {
		t.Fatal(err)
	}
	want := "drain node-1 --ignore-daemonsets --delete-emptydir-data --timeout=240s --context dev"
	if got := f.Calls(); len(got) != 1 || got[0] != want {
		t.Errorf("calls = %v, want %q", got, want)
	}
	f.Set(t, "node.err", "Error from server (Forbidden): nodes \"node-1\" is forbidden: User \"dev\" cannot patch resource\n")
	if err := c.Drain(context.Background(), "node-1"); kube.KindOf(err) != kube.ErrForbidden {
		t.Errorf("drain err = %v", err)
	}
	if err := c.Cordon(context.Background(), "node-1"); kube.KindOf(err) != kube.ErrForbidden {
		t.Errorf("cordon err = %v", err)
	}
}

func TestNodeActionsRejectFlagNames(t *testing.T) {
	f, c := fakeClient(t)
	for _, name := range []string{"", "--all", "-l"} {
		if err := c.Cordon(context.Background(), name); err == nil {
			t.Errorf("cordon %q ran", name)
		}
		if err := c.Drain(context.Background(), name); err == nil {
			t.Errorf("drain %q ran", name)
		}
	}
	if got := f.Calls(); len(got) != 0 {
		t.Errorf("calls = %v, want none", got)
	}
}

const hpaJSON = `{"items": [
 {"metadata": {"name": "api", "namespace": "shop", "creationTimestamp": "2026-11-01T10:00:00Z"},
  "spec": {"scaleTargetRef": {"kind": "Deployment", "name": "api"}, "minReplicas": 2, "maxReplicas": 10,
   "metrics": [{"type": "Resource", "resource": {"name": "cpu", "target": {"type": "Utilization", "averageUtilization": 80}}},
               {"type": "Resource", "resource": {"name": "memory", "target": {"type": "Utilization", "averageUtilization": 70}}}]},
  "status": {"currentReplicas": 3, "desiredReplicas": 4,
   "currentMetrics": [{"type": "Resource", "resource": {"name": "cpu", "current": {"averageUtilization": 91}}}]}},
 {"metadata": {"name": "worker", "namespace": "shop", "creationTimestamp": "2026-11-01T10:00:00Z"},
  "spec": {"scaleTargetRef": {"kind": "Deployment", "name": "worker"}, "maxReplicas": 5,
   "metrics": [{"type": "External"}]},
  "status": {"currentReplicas": 1, "desiredReplicas": 1}}
]}`

func TestHPAsParse(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "horizontalpodautoscalers.json", hpaJSON)
	got, err := c.HPAs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls := f.Calls(); len(calls) != 1 || calls[0] != "get horizontalpodautoscalers -o json --context dev --namespace shop" {
		t.Errorf("calls = %v", calls)
	}
	if len(got) != 2 {
		t.Fatalf("hpas = %+v", got)
	}
	a := got[0]
	if a.Name != "api" || a.Reference() != "Deployment/api" || a.Min != 2 || a.Max != 10 || a.Current != 3 || a.Desired != 4 {
		t.Errorf("api = %+v", a)
	}
	if a.Targets != "cpu 91%/80%, memory <unknown>/70%" {
		t.Errorf("api targets = %q", a.Targets)
	}
	w := got[1]
	if w.Min != 1 || w.Targets != "External" {
		t.Errorf("worker = %+v", w)
	}
	if _, err := kube.ParseHPAs([]byte("{")); err == nil {
		t.Error("bad JSON must fail")
	}
}
