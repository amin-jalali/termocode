package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKubeLogOptionsPickerOnAPod(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	jobs := kubeJobs(t)
	kubeCursorTo(t, m, "pods", "worker-5c4b-77k2p")
	kubeKey(t, m, "L")
	if !m.pickerOpen {
		t.Fatal("L opened no picker")
	}
	m.pickerOpen = false
	m.handleTasksPickerSelect("k8s:logopt:worker-5c4b-77k2p|previous")
	if len(*jobs) != 1 {
		t.Fatalf("%d tabs", len(*jobs))
	}
	j := (*jobs)[0]
	if !strings.Contains(j.Cmdline, "logs --tail 200 --previous --context dev --namespace shop worker-5c4b-77k2p --container worker") || strings.Contains(j.Cmdline, "--follow") {
		t.Errorf("cmdline = %q", j.Cmdline)
	}
	if !strings.Contains(j.Label, "(previous)") {
		t.Errorf("label = %q", j.Label)
	}
	m.handleTasksPickerSelect("k8s:logopt:worker-5c4b-77k2p|since1h")
	if len(*jobs) != 2 || !strings.Contains((*jobs)[1].Cmdline, "--since 1h0m0s") {
		t.Errorf("since tab = %+v", *jobs)
	}
	m.handleTasksPickerSelect("k8s:logopt:worker-5c4b-77k2p|timestamps")
	if len(*jobs) != 3 || !strings.Contains((*jobs)[2].Cmdline, "--timestamps") {
		t.Errorf("timestamps tab = %+v", *jobs)
	}
}

func TestKubeLogOptionsOfAMultiContainerPodAskTheContainer(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	jobs := kubeJobs(t)
	m.handleTasksPickerSelect("k8s:logopt:api-7d9f8c6b5-x2x4q|previous")
	if len(*jobs) != 0 || !m.pickerOpen {
		t.Fatalf("tabs %d picker %v", len(*jobs), m.pickerOpen)
	}
	m.pickerOpen = false
	m.handleTasksPickerSelect("k8s:logc:previous|api-7d9f8c6b5-x2x4q/sidecar")
	if len(*jobs) != 1 || !strings.Contains((*jobs)[0].Cmdline, "--container sidecar") {
		t.Errorf("tab = %+v", *jobs)
	}
}

func TestKubeDeploymentLogsOfAllPods(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	jobs := kubeJobs(t)
	kubeCursorTo(t, m, "deployments", "worker")
	kubeKey(t, m, "L")
	if !m.pickerOpen {
		t.Fatal("L on a deployment opened no picker")
	}
	m.pickerOpen = false
	m.handleTasksPickerSelect("k8s:dlog:worker|follow")
	if len(*jobs) != 1 {
		t.Fatalf("%d tabs", len(*jobs))
	}
	if c := (*jobs)[0].Cmdline; !strings.Contains(c, "--all-pods") || !strings.Contains(c, "deployment/worker") {
		t.Errorf("cmdline = %q", c)
	}
}

func TestKubeSaveLogsAsksAFileAndNeverOverwrites(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "logs.txt", "line one\nline two\n")
	m.handleTasksPickerSelect("k8s:logopt:worker-5c4b-77k2p|save")
	if !m.promptOpen || m.promptKind != promptKindKubeSaveLogs {
		t.Fatal("no file prompt")
	}
	path := filepath.Join(t.TempDir(), "worker.log")
	m.promptOpen = false
	cmd := m.kubeSaveLogs(path)
	if cmd == nil {
		t.Fatal("no save command")
	}
	m.handleKubeMsg(cmd())
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "line one\nline two\n" {
		t.Fatalf("file = %q, %v", b, err)
	}
	if f.Count("logs --tail -1 --context dev --namespace shop worker-5c4b-77k2p --container worker") != 1 {
		t.Errorf("no full read in %v", f.Calls())
	}
	m.handleTasksPickerSelect("k8s:logopt:worker-5c4b-77k2p|save")
	m.promptOpen = false
	m.handleKubeMsg(m.kubeSaveLogs(path)())
	if b2, _ := os.ReadFile(path); string(b2) != string(b) {
		t.Error("a second save overwrote the file")
	}
}
