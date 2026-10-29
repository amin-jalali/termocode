package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKubeMouseSelectsOpensAndFolds(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	jobs := kubeJobs(t)
	m.focus = FocusEditor
	click := func(y int) {
		nm, _ := m.handleKubeSidebarMouse(3, y, tea.MouseLeft)
		*m = nm
	}
	click(kubeListTop + 2) // PODS, api, worker
	if got := kubeCursorName(m); got != "worker-5c4b-77k2p" || m.focus != FocusExplorer {
		t.Fatalf("cursor %q focus %v", got, m.focus)
	}
	if len(*jobs) != 0 {
		t.Fatal("one click opened logs")
	}
	click(kubeListTop + 2) // a second click opens it
	if len(*jobs) != 1 {
		t.Errorf("second click: %d tabs", len(*jobs))
	}
	click(kubeListTop) // the PODS header folds
	if !m.kube.collapsed["pods"] {
		t.Error("PODS not folded")
	}
	found := false
	for _, h := range m.kubeHits(29) {
		if h.ID == "k8s:sec:deployments" && h.Y == kubeListTop+1 {
			found = true
		}
	}
	if !found {
		t.Errorf("hits = %+v", m.kubeHits(29))
	}
}
