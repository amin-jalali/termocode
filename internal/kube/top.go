package kube

import (
	"context"
	"strconv"
	"strings"
)

// PodMetrics is one row of `kubectl top pods`.
type PodMetrics struct {
	Name      string
	Namespace string
	CPUMilli  int64 // millicores
	MemBytes  int64
}

// NodeMetrics is one row of `kubectl top nodes`. Known is false when the
// node reports <unknown> (not ready, no metrics yet).
type NodeMetrics struct {
	Name       string
	CPUMilli   int64
	CPUPercent int
	MemBytes   int64
	MemPercent int
	Known      bool
}

// TopPods runs `kubectl top pods` in the session's namespace (needs the
// cluster's metrics-server).
func (c Client) TopPods(ctx context.Context) ([]PodMetrics, error) {
	args := append([]string{"top", "pods", "--no-headers"}, c.scope(true)...)
	out, err := c.capture(ctx, args...)
	if err != nil {
		return nil, topError(err)
	}
	return ParseTopPods(out, c.Namespace), nil
}

// TopNodes runs `kubectl top nodes` (nodes have no namespace).
func (c Client) TopNodes(ctx context.Context) ([]NodeMetrics, error) {
	args := append([]string{"top", "nodes", "--no-headers"}, c.scope(false)...)
	out, err := c.capture(ctx, args...)
	if err != nil {
		return nil, topError(err)
	}
	return ParseTopNodes(out), nil
}

// topError makes "Metrics API not available" one clear line.
func topError(err error) error {
	if strings.Contains(strings.ToLower(err.Error()), "metrics api not available") {
		return &Error{Kind: ErrOther, Line: "No metrics: the cluster has no metrics-server"}
	}
	return err
}

// ParseTopPods reads `kubectl top pods --no-headers` lines: NAME CPU MEM,
// or NAMESPACE NAME CPU MEM with --all-namespaces. ns is the namespace
// of the 3-column form. Bad lines are skipped.
func ParseTopPods(out, ns string) []PodMetrics {
	var pods []PodMetrics
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		p := PodMetrics{Namespace: ns}
		switch len(f) {
		case 3:
			p.Name = f[0]
		case 4:
			p.Namespace, p.Name = f[0], f[1]
			f = f[1:]
		default:
			continue
		}
		cpu, ok1 := ParseCPU(f[1])
		mem, ok2 := ParseMemory(f[2])
		if !ok1 || !ok2 {
			continue
		}
		p.CPUMilli, p.MemBytes = cpu, mem
		pods = append(pods, p)
	}
	return pods
}

// ParseTopNodes reads `kubectl top nodes --no-headers` lines:
// NAME CPU CPU% MEM MEM%. A node with <unknown> values is kept with
// Known false.
func ParseTopNodes(out string) []NodeMetrics {
	var nodes []NodeMetrics
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) != 5 {
			continue
		}
		n := NodeMetrics{Name: f[0]}
		cpu, ok1 := ParseCPU(f[1])
		cp, ok2 := parsePercent(f[2])
		mem, ok3 := ParseMemory(f[3])
		mp, ok4 := parsePercent(f[4])
		if ok1 && ok2 && ok3 && ok4 {
			n.CPUMilli, n.CPUPercent, n.MemBytes, n.MemPercent, n.Known = cpu, cp, mem, mp, true
		}
		nodes = append(nodes, n)
	}
	return nodes
}

func parsePercent(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSuffix(s, "%"))
	if err != nil || n < 0 || !strings.HasSuffix(s, "%") {
		return 0, false
	}
	return n, true
}

// ParseCPU reads a CPU quantity ("250m", "1", "1500000n", "100u") as
// millicores, rounded down.
func ParseCPU(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	div, mul := int64(1), int64(1000)
	switch s[len(s)-1] {
	case 'm':
		mul, s = 1, s[:len(s)-1]
	case 'u':
		mul, div, s = 1, 1000, s[:len(s)-1]
	case 'n':
		mul, div, s = 1, 1000000, s[:len(s)-1]
	}
	if s == "" || strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' }) >= 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return int64(v * float64(mul) / float64(div)), true
}

var memUnits = []struct {
	suffix string
	mul    int64
}{
	{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40},
	{"k", 1e3}, {"K", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12},
}

// ParseMemory reads a memory quantity ("128Mi", "1Gi", "100M", "42") as
// bytes.
func ParseMemory(s string) (int64, bool) {
	mul := int64(1)
	for _, u := range memUnits {
		if strings.HasSuffix(s, u.suffix) {
			s, mul = strings.TrimSuffix(s, u.suffix), u.mul
			break
		}
	}
	if s == "" || strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v * mul, true
}

// MetricsHistory keeps the newest samples of each pod and node, for
// sparklines. Not safe for use from more than one goroutine.
type MetricsHistory struct {
	max   int
	pods  map[string]*series
	nodes map[string]*series
}

type series struct{ cpu, mem []int64 }

func (s *series) add(cpu, mem int64, max int) {
	s.cpu = append(s.cpu, cpu)
	s.mem = append(s.mem, mem)
	if len(s.cpu) > max {
		s.cpu = append([]int64(nil), s.cpu[len(s.cpu)-max:]...)
		s.mem = append([]int64(nil), s.mem[len(s.mem)-max:]...)
	}
}

// NewMetricsHistory keeps max samples per name (at least 1).
func NewMetricsHistory(max int) *MetricsHistory {
	if max < 1 {
		max = 1
	}
	return &MetricsHistory{max: max, pods: map[string]*series{}, nodes: map[string]*series{}}
}

// AddPods adds one sample; a pod not in it is gone and is dropped.
func (h *MetricsHistory) AddPods(pods []PodMetrics) {
	next := make(map[string]*series, len(pods))
	for _, p := range pods {
		k := p.Namespace + "/" + p.Name
		s := h.pods[k]
		if s == nil {
			s = &series{}
		}
		s.add(p.CPUMilli, p.MemBytes, h.max)
		next[k] = s
	}
	h.pods = next
}

// AddNodes adds one sample; unknown nodes add nothing, gone nodes are
// dropped.
func (h *MetricsHistory) AddNodes(nodes []NodeMetrics) {
	next := make(map[string]*series, len(nodes))
	for _, n := range nodes {
		s := h.nodes[n.Name]
		if !n.Known {
			if s != nil {
				next[n.Name] = s
			}
			continue
		}
		if s == nil {
			s = &series{}
		}
		s.add(n.CPUMilli, n.MemBytes, h.max)
		next[n.Name] = s
	}
	h.nodes = next
}

// Pod is the CPU (millicores) and memory (bytes) samples of one pod,
// oldest first; nil when it has none.
func (h *MetricsHistory) Pod(ns, name string) (cpu, mem []int64) {
	return h.pods[ns+"/"+name].get()
}

// Node is the samples of one node, oldest first; nil when it has none.
func (h *MetricsHistory) Node(name string) (cpu, mem []int64) {
	return h.nodes[name].get()
}

func (s *series) get() (cpu, mem []int64) {
	if s == nil {
		return nil, nil
	}
	return append([]int64(nil), s.cpu...), append([]int64(nil), s.mem...)
}
