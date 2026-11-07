package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: port-forward ────────────────────────────────────────
//
// f on a pod, a service or a deployment starts `kubectl port-forward` to
// one of its ports (pod: declared container ports; service: its ports;
// deployment: the pod template's container ports; more than one asks
// which); kubectl picks a free local port, shown on the row and in the
// footer. f again stops it. While any forward runs, a FORWARDS section at
// the end lists them all (every scope); f there stops one. Every forward
// stops when termocode quits (kubeState.shutdown). The AI can not start
// one.

// kubeForwardsSection is the section of the active forwards.
const kubeForwardsSection = "forwards"

// forwardKey names a forward: "<context>/<namespace>/<kind>/<name>"; the
// same name in another scope is another object.
func (st *kubeState) forwardKey(kind, name string) string {
	return st.context + "/" + st.namespace + "/" + kind + "/" + name
}

// forward is the running forward of a pod in the current scope.
func (st *kubeState) forward(pod string) *kube.Forward { return st.forwardOf("pod", pod) }

// forwardOf is the running forward of an object in the current scope.
func (st *kubeState) forwardOf(kind, name string) *kube.Forward {
	return st.forwards[st.forwardKey(kind, name)]
}

// forwardKeys are the active forwards' keys, sorted.
func (st *kubeState) forwardKeys() []string {
	keys := make([]string, 0, len(st.forwards))
	for k := range st.forwards {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// forwardLabel is a FORWARDS row's name: "svc/web" in the current scope,
// the whole key in another.
func (st *kubeState) forwardLabel(key string) string {
	return strings.TrimPrefix(key, st.context+"/"+st.namespace+"/")
}

// selectedForward is the FORWARDS row under the cursor.
func (st *kubeState) selectedForward() (string, *kube.Forward, bool) {
	r, ok := st.currentRow()
	if !ok || r.kind != kubeRowItem || r.section != kubeForwardsSection {
		return "", nil, false
	}
	keys := st.forwardKeys()
	if r.idx >= len(keys) {
		return "", nil, false
	}
	return keys[r.idx], st.forwards[keys[r.idx]], true
}

type kubeForwardMsg struct {
	key string
	fw  *kube.Forward
	err error
}

// kubeToggleForward is f on a pod: stop its forward, or start one.
func (m *Model) kubeToggleForward(p kube.Pod, port int) tea.Cmd {
	return m.kubeToggleForwardTo("pod", p.Name, p.Ports, port)
}

// kubeStopForward stops the forward under key.
func (m *Model) kubeStopForward(key string) tea.Cmd {
	st := m.kube
	fw := st.forwards[key]
	if fw == nil {
		return nil
	}
	delete(st.forwards, key)
	fw.Stop()
	st.clampCursor()
	st.touch()
	return m.servicesToast(toast.Info, "Stopped port-forward", fmt.Sprintf("127.0.0.1:%d %s %s:%d", fw.Local, kubeArrow(), fw.Target(), fw.Remote))
}

// kubeToggleForwardTo is f on a pod, service ("svc") or deployment:
// stop its forward, or start one to port (0 = pick one of ports).
func (m *Model) kubeToggleForwardTo(kind, name string, ports []int, port int) tea.Cmd {
	st := m.kube
	key := st.forwardKey(kind, name)
	if st.forwards[key] != nil {
		return m.kubeStopForward(key)
	}
	if port == 0 {
		switch len(ports) {
		case 0:
			return m.servicesToast(toast.Info, "No port to forward", name+" declares no port")
		case 1:
			port = ports[0]
		default:
			pick := "pf"
			if kind != "pod" {
				pick = "pf-" + kind
			}
			items := make([]picker.Item, 0, len(ports))
			for _, n := range ports {
				items = append(items, picker.Item{ID: "k8s:" + pick + ":" + name + "/" + strconv.Itoa(n), Title: strconv.Itoa(n), Hint: name})
			}
			m.openTasksPickerItems(" Forward Port ", items)
			return nil
		}
	}
	c := st.client()
	return func() tea.Msg {
		fw, err := c.PortForwardTo(context.Background(), kind, name, port)
		return kubeForwardMsg{key: key, fw: fw, err: err}
	}
}

// kubeToggleSelectedForward is f on any row: a FORWARDS row stops, a
// pod, service or deployment starts or stops.
func (m *Model) kubeToggleSelectedForward() tea.Cmd {
	st := m.kube
	if key, _, ok := st.selectedForward(); ok {
		return m.kubeStopForward(key)
	}
	if p, ok := st.selectedPod(); ok {
		return m.kubeToggleForward(p, 0)
	}
	if d, ok := st.selectedDeployment(); ok {
		return m.kubeToggleForwardTo("deployment", d.Name, d.Ports, 0)
	}
	if s, ok := st.selectedService(); ok {
		return m.kubeToggleForwardTo("svc", s.Name, kubeServicePorts(s), 0)
	}
	return nil
}

// selectedService is the service under the cursor.
func (st *kubeState) selectedService() (kube.Service, bool) {
	r, ok := st.currentRow()
	if !ok || r.kind != kubeRowItem || r.section != "services" || r.idx >= len(st.services) {
		return kube.Service{}, false
	}
	return st.services[r.idx], true
}

// kubeServicePorts are a service's port numbers, spec order.
func kubeServicePorts(s kube.Service) []int {
	out := make([]int, 0, len(s.Ports))
	for _, p := range s.Ports {
		if p.Port > 0 {
			out = append(out, p.Port)
		}
	}
	return out
}

// handleKubeForwardPick runs a picked "pf-svc" / "pf-deployment" port:
// value "<name>/<port>".
func (m *Model) handleKubeForwardPick(kind, value string) tea.Cmd {
	name, p, _ := strings.Cut(value, "/")
	port, err := strconv.Atoi(p)
	if err != nil || port <= 0 {
		return nil
	}
	return m.kubeToggleForwardTo(strings.TrimPrefix(kind, "pf-"), name, nil, port)
}

func (m *Model) onKubeForward(msg kubeForwardMsg) tea.Cmd {
	st := m.kube
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot forward the port", msg.err.Error())
	}
	if old := st.forwards[msg.key]; old != nil {
		old.Stop() // f pressed twice: the newest wins
	}
	st.forwards[msg.key] = msg.fw
	st.touch()
	return m.servicesToast(toast.Info, fmt.Sprintf("Forwarding 127.0.0.1:%d", msg.fw.Local),
		fmt.Sprintf("%s %s:%d · f stops it", kubeArrow(), msg.fw.Target(), msg.fw.Remote))
}
