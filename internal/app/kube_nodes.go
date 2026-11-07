package app

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes: nodes and autoscalers (CTX-29) ───────────────────────────
//
// "Kubernetes: Nodes…" (N in the view) lists the cluster's nodes in a
// picker (status, roles, version). A node opens its actions: cordon or
// uncordon (whichever applies) and drain. Each asks first on a card with
// Cancel first and focused, naming the context and the command; drain
// also says that its pods are evicted and emptyDir data is lost. The AI
// can not reach them (policy never).
//
// "Kubernetes: Autoscalers (HPA)" (H in the view) is a read-only page of
// the namespace's horizontal pod autoscalers. The AI may open it.

// confirmKindKubeNode is the cordon / uncordon / drain card.
const confirmKindKubeNode confirmKindEnum = 3491

// kubeNodeTarget is the node and action a node card asks about.
type kubeNodeTarget struct {
	context, node, action string // action: cordon, uncordon, drain
}

type kubeNodesMsg struct {
	context string
	nodes   []kube.Node
	err     error
}

type kubeNodeActionMsg struct {
	node, action string
	err          error
}

// kubeNodes loads the nodes for the picker.
func (m *Model) kubeNodes() tea.Cmd {
	c := m.kube.client()
	return func() tea.Msg {
		ctx := context.Background()
		c, err := kubeResolve(ctx, c)
		if err != nil {
			return kubeNodesMsg{err: err}
		}
		nodes, err := c.Nodes(ctx)
		return kubeNodesMsg{context: c.Context, nodes: nodes, err: err}
	}
}

func (m *Model) onKubeNodes(msg kubeNodesMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot list the nodes", msg.err.Error())
	}
	if len(msg.nodes) == 0 {
		return m.servicesToast(toast.Info, "No nodes", "The cluster shows no nodes to you")
	}
	m.kube.nodes, m.kube.nodesContext = msg.nodes, msg.context
	m.openTasksPickerItems(" Nodes · "+msg.context+" ", kubeNodeItems(msg.nodes))
	return nil
}

// kubeNodeItems are the node picker rows ("k8s:node:<name>").
func kubeNodeItems(nodes []kube.Node) []picker.Item {
	now := clock.Now()
	items := make([]picker.Item, 0, len(nodes))
	for _, n := range nodes {
		items = append(items, picker.Item{ID: "k8s:node:" + n.Name, Title: n.Name,
			Hint: n.Status + " · " + n.RolesText() + " · " + n.Version + " · " + kube.Age(n.Created, now)})
	}
	return items
}

// kubeNodeCordoned: the node takes no new pods.
func kubeNodeCordoned(n kube.Node) bool { return strings.Contains(n.Status, "SchedulingDisabled") }

// kubeNodeActionItems are a node's actions ("k8s:nodeact:<action>/<node>").
func kubeNodeActionItems(n kube.Node) []picker.Item {
	first := picker.Item{ID: "k8s:nodeact:cordon/" + n.Name, Title: "Cordon", Hint: "no new pods on it (asks first)"}
	if kubeNodeCordoned(n) {
		first = picker.Item{ID: "k8s:nodeact:uncordon/" + n.Name, Title: "Uncordon", Hint: "takes new pods again (asks first)"}
	}
	return []picker.Item{first,
		{ID: "k8s:nodeact:drain/" + n.Name, Title: "Drain", Hint: "cordon and evict its pods (asks first)"}}
}

// handleKubeNodePick runs a picked node row or action row.
func (m *Model) handleKubeNodePick(kind, value string) tea.Cmd {
	st := m.kube
	switch kind {
	case "node":
		for _, n := range st.nodes {
			if n.Name == value {
				m.openTasksPickerItems(" Node "+n.Name+" ", kubeNodeActionItems(n))
				return nil
			}
		}
		return m.servicesToast(toast.Info, "The node is gone", value)
	case "nodeact":
		action, node, _ := strings.Cut(value, "/")
		m.askKubeNode(kubeNodeTarget{context: st.nodesContext, node: node, action: action})
	}
	return nil
}

// askKubeNode opens the node card.
func (m *Model) askKubeNode(t kubeNodeTarget) {
	var title, body, button string
	switch t.action {
	case "cordon":
		title, button = "Cordon node?", "Cordon"
		body = "No new pods are placed on " + t.node + ". Its running pods stay."
	case "uncordon":
		title, button = "Uncordon node?", "Uncordon"
		body = "New pods can be placed on " + t.node + " again."
	case "drain":
		title, button = "Drain node?", "Drain"
		body = "This cordons " + t.node + " and evicts its pods; they start again on other nodes.\n" +
			"DaemonSet pods stay. emptyDir data of the evicted pods is lost."
	default:
		return
	}
	cmd := "kubectl " + t.action + " " + t.node
	if t.action == "drain" {
		cmd += " --ignore-daemonsets --delete-emptydir-data"
	}
	body += "\n\nContext: " + t.context + "\nCommand: " + cmd
	m.kube.pendingNode = t
	m.confirm = confirm.Risky(title, body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: t.action, Title: button, Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeNode
}

// answerKubeNode runs the node card's answer.
func (m *Model) answerKubeNode(id string) tea.Cmd {
	t := m.kube.pendingNode
	m.kube.pendingNode = kubeNodeTarget{}
	if t.node == "" || id != t.action {
		return nil
	}
	c := m.kube.client()
	c.Context = t.context // the context the card showed
	return func() tea.Msg {
		ctx := context.Background()
		c, err := kubeResolve(ctx, c)
		if err == nil {
			switch t.action {
			case "cordon":
				err = c.Cordon(ctx, t.node)
			case "uncordon":
				err = c.Uncordon(ctx, t.node)
			case "drain":
				err = c.Drain(ctx, t.node)
			}
		}
		return kubeNodeActionMsg{node: t.node, action: t.action, err: err}
	}
}

func (m *Model) onKubeNodeAction(msg kubeNodeActionMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot "+msg.action+" "+msg.node, msg.err.Error())
	}
	done := map[string]string{"cordon": "Cordoned ", "uncordon": "Uncordoned ", "drain": "Drained "}[msg.action]
	return tea.Batch(m.servicesToast(toast.Info, done+msg.node, ""), m.kubeReload())
}

// kubeHPAText is the autoscaler page.
func kubeHPAText(hs []kube.HPA) string {
	if len(hs) == 0 {
		return "No horizontal pod autoscalers in this namespace.\n"
	}
	now := clock.Now()
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTARGET\tMIN\tMAX\tNOW\tWANTS\tMETRICS\tAGE")
	for _, h := range hs {
		metrics := h.Targets
		if metrics == "" {
			metrics = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%s\t%s\n", h.Name, h.Reference(), h.Min, h.Max, h.Current, h.Desired, metrics, kube.Age(h.Created, now))
	}
	_ = tw.Flush()
	return b.String()
}

// kubeHPA opens the autoscaler page of the session's namespace.
func (m *Model) kubeHPA() tea.Cmd {
	return m.kubePage("the autoscalers", "hpa", func(ctx context.Context, c kube.Client) (string, error) {
		hs, err := c.HPAs(ctx)
		if err != nil {
			return "", err
		}
		return kubeHPAText(hs), nil
	})
}
