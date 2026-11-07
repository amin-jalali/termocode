package app

import (
	"strings"
	"testing"
	"time"
)

func TestKubeLoadReadsPodMetricsAndKeepsAHistory(t *testing.T) {
	f := kubeFake(t)
	f.Set(t, "top-pods.txt", "web-0 12m 64Mi\napi-0 250m 1Gi\n")
	m := kubeTestModel(t, 150, 42)
	kubeRunLoad(t, m, m.kubeSync())
	st := m.kube
	if f.Count("top pods --no-headers") != 1 {
		t.Fatalf("no top pods call in %v", f.Calls())
	}
	if p, ok := st.top["web-0"]; !ok || p.CPUMilli != 12 || p.MemBytes != 64<<20 {
		t.Errorf("web-0 metrics = %+v %v", p, ok)
	}
	kubeRunLoad(t, m, m.kubeReload())
	if cpu, _ := st.metrics.Pod("shop", "web-0"); len(cpu) != 2 {
		t.Errorf("history = %v", cpu)
	}
}

func TestKubeNoMetricsServerStopsAsking(t *testing.T) {
	f := kubeFake(t)
	f.Set(t, "top-pods.err", "error: Metrics API not available")
	m := kubeTestModel(t, 150, 42)
	kubeRunLoad(t, m, m.kubeSync())
	if !m.kube.topOff || m.kube.err != "" || len(m.kube.errs) != 0 {
		t.Fatalf("topOff %v err %q errs %v", m.kube.topOff, m.kube.err, m.kube.errs)
	}
	m.kube.stopPolling()
	m.kube.polling = true
	m.kube.gen++
	kubeRunLoad(t, m, m.kubeLoad())
	if n := f.Count("top pods"); n != 1 {
		t.Errorf("top pods asked %d times", n)
	}
}

func TestKubePodRowAndFooterShowMetrics(t *testing.T) {
	f := kubeFake(t)
	f.Set(t, "top-pods.txt", "web-0 12m 64Mi\n")
	m := kubeTestModel(t, 150, 42)
	kubeRunLoad(t, m, m.kubeSync())
	kubeCursorTo(t, m, "pods", "web-0")
	foot := m.kube.footer(time.Now())
	if !strings.Contains(foot[0], "cpu 12m") || !strings.Contains(foot[0], "mem 64Mi") {
		t.Errorf("footer = %q", foot[0])
	}
	r, _ := m.kube.currentRow()
	if _, _, right := m.kube.itemParts(r, 60, time.Now()); !strings.Contains(right, "12m") {
		t.Errorf("row right = %q", right)
	}
}

func TestKubeTopPageListsNodesAndPods(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42) // kubeLoaded makes the fake kubectl
	f.Set(t, "top-pods.txt", "web-0 12m 64Mi\n")
	f.Set(t, "top-nodes.txt", "node-1 500m 25% 2Gi 50%\nnode-2 <unknown> <unknown> <unknown> <unknown>\n")
	cmd := kubeKey(t, m, "t")
	if cmd == nil {
		t.Fatal("t ran nothing")
	}
	msg, ok := cmd().(kubeTopMsg)
	if !ok {
		t.Fatal("not a top result")
	}
	text := kubeTopText(msg, m.kube.metrics)
	for _, want := range []string{"NODES", "node-1", "500m", "25%", "2.0Gi", "node-2", "unknown", "PODS", "web-0", "12m", "64Mi"} {
		if !strings.Contains(text, want) {
			t.Errorf("page lacks %q:\n%s", want, text)
		}
	}
}

func TestKubeTopPaletteIsReadOnlyForTheAI(t *testing.T) {
	p, ok := aiActionPolicies["kubernetes-top"]
	if !ok || p.Gate != aiGateAuto {
		t.Errorf("policy = %+v %v", p, ok)
	}
}

func TestKubeMemText(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 512: "512B", 64 << 20: "64Mi", 3 << 30: "3.0Gi", 1536 << 10: "1.5Mi"} {
		if got := kubeMemText(in); got != want {
			t.Errorf("kubeMemText(%d) = %q, want %q", in, got, want)
		}
	}
}
