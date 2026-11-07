package kube_test

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

const healthPods = `{"items": [
 {"metadata": {"name": "api-7d9f8c6b5-x2x4q", "namespace": "shop", "labels": {"app": "api"},
   "ownerReferences": [{"kind": "ReplicaSet", "name": "api-7d9f8c6b5", "controller": true}]},
  "spec": {"containers": [{"name": "api", "image": "shop/api:1.4",
    "resources": {"limits": {"cpu": "1", "memory": "256Mi"}}, "readinessProbe": {"httpGet": {"path": "/ready"}}}]},
  "status": {"phase": "Running", "containerStatuses": [{"name": "api", "ready": true, "restartCount": 0, "state": {"running": {}}}]}},
 {"metadata": {"name": "worker-5c4b-77k2p", "namespace": "shop", "labels": {"app": "worker"},
   "ownerReferences": [{"kind": "ReplicaSet", "name": "worker-5c4b", "controller": true}]},
  "spec": {"containers": [{"name": "worker", "image": "shop/worker"}]},
  "status": {"phase": "Running", "containerStatuses": [{"name": "worker", "ready": false, "restartCount": 7, "state": {"waiting": {"reason": "CrashLoopBackOff"}}}]}},
 {"metadata": {"name": "migrate-29kq8", "namespace": "shop",
   "ownerReferences": [{"kind": "Job", "name": "migrate", "controller": true}]},
  "spec": {"containers": [{"name": "migrate", "image": "shop/migrate:latest",
    "resources": {"limits": {"cpu": "1", "memory": "1Gi"}}}]},
  "status": {"phase": "Succeeded", "containerStatuses": [{"name": "migrate", "ready": false, "restartCount": 0, "state": {"terminated": {"reason": "Completed"}}}]}},
 {"metadata": {"name": "debug", "namespace": "shop"},
  "spec": {"containers": [{"name": "sh", "image": "busybox:1.36",
    "resources": {"limits": {"cpu": "100m", "memory": "64Mi"}}, "livenessProbe": {"exec": {}}}]},
  "status": {"phase": "Running", "containerStatuses": [{"name": "sh", "ready": true, "restartCount": 0, "state": {"running": {}}}]}}
]}`

const healthDeploys = `{"items": [
 {"metadata": {"name": "api", "namespace": "shop"}, "spec": {"replicas": 1}, "status": {"readyReplicas": 1, "updatedReplicas": 1, "availableReplicas": 1}},
 {"metadata": {"name": "worker", "namespace": "shop"}, "spec": {"replicas": 1}, "status": {"readyReplicas": 0, "updatedReplicas": 1, "availableReplicas": 0}}
]}`

const healthServices = `{"items": [
 {"metadata": {"name": "api", "namespace": "shop"}, "spec": {"type": "ClusterIP", "selector": {"app": "api"}, "ports": [{"port": 80}]}},
 {"metadata": {"name": "old", "namespace": "shop"}, "spec": {"type": "ClusterIP", "selector": {"app": "gone"}, "ports": [{"port": 80}]}}
]}`

const healthJobs = `{"items": [
 {"metadata": {"name": "migrate", "namespace": "shop"}, "spec": {"completions": 1}, "status": {"succeeded": 1}}
]}`

func healthReport(t *testing.T) kube.Report {
	t.Helper()
	in, err := kube.ParseReportInput([]byte(healthPods), []byte(healthDeploys), []byte(healthServices), []byte(healthJobs))
	if err != nil {
		t.Fatal(err)
	}
	return kube.BuildReport(in)
}

func TestReportPulse(t *testing.T) {
	r := healthReport(t)
	want := map[string][4]int{ // total, ok, warn, fail
		"Pods":        {4, 3, 0, 1},
		"Deployments": {2, 1, 0, 1},
		"Services":    {2, 1, 1, 0},
		"Jobs":        {1, 1, 0, 0},
	}
	if len(r.Pulse) != len(want) {
		t.Fatalf("pulse rows %+v", r.Pulse)
	}
	for _, p := range r.Pulse {
		w, ok := want[p.Kind]
		if !ok || [4]int{p.Total, p.OK, p.Warn, p.Fail} != w {
			t.Errorf("%s: %+v, want %v", p.Kind, p, w)
		}
	}
}

func TestReportXrayOwnerTree(t *testing.T) {
	r := healthReport(t)
	var lines []string
	var walk func(ns []kube.TreeNode, depth int)
	walk = func(ns []kube.TreeNode, depth int) {
		for _, n := range ns {
			lines = append(lines, strings.Repeat("  ", depth)+n.Kind+" "+n.Name)
			walk(n.Children, depth+1)
		}
	}
	walk(r.Tree, 0)
	want := []string{
		"Deployment api",
		"  ReplicaSet api-7d9f8c6b5",
		"    Pod api-7d9f8c6b5-x2x4q",
		"Deployment worker",
		"  ReplicaSet worker-5c4b",
		"    Pod worker-5c4b-77k2p",
		"Job migrate",
		"  Pod migrate-29kq8",
		"Service api",
		"  Pod api-7d9f8c6b5-x2x4q",
		"Service old",
		"Pod debug",
	}
	if got := strings.Join(lines, "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("tree\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
}

func TestReportSanitizer(t *testing.T) {
	r := healthReport(t)
	var got []string
	for _, f := range r.Findings {
		got = append(got, f.Level+" "+f.Kind+" "+f.Name+": "+f.Text)
	}
	want := []string{
		"error Deployment worker: 0 of 1 replicas available",
		"error Pod worker-5c4b-77k2p: CrashLoopBackOff",
		"warning Pod debug: no controller (a bare pod is not restarted on another node)",
		"warning Pod debug: no readiness probe: sh",
		"warning Pod migrate-29kq8: image without a fixed tag: shop/migrate:latest",
		"warning Pod worker-5c4b-77k2p: image without a fixed tag: shop/worker",
		"warning Pod worker-5c4b-77k2p: no CPU / memory limits: worker",
		"warning Pod worker-5c4b-77k2p: no readiness probe: worker",
		"warning Pod worker-5c4b-77k2p: restarted 7 times",
		"warning Service old: the selector matches no pods",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("findings\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestReportText(t *testing.T) {
	text := healthReport(t).Text()
	for _, want := range []string{"Pulse", "Pods", "ok 3", "fail 1", "Xray", "    Pod api-7d9f8c6b5-x2x4q", "Report: 2 errors, 8 warnings", "error  Pod worker-5c4b-77k2p: CrashLoopBackOff"} {
		if !strings.Contains(text, want) {
			t.Errorf("text misses %q:\n%s", want, text)
		}
	}
	if empty := kube.BuildReport(kube.ReportInput{}).Text(); !strings.Contains(empty, "Report: no problems found") {
		t.Errorf("empty report: %s", empty)
	}
}

func TestClientReportReadsTheFixtures(t *testing.T) {
	_, c := fakeClient(t)
	r, err := c.Report(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	total := map[string]int{}
	for _, p := range r.Pulse {
		total[p.Kind] = p.Total
	}
	if total["Pods"] != 5 || total["Deployments"] != 3 || total["Services"] != 3 || total["Jobs"] != 3 {
		t.Fatalf("pulse %+v", r.Pulse)
	}
}

func TestParseReportInputBadJSON(t *testing.T) {
	if _, err := kube.ParseReportInput([]byte("x"), nil, nil, nil); err == nil {
		t.Fatal("bad pods JSON: no error")
	}
}
