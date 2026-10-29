package app

import (
	"context"
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: port-forward ────────────────────────────────────────
//
// f on a pod starts `kubectl port-forward` to one of its declared ports
// (a pod with more asks which); kubectl picks a free local port, shown on
// the row and in the footer. f again stops it. Every forward stops when
// termocode quits (kubeState.shutdown). The AI can not start one.

// forwardKey names a forward: the same pod name in another scope is
// another pod.
func (st *kubeState) forwardKey(pod string) string {
	return st.context + "/" + st.namespace + "/" + pod
}

// forward is the running forward of a pod in the current scope.
func (st *kubeState) forward(pod string) *kube.Forward {
	return st.forwards[st.forwardKey(pod)]
}

type kubeForwardMsg struct {
	key string
	fw  *kube.Forward
	err error
}

// kubeToggleForward is f: stop the pod's forward, or start one.
func (m *Model) kubeToggleForward(p kube.Pod, port int) tea.Cmd {
	st := m.kube
	key := st.forwardKey(p.Name)
	if fw := st.forwards[key]; fw != nil {
		delete(st.forwards, key)
		fw.Stop()
		st.touch()
		return m.servicesToast(toast.Info, "Stopped port-forward", fmt.Sprintf("127.0.0.1:%d %s %s:%d", fw.Local, kubeArrow(), p.Name, fw.Remote))
	}
	if port == 0 {
		switch len(p.Ports) {
		case 0:
			return m.servicesToast(toast.Info, "No port to forward", p.Name+" declares no container port")
		case 1:
			port = p.Ports[0]
		default:
			items := make([]picker.Item, 0, len(p.Ports))
			for _, n := range p.Ports {
				items = append(items, picker.Item{ID: "k8s:pf:" + p.Name + "/" + strconv.Itoa(n), Title: strconv.Itoa(n), Hint: p.Name})
			}
			m.openTasksPickerItems(" Forward Port ", items)
			return nil
		}
	}
	c := st.client()
	pod := p.Name
	return func() tea.Msg {
		fw, err := c.PortForward(context.Background(), pod, port)
		return kubeForwardMsg{key: key, fw: fw, err: err}
	}
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
		fmt.Sprintf("%s %s:%d · f stops it", kubeArrow(), msg.fw.Pod, msg.fw.Remote))
}
