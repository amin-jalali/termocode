package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: metrics (CTX-14) ────────────────────────────────────
//
// Each load also runs `kubectl top pods` (it needs the cluster's
// metrics-server). A pod row then shows its CPU and memory, and the
// footer a sparkline of the last samples, like the Containers view. A
// cluster with no metrics-server is asked once: the view stops asking
// until R. t (or Kubernetes: Show Metrics) opens a read-only page with
// the nodes and the pods. Nothing here changes the cluster.

// kubeTopSamples is how many samples a sparkline keeps (5 s apart).
const kubeTopSamples = 24

// kubeTopNoServer is topError's line for a cluster without metrics.
const kubeTopNoServer = "metrics-server"

// noteTop takes one load's metrics. err != nil: none this time.
func (st *kubeState) noteTop(pods []kube.PodMetrics, err error) {
	if st.metrics == nil {
		st.metrics = kube.NewMetricsHistory(kubeTopSamples)
	}
	if err != nil {
		st.top = nil
		if strings.Contains(err.Error(), kubeTopNoServer) {
			st.topOff = true // no metrics-server: ask again on R only
		}
		return
	}
	st.top = make(map[string]kube.PodMetrics, len(pods))
	for _, p := range pods {
		st.top[p.Name] = p
	}
	st.metrics.AddPods(pods)
}

// podTop is the metrics of one pod of the view's namespace.
func (st *kubeState) podTop(name string) (kube.PodMetrics, bool) {
	p, ok := st.top[name]
	return p, ok
}

// kubeCPUText is millicores as kubectl shows them ("250m").
func kubeCPUText(m int64) string { return fmt.Sprintf("%dm", m) }

// kubeMemText is bytes in kubectl's binary units ("64Mi", "1.5Gi").
func kubeMemText(b int64) string {
	if b <= 0 {
		return "0"
	}
	units := []string{"Ki", "Mi", "Gi", "Ti"}
	if b < 1024 {
		return fmt.Sprintf("%dB", b)
	}
	v := float64(b)
	u := ""
	for _, next := range units {
		if v < 1024 {
			break
		}
		v /= 1024
		u = next
	}
	if v == float64(int64(v)) && v < 1000 && u != "Gi" && u != "Ti" {
		return fmt.Sprintf("%d%s", int64(v), u)
	}
	return fmt.Sprintf("%.1f%s", v, u)
}

// kubeSpark is a sparkline of samples scaled to their own top.
func kubeSpark(vals []int64, width int) string {
	if len(vals) == 0 {
		return ""
	}
	fs := make([]float64, len(vals))
	var top float64
	for i, v := range vals {
		fs[i] = float64(v)
		top = max(top, fs[i])
	}
	return strings.TrimLeft(statsSparkline(fs, top, width), " ")
}

// podTopFooter is " · cpu 12m ▁▂ · mem 64Mi ▂▃", or "" without metrics.
func (st *kubeState) podTopFooter(name string) string {
	p, ok := st.podTop(name)
	if !ok {
		return ""
	}
	cpu, mem := st.metrics.Pod(p.Namespace, p.Name)
	s := " · cpu " + kubeCPUText(p.CPUMilli)
	if sp := kubeSpark(cpu, 8); len(cpu) > 1 {
		s += " " + sp
	}
	s += " · mem " + kubeMemText(p.MemBytes)
	if sp := kubeSpark(mem, 8); len(mem) > 1 {
		s += " " + sp
	}
	return s
}

type kubeTopMsg struct {
	namespace string
	nodes     []kube.NodeMetrics
	pods      []kube.PodMetrics
	nodesErr  error
	podsErr   error
}

// kubeTop reads `kubectl top nodes` and `kubectl top pods` for the page.
func (m *Model) kubeTop() tea.Cmd {
	st := m.kube
	if st == nil || st.binary == "" {
		return m.servicesToast(toast.Info, "kubectl is not installed", "")
	}
	c := st.client()
	return func() tea.Msg {
		ctx := context.Background() // each call has its own timeout
		msg := kubeTopMsg{namespace: c.Namespace}
		msg.nodes, msg.nodesErr = c.TopNodes(ctx)
		msg.pods, msg.podsErr = c.TopPods(ctx)
		return msg
	}
}

func (m *Model) onKubeTop(msg kubeTopMsg) tea.Cmd {
	st := m.kube
	if msg.nodesErr != nil && msg.podsErr != nil {
		return m.servicesToast(toast.Errr, "Cannot read the metrics", msg.podsErr.Error())
	}
	if st.metrics == nil {
		st.metrics = kube.NewMetricsHistory(kubeTopSamples)
	}
	if msg.nodesErr == nil {
		st.metrics.AddNodes(msg.nodes)
	}
	if err := m.openReadOnlyPage("top: namespace "+msg.namespace, kubeTopText(msg, st.metrics), "", ""); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open the metrics page", err.Error())
	}
	return nil
}

// kubeTopText is the metrics page: a NODES table, then a PODS table,
// each with sparklines of the samples this session saw.
func kubeTopText(msg kubeTopMsg, h *kube.MetricsHistory) string {
	var b strings.Builder
	b.WriteString("NODES\n")
	if msg.nodesErr != nil {
		b.WriteString("  " + msg.nodesErr.Error() + "\n")
	} else if len(msg.nodes) == 0 {
		b.WriteString("  No nodes\n")
	}
	for _, n := range msg.nodes {
		if !n.Known {
			fmt.Fprintf(&b, "  %-32s unknown (no metrics yet)\n", n.Name)
			continue
		}
		line := fmt.Sprintf("  %-32s cpu %7s %4d%%  mem %7s %4d%%", n.Name, kubeCPUText(n.CPUMilli), n.CPUPercent, kubeMemText(n.MemBytes), n.MemPercent)
		if h != nil {
			cpu, mem := h.Node(n.Name)
			line += "  " + kubeSpark(cpu, 12) + "  " + kubeSpark(mem, 12)
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	b.WriteString("\nPODS · namespace " + msg.namespace + "\n")
	if msg.podsErr != nil {
		b.WriteString("  " + msg.podsErr.Error() + "\n")
	} else if len(msg.pods) == 0 {
		b.WriteString("  No pods\n")
	}
	for _, p := range msg.pods {
		line := fmt.Sprintf("  %-40s cpu %7s  mem %7s", p.Name, kubeCPUText(p.CPUMilli), kubeMemText(p.MemBytes))
		if h != nil {
			cpu, mem := h.Pod(p.Namespace, p.Name)
			line += "  " + kubeSpark(cpu, 12) + "  " + kubeSpark(mem, 12)
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	b.WriteString("\nSparklines: the samples this window saw, 5 s apart, oldest first.\n")
	return b.String()
}
