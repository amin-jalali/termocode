package app

import (
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/kube"
)

// kubeDemo is the Kubernetes view with a canned cluster (DevActionMsg
// "kubedemo", for the screenshots and goldens; kubectl is never called).
// variant "restart": the restart card of a deployment; "down": the
// cluster does not answer; "keys": the ? card.
func (m *Model) kubeDemo(variant string) {
	st := newKubeState()
	m.kube = st
	st.off = true
	st.detected, st.binary, st.loaded = true, "kubectl", true
	st.context, st.namespace = "dev", "shop"
	m.activity.SetActive(activity.ViewKubernetes)
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	m.focus = FocusExplorer
	st.visible = true
	if variant == "down" {
		st.context = "prod"
		st.err = (&kube.Error{Kind: kube.ErrNoCluster}).Error()
		st.touch()
		return
	}
	now := clock.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	st.pods = []kube.Pod{
		{Name: "api-7d9f8c6b5-x2x4q", Status: "Running", ReadyCount: 2, Total: 2, Restarts: 1, Created: ago(50 * time.Hour), Containers: []string{"api", "sidecar"}, Ports: []int{8080, 9090}},
		{Name: "worker-5c4b-77k2p", Status: "CrashLoopBackOff", ReadyCount: 0, Total: 1, Restarts: 7, Created: ago(30 * time.Minute), Containers: []string{"worker"}},
		{Name: "migrate-29kq8", Status: "Completed", ReadyCount: 0, Total: 1, Created: ago(4 * time.Hour), Containers: []string{"migrate"}},
		{Name: "web-0", Status: "Running", ReadyCount: 1, Total: 1, Created: ago(9 * 24 * time.Hour), Containers: []string{"web"}, Ports: []int{80}},
		{Name: "cache-0", Status: "Pending", ReadyCount: 0, Total: 1, Created: ago(10 * time.Second), Containers: []string{"redis"}},
	}
	st.deploys = []kube.Deployment{
		{Name: "api", Replicas: 3, ReadyN: 3, Updated: 3, Available: 3, Created: ago(9 * 24 * time.Hour)},
		{Name: "worker", Replicas: 2, ReadyN: 1, Updated: 2, Available: 1, Created: ago(24 * time.Hour)},
	}
	st.services = []kube.Service{
		{Name: "api", Type: "ClusterIP", ClusterIP: "10.96.12.4", Ports: []kube.ServicePort{{Port: 80, Protocol: "TCP"}}},
		{Name: "web", Type: "NodePort", ClusterIP: "10.96.40.2", Ports: []kube.ServicePort{{Port: 80, NodePort: 30080, Protocol: "TCP"}}},
	}
	st.jobs = []kube.Job{
		{Name: "migrate", Wanted: 1, Succeeded: 1, Created: ago(4 * time.Hour)},
		{Name: "backfill", Wanted: 5, Succeeded: 2, Active: 1, Created: ago(time.Hour)},
	}
	st.cursor = 2 // the crashing worker
	switch variant {
	case "restart":
		for i, r := range st.rows() {
			if r.kind == kubeRowItem && r.section == "deployments" && r.name == "worker" {
				st.cursor = i
			}
		}
		if d, ok := st.selectedDeployment(); ok {
			m.askKubeRestart(d)
		}
	case "keys":
		m.openKeyCard("Kubernetes keys", kubeKeyRows(), helpLocKube(), nil)
	}
	st.touch()
}
