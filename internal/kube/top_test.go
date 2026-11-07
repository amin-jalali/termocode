package kube_test

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

func TestParseQuantities(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{{"250m", 250}, {"1", 1000}, {"2", 2000}, {"1500000n", 1}, {"0m", 0}} {
		if got, ok := kube.ParseCPU(tc.in); !ok || got != tc.want {
			t.Errorf("ParseCPU(%q) = %d,%v want %d", tc.in, got, ok, tc.want)
		}
	}
	for _, tc := range []struct {
		in   string
		want int64
	}{{"128Mi", 128 << 20}, {"1Gi", 1 << 30}, {"512Ki", 512 << 10}, {"100M", 100e6}, {"42", 42}} {
		if got, ok := kube.ParseMemory(tc.in); !ok || got != tc.want {
			t.Errorf("ParseMemory(%q) = %d,%v want %d", tc.in, got, ok, tc.want)
		}
	}
	for _, bad := range []string{"", "<unknown>", "x1Mi", "-5m"} {
		if _, ok := kube.ParseCPU(bad); ok {
			t.Errorf("ParseCPU(%q) accepted", bad)
		}
		if _, ok := kube.ParseMemory(bad); ok {
			t.Errorf("ParseMemory(%q) accepted", bad)
		}
	}
}

func TestParseTopPodsAndNodes(t *testing.T) {
	pods := kube.ParseTopPods("api-7d9  250m  128Mi\nweb-1 1 1Gi\n\nbroken line\n", "shop")
	if len(pods) != 2 || pods[0].Name != "api-7d9" || pods[0].Namespace != "shop" || pods[0].CPUMilli != 250 || pods[0].MemBytes != 128<<20 || pods[1].CPUMilli != 1000 {
		t.Fatalf("pods = %+v", pods)
	}
	all := kube.ParseTopPods("kube-system coredns-1 3m 20Mi\n", "")
	if len(all) != 1 || all[0].Namespace != "kube-system" || all[0].Name != "coredns-1" {
		t.Fatalf("all-namespaces pods = %+v", all)
	}
	nodes := kube.ParseTopNodes("node-a 500m 12% 2Gi 25%\nnode-b <unknown> <unknown> <unknown> <unknown>\n")
	if len(nodes) != 2 {
		t.Fatalf("nodes = %+v", nodes)
	}
	a := nodes[0]
	if a.Name != "node-a" || a.CPUMilli != 500 || a.CPUPercent != 12 || a.MemBytes != 2<<30 || a.MemPercent != 25 || !a.Known {
		t.Fatalf("node-a = %+v", a)
	}
	if nodes[1].Known {
		t.Fatalf("unknown node must be marked: %+v", nodes[1])
	}
}

func TestTopCallsKubectlTopWithScope(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "top-pods.txt", "api-7d9 250m 128Mi\n")
	f.Set(t, "top-nodes.txt", "node-a 500m 12% 2Gi 25%\n")
	ctx := context.Background()
	pods, err := c.TopPods(ctx)
	if err != nil || len(pods) != 1 || pods[0].Namespace != "shop" {
		t.Fatalf("TopPods = %+v, %v", pods, err)
	}
	nodes, err := c.TopNodes(ctx)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("TopNodes = %+v, %v", nodes, err)
	}
	calls := strings.Join(f.Calls(), "\n")
	if !strings.Contains(calls, "top pods --no-headers --context dev --namespace shop") {
		t.Fatalf("pods call: %s", calls)
	}
	if !strings.Contains(calls, "top nodes --no-headers --context dev") || strings.Contains(calls, "top nodes --no-headers --context dev --namespace") {
		t.Fatalf("nodes call must not pass a namespace: %s", calls)
	}
}

func TestTopWithoutMetricsServerIsOneClearLine(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "top-pods.err", "error: Metrics API not available\n")
	_, err := c.TopPods(context.Background())
	if err == nil || !strings.Contains(err.Error(), "metrics-server") {
		t.Fatalf("err = %v", err)
	}
}

func TestMetricsHistoryKeepsNewestSamplesPerName(t *testing.T) {
	h := kube.NewMetricsHistory(3)
	for i := int64(1); i <= 5; i++ {
		h.AddPods([]kube.PodMetrics{{Name: "api", Namespace: "shop", CPUMilli: i * 10, MemBytes: i}})
	}
	h.AddPods([]kube.PodMetrics{{Name: "web", Namespace: "shop", CPUMilli: 7, MemBytes: 8}})
	cpu, mem := h.Pod("shop", "api")
	if len(cpu) != 3 || cpu[0] != 30 || cpu[2] != 50 || mem[2] != 5 {
		t.Fatalf("api cpu=%v mem=%v", cpu, mem)
	}
	// a pod missing from the newest sample is dropped (it is gone)
	if c, _ := h.Pod("shop", "api"); len(c) != 3 {
		t.Fatalf("api history lost early")
	}
	h.AddPods([]kube.PodMetrics{{Name: "web", Namespace: "shop", CPUMilli: 9, MemBytes: 9}})
	if c, _ := h.Pod("shop", "api"); c != nil {
		t.Fatalf("gone pod kept: %v", c)
	}
	h.AddNodes([]kube.NodeMetrics{{Name: "n", CPUMilli: 1, MemBytes: 2, Known: true}, {Name: "x"}})
	if c, m := h.Node("n"); len(c) != 1 || m[0] != 2 {
		t.Fatalf("node = %v %v", c, m)
	}
	if c, _ := h.Node("x"); c != nil {
		t.Fatalf("unknown node sample stored: %v", c)
	}
}
