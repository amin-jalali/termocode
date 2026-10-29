package kube

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParsePods(t *testing.T) {
	pods, err := ParsePods(fixture(t, "pods.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 5 {
		t.Fatalf("%d pods", len(pods))
	}
	type row struct {
		name, ready, status string
		restarts            int
	}
	want := []row{
		{"api-7d9f8c6b5-x2x4q", "2/2", "Running", 1},
		{"worker-5c4b-77k2p", "0/1", "CrashLoopBackOff", 7},
		{"migrate-29kq8", "0/1", "Completed", 0},
		{"web-0", "1/1", "Terminating", 0},
		{"cache-0", "0/1", "Pending", 0},
	}
	for i, w := range want {
		p := pods[i]
		got := row{p.Name, p.Ready(), p.Status, p.Restarts}
		if got != w {
			t.Errorf("pod %d = %+v, want %+v", i, got, w)
		}
		if p.Namespace != "shop" {
			t.Errorf("pod %d namespace %q", i, p.Namespace)
		}
	}
	if got := pods[0].Containers; !reflect.DeepEqual(got, []string{"api", "sidecar"}) {
		t.Errorf("containers = %v", got)
	}
	if got := pods[0].Ports; !reflect.DeepEqual(got, []int{8080, 9090}) {
		t.Errorf("ports = %v", got)
	}
	if want := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC); !pods[0].Created.Equal(want) {
		t.Errorf("created = %v", pods[0].Created)
	}
}

func TestParseDeployments(t *testing.T) {
	ds, err := ParseDeployments(fixture(t, "deployments.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 3 {
		t.Fatalf("%d deployments", len(ds))
	}
	got := []string{ds[0].Ready(), ds[1].Ready(), ds[2].Ready()}
	if !reflect.DeepEqual(got, []string{"3/3", "1/2", "0/0"}) {
		t.Errorf("ready = %v", got)
	}
	if ds[1].Updated != 2 || ds[1].Available != 1 {
		t.Errorf("worker = %+v", ds[1])
	}
}

func TestParseServices(t *testing.T) {
	ss, err := ParseServices(fixture(t, "services.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 3 {
		t.Fatalf("%d services", len(ss))
	}
	if ss[0].Type != "ClusterIP" || ss[0].ClusterIP != "10.96.12.4" || ss[0].PortsText() != "80/TCP" {
		t.Errorf("api = %+v %q", ss[0], ss[0].PortsText())
	}
	if got := ss[1].PortsText(); got != "80:30080/TCP,443/TCP" {
		t.Errorf("web ports = %q", got)
	}
}

func TestParseJobs(t *testing.T) {
	js, err := ParseJobs(fixture(t, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(js) != 3 {
		t.Fatalf("%d jobs", len(js))
	}
	got := []string{js[0].Completions(), js[1].Completions(), js[2].Completions()}
	if !reflect.DeepEqual(got, []string{"1/1", "2/5", "0/1"}) {
		t.Errorf("completions = %v", got)
	}
	st := []string{js[0].Status(), js[1].Status(), js[2].Status()}
	if !reflect.DeepEqual(st, []string{"Complete", "Running", "Failed"}) {
		t.Errorf("status = %v", st)
	}
}

func TestParseNamespaces(t *testing.T) {
	ns, err := ParseNamespaces(fixture(t, "namespaces.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ns, []string{"default", "kube-system", "shop"}) {
		t.Errorf("namespaces = %v", ns)
	}
}

func TestParseBadJSON(t *testing.T) {
	if _, err := ParsePods([]byte("not json")); err == nil {
		t.Error("no error for bad JSON")
	}
	if pods, err := ParsePods([]byte(`{"items": []}`)); err != nil || len(pods) != 0 {
		t.Errorf("empty list = %v, %v", pods, err)
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{
		{40 * time.Second, "40s"},
		{12 * time.Minute, "12m"},
		{3*time.Hour + 20*time.Minute, "3h"},
		{50 * time.Hour, "2d"},
		{-time.Minute, "0s"},
	} {
		if got := Age(now.Add(-c.ago), now); got != c.want {
			t.Errorf("Age(%v) = %q, want %q", c.ago, got, c.want)
		}
	}
	if got := Age(time.Time{}, now); got != "" {
		t.Errorf("zero time = %q", got)
	}
}
