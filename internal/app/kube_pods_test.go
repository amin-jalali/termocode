package app

import (
	"strings"
	"sync"
	"testing"
)

// kubeJobs records the terminal tabs the view opens.
func kubeJobs(t *testing.T) *[]terminalJob {
	t.Helper()
	var mu sync.Mutex
	var jobs []terminalJob
	old := kubeSpawnJob
	kubeSpawnJob = func(m *Model, j terminalJob) error {
		mu.Lock()
		defer mu.Unlock()
		jobs = append(jobs, j)
		return nil
	}
	t.Cleanup(func() { kubeSpawnJob = old })
	return &jobs
}

// kubeCursorTo puts the cursor on the item name of section.
func kubeCursorTo(t *testing.T, m *Model, section, name string) {
	t.Helper()
	for i, r := range m.kube.rows() {
		if r.kind == kubeRowItem && r.section == section && r.name == name {
			m.kube.cursor = i
			return
		}
	}
	t.Fatalf("no %s %s", section, name)
}

func TestKubeLogsOfOneContainerPod(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	jobs := kubeJobs(t)
	kubeCursorTo(t, m, "pods", "worker-5c4b-77k2p")
	kubeKey(t, m, "l")
	if len(*jobs) != 1 {
		t.Fatalf("%d tabs", len(*jobs))
	}
	j := (*jobs)[0]
	if !strings.Contains(j.Cmdline, "logs --follow --tail 200 --context dev --namespace shop worker-5c4b-77k2p --container worker") {
		t.Errorf("cmdline = %q", j.Cmdline)
	}
	if j.Label != "logs: worker-5c4b-77k2p/worker" || j.Service != "k8s:logs:dev/shop/worker-5c4b-77k2p/worker" {
		t.Errorf("job = %+v", j)
	}
}

func TestKubeMultiContainerPodAsksWhichContainer(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	jobs := kubeJobs(t)
	kubeCursorTo(t, m, "pods", "api-7d9f8c6b5-x2x4q")
	kubeKey(t, m, "e")
	if len(*jobs) != 0 || !m.pickerOpen {
		t.Fatalf("tabs %d picker %v", len(*jobs), m.pickerOpen)
	}
	if cur, ok := m.picker.Current(); !ok || cur.ID != "k8s:shell:api-7d9f8c6b5-x2x4q/api" {
		t.Errorf("first row = %+v", cur)
	}
	m.pickerOpen = false
	m.handleTasksPickerSelect("k8s:shell:api-7d9f8c6b5-x2x4q/sidecar")
	if len(*jobs) != 1 {
		t.Fatalf("%d tabs", len(*jobs))
	}
	j := (*jobs)[0]
	if !strings.Contains(j.Cmdline, "exec -it --context dev --namespace shop api-7d9f8c6b5-x2x4q --container sidecar -- sh -c") || j.Label != "shell: api-7d9f8c6b5-x2x4q/sidecar" {
		t.Errorf("job = %+v", j)
	}
}

func TestKubeDescribeOpensText(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "describe.txt", "Name: worker\nReplicas: 2 desired\n")
	kubeCursorTo(t, m, "deployments", "worker")
	msg, ok := kubeKey(t, m, "i")().(kubeDescribeMsg)
	if !ok || msg.err != nil || msg.kind != "deployment" || !strings.Contains(msg.text, "Replicas: 2 desired") {
		t.Fatalf("describe = %+v", msg)
	}
	if f.Count("describe deployment worker --context dev --namespace shop") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	m.onKubeDescribe(msg) // no editor in the test: nothing to open, no panic
	// Enter on a non-pod object describes too.
	kubeCursorTo(t, m, "services", "web")
	if msg, ok := kubeKey(t, m, "enter")().(kubeDescribeMsg); !ok || msg.kind != "service" || msg.name != "web" {
		t.Errorf("enter on a service = %+v", msg)
	}
}

func TestKubeCopyName(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	var got string
	t.Cleanup(SetClipboardSink(func(s string) { got = s }))
	kubeCursorTo(t, m, "jobs", "backfill")
	kubeKey(t, m, "y")
	if got != "backfill" {
		t.Errorf("copied %q", got)
	}
}
