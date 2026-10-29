package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
)

func TestKubePaletteShowOpensTheView(t *testing.T) {
	f := kubeFake(t)
	m := kubeTestModel(t, 150, 42)
	m.activity.SetActive(activity.ViewFiles)
	m.showExp, m.focus = false, FocusEditor
	cmd, ok := m.dispatchKubePalette("kubernetes-show")
	if !ok {
		t.Fatal("kubernetes-show not handled")
	}
	if m.activity.Active() != activity.ViewKubernetes || !m.showExp || m.focus != FocusExplorer {
		t.Errorf("view %v shown %v focus %v", m.activity.Active(), m.showExp, m.focus)
	}
	kubeRunLoad(t, m, cmd)
	if len(m.kube.pods) != 5 || f.Count("get pods") != 1 {
		t.Errorf("pods %d, calls %v", len(m.kube.pods), f.Calls())
	}
	found := false
	for _, it := range kubePaletteItems() {
		if it.ID == "kubernetes-show" && it.Title == "Kubernetes: Show" {
			found = true
		}
	}
	if !found {
		t.Error("no \"Kubernetes: Show\" palette row")
	}
}

func TestKubeAIPolicy(t *testing.T) {
	for id, gate := range map[string]aiGate{"kubernetes-show": aiGateAuto, "kubernetes-refresh": aiGateAuto} {
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != gate {
			t.Errorf("%s policy = %+v, want %s", id, p, gate)
		}
	}
}
